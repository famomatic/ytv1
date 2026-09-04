package client

import (
	"bytes"
	"context"
	"errors"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/famomatic/ytv1/internal/muxer"
)

// TestDownloadAndMerge_WebMUsesPureMux verifies that a WebM video+audio pair
// is merged through the pure-Go puremux muxer when no FFmpeg fallback is
// configured, producing a real WebM container without requiring an ffmpeg
// binary on the host.
func TestDownloadAndMerge_WebMUsesPureMux(t *testing.T) {
	videoID := "jNQXAC9IVRw"
	mediaBase := "https://media.example"

	vidBytes := minimalWebMFixture(true)
	audBytes := minimalWebMFixture(false)

	httpClient := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			switch {
			case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/youtubei/v1/player"):
				body := `{
					"playabilityStatus":{"status":"OK"},
					"videoDetails":{"videoId":"jNQXAC9IVRw","title":"x","author":"y"},
					"streamingData":{"adaptiveFormats":[
						{"itag":248,"url":"` + mediaBase + `/v.webm","mimeType":"video/webm; codecs=\"vp9\"","bitrate":1000},
						{"itag":251,"url":"` + mediaBase + `/a.webm","mimeType":"audio/webm; codecs=\"opus\"","bitrate":1000}
					]}
				}`
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
			case r.Method == http.MethodGet && r.URL.Path == "/watch":
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`<html><script src="/s/player/test/base.js"></script></html>`)), Header: make(http.Header)}, nil
			case r.Method == http.MethodGet && r.URL.String() == mediaBase+"/v.webm":
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(vidBytes)), Header: make(http.Header)}, nil
			case r.Method == http.MethodGet && r.URL.String() == mediaBase+"/a.webm":
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(audBytes)), Header: make(http.Header)}, nil
			default:
				return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("not found")), Header: make(http.Header)}, nil
			}
		}),
	}

	c := New(Config{
		HTTPClient:      httpClient,
		ClientOverrides: []string{"mweb"},
		Muxer:           muxer.NewPureMuxMuxer(nil),
	})
	out := filepath.Join(t.TempDir(), "merged.webm")
	// NoEmbedMetadata mirrors the yt-dlp-parity CLI default: puremux cannot
	// tag containers, so embedding is off and the merge routes through puremux.
	res, err := c.Download(context.Background(), videoID, DownloadOptions{
		Mode:            SelectionModeBest,
		OutputPath:      out,
		NoEmbedMetadata: true,
	})
	if err != nil {
		t.Fatalf("Download() error = %v", err)
	}
	if res.OutputPath != out {
		t.Fatalf("output path=%q want=%q", res.OutputPath, out)
	}

	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read merged output: %v", err)
	}
	// WebM/Matroska EBML magic 0x1A 0x45 0xDF 0xA3 confirms puremux wrote a
	// real container rather than leaving raw concatenated bytes.
	if len(b) < 4 || b[0] != 0x1A || b[1] != 0x45 || b[2] != 0xDF || b[3] != 0xA3 {
		t.Fatalf("merged output is not a WebM/Matroska container: % x", firstNBytes(b, 8))
	}

	// Intermediates are cleaned up after a successful puremux merge.
	if _, err := os.Stat(out + ".f248.video"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("video intermediate should be removed, stat err=%v", err)
	}
	if _, err := os.Stat(out + ".f251.audio"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("audio intermediate should be removed, stat err=%v", err)
	}
}

// minimalWebMFixture returns the bytes of a tiny spec-valid WebM file with a
// single VP9 video track (video=true) or Opus audio track (video=false).
func minimalWebMFixture(video bool) []byte {
	var codecID string
	var codecPrivate []byte
	var defaultDuration uint64
	var trackType uint64
	if video {
		codecID = "V_VP9"
		codecPrivate = []byte{1, 1, 0, 2, 1, 0, 3, 1, 8, 4, 1, 0}
		defaultDuration = 40_000_000
		trackType = 1
	} else {
		codecID = "A_OPUS"
		codecPrivate = append([]byte("OpusHead"), 1, 2, 0x38, 0x01, 0x80, 0xbb, 0, 0, 0, 0, 0)
		defaultDuration = 20_000_000
		trackType = 2
	}

	trackEntry := bytes.NewBuffer(nil)
	trackEntry.Write(webmUint(0xD7, 1))
	trackEntry.Write(webmUint(0x73C5, 1))
	trackEntry.Write(webmUint(0x83, trackType))
	trackEntry.Write(webmBytes(0x86, []byte(codecID)))
	trackEntry.Write(webmBytes(0x63A2, codecPrivate))
	trackEntry.Write(webmUint(0x23E383, defaultDuration))
	if video {
		videoEl := bytes.NewBuffer(nil)
		videoEl.Write(webmUint(0xB0, 320))
		videoEl.Write(webmUint(0xBA, 240))
		trackEntry.Write(webmMaster(0xE0, videoEl.Bytes()))
	} else {
		audioEl := bytes.NewBuffer(nil)
		audioEl.Write(webmUint(0x9F, 2))
		audioEl.Write(webmFloat(0xB5, 48_000))
		trackEntry.Write(webmMaster(0xE1, audioEl.Bytes()))
	}

	tracks := webmMaster(0x1654AE6B, webmMaster(0xAE, trackEntry.Bytes()))

	info := bytes.NewBuffer(nil)
	info.Write(webmUint(0x2AD7B1, 1_000_000))
	info.Write(webmBytes(0x4D80, []byte("ytv1-test")))
	info.Write(webmBytes(0x5741, []byte("ytv1-test")))

	cluster := bytes.NewBuffer(nil)
	cluster.Write(webmUint(0xE7, 0))
	flags := byte(0)
	if video {
		flags |= 0x80
	}
	block := []byte{0x80 | 1, 0, 0, flags, 0x01, 0x02}
	cluster.Write(webmBytes(0xA3, block))

	seg := bytes.NewBuffer(nil)
	seg.Write(webmMaster(0x1549A966, info.Bytes()))
	seg.Write(tracks)
	seg.Write(webmMaster(0x1F43B675, cluster.Bytes()))

	header := bytes.NewBuffer(nil)
	header.Write(webmUint(0x4286, 1))
	header.Write(webmUint(0x42F7, 1))
	header.Write(webmUint(0x42F2, 4))
	header.Write(webmUint(0x42F3, 8))
	header.Write(webmBytes(0x4282, []byte("webm")))
	header.Write(webmUint(0x4287, 4))
	header.Write(webmUint(0x4285, 2))

	out := bytes.NewBuffer(nil)
	out.Write(webmMaster(0x1A45DFA3, header.Bytes()))
	out.Write(webmUnknownSeg(seg.Bytes()))
	return out.Bytes()
}

func webmMaster(id uint32, payload []byte) []byte {
	var b bytes.Buffer
	b.Write(webmID(id))
	b.Write(webmSize(uint64(len(payload))))
	b.Write(payload)
	return b.Bytes()
}

func webmUint(id uint32, val uint64) []byte {
	payload := webmUintBytes(val)
	var b bytes.Buffer
	b.Write(webmID(id))
	b.Write(webmSize(uint64(len(payload))))
	b.Write(payload)
	return b.Bytes()
}

func webmBytes(id uint32, val []byte) []byte {
	var b bytes.Buffer
	b.Write(webmID(id))
	b.Write(webmSize(uint64(len(val))))
	b.Write(val)
	return b.Bytes()
}

func webmFloat(id uint32, val float64) []byte {
	bits := math.Float64bits(val)
	return webmBytes(id, []byte{
		byte(bits >> 56), byte(bits >> 48), byte(bits >> 40), byte(bits >> 32),
		byte(bits >> 24), byte(bits >> 16), byte(bits >> 8), byte(bits),
	})
}

func webmID(id uint32) []byte {
	switch {
	case id <= 0xFF:
		return []byte{byte(id)}
	case id <= 0xFFFF:
		return []byte{byte(id >> 8), byte(id)}
	case id <= 0xFFFFFF:
		return []byte{byte(id >> 16), byte(id >> 8), byte(id)}
	default:
		return []byte{byte(id >> 24), byte(id >> 16), byte(id >> 8), byte(id)}
	}
}

func webmSize(val uint64) []byte {
	if val == 0 {
		return []byte{0x80}
	}
	for width := 1; width <= 8; width++ {
		if val>>webmDataBits(width) != 0 {
			continue
		}
		out := make([]byte, width)
		out[0] = byte(0x80 >> uint(width-1))
		v := val
		for i := width - 1; i >= 0; i-- {
			out[i] |= byte(v & 0xFF)
			v >>= 8
		}
		return out
	}
	return []byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}
}

func webmDataBits(width int) uint {
	if width == 8 {
		return 56
	}
	return uint(8-width) + uint(8*(width-1))
}

func webmUintBytes(val uint64) []byte {
	if val == 0 {
		return []byte{0}
	}
	var buf []byte
	for v := val; v > 0; v >>= 8 {
		buf = append([]byte{byte(v)}, buf...)
	}
	return buf
}

func webmUnknownSeg(body []byte) []byte {
	out := webmID(0x18538067)
	out = append(out, 0x01, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF)
	out = append(out, body...)
	return out
}

func firstNBytes(b []byte, n int) []byte {
	if len(b) < n {
		return b
	}
	return b[:n]
}
