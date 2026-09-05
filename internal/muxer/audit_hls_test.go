package muxer

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/famomatic/puremux/pkg/media"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestHLSIndependentSequencesAndContinuousPackedAudio(t *testing.T) {
	video := testLiveHLSTSSegment(t, true)
	audio := testADTSFrame(64)
	var videoOnce, audioOnce sync.Once
	videoStarted := make(chan struct{})
	audioStarted := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v.m3u8":
			fmt.Fprint(w, "#EXTM3U\n#EXT-X-MEDIA-SEQUENCE:7\n#EXTINF:1,\nv.ts\n#EXT-X-ENDLIST\n")
		case "/a.m3u8":
			fmt.Fprint(w, "#EXTM3U\n#EXT-X-MEDIA-SEQUENCE:100\n#EXTINF:0.023,\na1.aac\n#EXTINF:0.023,\na2.aac\n#EXT-X-ENDLIST\n")
		case "/v.ts":
			videoOnce.Do(func() { close(videoStarted) })
			select {
			case <-audioStarted:
			case <-r.Context().Done():
				return
			}
			w.Write(video)
		default:
			audioOnce.Do(func() { close(audioStarted) })
			select {
			case <-videoStarted:
			case <-r.Context().Done():
				return
			}
			w.Write(audio)
		}
	}))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var out bytes.Buffer
	if err := MuxHLSStreams(ctx, HLSStreamInput{URL: srv.URL + "/v.m3u8", Client: srv.Client()}, HLSStreamInput{URL: srv.URL + "/a.m3u8", Client: srv.Client()}, &out); err != nil {
		t.Fatal(err)
	}
	demux, err := media.Open(ctx, media.MemorySource("out.ts", out.Bytes()), media.OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer demux.Close()
	aid := -1
	for _, s := range demux.Streams() {
		if s.Type == media.MediaAudio {
			aid = s.Index
		}
	}
	count := 0
	var last int64
	for {
		packet, err := demux.ReadPacket(ctx)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if packet.StreamIndex == aid {
			if count > 0 && packet.PTS.Value <= last {
				t.Fatal("audio timestamp reset")
			}
			last = packet.PTS.Value
			count++
		}
		packet.Release()
	}
	if count != 2 {
		t.Fatalf("got %d audio packets, want 2", count)
	}
}
