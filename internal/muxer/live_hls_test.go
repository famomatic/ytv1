package muxer

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/famomatic/puremux/pkg/media"
)

func TestMuxHLSStreamsProducesVideoAndAudio(t *testing.T) {
	videoSegment := testLiveHLSTSSegment(t, true)
	id3 := []byte{'I', 'D', '3', 3, 0, 0, 0, 0, 0, 4, 't', 'e', 's', 't'}
	audioSegment := append(id3, testADTSFrame(64)...)
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/video.m3u8":
			fmt.Fprint(w, "#EXTM3U\n#EXT-X-MEDIA-SEQUENCE:7\n#EXT-X-TARGETDURATION:1\n#EXTINF:1,\nvideo.ts\n#EXT-X-ENDLIST\n")
		case "/audio.m3u8":
			fmt.Fprint(w, "#EXTM3U\n#EXT-X-MEDIA-SEQUENCE:7\n#EXT-X-TARGETDURATION:1\n#EXTINF:1,\naudio.ts\n#EXT-X-ENDLIST\n")
		case "/video.ts":
			_, _ = w.Write(videoSegment)
		case "/audio.ts":
			_, _ = w.Write(audioSegment)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	var output bytes.Buffer
	err := MuxHLSStreams(context.Background(),
		HLSStreamInput{URL: srv.URL + "/video.m3u8", Client: srv.Client()},
		HLSStreamInput{URL: srv.URL + "/audio.m3u8", Client: srv.Client()},
		&output,
	)
	if err != nil {
		t.Fatalf("MuxHLSStreams: %v", err)
	}
	assertMediaHasAV(t, output.Bytes())
}

func testLiveHLSTSSegment(t *testing.T, video bool) []byte {
	t.Helper()
	var output bytes.Buffer
	mux, err := media.NewMuxer(&output, media.MuxOptions{Format: media.FormatMPEGTS})
	if err != nil {
		t.Fatal(err)
	}
	if video {
		track, err := mux.AddStream(media.Stream{
			Type: media.MediaVideo, Codec: media.CodecH264,
			TimeBase: media.Rational{Num: 1, Den: 90_000}, Width: 320, Height: 180,
		})
		if err != nil {
			t.Fatal(err)
		}
		annexB := []byte{
			0, 0, 0, 1, 0x67, 0x42, 0x00, 0x1e, 0xe9, 0x01, 0x40, 0x7b, 0x20,
			0, 0, 0, 1, 0x68, 0xce, 0x06, 0xe2,
			0, 0, 0, 1, 0x65, 0x88, 0x84, 0x00,
		}
		if err := mux.WritePacket(context.Background(), &media.Packet{
			StreamIndex: track, Data: annexB,
			PTS: media.KnownTimestamp(0), DTS: media.KnownTimestamp(0),
			Duration: media.KnownTimestamp(3_000), Flags: media.PacketKeyframe,
		}); err != nil {
			t.Fatal(err)
		}
	} else {
		track, err := mux.AddStream(media.Stream{
			Type: media.MediaAudio, Codec: media.CodecAAC,
			TimeBase: media.Rational{Num: 1, Den: 44_100}, Channels: 2, SampleRate: 44_100,
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := mux.WritePacket(context.Background(), &media.Packet{
			StreamIndex: track, Data: testADTSFrame(64),
			PTS: media.KnownTimestamp(0), DTS: media.KnownTimestamp(0),
			Duration: media.KnownTimestamp(1_024), Flags: media.PacketKeyframe,
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := mux.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func testADTSFrame(size int) []byte {
	frame := make([]byte, size)
	frame[0], frame[1], frame[2] = 0xff, 0xf1, 0x50
	frame[3] = 0x80 | byte((size>>11)&0x03)
	frame[4] = byte(size >> 3)
	frame[5] = byte((size&0x07)<<5) | 0x1f
	frame[6] = 0xfc
	return frame
}

func assertMediaHasAV(t *testing.T, data []byte) {
	t.Helper()
	demuxer, err := media.Open(context.Background(), media.MemorySource("merged.ts", data), media.OpenOptions{})
	if err != nil {
		t.Fatalf("open merged TS: %v", err)
	}
	defer demuxer.Close()
	var video, audio bool
	for _, stream := range demuxer.Streams() {
		video = video || stream.Type == media.MediaVideo
		audio = audio || stream.Type == media.MediaAudio
	}
	if !video || !audio {
		t.Fatalf("merged streams = %+v, want video+audio", demuxer.Streams())
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	packets := 0
	for {
		packet, readErr := demuxer.ReadPacket(ctx)
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			t.Fatalf("read merged packet: %v", readErr)
		}
		packets++
		packet.Release()
	}
	if packets < 2 {
		t.Fatalf("merged packet count = %d, want at least 2", packets)
	}
}
