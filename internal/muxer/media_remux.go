package muxer

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/famomatic/puremux/pkg/bitstream/aac"
	"github.com/famomatic/puremux/pkg/bitstream/h264"
	"github.com/famomatic/puremux/pkg/bitstream/hevc"
	"github.com/famomatic/puremux/pkg/media"
	puremux "github.com/famomatic/puremux/pkg/puremux"
)

type mediaRemuxInput struct {
	demuxer media.Demuxer
	stream  media.Stream
	trackID int
	private []byte

	h264 *h264.Configuration
	hevc *hevc.Configuration
	aac  *aac.Config
}

func remuxMediaFiles(ctx context.Context, videoPath, audioPath, outputPath string, cfg puremux.Config) error {
	out, err := outputContainer(outputPath)
	if err != nil {
		return err
	}

	inputs := make([]*mediaRemuxInput, 0, 2)
	defer func() {
		for _, input := range inputs {
			_ = input.demuxer.Close()
		}
	}()

	for _, spec := range []struct {
		path string
		kind media.MediaType
	}{{videoPath, media.MediaVideo}, {audioPath, media.MediaAudio}} {
		input, openErr := openMediaRemuxInput(ctx, spec.path, spec.kind, out)
		if openErr != nil {
			return openErr
		}
		inputs = append(inputs, input)
	}

	if cfg.TimecodeScale == 0 {
		cfg = puremux.DefaultConfig()
	}
	cfg.OutputContainer = out

	f, err := os.OpenFile(outputPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("puremux: open output %s: %w", outputPath, err)
	}
	session, err := puremux.NewSession(f, cfg)
	if err != nil {
		_ = f.Close()
		_ = os.Remove(outputPath)
		return fmt.Errorf("puremux: create session: %w", err)
	}

	for _, input := range inputs {
		track, trackErr := session.AddTrack(puremux.Track{
			Codec:        mediaCodec(input.stream.Codec),
			IsVideo:      input.stream.Type == media.MediaVideo,
			Width:        input.stream.Width,
			Height:       input.stream.Height,
			Channels:     input.stream.Channels,
			SampleRate:   float64(input.stream.SampleRate),
			CodecPrivate: input.private,
		})
		if trackErr != nil {
			_ = session.Close()
			_ = f.Close()
			_ = os.Remove(outputPath)
			return fmt.Errorf("%w: %s in %s", puremux.ErrIncompatible, input.stream.Codec, out)
		}
		input.trackID = track
	}

	remuxErr := remuxMediaPackets(ctx, session, inputs)
	closeErr := session.Close()
	fileErr := f.Close()
	if remuxErr != nil || closeErr != nil || fileErr != nil {
		_ = os.Remove(outputPath)
		switch {
		case remuxErr != nil:
			return remuxErr
		case closeErr != nil:
			return fmt.Errorf("puremux: finalize output: %w", closeErr)
		default:
			return fmt.Errorf("puremux: close output: %w", fileErr)
		}
	}
	return nil
}

func outputContainer(path string) (puremux.Container, error) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".webm":
		return puremux.ContainerWebM, nil
	case ".mkv", ".mka":
		return puremux.ContainerMKV, nil
	case ".ts", ".mpegts":
		return puremux.ContainerMPEGTS, nil
	default:
		return puremux.ContainerUnknown, fmt.Errorf("%w: extension %s", puremux.ErrUnsupportedOutput, filepath.Ext(path))
	}
}

func openMediaRemuxInput(ctx context.Context, path string, kind media.MediaType, out puremux.Container) (*mediaRemuxInput, error) {
	source, err := media.OpenFile(path)
	if err != nil {
		return nil, fmt.Errorf("puremux: open input %s: %w", path, err)
	}
	demuxer, err := media.Open(ctx, source, media.OpenOptions{})
	if err != nil {
		_ = source.Close()
		return nil, fmt.Errorf("puremux: demux input %s: %w", path, err)
	}
	stream, ok := bestMediaStream(demuxer.Streams(), kind)
	if !ok {
		_ = demuxer.Close()
		return nil, fmt.Errorf("%w: %s contains no %s stream", puremux.ErrUnsupportedInput, path, mediaTypeName(kind))
	}
	codec := mediaCodec(stream.Codec)
	if codec == puremux.CodecUnknown || !puremux.CanRemuxCodecs(out, []puremux.CodecType{codec}) {
		_ = demuxer.Close()
		return nil, fmt.Errorf("%w: codec %s cannot be written to %s", puremux.ErrIncompatible, stream.Codec, out)
	}

	input := &mediaRemuxInput{demuxer: demuxer, stream: stream}
	if err := input.configureBitstream(out); err != nil {
		_ = demuxer.Close()
		return nil, err
	}
	return input, nil
}

func (input *mediaRemuxInput) configureBitstream(out puremux.Container) error {
	private, err := codecPrivate(input.stream)
	if err != nil {
		return fmt.Errorf("%w: %v", puremux.ErrIncompatible, err)
	}
	input.private = private
	if out != puremux.ContainerMPEGTS {
		return nil
	}
	switch input.stream.Codec {
	case media.CodecH264:
		if input.stream.Config.Format == media.CodecConfigAVCC {
			cfg, err := h264.ParseAVCC(input.stream.Config.Data)
			if err != nil {
				return fmt.Errorf("%w: invalid H.264 AVCC: %v", puremux.ErrIncompatible, err)
			}
			input.h264 = &cfg
		} else if input.stream.Config.Format != media.CodecConfigUnknown {
			return fmt.Errorf("%w: unsupported H.264 configuration", puremux.ErrIncompatible)
		}
	case media.CodecHEVC:
		if input.stream.Config.Format == media.CodecConfigHVCC {
			cfg, err := hevc.ParseHVCC(input.stream.Config.Data)
			if err != nil {
				return fmt.Errorf("%w: invalid HEVC HVCC: %v", puremux.ErrIncompatible, err)
			}
			input.hevc = &cfg
		} else if input.stream.Config.Format != media.CodecConfigUnknown {
			return fmt.Errorf("%w: unsupported HEVC configuration", puremux.ErrIncompatible)
		}
	case media.CodecAAC:
		if input.stream.Config.Format != media.CodecConfigASC {
			return fmt.Errorf("%w: AAC input has no AudioSpecificConfig", puremux.ErrIncompatible)
		}
		cfg, err := aac.ParseASC(input.stream.Config.Data)
		if err != nil {
			return fmt.Errorf("%w: invalid AAC AudioSpecificConfig: %v", puremux.ErrIncompatible, err)
		}
		input.aac = &cfg
	}
	return nil
}

func remuxMediaPackets(ctx context.Context, session *puremux.Session, inputs []*mediaRemuxInput) error {
	current := make([]*media.Packet, len(inputs))
	written := make([]int, len(inputs))
	defer func() {
		for _, packet := range current {
			if packet != nil {
				packet.Release()
			}
		}
	}()

	for i, input := range inputs {
		packet, err := readSelectedPacket(ctx, input)
		if err != nil && !errors.Is(err, io.EOF) {
			return fmt.Errorf("puremux: read input packet: %w", err)
		}
		current[i] = packet
	}

	for {
		pick := -1
		var pickTime time.Duration
		for i, packet := range current {
			if packet == nil {
				continue
			}
			stamp, err := packetDecodeTime(packet, inputs[i].stream)
			if err != nil {
				return err
			}
			if pick < 0 || stamp < pickTime {
				pick, pickTime = i, stamp
			}
		}
		if pick < 0 {
			break
		}

		input := inputs[pick]
		packet := current[pick]
		pts, dts, err := packetTimes(packet, input.stream)
		if err != nil {
			return err
		}
		data, err := input.packetData(packet)
		if err != nil {
			return fmt.Errorf("puremux: convert %s packet: %w", input.stream.Codec, err)
		}
		outPacket := &puremux.Packet{
			Data:       data,
			PTS:        pts,
			DTS:        dts,
			IsKeyframe: packet.Keyframe(),
			Codec:      mediaCodec(input.stream.Codec),
			TrackID:    input.trackID,
		}
		packet.Release()
		current[pick] = nil
		if err := session.WritePacket(outPacket); err != nil {
			return fmt.Errorf("puremux: write packet: %w", err)
		}
		written[pick]++

		next, err := readSelectedPacket(ctx, input)
		if err != nil && !errors.Is(err, io.EOF) {
			return fmt.Errorf("puremux: read input packet: %w", err)
		}
		current[pick] = next
	}

	for i, count := range written {
		if count == 0 {
			return fmt.Errorf("%w: selected %s stream contains no packets", puremux.ErrUnsupportedInput, mediaTypeName(inputs[i].stream.Type))
		}
	}
	return nil
}

func readSelectedPacket(ctx context.Context, input *mediaRemuxInput) (*media.Packet, error) {
	for {
		packet, err := input.demuxer.ReadPacket(ctx)
		if err != nil {
			return nil, err
		}
		if packet.StreamIndex == input.stream.Index {
			return packet, nil
		}
		packet.Release()
	}
}

func packetDecodeTime(packet *media.Packet, stream media.Stream) (time.Duration, error) {
	stamp := packet.DTS
	if !stamp.Valid {
		stamp = packet.PTS
	}
	if !stamp.Valid {
		return 0, errors.New("puremux: packet has neither DTS nor PTS")
	}
	value, ok := stamp.Duration(stream.TimeBase)
	if !ok {
		return 0, errors.New("puremux: packet timestamp cannot be represented as duration")
	}
	return value, nil
}

func packetTimes(packet *media.Packet, stream media.Stream) (time.Duration, time.Duration, error) {
	dts, err := packetDecodeTime(packet, stream)
	if err != nil {
		return 0, 0, err
	}
	stamp := packet.PTS
	if !stamp.Valid {
		stamp = packet.DTS
	}
	pts, ok := stamp.Duration(stream.TimeBase)
	if !ok {
		return 0, 0, errors.New("puremux: packet PTS cannot be represented as duration")
	}
	return pts, dts, nil
}

func (input *mediaRemuxInput) packetData(packet *media.Packet) ([]byte, error) {
	switch {
	case input.h264 != nil:
		return h264.AVCCToAnnexB(*input.h264, packet.Data, packet.Keyframe())
	case input.hevc != nil:
		return hevc.HVCCToAnnexB(*input.hevc, packet.Data, packet.Keyframe())
	case input.aac != nil:
		return aac.WrapADTS(*input.aac, packet.Data)
	default:
		return append([]byte(nil), packet.Data...), nil
	}
}

func mediaCodec(codec media.CodecID) puremux.CodecType {
	switch codec {
	case media.CodecVP8:
		return puremux.CodecVP8
	case media.CodecVP9:
		return puremux.CodecVP9
	case media.CodecAV1:
		return puremux.CodecAV1
	case media.CodecOpus:
		return puremux.CodecOpus
	case media.CodecVorbis:
		return puremux.CodecVorbis
	case media.CodecFLAC:
		return puremux.CodecFLAC
	case media.CodecAAC:
		return puremux.CodecAAC
	case media.CodecMP3:
		return puremux.CodecMP3
	case media.CodecH264:
		return puremux.CodecH264
	case media.CodecHEVC:
		return puremux.CodecHEVC
	default:
		return puremux.CodecUnknown
	}
}

func codecPrivate(stream media.Stream) ([]byte, error) {
	if stream.Codec == media.CodecOpus && stream.Config.Format == media.CodecConfigDOPS {
		return opusHeadFromDOPS(stream.Config.Data)
	}
	if stream.Codec == media.CodecVP9 && stream.Config.Format == media.CodecConfigVPCC {
		return nil, nil
	}
	return append([]byte(nil), stream.Config.Data...), nil
}

func opusHeadFromDOPS(dops []byte) ([]byte, error) {
	if len(dops) < 11 || dops[0] != 0 || dops[1] == 0 {
		return nil, errors.New("puremux: invalid dOps")
	}
	channels := int(dops[1])
	family := dops[10]
	want := 11
	if family != 0 {
		want += 2 + channels
	}
	if len(dops) < want {
		return nil, errors.New("puremux: truncated dOps mapping")
	}
	head := make([]byte, 19+want-11)
	copy(head, "OpusHead")
	head[8] = 1
	head[9] = dops[1]
	binary.LittleEndian.PutUint16(head[10:12], binary.BigEndian.Uint16(dops[2:4]))
	binary.LittleEndian.PutUint32(head[12:16], binary.BigEndian.Uint32(dops[4:8]))
	binary.LittleEndian.PutUint16(head[16:18], binary.BigEndian.Uint16(dops[8:10]))
	head[18] = family
	copy(head[19:], dops[11:want])
	return head, nil
}

func bestMediaStream(streams []media.Stream, kind media.MediaType) (media.Stream, bool) {
	best := -1
	for i := range streams {
		if streams[i].Type != kind {
			continue
		}
		if best < 0 || betterMediaStream(streams[i], streams[best], kind) {
			best = i
		}
	}
	if best < 0 {
		return media.Stream{}, false
	}
	return streams[best], true
}

func betterMediaStream(left, right media.Stream, kind media.MediaType) bool {
	leftDefault := left.Disposition&media.DispositionDefault != 0
	rightDefault := right.Disposition&media.DispositionDefault != 0
	if leftDefault != rightDefault {
		return leftDefault
	}
	if kind == media.MediaVideo {
		leftPixels, rightPixels := int64(left.Width)*int64(left.Height), int64(right.Width)*int64(right.Height)
		if leftPixels != rightPixels {
			return leftPixels > rightPixels
		}
	} else {
		if left.Channels != right.Channels {
			return left.Channels > right.Channels
		}
		if left.SampleRate != right.SampleRate {
			return left.SampleRate > right.SampleRate
		}
	}
	if left.BitRate != right.BitRate {
		return left.BitRate > right.BitRate
	}
	return left.Index < right.Index
}

func mediaTypeName(kind media.MediaType) string {
	if kind == media.MediaVideo {
		return "video"
	}
	if kind == media.MediaAudio {
		return "audio"
	}
	return "media"
}
