package muxer

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"

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
// playable MPEG-TS stream. Each rendition advances independently; packets are
// interleaved by timestamp into a persistent puremux LiveMuxer so this works
// for unbounded live playlists without temporary files or FFmpeg.
func MuxHLSStreams(ctx context.Context, video, audio HLSStreamInput, dst io.Writer) error {
	ctx, cancel := context.WithCancel(ctx)
	type nextResult struct {
		segment *downloader.HLSMediaSegment
		err     error
	}
	var wg sync.WaitGroup
	defer func() { cancel(); wg.Wait() }()
	prefetch := func(input HLSStreamInput) func(context.Context) (*downloader.HLSMediaSegment, error) {
		reader := downloader.NewHLSMediaSegmentReader(httpClientOrDefault(input), input.URL).WithRequestHeaders(input.Headers).WithTransportConfig(input.Transport).WithLiveEdgeSegments(3)
		ch := make(chan nextResult, 1)
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer close(ch)
			for {
				seg, err := reader.Next(ctx)
				select {
				case ch <- nextResult{seg, err}:
				case <-ctx.Done():
					return
				}
				if err != nil {
					return
				}
			}
		}()
		return func(callCtx context.Context) (*downloader.HLSMediaSegment, error) {
			select {
			case r, ok := <-ch:
				if !ok {
					return nil, io.EOF
				}
				return r.segment, r.err
			case <-callCtx.Done():
				return nil, callCtx.Err()
			}
		}
	}
	next := []func(context.Context) (*downloader.HLSMediaSegment, error){prefetch(video), prefetch(audio)}
	inputs := make([]*mediaRemuxInput, 0, 2)
	defer func() { closeInputs(inputs) }()
	segments := make([]*downloader.HLSMediaSegment, 2)
	for i, kind := range []media.MediaType{media.MediaVideo, media.MediaAudio} {
		seg, err := next[i](ctx)
		if err != nil {
			return err
		}
		segments[i] = seg
		input, err := openHLSMediaSegment(ctx, seg, kind)
		if err != nil {
			return err
		}
		inputs = append(inputs, input)
		packet, err := readSelectedPacket(ctx, input)
		if err != nil {
			return err
		}
		input.pending = packet
	}
	// Container timestamps share the transport clock. Only packed AAC without
	// timestamps needs an inferred origin; retain that timeline across fragments.
	videoStamp, err := decodeTimestamp(inputs[0].pending)
	if err != nil {
		return err
	}
	for i, input := range inputs {
		var end int64
		base := input.stream.TimeBase
		seed := int64(0)
		if i == 1 {
			var ok bool
			seed, ok = inputs[0].stream.TimeBase.Rescale(videoStamp, base)
			if !ok {
				return fmt.Errorf("invalid HLS time base")
			}
		}
		configure := func(seg *downloader.HLSMediaSegment) error {
			stamp, err := decodeTimestamp(input.pending)
			if err != nil {
				return err
			}
			if packedHLSAudio(seg) && input.stream.Type == media.MediaAudio {
				origin := seed
				if ticks, ok := hlsID3Timestamp(seg.Data); ok {
					var valid bool
					origin, valid = (media.Rational{Num: 1, Den: 90000}).Rescale(ticks, input.stream.TimeBase)
					if !valid {
						return fmt.Errorf("invalid packed audio timestamp")
					}
				} else if end != 0 {
					origin = end
				}
				offset, ok := subtractTimestamp(origin, stamp)
				if !ok {
					return fmt.Errorf("HLS timestamp overflow")
				}
				input.offset = offset
			} else if seg.Discontinuity && end != 0 {
				offset, ok := subtractTimestamp(end, stamp)
				if !ok {
					return fmt.Errorf("HLS timestamp overflow")
				}
				input.offset = offset
			}
			return nil
		}
		if err := configure(segments[i]); err != nil {
			return err
		}
		input.observe = func(packet *media.Packet) {
			stamp, err := decodeTimestamp(packet)
			if err != nil {
				return
			}
			value, ok := addTimestampOffset(stamp, input.offset)
			if !ok {
				return
			}
			if packet.Duration.Valid {
				value, ok = addTimestampOffset(value, packet.Duration.Value)
				if !ok {
					return
				}
			}
			if value > end {
				end = value
			}
		}
		input.observe(input.pending)
		index := i
		input.advance = func(ctx context.Context) error {
			seg, err := next[index](ctx)
			if err != nil {
				return err
			}
			fresh, err := openHLSMediaSegment(ctx, seg, input.stream.Type)
			if err != nil {
				return err
			}
			if fresh.stream.Codec != input.stream.Codec {
				fresh.demuxer.Close()
				return fmt.Errorf("HLS codec changed")
			}
			packet, err := readSelectedPacket(ctx, fresh)
			if err != nil {
				fresh.demuxer.Close()
				return err
			}
			converted, ok := input.stream.TimeBase.Rescale(end, fresh.stream.TimeBase)
			if !ok {
				packet.Release()
				fresh.demuxer.Close()
				return fmt.Errorf("invalid HLS time base")
			}
			end = converted
			offset, ok := input.stream.TimeBase.Rescale(input.offset, fresh.stream.TimeBase)
			if !ok {
				packet.Release()
				fresh.demuxer.Close()
				return fmt.Errorf("invalid HLS offset time base")
			}
			input.offset = offset
			old := input.demuxer
			input.demuxer = fresh.demuxer
			input.stream = fresh.stream
			input.h264, input.hevc, input.aac = fresh.h264, fresh.hevc, fresh.aac
			input.pending = packet
			_ = old.Close()
			if err := configure(seg); err != nil {
				return err
			}
			input.observe(packet)
			return nil
		}
	}
	// The native playback path may omit unsupported track labels/disposition.
	// Requested metadata embedding uses the configured fallback; codec/timing
	// incompatibilities remain errors even with AllowMetadataLoss.
	mux, err := media.NewMuxer(dst, media.MuxOptions{Format: media.FormatMPEGTS, AllowMetadataLoss: true})
	if err != nil {
		return err
	}
	live, err := media.NewLiveMuxer(mux, media.DefaultLiveIngestOptions())
	if err != nil {
		mux.Close()
		return err
	}
	for _, input := range inputs {
		id, err := live.AddStream(input.stream)
		if err != nil {
			live.Close()
			return err
		}
		input.trackID = id
		input.outputTimeBase = input.stream.TimeBase
	}
	err = remuxMediaPackets(ctx, live, inputs)
	closeErr := live.Close()
	if err != nil {
		return err
	}
	return closeErr
}

func packedHLSAudio(seg *downloader.HLSMediaSegment) bool {
	if len(seg.Init) > 0 {
		return false
	}
	data, err := stripLeadingID3(seg.Data)
	return err == nil && len(data) > 1 && data[0] == 0xff && data[1]&0xf6 == 0xf0
}
func hlsID3Timestamp(data []byte) (int64, bool) {
	// Restrict lookup to an ID3 tag, never to compressed audio bytes.
	if len(data) < 10 || string(data[:3]) != "ID3" {
		return 0, false
	}
	for _, b := range data[6:10] {
		if b&0x80 != 0 {
			return 0, false
		}
	}
	size := int(data[6])<<21 | int(data[7])<<14 | int(data[8])<<7 | int(data[9])
	if size > len(data)-10 {
		return 0, false
	}
	tag := data[10 : 10+size]
	owner := []byte("com.apple.streaming.transportStreamTimestamp\x00")
	i := bytes.Index(tag, owner)
	if i < 0 || i+len(owner)+8 > len(tag) {
		return 0, false
	}
	return int64(binary.BigEndian.Uint64(tag[i+len(owner):]) & ((1 << 33) - 1)), true
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
