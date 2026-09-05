package downloader

import (
	"bufio"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/hex"
	"fmt"
	"io"
	"math"
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

	// onSegment, when set, is called with a segment's playback duration
	// (seconds) after it is written, so the caller can report duration-based
	// progress. Segments without an EXTINF duration report 0 and are ignored.
	onSegment func(seconds float64)

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
	Discontinuity bool
	URL           string
	Duration      float64
	Key           *hlsKey
	Map           *hlsMap
	Seq           int
	Range         *hlsByteRange
}

type hlsKey struct {
	Method string
	URI    string
	IV     []byte
	Key    []byte
}

type hlsMap struct {
	URI   string
	Range *hlsByteRange
	Key   *hlsKey
}
type hlsByteRange struct{ Offset, Length int64 }

func NewHLSDownloader(client *http.Client, playlistURL string) *HLSDownloader {
	if client == nil {
		client = http.DefaultClient
	}

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

// WithProgressCallback registers a callback invoked with each written
// segment's playback duration (seconds), enabling duration-based progress.
func (h *HLSDownloader) WithProgressCallback(fn func(seconds float64)) *HLSDownloader {
	h.onSegment = fn
	return h
}

func (h *HLSDownloader) reportSegment(seconds float64) {
	if h.onSegment != nil && seconds > 0 {
		h.onSegment(seconds)
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
		segments, targetDuration, err := h.parseSegments(ctx, manifest, h.PlaylistURL)
		if err != nil {
			return err
		}
		isLive := !strings.Contains(manifest, "#EXT-X-ENDLIST")

		// 3a. Fast path: a completed (VOD) playlist has its full segment list
		// available in a single fetch, so segments can be downloaded in parallel
		// and written in order — mirroring the DASH concurrent path. Live
		// playlists must stay sequential: segments arrive over time and the
		// stream is written as an ordered append, so we fall through to the
		// per-segment loop below. Concurrency is only engaged when configured
		// (MaxConcurrency > 1) and there is more than one new segment to fetch.
		if !isLive && normalizeTransportConfig(h.Transport).MaxConcurrency > 1 {
			newSegs := h.filterNewSegments(segments)
			if len(newSegs) > 1 {
				if err := h.downloadSegmentsConcurrent(ctx, newSegs, w); err != nil {
					return err
				}
				return nil
			}
		}

		// 3b. Process new segments sequentially.
		newSegments := 0
		for _, seg := range segments {
			// Basic dedup by Sequence Number if available, else URL
			if seg.Seq <= h.lastSeq && h.lastSeq != -1 {
				continue
			}
			if h.seenSegments[hlsSegmentKey(seg)] {
				// Fallback dedup (shouldn't happen with proper Seq)
				continue
			}

			// A segment may carry its own EXT-X-MAP (initialization segment) that
			// differs from the playlist-level one. If the referenced init URI has
			// not been written yet (or changed since the last write), download and
			// write it before the segment body so the fMP4 stream stays valid.
			if seg.Map != nil && hlsMapKey(seg.Map) != h.writtenInitURI {
				if err := h.downloadInitSegment(ctx, *seg.Map, w); err != nil {
					return fmt.Errorf("failed to download init segment for seq=%d: %w", seg.Seq, err)
				}
				h.writtenInitURI = hlsMapKey(seg.Map)
			}

			if err := h.downloadSegment(ctx, seg, w); err != nil {
				if isLive && shouldSkipFragmentError(err, h.Transport) {
					h.skippedFragments++
					if limit := h.Transport.MaxSkippedFragments; limit > 0 && h.skippedFragments > limit {
						return fmt.Errorf("failed to download segment seq=%d (skip limit exceeded): %w", seg.Seq, err)
					}
					h.lastSeq = seg.Seq
					h.seenSegments = trackSeen(h.seenSegments, hlsSegmentKey(seg))
					h.reportSegment(seg.Duration)
					continue
				}
				return fmt.Errorf("failed to download segment seq=%d: %w", seg.Seq, err)
			}

			h.lastSeq = seg.Seq
			h.seenSegments = trackSeen(h.seenSegments, hlsSegmentKey(seg))
			h.reportSegment(seg.Duration)
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
	if !strings.HasPrefix(strings.TrimSpace(manifest), "#EXTM3U") {
		return nil, 0, fmt.Errorf("invalid HLS playlist header")
	}
	scanner := bufio.NewScanner(strings.NewReader(manifest))
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	var segments []hlsSegment
	var key *hlsKey
	var init *hlsMap
	var target, duration float64
	var pending bool
	var discontinuity bool
	var rawRange string
	var nextOffset int64
	var previousURL string
	seq := 0
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		switch {
		case line == "#EXT-X-DISCONTINUITY":
			discontinuity = true
		case strings.HasPrefix(line, "#EXT-X-TARGETDURATION:"):
			v, err := strconv.ParseFloat(strings.TrimPrefix(line, "#EXT-X-TARGETDURATION:"), 64)
			if err != nil || v <= 0 || math.IsInf(v, 0) || math.IsNaN(v) || v > 86400 {
				return nil, 0, fmt.Errorf("invalid HLS target duration")
			}
			target = v
		case strings.HasPrefix(line, "#EXT-X-MEDIA-SEQUENCE:"):
			v, err := strconv.Atoi(strings.TrimPrefix(line, "#EXT-X-MEDIA-SEQUENCE:"))
			if err != nil || v < 0 {
				return nil, 0, fmt.Errorf("invalid HLS media sequence")
			}
			seq = v
		case strings.HasPrefix(line, "#EXT-X-KEY:"):
			var err error
			key, err = parseKey(strings.TrimPrefix(line, "#EXT-X-KEY:"), manifestURL)
			if err != nil {
				return nil, 0, err
			}
			if key != nil {
				key.Key, err = h.fetchKey(ctx, key.URI)
				if err != nil {
					return nil, 0, err
				}
				if len(key.Key) != 16 {
					return nil, 0, fmt.Errorf("HLS AES-128 key must be 16 bytes")
				}
			}
		case strings.HasPrefix(line, "#EXT-X-MAP:"):
			var err error
			init, err = parseMap(strings.TrimPrefix(line, "#EXT-X-MAP:"), manifestURL)
			if err != nil {
				return nil, 0, err
			}
			init.Key = key
			if key != nil && len(key.IV) == 0 {
				return nil, 0, fmt.Errorf("encrypted HLS init requires an explicit IV")
			}
		case strings.HasPrefix(line, "#EXT-X-BYTERANGE:"):
			rawRange = strings.TrimPrefix(line, "#EXT-X-BYTERANGE:")
		case strings.HasPrefix(line, "#EXTINF:"):
			if pending {
				return nil, 0, fmt.Errorf("HLS segment URI missing")
			}
			duration = parseExtInf(line)
			pending = true
		case strings.HasPrefix(line, "#"):
			continue
		default:
			if !pending {
				continue
			}
			rawURL := resolveURL(manifestURL, line)
			var br *hlsByteRange
			if rawRange != "" {
				if !strings.Contains(rawRange, "@") && previousURL != rawURL {
					return nil, 0, fmt.Errorf("implicit HLS byte range requires preceding range on same URI")
				}
				var err error
				br, err = parseHLSByteRange(rawRange, nextOffset)
				if err != nil {
					return nil, 0, err
				}
				nextOffset = br.Offset + br.Length
			} else {
				nextOffset = 0
			}
			segments = append(segments, hlsSegment{Discontinuity: discontinuity, URL: rawURL, Duration: duration, Key: key, Map: init, Seq: seq, Range: br})
			if seq == math.MaxInt {
				return nil, 0, fmt.Errorf("HLS media sequence overflow")
			}
			seq++
			discontinuity = false
			if br != nil {
				previousURL = rawURL
			} else {
				previousURL = ""
			}
			pending = false
			rawRange = ""
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, 0, err
	}
	if pending {
		return nil, 0, fmt.Errorf("HLS segment URI missing")
	}
	return segments, target, nil
}

func parseHLSByteRange(raw string, offset int64) (*hlsByteRange, error) {
	parts := strings.Split(raw, "@")
	if len(parts) > 2 {
		return nil, fmt.Errorf("invalid HLS byte range")
	}
	length, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || length <= 0 {
		return nil, fmt.Errorf("invalid HLS byte range length")
	}
	if len(parts) == 2 {
		offset, err = strconv.ParseInt(parts[1], 10, 64)
	}
	if err != nil || offset < 0 || offset > math.MaxInt64-length {
		return nil, fmt.Errorf("invalid HLS byte range offset")
	}
	return &hlsByteRange{Offset: offset, Length: length}, nil
}

func hlsSegmentKey(seg hlsSegment) string {
	return fmt.Sprintf("%d|%s|%v", seg.Seq, seg.URL, rangeKey(seg.Range))
}
func rangeKey(r *hlsByteRange) string {
	if r == nil {
		return ""
	}
	return fmt.Sprintf("%d@%d", r.Length, r.Offset)
}
func hlsMapKey(m *hlsMap) string {
	if m == nil {
		return ""
	}
	return fmt.Sprintf("%s|%s|%v", m.URI, rangeKey(m.Range), m.Key)
}

func (h *HLSDownloader) fetchObject(ctx context.Context, raw string, br *hlsByteRange) ([]byte, error) {
	headers := cloneHeader(h.Headers)
	if br != nil {
		if headers == nil {
			headers = make(http.Header)
		}
		headers.Set("Range", fmt.Sprintf("bytes=%d-%d", br.Offset, br.Offset+br.Length-1))
	}
	body, err := doGETBytesWithRetry(ctx, h.Client, raw, headers, h.Transport)
	if err != nil {
		return nil, err
	}
	if len(body) == 0 {
		return nil, fmt.Errorf("empty HLS object: %s", raw)
	}
	if br != nil && int64(len(body)) != br.Length {
		return nil, io.ErrUnexpectedEOF
	}
	return body, nil
}

func (h *HLSDownloader) downloadSegment(ctx context.Context, seg hlsSegment, w io.Writer) error {
	body, err := h.fetchSegmentBody(ctx, seg)
	if err != nil {
		return err
	}
	_, err = w.Write(body)
	return err
}

// fetchSegmentBody downloads a segment and returns its decrypted (plaintext)
// body without writing it. Splitting fetch/decrypt from the write lets the
// concurrent VOD path fetch many segments in parallel and then write them in
// playlist order.
func (h *HLSDownloader) fetchSegmentBody(ctx context.Context, seg hlsSegment) ([]byte, error) {
	body, err := h.fetchObject(ctx, seg.URL, seg.Range)
	if err != nil {
		return nil, err
	}
	return decryptHLSBody(body, seg.Key, seg.Seq)
}

func decryptHLSBody(body []byte, key *hlsKey, seq int) ([]byte, error) {
	// Decrypt if needed
	if key != nil && key.Method == "AES-128" {
		if len(key.Key) == 0 {
			return nil, fmt.Errorf("key not fetched for encrypted segment")
		}
		if len(body) == 0 {
			return nil, fmt.Errorf("empty encrypted HLS object")
		}
		block, err := aes.NewCipher(key.Key)
		if err != nil {
			return nil, err
		}
		// The HLS spec says: if EXT-X-KEY has no IV, the IV is the segment
		// sequence number as a 128-bit big-endian integer. cipher.NewCBCDecrypter
		// panics if the IV is nil, so we must synthesize it here.
		iv := key.IV
		if len(iv) == 0 {
			iv = defaultAESIVForSeq(seq)
		}
		if len(iv) != aes.BlockSize {
			return nil, fmt.Errorf("invalid HLS IV length: %d", len(iv))
		}
		cbc := cipher.NewCBCDecrypter(block, iv)
		if len(body)%aes.BlockSize != 0 {
			return nil, fmt.Errorf("encrypted data not block aligned")
		}
		cbc.CryptBlocks(body, body)
		// Validate and remove PKCS7 padding.
		padding := int(body[len(body)-1])
		if padding == 0 || padding > len(body) || padding > aes.BlockSize {
			return nil, fmt.Errorf("invalid PKCS7 padding: value=%d len=%d", padding, len(body))
		}
		for i := 0; i < padding; i++ {
			if int(body[len(body)-1-i]) != padding {
				return nil, fmt.Errorf("invalid PKCS7 padding: byte at %d is %d, expected %d", len(body)-1-i, body[len(body)-1-i], padding)
			}
		}
		body = body[:len(body)-padding]
	}
	if len(body) == 0 {
		return nil, fmt.Errorf("empty HLS media")
	}
	return body, nil
}

// filterNewSegments returns, in playlist order, the segments that have not yet
// been written to the output — applying the same media-sequence / URL dedup as
// the sequential loop so a refetched playlist does not re-download segments.
func (h *HLSDownloader) filterNewSegments(segments []hlsSegment) []hlsSegment {
	out := make([]hlsSegment, 0, len(segments))
	for _, seg := range segments {
		if seg.Seq <= h.lastSeq && h.lastSeq != -1 {
			continue
		}
		if h.seenSegments[hlsSegmentKey(seg)] {
			continue
		}
		out = append(out, seg)
	}
	return out
}

// downloadSegmentsConcurrent fetches with fixed workers and writes in playlist order.
func (h *HLSDownloader) downloadSegmentsConcurrent(ctx context.Context, segments []hlsSegment, w io.Writer) error {
	return orderedFetch(ctx, len(segments), fragmentWindow(h.Transport),
		func(ctx context.Context, i int) ([]byte, error) {
			body, err := h.fetchSegmentBody(ctx, segments[i])
			if err != nil {
				return nil, fmt.Errorf("failed to download segment seq=%d: %w", segments[i].Seq, err)
			}
			return body, nil
		},
		func(ctx context.Context, i int, body []byte) error {
			seg := segments[i]
			if seg.Map != nil && hlsMapKey(seg.Map) != h.writtenInitURI {
				if err := h.downloadInitSegment(ctx, *seg.Map, w); err != nil {
					return err
				}
				h.writtenInitURI = hlsMapKey(seg.Map)
			}
			if _, err := w.Write(body); err != nil {
				return err
			}
			h.lastSeq = seg.Seq
			h.seenSegments = trackSeen(h.seenSegments, hlsSegmentKey(seg))
			h.reportSegment(seg.Duration)
			return nil
		})
}

// downloadInitSegment fetches the EXT-X-MAP initialization segment and writes
// it to the output, decrypting AES-128 initialization data when declared.
func (h *HLSDownloader) downloadInitSegment(ctx context.Context, m hlsMap, w io.Writer) error {
	if strings.TrimSpace(m.URI) == "" {
		return nil
	}
	body, err := h.fetchObject(ctx, m.URI, m.Range)
	if err != nil {
		return err
	}
	body, err = decryptHLSBody(body, m.Key, 0)
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

// parseExtInf extracts the segment duration (seconds) from an #EXTINF line of
// the form "#EXTINF:<duration>,<optional title>". Returns 0 when unparseable.
func parseExtInf(line string) float64 {
	v := strings.TrimPrefix(line, "#EXTINF:")
	if i := strings.IndexByte(v, ','); i >= 0 {
		v = v[:i]
	}
	d, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
	if err != nil || d < 0 || math.IsNaN(d) || math.IsInf(d, 0) {
		return 0
	}
	return d
}

func parseKey(attrs, manifestURL string) (*hlsKey, error) {
	m := formats.ParseM3U8Attrs(attrs)

	if m["METHOD"] == "NONE" {
		return nil, nil
	}
	if format := m["KEYFORMAT"]; format != "" && format != "identity" {
		return nil, fmt.Errorf("unsupported HLS key format %q", format)
	}
	if m["METHOD"] != "AES-128" {
		return nil, fmt.Errorf("unsupported HLS encryption method: %s", m["METHOD"])
	}
	if m["URI"] == "" {
		return nil, fmt.Errorf("missing HLS key URI")
	}
	key := &hlsKey{
		Method: m["METHOD"],
		URI:    resolveURL(manifestURL, m["URI"]),
	}
	if ivHex, ok := m["IV"]; ok {
		ivHex = strings.TrimPrefix(strings.TrimPrefix(ivHex, "0x"), "0X")
		if len(ivHex) == 0 || len(ivHex) > aes.BlockSize*2 {
			return nil, fmt.Errorf("invalid HLS IV length")
		}
		ivHex = strings.Repeat("0", aes.BlockSize*2-len(ivHex)) + ivHex
		iv, err := hex.DecodeString(ivHex)
		if err != nil {
			return nil, fmt.Errorf("invalid HLS IV: %w", err)
		}
		key.IV = iv
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
	mout := &hlsMap{URI: resolveURL(manifestURL, uri)}
	if raw := m["BYTERANGE"]; raw != "" {
		var err error
		mout.Range, err = parseHLSByteRange(raw, 0)
		if err != nil {
			return nil, err
		}
	}
	return mout, nil
}
