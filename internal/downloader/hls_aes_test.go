package downloader

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// padPKCS7 applies PKCS7 padding to make the input a multiple of aes.BlockSize.
func padPKCS7(b []byte) []byte {
	pad := aes.BlockSize - len(b)%aes.BlockSize
	return append(b, bytes.Repeat([]byte{byte(pad)}, pad)...)
}

func TestHLSDownloader_AES128DefaultIVFromSeq(t *testing.T) {
	// AES-128 HLS stream WITHOUT an IV attribute on EXT-X-KEY. The IV must
	// default to the segment sequence number as a 128-bit big-endian integer.
	keyHex := "0123456789abcdef0123456789abcdef"
	keyBytes, err := hex.DecodeString(keyHex)
	if err != nil {
		t.Fatalf("decode key: %v", err)
	}
	block, err := aes.NewCipher(keyBytes)
	if err != nil {
		t.Fatalf("new cipher: %v", err)
	}
	iv := defaultAESIVForSeq(0)
	// The segment payload starts at media sequence 0.
	plaintext := []byte("init-segment-data-here")
	padded := padPKCS7(plaintext)
	ciphertext := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(ciphertext, padded)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/playlist.m3u8":
			fmt.Fprintf(w, "#EXTM3U\n#EXT-X-VERSION:6\n#EXT-X-TARGETDURATION:1\n#EXT-X-MEDIA-SEQUENCE:0\n#EXT-X-KEY:METHOD=AES-128,URI=\"key.bin\"\n#EXTINF:1.0,\nseg-0.bin\n#EXT-X-ENDLIST\n")
		case "/key.bin":
			w.Write(keyBytes)
		case "/seg-0.bin":
			w.Write(ciphertext)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	dl := NewHLSDownloader(srv.Client(), srv.URL+"/playlist.m3u8")
	var buf bytes.Buffer
	if err := dl.Download(context.Background(), &buf); err != nil {
		t.Fatalf("Download() error = %v", err)
	}
	if got := buf.String(); got != string(plaintext) {
		t.Fatalf("decrypted payload mismatch:\n got: %q\nwant: %q", got, plaintext)
	}
}

func TestHLSDownloader_InitSegmentWrittenForFMP4(t *testing.T) {
	// An EXT-X-MAP init segment must be downloaded and written before any
	// media segment, otherwise fMP4 output is corrupt/unplayable.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/playlist.m3u8":
			fmt.Fprintf(w, "#EXTM3U\n#EXT-X-VERSION:6\n#EXT-X-TARGETDURATION:1\n#EXT-X-MAP:URI=\"init.mp4\"\n#EXTINF:1.0,\nseg-0.m4s\n#EXT-X-ENDLIST\n")
		case "/init.mp4":
			w.Write([]byte("INIT-PAYLOAD"))
		case "/seg-0.m4s":
			w.Write([]byte("SEG-0-PAYLOAD"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	dl := NewHLSDownloader(srv.Client(), srv.URL+"/playlist.m3u8")
	var buf bytes.Buffer
	if err := dl.Download(context.Background(), &buf); err != nil {
		t.Fatalf("Download() error = %v", err)
	}
	want := "INIT-PAYLOADSEG-0-PAYLOAD"
	if got := buf.String(); got != want {
		t.Fatalf("output mismatch (init must precede segment):\n got: %q\nwant: %q", got, want)
	}
}

func TestHLSDownloader_InitSegmentWrittenOnce(t *testing.T) {
	// On a live playlist refresh, the init segment must not be re-written.
	var initCalls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/playlist.m3u8":
			fmt.Fprintf(w, "#EXTM3U\n#EXT-X-VERSION:6\n#EXT-X-TARGETDURATION:0.01\n#EXT-X-MEDIA-SEQUENCE:0\n#EXT-X-MAP:URI=\"init.mp4\"\n#EXTINF:1.0,\nseg-0.m4s\n#EXTINF:1.0,\nseg-1.m4s\n")
		case "/init.mp4":
			atomic.AddInt32(&initCalls, 1)
			w.Write([]byte("I"))
		case "/seg-0.m4s":
			w.Write([]byte("0"))
		case "/seg-1.m4s":
			w.Write([]byte("1"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 0)
	cancel()
	dl := NewHLSDownloader(srv.Client(), srv.URL+"/playlist.m3u8")
	var buf bytes.Buffer
	_ = dl.Download(ctx, &buf)

	if atomic.LoadInt32(&initCalls) > 1 {
		t.Fatalf("init segment fetched more than once: %d", atomic.LoadInt32(&initCalls))
	}
}

func TestHLSDownloader_InitSegmentRewrittenOnMapChange(t *testing.T) {
	// When a playlist changes its EXT-X-MAP URI (e.g. live rotation), the
	// new init segment must be downloaded and written before segments that
	// reference it. This verifies the writtenInitURI tracking rewrites a
	// changed init instead of silently dropping it.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/playlist.m3u8":
			fmt.Fprintf(w, "#EXTM3U\n#EXT-X-VERSION:6\n#EXT-X-TARGETDURATION:1\n#EXT-X-MAP:URI=\"init-v1.mp4\"\n#EXTINF:1.0,\nseg-0.m4s\n#EXT-X-MAP:URI=\"init-v2.mp4\"\n#EXTINF:1.0,\nseg-1.m4s\n#EXT-X-ENDLIST\n")
		case "/init-v1.mp4":
			w.Write([]byte("INIT1"))
		case "/init-v2.mp4":
			w.Write([]byte("INIT2"))
		case "/seg-0.m4s":
			w.Write([]byte("S0"))
		case "/seg-1.m4s":
			w.Write([]byte("S1"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	dl := NewHLSDownloader(srv.Client(), srv.URL+"/playlist.m3u8")
	var buf bytes.Buffer
	if err := dl.Download(context.Background(), &buf); err != nil {
		t.Fatalf("Download() error = %v", err)
	}
	// Both inits must appear: INIT1 before S0, INIT2 before S1.
	got := buf.String()
	want := "INIT1S0INIT2S1"
	if got != want {
		t.Fatalf("rotated init not written before its segment:\n got: %q\nwant: %q", got, want)
	}
}
