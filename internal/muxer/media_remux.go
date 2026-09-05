package muxer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"math/bits"
	"os"
	"path/filepath"
	"strings"

	"github.com/famomatic/puremux/pkg/bitstream/aac"
	"github.com/famomatic/puremux/pkg/bitstream/h264"
	"github.com/famomatic/puremux/pkg/bitstream/hevc"
	"github.com/famomatic/puremux/pkg/media"
)

type mediaRemuxInput struct {
	advance func(context.Context) error
	observe func(*media.Packet)
	demuxer media.Demuxer
	stream  media.Stream
	trackID int
	// outputTimeBase is set when a persistent live output keeps its original
	// stream registration across segments whose demuxer time base may change.
	outputTimeBase media.Rational
	pending        *media.Packet
	offset         int64

	h264 *h264.Configuration
	hevc *hevc.Configuration
	aac  *aac.Config
}

func remuxMediaFiles(ctx context.Context, videoPath, audioPath, outputPath string) error {
	out, err := outputMediaFormat(outputPath)
	if err != nil {
		return err
	}
	return remuxSelectedMediaFiles(ctx, videoPath, audioPath, outputPath, out)
}

// remuxSelectedMediaFiles keeps only the requested video stream from the
// first input and audio stream from the second. That matters when a selected
// video-side format also contains audio: the generic multi-input Remux helper
// would otherwise retain both audio tracks. MPEG-TS additionally applies the
// explicit framing conversions that puremux's generic Remux leaves to callers.
func remuxSelectedMediaFiles(ctx context.Context, videoPath, audioPath, outputPath string, out media.Format) error {
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

	temporary, err := os.CreateTemp(filepath.Dir(outputPath), ".ytv1-puremux-*.tmp")
	if err != nil {
		return fmt.Errorf("puremux: create output %s: %w", outputPath, err)
	}
	temporaryPath := temporary.Name()
	installed := false
	defer func() {
		_ = temporary.Close()
		if !installed {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0o644); err != nil {
		return fmt.Errorf("puremux: set output permissions: %w", err)
	}
	// The native playback path may omit unsupported track labels/disposition.
	// Requested metadata embedding uses the configured fallback; codec/timing
	// incompatibilities remain errors even with AllowMetadataLoss.
	mux, err := media.NewMuxer(temporary, media.MuxOptions{Format: out, AllowMetadataLoss: true})
	if err != nil {
		return fmt.Errorf("puremux: create muxer: %w", err)
	}

	for _, input := range inputs {
		track, trackErr := mux.AddStream(input.stream)
		if trackErr != nil {
			_ = mux.Close()
			return fmt.Errorf("%w: %s in %s: %v", media.ErrIncompatible, input.stream.Codec, out, trackErr)
		}
		input.trackID = track
	}

	remuxErr := remuxMediaPackets(ctx, mux, inputs)
	closeErr := mux.Close()
	fileErr := temporary.Close()
	if remuxErr != nil || closeErr != nil || fileErr != nil {
		switch {
		case remuxErr != nil:
			return remuxErr
		case closeErr != nil:
			return fmt.Errorf("puremux: finalize output: %w", closeErr)
		default:
			return fmt.Errorf("puremux: close output: %w", fileErr)
		}
	}
	if err := installPureMuxOutput(temporaryPath, outputPath); err != nil {
		return fmt.Errorf("puremux: install output %s: %w", outputPath, err)
	}
	installed = true
	return nil
}

// installPureMuxOutput replaces outputPath while preserving the previous file
// if the final rename fails. The temporary and destination paths share a
// directory, so each successful rename stays on one filesystem.
func installPureMuxOutput(temporaryPath, outputPath string) error {
	if err := os.Rename(temporaryPath, outputPath); err == nil {
		return nil
	} else if _, statErr := os.Stat(outputPath); statErr != nil {
		return err
	}

	backup, err := os.CreateTemp(filepath.Dir(outputPath), ".ytv1-puremux-backup-*.tmp")
	if err != nil {
		return err
	}
	backupPath := backup.Name()
	if err := backup.Close(); err != nil {
		_ = os.Remove(backupPath)
		return err
	}
	if err := os.Remove(backupPath); err != nil {
		return err
	}
	if err := os.Rename(outputPath, backupPath); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, outputPath); err != nil {
		_ = os.Rename(backupPath, outputPath)
		return err
	}
	_ = os.Remove(backupPath)
	return nil
}

func outputMediaFormat(path string) (media.Format, error) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".webm":
		return media.FormatWebM, nil
	case ".mkv", ".mka":
		return media.FormatMatroska, nil
	case ".mp4", ".m4a", ".m4v":
		return media.FormatMP4, nil
	case ".ts", ".mpegts", ".m2ts":
		return media.FormatMPEGTS, nil
	default:
		return media.FormatUnknown, fmt.Errorf("%w: output extension %s", media.ErrUnsupportedFormat, filepath.Ext(path))
	}
}

func openMediaRemuxInput(ctx context.Context, path string, kind media.MediaType, out media.Format) (*mediaRemuxInput, error) {
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
		return nil, fmt.Errorf("%w: %s contains no %s stream", media.ErrInvalidData, path, mediaTypeName(kind))
	}
	if out == media.FormatMPEGTS && !canMuxMPEGTSCodec(stream.Codec) {
		_ = demuxer.Close()
		return nil, fmt.Errorf("%w: codec %s cannot be written to mpegts", media.ErrIncompatible, stream.Codec)
	}

	input := &mediaRemuxInput{demuxer: demuxer, stream: stream}
	if err := input.configureBitstream(out); err != nil {
		_ = demuxer.Close()
		return nil, err
	}
	return input, nil
}

func (input *mediaRemuxInput) configureBitstream(out media.Format) error {
	if out != media.FormatMPEGTS {
		return nil
	}
	switch input.stream.Codec {
	case media.CodecH264:
		if input.stream.Config.Format == media.CodecConfigAVCC {
			cfg, err := h264.ParseAVCC(input.stream.Config.Data)
			if err != nil {
				return fmt.Errorf("%w: invalid H.264 AVCC: %v", media.ErrIncompatible, err)
			}
			input.h264 = &cfg
		} else if input.stream.Config.Format != media.CodecConfigUnknown {
			return fmt.Errorf("%w: unsupported H.264 configuration", media.ErrIncompatible)
		}
	case media.CodecHEVC:
		if input.stream.Config.Format == media.CodecConfigHVCC {
			cfg, err := hevc.ParseHVCC(input.stream.Config.Data)
			if err != nil {
				return fmt.Errorf("%w: invalid HEVC HVCC: %v", media.ErrIncompatible, err)
			}
			input.hevc = &cfg
		} else if input.stream.Config.Format != media.CodecConfigUnknown {
			return fmt.Errorf("%w: unsupported HEVC configuration", media.ErrIncompatible)
		}
	case media.CodecAAC:
		if input.stream.Config.Format != media.CodecConfigASC {
			return fmt.Errorf("%w: AAC input has no AudioSpecificConfig", media.ErrIncompatible)
		}
		cfg, err := aac.ParseASC(input.stream.Config.Data)
		if err != nil {
			return fmt.Errorf("%w: invalid AAC AudioSpecificConfig: %v", media.ErrIncompatible, err)
		}
		input.aac = &cfg
	}
	return nil
}

func remuxMediaPackets(ctx context.Context, mux media.Muxer, inputs []*mediaRemuxInput) error {
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
		for i, packet := range current {
			if packet == nil {
				continue
			}
			if pick < 0 {
				pick = i
				continue
			}
			comparison, err := comparePacketDecodeTime(packet, inputs[i], current[pick], inputs[pick])
			if err != nil {
				return err
			}
			if comparison < 0 {
				pick = i
			}
		}
		if pick < 0 {
			break
		}

		input := inputs[pick]
		packet := current[pick]
		data, err := input.packetData(packet)
		if err != nil {
			return fmt.Errorf("puremux: convert %s packet: %w", input.stream.Codec, err)
		}
		outPacket, err := input.outputPacket(packet, data)
		if err != nil {
			return err
		}
		packet.Release()
		current[pick] = nil
		if err := mux.WritePacket(ctx, outPacket); err != nil {
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
			return fmt.Errorf("%w: selected %s stream contains no packets", media.ErrInvalidData, mediaTypeName(inputs[i].stream.Type))
		}
	}
	return nil
}

func readSelectedPacket(ctx context.Context, input *mediaRemuxInput) (*media.Packet, error) {
	if input.pending != nil {
		packet := input.pending
		input.pending = nil
		return packet, nil
	}
	for {
		packet, err := input.demuxer.ReadPacket(ctx)
		if errors.Is(err, io.EOF) && input.advance != nil {
			if nextErr := input.advance(ctx); nextErr != nil {
				return nil, nextErr
			}
			return readSelectedPacket(ctx, input)
		}
		if err != nil {
			return nil, err
		}
		if packet.StreamIndex == input.stream.Index {
			if input.observe != nil {
				input.observe(packet)
			}
			return packet, nil
		}
		packet.Release()
	}
}

func packetDecodeTimestamp(packet *media.Packet, input *mediaRemuxInput) (int64, error) {
	stamp := packet.DTS
	if !stamp.Valid {
		stamp = packet.PTS
	}
	if !stamp.Valid {
		return 0, errors.New("puremux: packet has neither DTS nor PTS")
	}
	value, ok := addTimestampOffset(stamp.Value, input.offset)
	if !ok {
		return 0, errors.New("puremux: packet timestamp overflow")
	}
	return value, nil
}

// comparePacketDecodeTime compares timestamps without converting them to
// time.Duration. Cross-multiplication can require 192 bits for full int64
// timestamp and Rational ranges, so fixed-width limbs avoid both precision
// loss and per-packet big.Int allocations.
func comparePacketDecodeTime(left *media.Packet, leftInput *mediaRemuxInput, right *media.Packet, rightInput *mediaRemuxInput) (int, error) {
	leftValue, err := packetDecodeTimestamp(left, leftInput)
	if err != nil {
		return 0, err
	}
	rightValue, err := packetDecodeTimestamp(right, rightInput)
	if err != nil {
		return 0, err
	}
	leftTB, rightTB := leftInput.stream.TimeBase, rightInput.stream.TimeBase
	if !leftTB.Valid() || leftTB.Num <= 0 || !rightTB.Valid() || rightTB.Num <= 0 {
		return 0, errors.New("puremux: invalid stream time base")
	}
	if leftValue < 0 && rightValue >= 0 {
		return -1, nil
	}
	if leftValue >= 0 && rightValue < 0 {
		return 1, nil
	}
	leftProduct := multiplyTimestamp192(timestampMagnitude(leftValue), uint64(leftTB.Num), uint64(rightTB.Den))
	rightProduct := multiplyTimestamp192(timestampMagnitude(rightValue), uint64(rightTB.Num), uint64(leftTB.Den))
	comparison := compareTimestamp192(leftProduct, rightProduct)
	if leftValue < 0 {
		comparison = -comparison
	}
	return comparison, nil
}

func timestampMagnitude(value int64) uint64 {
	if value >= 0 {
		return uint64(value)
	}
	return uint64(-(value + 1)) + 1
}

func multiplyTimestamp192(a, b, c uint64) [3]uint64 {
	hi, lo := bits.Mul64(a, b)
	upperHi, upperLo := bits.Mul64(hi, c)
	lowerHi, lowerLo := bits.Mul64(lo, c)
	middle, carry := bits.Add64(upperLo, lowerHi, 0)
	return [3]uint64{lowerLo, middle, upperHi + carry}
}

func compareTimestamp192(left, right [3]uint64) int {
	for i := 2; i >= 0; i-- {
		if left[i] < right[i] {
			return -1
		}
		if left[i] > right[i] {
			return 1
		}
	}
	return 0
}

func (input *mediaRemuxInput) outputPacket(packet *media.Packet, data []byte) (*media.Packet, error) {
	pts, dts := packet.PTS, packet.DTS
	if !pts.Valid {
		pts = dts
	}
	if !dts.Valid {
		dts = pts
	}
	if !pts.Valid || !dts.Valid {
		return nil, errors.New("puremux: packet has neither DTS nor PTS")
	}
	var ok bool
	pts.Value, ok = addTimestampOffset(pts.Value, input.offset)
	if !ok {
		return nil, errors.New("puremux: packet PTS overflow")
	}
	dts.Value, ok = addTimestampOffset(dts.Value, input.offset)
	if !ok {
		return nil, errors.New("puremux: packet DTS overflow")
	}
	duration := packet.Duration
	if duration.Valid && duration.Value <= 0 {
		// LiveMuxer can complete an omitted duration with bounded lookahead,
		// while a known non-positive duration is explicitly invalid.
		duration = media.Timestamp{}
	}
	outputTimeBase := input.outputTimeBase
	if !outputTimeBase.Valid() {
		outputTimeBase = input.stream.TimeBase
	}
	if outputTimeBase != input.stream.TimeBase {
		pts.Value, ok = input.stream.TimeBase.Rescale(pts.Value, outputTimeBase)
		if !ok {
			return nil, errors.New("puremux: packet PTS rescale overflow")
		}
		dts.Value, ok = input.stream.TimeBase.Rescale(dts.Value, outputTimeBase)
		if !ok {
			return nil, errors.New("puremux: packet DTS rescale overflow")
		}
		if duration.Valid {
			duration.Value, ok = input.stream.TimeBase.Rescale(duration.Value, outputTimeBase)
			if !ok {
				return nil, errors.New("puremux: packet duration rescale overflow")
			}
			if duration.Value <= 0 {
				duration = media.Timestamp{}
			}
		}
	}
	return &media.Packet{
		StreamIndex:    input.trackID,
		Data:           data,
		PTS:            pts,
		DTS:            dts,
		Duration:       duration,
		Flags:          packet.Flags,
		Pos:            packet.Pos,
		DiscardPadding: packet.DiscardPadding,
	}, nil
}

func addTimestampOffset(value, offset int64) (int64, bool) {
	if offset > 0 && value > math.MaxInt64-offset || offset < 0 && value < math.MinInt64-offset {
		return 0, false
	}
	return value + offset, true
}

func subtractTimestamp(left, right int64) (int64, bool) {
	if right > 0 && left < math.MinInt64+right || right < 0 && left > math.MaxInt64+right {
		return 0, false
	}
	return left - right, true
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

func canMuxMPEGTSCodec(codec media.CodecID) bool {
	switch codec {
	case media.CodecH264, media.CodecHEVC, media.CodecAAC:
		return true
	default:
		return false
	}
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
