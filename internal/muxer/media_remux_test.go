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

func TestPureMuxMuxerKeepsOnlyRequestedInputStreams(t *testing.T) {
	dir := t.TempDir()
	videoPath := filepath.Join(dir, "muxed-input.webm")
	audioPath := filepath.Join(dir, "replacement-audio.webm")
	outputPath := filepath.Join(dir, "selected.webm")
	if err := writeMuxedWebM(videoPath); err != nil {
		t.Fatal(err)
	}
	if err := writeMinimalWebM(audioPath, false); err != nil {
		t.Fatal(err)
	}

	if err := NewPureMuxMuxer(nil).Merge(context.Background(), videoPath, audioPath, outputPath, types.Metadata{}); err != nil {
		t.Fatalf("Merge: %v", err)
	}
	source, err := media.OpenFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	demuxer, err := media.Open(context.Background(), source, media.OpenOptions{})
	if err != nil {
		_ = source.Close()
		t.Fatal(err)
	}
	defer demuxer.Close()
	var videoStreams, audioStreams int
	for _, stream := range demuxer.Streams() {
		switch stream.Type {
		case media.MediaVideo:
			videoStreams++
		case media.MediaAudio:
			audioStreams++
		}
	}
	if videoStreams != 1 || audioStreams != 1 {
		t.Fatalf("streams=%+v, want exactly one selected video and audio", demuxer.Streams())
	}
}

func TestMediaRemuxAVCCAndASCToMPEGTS(t *testing.T) {
	dir := t.TempDir()
	outputPath := filepath.Join(dir, "merged.ts")
	f, err := os.Create(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	mux, err := media.NewMuxer(f, media.MuxOptions{Format: media.FormatMPEGTS})
	if err != nil {
		t.Fatal(err)
	}

	video := &mediaRemuxInput{
		demuxer: &packetDemuxer{packets: []*media.Packet{{
			StreamIndex: 0,
			Data:        []byte{0, 0, 0, 2, 0x65, 0x88},
			PTS:         media.KnownTimestamp(0),
			DTS:         media.KnownTimestamp(0),
			Duration:    media.KnownTimestamp(3_000),
			Flags:       media.PacketKeyframe,
		}}},
		stream: media.Stream{
			Index: 0, Type: media.MediaVideo, Codec: media.CodecH264,
			TimeBase: media.Rational{Num: 1, Den: 90_000}, Width: 320, Height: 180,
			Config: media.CodecConfig{Format: media.CodecConfigAVCC, Data: []byte{
				1, 0x42, 0, 0x1f, 0xff, 0xe1, 0, 1, 0x67, 1, 0, 1, 0x68,
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
			Duration:    media.KnownTimestamp(1_024),
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
		if err := input.configureBitstream(media.FormatMPEGTS); err != nil {
			t.Fatal(err)
		}
		input.trackID, err = mux.AddStream(input.stream)
		if err != nil {
			t.Fatal(err)
		}
	}

	if err := remuxMediaPackets(context.Background(), mux, inputs); err != nil {
		t.Fatalf("remuxMediaPackets: %v", err)
	}
	if err := mux.Close(); err != nil {
		t.Fatalf("mux.Close: %v", err)
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
			gotVideo = bytes.Contains(packet.Data, []byte{0, 0, 0, 1, 0x67}) &&
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

func TestMediaRemuxPacketRescalesToPersistentTrackTimeBase(t *testing.T) {
	input := &mediaRemuxInput{
		stream:         media.Stream{TimeBase: media.Rational{Num: 1, Den: 1_000}},
		outputTimeBase: media.Rational{Num: 1, Den: 90_000},
		trackID:        3,
		offset:         5,
	}
	out, err := input.outputPacket(&media.Packet{
		PTS: media.KnownTimestamp(10), DTS: media.KnownTimestamp(9),
		Duration: media.KnownTimestamp(2), Flags: media.PacketKeyframe,
	}, []byte{1, 2})
	if err != nil {
		t.Fatal(err)
	}
	if out.StreamIndex != 3 || out.PTS.Value != 1_350 || out.DTS.Value != 1_260 ||
		out.Duration.Value != 180 || !out.Duration.Valid {
		t.Fatalf("rescaled packet = %+v", out)
	}
}

func TestComparePacketDecodeTimePreservesSubNanosecondOrder(t *testing.T) {
	leftInput := &mediaRemuxInput{stream: media.Stream{TimeBase: media.Rational{Num: 1, Den: 2_000_000_000}}}
	rightInput := &mediaRemuxInput{stream: media.Stream{TimeBase: media.Rational{Num: 1, Den: 1_000_000_000}}}
	left := &media.Packet{DTS: media.KnownTimestamp(1)}
	right := &media.Packet{DTS: media.KnownTimestamp(0)}
	comparison, err := comparePacketDecodeTime(left, leftInput, right, rightInput)
	if err != nil {
		t.Fatal(err)
	}
	if comparison <= 0 {
		t.Fatalf("comparison=%d, want left 0.5ns timestamp after right 0ns", comparison)
	}
}

func TestInstallPureMuxOutputReplacesExistingFile(t *testing.T) {
	dir := t.TempDir()
	temporaryPath := filepath.Join(dir, "new.tmp")
	outputPath := filepath.Join(dir, "output.mp4")
	if err := os.WriteFile(temporaryPath, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(outputPath, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := installPureMuxOutput(temporaryPath, outputPath); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new" {
		t.Fatalf("output = %q, want new", got)
	}
	if matches, err := filepath.Glob(filepath.Join(dir, ".ytv1-puremux-backup-*.tmp")); err != nil || len(matches) != 0 {
		t.Fatalf("backup leftovers=%v err=%v", matches, err)
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

type packetDemuxer struct {
	packets []*media.Packet
	next    int
}

func writeMuxedWebM(path string) (retErr error) {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() {
		if err := f.Close(); retErr == nil {
			retErr = err
		}
	}()
	mux, err := media.NewMuxer(f, media.MuxOptions{Format: media.FormatWebM})
	if err != nil {
		return err
	}
	video, err := mux.AddStream(media.Stream{
		Type: media.MediaVideo, Codec: media.CodecVP9,
		TimeBase: media.Rational{Num: 1, Den: 1_000}, Width: 320, Height: 240,
		Config: media.CodecConfig{Format: media.CodecConfigVP9FeatureMetadata,
			Data: []byte{1, 1, 0, 2, 1, 0, 3, 1, 8, 4, 1, 0}},
	})
	if err != nil {
		_ = mux.Close()
		return err
	}
	opusHead := append([]byte("OpusHead"), 1, 2, 0x38, 0x01, 0x80, 0xbb, 0, 0, 0, 0, 0)
	audio, err := mux.AddStream(media.Stream{
		Type: media.MediaAudio, Codec: media.CodecOpus,
		TimeBase: media.Rational{Num: 1, Den: 1_000}, SampleRate: 48_000, Channels: 2,
		Config: media.CodecConfig{Format: media.CodecConfigOpusHead, Data: opusHead},
	})
	if err != nil {
		_ = mux.Close()
		return err
	}
	for _, packet := range []*media.Packet{
		{StreamIndex: video, Data: []byte{0x01, 0x02}, PTS: media.KnownTimestamp(0), DTS: media.KnownTimestamp(0), Duration: media.KnownTimestamp(40), Flags: media.PacketKeyframe},
		{StreamIndex: audio, Data: []byte{0xf8, 0x55}, PTS: media.KnownTimestamp(0), DTS: media.KnownTimestamp(0), Duration: media.KnownTimestamp(20), Flags: media.PacketKeyframe},
	} {
		if err := mux.WritePacket(context.Background(), packet); err != nil {
			_ = mux.Close()
			return err
		}
	}
	return mux.Close()
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
