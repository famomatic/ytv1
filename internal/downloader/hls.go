package downloader

import (
	"bufio"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/famomatic/ytv1/internal/formats"
)

// HLSDownloader implements Downloader for HLS streams.
type HLSDownloader struct {
	Client      *http.Client
	PlaylistURL string
	Headers     http.Header
	Transport   TransportConfig

	// State
	seenSegments     map[string]bool
	lastSeq          int
	skippedFragments int
	// writtenInitURI tracks the URI of the initialization segment that has
	// already been written to the output, so a changed EXT-X-MAP (e.g. during
	// a live rotation) triggers a rewrite of the new init, and the same URI is
	// not written twice across playlist refreshes.
	writtenInitURI string
}

type hlsSegment struct {
	URL      string
	Duration float64
	Key      *hlsKey
	Map      *hlsMap
	Seq      int
}

type hlsKey struct {
	Method string
	URI    string
	IV     []byte
	Key    []byte
}

type hlsMap struct {
	URI string
}

func NewHLSDownloader(client *http.Client, playlistURL string) *HLSDownloader {
	return &HLSDownloader{
		Client:       client,
		PlaylistURL:  playlistURL,
		seenSegments: make(map[string]bool),
		lastSeq:      -1,
	}
}

func (h *HLSDownloader) WithRequestHeaders(headers http.Header) *HLSDownloader {
	h.Headers = cloneHeader(headers)
	return h
}

func (h *HLSDownloader) WithTransportConfig(cfg TransportConfig) *HLSDownloader {
	h.Transport = cfg
	return h
}

func (h *HLSDownloader) Download(ctx context.Context, w io.Writer) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// 1. Fetch Media Playlist
		manifest, err := h.fetchManifest(ctx, h.PlaylistURL)
		if err != nil {
			return err
		}

		// 2. Parse Segments
		segments, targetDuration, err := h.parseSegments(ctx, manifest, h.PlaylistURL)
		if err != nil {
			return err
		}
		isLive := !strings.Contains(manifest, "#EXT-X-ENDLIST")

		// 3. Process new segments
		newSegments := 0
		for _, seg := range segments {
			// Basic dedup by Sequence Number if available, else URL
			if seg.Seq <= h.lastSeq && h.lastSeq != -1 {
				continue
			}
			if h.seenSegments[seg.URL] {
				// Fallback dedup (shouldn't happen with proper Seq)
				continue
			}

			// A segment may carry its own EXT-X-MAP (initialization segment) that
			// differs from the playlist-level one. If the referenced init URI has
			// not been written yet (or changed since the last write), download and
			// write it before the segment body so the fMP4 stream stays valid.
			if seg.Map != nil && seg.Map.URI != h.writtenInitURI {
				if err := h.downloadInitSegment(ctx, *seg.Map, w); err != nil {
					return fmt.Errorf("failed to download init segment for seq=%d: %w", seg.Seq, err)
				}
				h.writtenInitURI = seg.Map.URI
			}

			if err := h.downloadSegment(ctx, seg, w); err != nil {
				if isLive && shouldSkipFragmentError(err, h.Transport) {
					h.skippedFragments++
					if limit := h.Transport.MaxSkippedFragments; limit > 0 && h.skippedFragments > limit {
						return fmt.Errorf("failed to download segment seq=%d (skip limit exceeded): %w", seg.Seq, err)
					}
					h.lastSeq = seg.Seq
					h.seenSegments = trackSeen(h.seenSegments, seg.URL)
					continue
				}
				return fmt.Errorf("failed to download segment seq=%d: %w", seg.Seq, err)
			}

			h.lastSeq = seg.Seq
			h.seenSegments = trackSeen(h.seenSegments, seg.URL)
			newSegments++
		}

		// 4. Check for End List
		if !isLive {
			return nil
		}

		// 5. Wait before refresh
		sleepTime := time.Duration(targetDuration * float64(time.Second))
		if sleepTime == 0 {
			sleepTime = 5 * time.Second
		}

		timer := time.NewTimer(sleepTime)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (h *HLSDownloader) fetchManifest(ctx context.Context, url string) (string, error) {
	body, err := doGETBytesWithRetry(ctx, h.Client, url, h.Headers, h.Transport)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func (h *HLSDownloader) parseSegments(ctx context.Context, manifest, manifestURL string) ([]hlsSegment, float64, error) {
	scanner := bufio.NewScanner(strings.NewReader(manifest))
	// Bound individual line length to avoid OOM on malicious manifests.
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20) // 1 MiB max line
	var segments []hlsSegment
	var currentKey *hlsKey
	var currentMap *hlsMap
	var targetDuration float64

	seq := 0 // Default start

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		if strings.HasPrefix(line, "#EXT-X-TARGETDURATION:") {
			if v, err := strconv.ParseFloat(line[22:], 64); err == nil {
				targetDuration = v
			}
			continue
		}

		if strings.HasPrefix(line, "#EXT-X-MEDIA-SEQUENCE:") {
			if v, err := strconv.Atoi(line[22:]); err == nil {
				seq = v
			}
			continue
		}

		if strings.HasPrefix(line, "#EXT-X-KEY:") {
			k, err := parseKey(line[11:], manifestURL)
			if err != nil {
				return nil, 0, err
			}
			currentKey = k
			continue
		}

		if strings.HasPrefix(line, "#EXT-X-MAP:") {
			m, err := parseMap(line[11:], manifestURL)
			if err != nil {
				return nil, 0, fmt.Errorf("parse EXT-X-MAP: %w", err)
			}
			currentMap = m
			continue
		}

		if strings.HasPrefix(line, "#EXTINF:") {
			// duration := parseExtInf(line)
			// Next line is URL
			if scanner.Scan() {
				urlLine := strings.TrimSpace(scanner.Text())
				fullURL := resolveURL(manifestURL, urlLine)

				// Fetch Key if needed
				if currentKey != nil && currentKey.Method == "AES-128" && len(currentKey.Key) == 0 {
					keyBytes, err := h.fetchKey(ctx, currentKey.URI)
					if err != nil {
						return nil, 0, fmt.Errorf("failed to fetch key: %w", err)
					}
					currentKey.Key = keyBytes
				}

				segments = append(segments, hlsSegment{
					URL: fullURL,
					Key: currentKey,
					Map: currentMap,
					Seq: seq,
				})
				seq++
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, 0, err
	}
	return segments, targetDuration, nil
}

func (h *HLSDownloader) downloadSegment(ctx context.Context, seg hlsSegment, w io.Writer) error {
	body, err := doGETBytesWithRetry(ctx, h.Client, seg.URL, h.Headers, h.Transport)
	if err != nil {
		return err
	}
	// Decrypt if needed
	if seg.Key != nil && seg.Key.Method == "AES-128" {
		if len(seg.Key.Key) == 0 {
			return fmt.Errorf("key not fetched for encrypted segment")
		}
		block, err := aes.NewCipher(seg.Key.Key)
		if err != nil {
			return err
		}
		// The HLS spec says: if EXT-X-KEY has no IV, the IV is the segment
		// sequence number as a 128-bit big-endian integer. cipher.NewCBCDecrypter
		// panics if the IV is nil, so we must synthesize it here.
		iv := seg.Key.IV
		if len(iv) == 0 {
			iv = defaultAESIVForSeq(seg.Seq)
		}
		cbc := cipher.NewCBCDecrypter(block, iv)
		if len(body) == 0 {
			return nil
		}
		if len(body)%aes.BlockSize != 0 {
			return fmt.Errorf("encrypted data not block aligned")
		}
		cbc.CryptBlocks(body, body)
		// Validate and remove PKCS7 padding.
		padding := int(body[len(body)-1])
		if padding == 0 || padding > len(body) || padding > aes.BlockSize {
			return fmt.Errorf("invalid PKCS7 padding: value=%d len=%d", padding, len(body))
		}
		for i := 0; i < padding; i++ {
			if int(body[len(body)-1-i]) != padding {
				return fmt.Errorf("invalid PKCS7 padding: byte at %d is %d, expected %d", len(body)-1-i, body[len(body)-1-i], padding)
			}
		}
		body = body[:len(body)-padding]

		_, err = w.Write(body)
		return err
	}

	_, err = w.Write(body)
	return err
}

// downloadInitSegment fetches the EXT-X-MAP initialization segment and writes
// it to the output. The init segment of an fMP4 stream is not encrypted, so
// it is written verbatim.
func (h *HLSDownloader) downloadInitSegment(ctx context.Context, m hlsMap, w io.Writer) error {
	if strings.TrimSpace(m.URI) == "" {
		return nil
	}
	body, err := doGETBytesWithRetry(ctx, h.Client, m.URI, h.Headers, h.Transport)
	if err != nil {
		return err
	}
	_, err = w.Write(body)
	return err
}

// defaultAESIVForSeq builds the default IV used by HLS AES-128 when the
// EXT-X-KEY does not specify one: the segment's media sequence number as a
// 128-bit big-endian integer.
func defaultAESIVForSeq(seq int) []byte {
	iv := make([]byte, aes.BlockSize)
	u := uint64(seq)
	for i := 0; i < 8; i++ {
		iv[aes.BlockSize-1-i] = byte(u >> (8 * uint(i)))
	}
	return iv
}

func (h *HLSDownloader) fetchKey(ctx context.Context, url string) ([]byte, error) {
	return doGETBytesWithRetry(ctx, h.Client, url, h.Headers, h.Transport)
}

func parseKey(attrs, manifestURL string) (*hlsKey, error) {
	m := formats.ParseM3U8Attrs(attrs)

	key := &hlsKey{
		Method: m["METHOD"],
		URI:    resolveURL(manifestURL, m["URI"]),
	}
	if ivHex, ok := m["IV"]; ok {
		ivHex = strings.TrimPrefix(ivHex, "0x")
		iv, err := hex.DecodeString(ivHex)
		if err == nil {
			key.IV = iv
		}
	}
	return key, nil
}

func parseMap(attrs, manifestURL string) (*hlsMap, error) {
	m := formats.ParseM3U8Attrs(attrs)
	// URI is mandatory for MAP
	uri, ok := m["URI"]
	if !ok {
		return nil, fmt.Errorf("URI missing in EXT-X-MAP")
	}
	return &hlsMap{URI: resolveURL(manifestURL, uri)}, nil
}
