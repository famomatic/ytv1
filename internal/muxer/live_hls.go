package muxer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/famomatic/puremux/pkg/media"
	"github.com/famomatic/ytv1/internal/downloader"
)

// HLSStreamInput describes one direct HLS media rendition. Video and audio
// inputs may carry different request headers because YouTube URLs retain the
// source Innertube client that produced them.
type HLSStreamInput struct {
	URL       string
	Client    *http.Client
	Headers   http.Header
	Transport downloader.TransportConfig
}

// MuxHLSStreams follows separate HLS video and audio renditions and emits one
// playable MPEG-TS stream. Segments are aligned by media sequence, demuxed one
// pair at a time, and fed into a persistent puremux LiveMuxer so this works
// for unbounded live playlists without temporary files or FFmpeg.
func MuxHLSStreams(ctx context.Context, video, audio HLSStreamInput, dst io.Writer) error {
	videoReader := downloader.NewHLSMediaSegmentReader(httpClientOrDefault(video), video.URL).
		WithRequestHeaders(video.Headers).
		WithTransportConfig(video.Transport).
		WithLiveEdgeSegments(3)
	audioReader := downloader.NewHLSMediaSegmentReader(httpClientOrDefault(audio), audio.URL).
		WithRequestHeaders(audio.Headers).
		WithTransportConfig(audio.Transport).
		WithLiveEdgeSegments(3)

	baseMux, err := media.NewMuxer(dst, media.MuxOptions{Format: media.FormatMPEGTS})
	if err != nil {
		return fmt.Errorf("live HLS mux: create MPEG-TS muxer: %w", err)
	}
	liveMux, err := media.NewLiveMuxer(baseMux, media.DefaultLiveIngestOptions())
	if err != nil {
		_ = baseMux.Close()
		return fmt.Errorf("live HLS mux: create live muxer: %w", err)
	}

	var (
		videoSegment  *downloader.HLSMediaSegment
		audioSegment  *downloader.HLSMediaSegment
		trackIDs      [2]int
		trackCodecs   [2]media.CodecID
		trackTimeBase [2]media.Rational
		started       bool
	)
	finish := func(runErr error) error {
		closeErr := liveMux.Close()
		if runErr != nil {
			return runErr
		}
		if closeErr != nil {
			return fmt.Errorf("live HLS mux: finalize: %w", closeErr)
		}
		return nil
	}

	for {
		if videoSegment == nil {
			videoSegment, err = videoReader.Next(ctx)
			if err != nil {
				if errors.Is(err, io.EOF) && started {
					return finish(nil)
				}
				return finish(fmt.Errorf("live HLS mux: read video segment: %w", err))
			}
		}
		if audioSegment == nil {
			audioSegment, err = audioReader.Next(ctx)
			if err != nil {
				if errors.Is(err, io.EOF) && started {
					return finish(nil)
				}
				return finish(fmt.Errorf("live HLS mux: read audio segment: %w", err))
			}
		}

		// Independently refreshed renditions may expose slightly different live
		// windows. Discard only the older side until both sequence numbers meet.
		if videoSegment.Sequence < audioSegment.Sequence {
			videoSegment = nil
			continue
		}
		if audioSegment.Sequence < videoSegment.Sequence {
			audioSegment = nil
			continue
		}

		inputs := make([]*mediaRemuxInput, 0, 2)
		videoInput, openErr := openHLSMediaSegment(ctx, videoSegment, media.MediaVideo)
		if openErr != nil {
			return finish(fmt.Errorf("live HLS mux: demux video sequence %d: %w", videoSegment.Sequence, openErr))
		}
		inputs = append(inputs, videoInput)
		audioInput, openErr := openHLSMediaSegment(ctx, audioSegment, media.MediaAudio)
		if openErr != nil {
			_ = videoInput.demuxer.Close()
			return finish(fmt.Errorf("live HLS mux: demux audio sequence %d: %w", audioSegment.Sequence, openErr))
		}
		inputs = append(inputs, audioInput)
		if primeErr := alignHLSInputs(ctx, inputs); primeErr != nil {
			closeInputs(inputs)
			return finish(fmt.Errorf("live HLS mux: align sequence %d: %w", videoSegment.Sequence, primeErr))
		}

		if !started {
			for i, input := range inputs {
				trackID, trackErr := liveMux.AddStream(input.stream)
				if trackErr != nil {
					closeInputs(inputs)
					return finish(fmt.Errorf("live HLS mux: add %s track: %w", mediaTypeName(input.stream.Type), trackErr))
				}
				trackIDs[i], trackCodecs[i], trackTimeBase[i] = trackID, input.stream.Codec, input.stream.TimeBase
			}
			started = true
		} else {
			for i, input := range inputs {
				if input.stream.Codec != trackCodecs[i] {
					closeInputs(inputs)
					return finish(fmt.Errorf("live HLS mux: %s codec changed from %s to %s", mediaTypeName(input.stream.Type), trackCodecs[i], input.stream.Codec))
				}
			}
		}
		for i := range inputs {
			inputs[i].trackID = trackIDs[i]
			inputs[i].outputTimeBase = trackTimeBase[i]
		}
		remuxErr := remuxMediaPackets(ctx, liveMux, inputs)
		closeInputs(inputs)
		if remuxErr != nil {
			return finish(remuxErr)
		}

		videoSegment, audioSegment = nil, nil
	}
}

func openHLSMediaSegment(ctx context.Context, segment *downloader.HLSMediaSegment, kind media.MediaType) (*mediaRemuxInput, error) {
	name := fmt.Sprintf("hls-%d.ts", segment.Sequence)
	data := segment.Data
	if kind == media.MediaAudio && len(segment.Init) == 0 {
		var err error
		data, err = stripLeadingID3(data)
		if err != nil {
			return nil, err
		}
	}
	source := media.MemorySource(name, data)
	var (
		demuxer media.Demuxer
		err     error
	)
	if len(segment.Init) > 0 {
		demuxer, err = media.OpenMP4WithInit(ctx, segment.Init, source)
	} else {
		demuxer, err = media.Open(ctx, source, media.OpenOptions{})
	}
	if err != nil {
		_ = source.Close()
		return nil, err
	}
	stream, ok := bestMediaStream(demuxer.Streams(), kind)
	if !ok {
		_ = demuxer.Close()
		return nil, fmt.Errorf("%w: segment contains no %s stream", media.ErrInvalidData, mediaTypeName(kind))
	}
	if !canMuxMPEGTSCodec(stream.Codec) {
		_ = demuxer.Close()
		return nil, fmt.Errorf("%w: codec %s cannot be written to mpegts", media.ErrIncompatible, stream.Codec)
	}
	input := &mediaRemuxInput{demuxer: demuxer, stream: stream}
	if err := input.configureBitstream(media.FormatMPEGTS); err != nil {
		_ = demuxer.Close()
		return nil, err
	}
	return input, nil
}

func alignHLSInputs(ctx context.Context, inputs []*mediaRemuxInput) error {
	if len(inputs) != 2 {
		return errors.New("expected video and audio inputs")
	}
	for _, input := range inputs {
		packet, err := readSelectedPacket(ctx, input)
		if err != nil {
			return err
		}
		input.pending = packet
	}
	videoStamp, err := decodeTimestamp(inputs[0].pending)
	if err != nil {
		return err
	}
	audioStamp, err := decodeTimestamp(inputs[1].pending)
	if err != nil {
		return err
	}
	videoInAudioBase, ok := inputs[0].stream.TimeBase.Rescale(videoStamp, inputs[1].stream.TimeBase)
	if !ok {
		return errors.New("cannot align HLS stream time bases")
	}
	offset, ok := subtractTimestamp(videoInAudioBase, audioStamp)
	if !ok {
		return errors.New("HLS stream timestamp offset overflow")
	}
	inputs[1].offset = offset
	return nil
}

func decodeTimestamp(packet *media.Packet) (int64, error) {
	stamp := packet.DTS
	if !stamp.Valid {
		stamp = packet.PTS
	}
	if !stamp.Valid {
		return 0, errors.New("packet has neither DTS nor PTS")
	}
	return stamp.Value, nil
}

func stripLeadingID3(data []byte) ([]byte, error) {
	for len(data) >= 3 && string(data[:3]) == "ID3" {
		if len(data) < 10 {
			return nil, errors.New("truncated ID3 header")
		}
		for _, value := range data[6:10] {
			if value&0x80 != 0 {
				return nil, errors.New("invalid ID3 syncsafe size")
			}
		}
		size := int(data[6])<<21 | int(data[7])<<14 | int(data[8])<<7 | int(data[9])
		total := 10 + size
		if data[5]&0x10 != 0 {
			total += 10
		}
		if total > len(data) {
			return nil, errors.New("truncated ID3 tag")
		}
		data = data[total:]
	}
	return data, nil
}

func closeInputs(inputs []*mediaRemuxInput) {
	for _, input := range inputs {
		if input.pending != nil {
			input.pending.Release()
			input.pending = nil
		}
		_ = input.demuxer.Close()
	}
}

func httpClientOrDefault(input HLSStreamInput) *http.Client {
	if input.Client != nil {
		return input.Client
	}
	return http.DefaultClient
}
