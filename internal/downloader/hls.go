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

	// State
	seenSegments map[string]bool
	lastSeq      int
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
		segments, targetDuration, err := h.parseSegments(manifest, h.PlaylistURL)
		if err != nil {
			return err
		}

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
				return fmt.Errorf("failed to download segment seq=%d: %w", seg.Seq, err)
			}

			h.lastSeq = seg.Seq
			h.seenSegments[seg.URL] = true
			newSegments++
		}

		// 4. Check for End List
		if strings.Contains(manifest, "#EXT-X-ENDLIST") {
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
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", err
	}
	resp, err := h.Client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("manifest fetch failed: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func (h *HLSDownloader) parseSegments(manifest, manifestURL string) ([]hlsSegment, float64, error) {
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
					keyBytes, err := h.fetchKey(context.Background(), currentKey.URI) // TODO: pass ctx
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
	req, err := http.NewRequestWithContext(ctx, "GET", seg.URL, nil)
	if err != nil {
		return err
	}
	resp, err := h.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("segment fetch failed: %d", resp.StatusCode)
	}

	var reader io.Reader = resp.Body

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

		data, err := io.ReadAll(reader)
		if err != nil {
			return err
		}
		if len(data) == 0 {
			return nil
		}
		if len(data)%aes.BlockSize != 0 {
			return fmt.Errorf("encrypted data not block aligned")
		}
		cbc.CryptBlocks(data, data)
		// Remove padding (PKCS7)
		padding := int(data[len(data)-1])
		if padding > len(data) || padding == 0 {
			// This happens if key is wrong or data is corrupt.
			// For now, return error or maybe just warn and write raw?
			// Return error to be safe.
			return fmt.Errorf("invalid padding")
		}
		data = data[:len(data)-padding]

		_, err = w.Write(data)
		return err
	}

	_, err = io.Copy(w, reader)
	return err
}

func (h *HLSDownloader) fetchKey(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := h.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("key fetch failed: %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
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
