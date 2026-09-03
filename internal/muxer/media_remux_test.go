package muxer

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/famomatic/puremux/pkg/media"
	puremux "github.com/famomatic/puremux/pkg/puremux"
	"github.com/famomatic/ytv1/internal/types"
)

func TestPureMuxMuxerUsesMediaDemuxerForOggOpus(t *testing.T) {
	dir := t.TempDir()
	videoPath := filepath.Join(dir, "video.webm")
	audioPath := filepath.Join(dir, "audio.ogg")
	outputPath := filepath.Join(dir, "merged.webm")
	if err := writeMinimalWebM(videoPath, true); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(audioPath, testOggOpus(), 0o644); err != nil {
		t.Fatal(err)
	}

	m := NewPureMuxMuxer(nil)
	if err := m.Merge(context.Background(), videoPath, audioPath, outputPath, types.Metadata{}); err != nil {
		t.Fatalf("Merge: %v", err)
	}

	source, err := media.OpenFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	demuxer, err := media.Open(context.Background(), source, media.OpenOptions{})
	if err != nil {
		_ = source.Close()
		t.Fatalf("open merged output: %v", err)
	}
	defer demuxer.Close()

	var video, audio bool
	for _, stream := range demuxer.Streams() {
		video = video || stream.Type == media.MediaVideo && stream.Codec == media.CodecVP9
		audio = audio || stream.Type == media.MediaAudio && stream.Codec == media.CodecOpus
	}
	if !video || !audio {
		t.Fatalf("merged streams = %+v, want VP9 video and Opus audio", demuxer.Streams())
	}
}

func TestMediaRemuxAVCCAndASCToMPEGTS(t *testing.T) {
	dir := t.TempDir()
	outputPath := filepath.Join(dir, "merged.ts")
	f, err := os.Create(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg := puremux.DefaultConfig()
	cfg.OutputContainer = puremux.ContainerMPEGTS
	session, err := puremux.NewSession(f, cfg)
	if err != nil {
		t.Fatal(err)
	}

	video := &mediaRemuxInput{
		demuxer: &packetDemuxer{packets: []*media.Packet{{
			StreamIndex: 0,
			Data:        []byte{0, 0, 0, 2, 0x65, 0x88},
			PTS:         media.KnownTimestamp(0),
			DTS:         media.KnownTimestamp(0),
			Flags:       media.PacketKeyframe,
		}}},
		stream: media.Stream{
			Index: 0, Type: media.MediaVideo, Codec: media.CodecH264,
			TimeBase: media.Rational{Num: 1, Den: 90_000}, Width: 320, Height: 180,
			Config: media.CodecConfig{Format: media.CodecConfigAVCC, Data: []byte{
				1, 100, 0, 31, 0xff, 0xe1, 0, 2, 0x67, 0x64, 1, 0, 1, 0x68,
			}},
		},
	}
	audioPayload := []byte{0x21, 0x10, 0x56}
	audio := &mediaRemuxInput{
		demuxer: &packetDemuxer{packets: []*media.Packet{{
			StreamIndex: 0,
			Data:        append([]byte(nil), audioPayload...),
			PTS:         media.KnownTimestamp(0),
			DTS:         media.KnownTimestamp(0),
			Flags:       media.PacketKeyframe,
		}}},
		stream: media.Stream{
			Index: 0, Type: media.MediaAudio, Codec: media.CodecAAC,
			TimeBase: media.Rational{Num: 1, Den: 44_100}, SampleRate: 44_100, Channels: 2,
			Config: media.CodecConfig{Format: media.CodecConfigASC, Data: []byte{0x12, 0x10}},
		},
	}
	inputs := []*mediaRemuxInput{video, audio}
	for _, input := range inputs {
		if err := input.configureBitstream(puremux.ContainerMPEGTS); err != nil {
			t.Fatal(err)
		}
		input.trackID, err = session.AddTrack(puremux.Track{
			Codec: mediaCodec(input.stream.Codec), IsVideo: input.stream.Type == media.MediaVideo,
			Width: input.stream.Width, Height: input.stream.Height,
			SampleRate: float64(input.stream.SampleRate), Channels: input.stream.Channels,
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	if err := remuxMediaPackets(context.Background(), session, inputs); err != nil {
		t.Fatalf("remuxMediaPackets: %v", err)
	}
	if err := session.Close(); err != nil {
		t.Fatalf("session.Close: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	source, err := media.OpenFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	demuxer, err := media.Open(context.Background(), source, media.OpenOptions{})
	if err != nil {
		_ = source.Close()
		t.Fatalf("open TS output: %v", err)
	}
	defer demuxer.Close()

	var gotVideo, gotAudio bool
	for {
		packet, readErr := demuxer.ReadPacket(context.Background())
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			t.Fatalf("read TS packet: %v", readErr)
		}
		stream := demuxer.Streams()[packet.StreamIndex]
		switch stream.Codec {
		case media.CodecH264:
			gotVideo = bytes.Contains(packet.Data, []byte{0, 0, 0, 1, 0x67, 0x64}) &&
				bytes.Contains(packet.Data, []byte{0, 0, 0, 1, 0x65, 0x88})
		case media.CodecAAC:
			gotAudio = bytes.Equal(packet.Data, audioPayload) &&
				bytes.Equal(stream.Config.Data, []byte{0x12, 0x10})
		}
		packet.Release()
	}
	if !gotVideo || !gotAudio {
		t.Fatalf("TS round trip gotVideo=%v gotAudio=%v", gotVideo, gotAudio)
	}
}

func TestPureMuxMuxerFallsBackOnMediaParseFailure(t *testing.T) {
	dir := t.TempDir()
	videoPath := filepath.Join(dir, "broken.webm")
	audioPath := filepath.Join(dir, "audio.webm")
	outputPath := filepath.Join(dir, "merged.webm")
	if err := os.WriteFile(videoPath, []byte("not media"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeMinimalWebM(audioPath, false); err != nil {
		t.Fatal(err)
	}
	fallback := &fakeMuxer{available: true}
	m := NewPureMuxMuxer(fallback)
	if err := m.Merge(context.Background(), videoPath, audioPath, outputPath, types.Metadata{}); err != nil {
		t.Fatalf("Merge: %v", err)
	}
	if fallback.merged != 1 {
		t.Fatalf("fallback merged %d times, want 1", fallback.merged)
	}
}

func TestOpusHeadFromDOPSEndianAndMapping(t *testing.T) {
	// ISO/IEC 14496-12 OpusSpecificBox payload: version 0, two channels,
	// pre-skip 312, input rate 48 kHz, gain -2, mapping family 1, followed by
	// stream/coupled counts and the two channel-map entries. Integer fields in
	// dOps are big-endian; RFC 7845 OpusHead stores them little-endian.
	dops := []byte{0, 2, 0x01, 0x38, 0, 0, 0xbb, 0x80, 0xff, 0xfe, 1, 1, 1, 0, 1}
	head, err := opusHeadFromDOPS(dops)
	if err != nil {
		t.Fatal(err)
	}
	want := append([]byte("OpusHead"), 1, 2, 0x38, 0x01, 0x80, 0xbb, 0, 0, 0xfe, 0xff, 1, 1, 1, 0, 1)
	if !bytes.Equal(head, want) {
		t.Fatalf("OpusHead = %x, want %x", head, want)
	}
	for _, malformed := range [][]byte{nil, {0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}, {0, 2, 0, 0, 0, 0, 0, 0, 0, 0, 1}} {
		if _, err := opusHeadFromDOPS(malformed); err == nil {
			t.Fatalf("malformed dOps accepted: %x", malformed)
		}
	}
}

type packetDemuxer struct {
	packets []*media.Packet
	next    int
}

func (d *packetDemuxer) Streams() []media.Stream { return nil }
func (d *packetDemuxer) Info() media.Info        { return media.Info{} }
func (d *packetDemuxer) Seek(context.Context, media.SeekRequest) (media.SeekResult, error) {
	return media.SeekResult{}, media.ErrNotSeekable
}
func (d *packetDemuxer) Close() error { return nil }
func (d *packetDemuxer) ReadPacket(context.Context) (*media.Packet, error) {
	if d.next >= len(d.packets) {
		return nil, io.EOF
	}
	packet := d.packets[d.next]
	d.next++
	return packet, nil
}

func testOggOpus() []byte {
	head := append([]byte("OpusHead"), 1, 2, 0x38, 0x01, 0x80, 0xbb, 0, 0, 0, 0, 0)
	tags := append([]byte("OpusTags"), 0, 0, 0, 0, 0, 0, 0, 0)
	audio := []byte{0xf8, 0x55} // RFC 6716 TOC config 31/code 0: 960 samples.
	out := testOggPage(0x02, 0, 1, 0, head)
	out = append(out, testOggPage(0, 0, 1, 1, tags)...)
	out = append(out, testOggPage(0x04, 312+960, 1, 2, audio)...)
	return out
}

func testOggPage(flags byte, granule uint64, serial, sequence uint32, packet []byte) []byte {
	page := make([]byte, 28+len(packet))
	copy(page, "OggS")
	page[5] = flags
	binary.LittleEndian.PutUint64(page[6:14], granule)
	binary.LittleEndian.PutUint32(page[14:18], serial)
	binary.LittleEndian.PutUint32(page[18:22], sequence)
	page[26], page[27] = 1, byte(len(packet))
	copy(page[28:], packet)
	binary.LittleEndian.PutUint32(page[22:26], oggCRC(page))
	return page
}

func oggCRC(data []byte) uint32 {
	var crc uint32
	for _, value := range data {
		crc ^= uint32(value) << 24
		for range 8 {
			if crc&0x80000000 != 0 {
				crc = crc<<1 ^ 0x04c11db7
			} else {
				crc <<= 1
			}
		}
	}
	return crc
}
