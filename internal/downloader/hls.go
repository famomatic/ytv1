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

			if err := h.downloadSegment(ctx, seg, w); err != nil {
				if isLive && shouldSkipFragmentError(err, h.Transport) {
					h.skippedFragments++
					if limit := h.Transport.MaxSkippedFragments; limit > 0 && h.skippedFragments > limit {
						return fmt.Errorf("failed to download segment seq=%d (skip limit exceeded): %w", seg.Seq, err)
					}
					h.lastSeq = seg.Seq
					h.seenSegments[seg.URL] = true
					continue
				}
				return fmt.Errorf("failed to download segment seq=%d: %w", seg.Seq, err)
			}

			h.lastSeq = seg.Seq
			h.seenSegments[seg.URL] = true
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
		// If we found no new segments, maybe backoff slightly not needed as we sleep targetDuration
		// Usually targetDuration / 2 or full targetDuration.
		// yt-dlp logic is complex, simple approach: wait targetDuration.

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
			// m, err := parseMap(line[11:])
			// if err != nil { return nil, 0, err }
			// currentMap = m
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
		cbc := cipher.NewCBCDecrypter(block, seg.Key.IV)
		if len(body) == 0 {
			return nil
		}
		if len(body)%aes.BlockSize != 0 {
			return fmt.Errorf("encrypted data not block aligned")
		}
		cbc.CryptBlocks(body, body)
		// Remove padding (PKCS7)
		padding := int(body[len(body)-1])
		if padding > len(body) || padding == 0 {
			// This happens if key is wrong or data is corrupt.
			// For now, return error or maybe just warn and write raw?
			// Return error to be safe.
			return fmt.Errorf("invalid padding")
		}
		body = body[:len(body)-padding]

		_, err = w.Write(body)
		return err
	}

	_, err = w.Write(body)
	return err
}

func (h *HLSDownloader) fetchKey(ctx context.Context, url string) ([]byte, error) {
	return doGETBytesWithRetry(ctx, h.Client, url, h.Headers, h.Transport)
}

func parseKey(attrs, manifestURL string) (*hlsKey, error) {
	m := formats.ParseM3U8Attrs(attrs)

	key := &hlsKey{
		Method: m["METHOD"],
		URI:    m["URI"],
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

func parseMap(attrs string) (*hlsMap, error) {
	m := formats.ParseM3U8Attrs(attrs)
	// URI is mandatory for MAP
	uri, ok := m["URI"]
	if !ok {
		return nil, fmt.Errorf("URI missing in EXT-X-MAP")
	}
	return &hlsMap{URI: uri}, nil
}
