package downloader

import (
	"bufio"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
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
					h.reportSegment(seg.Duration)
					continue
				}
				return fmt.Errorf("failed to download segment seq=%d: %w", seg.Seq, err)
			}

			h.lastSeq = seg.Seq
			h.seenSegments = trackSeen(h.seenSegments, seg.URL)
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
			duration := parseExtInf(line)
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
					URL:      fullURL,
					Duration: duration,
					Key:      currentKey,
					Map:      currentMap,
					Seq:      seq,
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
	body, err := doGETBytesWithRetry(ctx, h.Client, seg.URL, h.Headers, h.Transport)
	if err != nil {
		return nil, err
	}
	// Decrypt if needed
	if seg.Key != nil && seg.Key.Method == "AES-128" {
		if len(seg.Key.Key) == 0 {
			return nil, fmt.Errorf("key not fetched for encrypted segment")
		}
		if len(body) == 0 {
			return body, nil
		}
		block, err := aes.NewCipher(seg.Key.Key)
		if err != nil {
			return nil, err
		}
		// The HLS spec says: if EXT-X-KEY has no IV, the IV is the segment
		// sequence number as a 128-bit big-endian integer. cipher.NewCBCDecrypter
		// panics if the IV is nil, so we must synthesize it here.
		iv := seg.Key.IV
		if len(iv) == 0 {
			iv = defaultAESIVForSeq(seg.Seq)
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
		if h.seenSegments[seg.URL] {
			continue
		}
		out = append(out, seg)
	}
	return out
}

// downloadSegmentsConcurrent downloads VOD segments with bounded concurrency
// and writes them in strict playlist order, streaming each segment to the
// output as soon as its turn arrives rather than buffering the whole batch.
//
// Fetches are gated by playlist index: segment i may not start until segment
// i-window has been written (the first `window` start immediately). This bounds
// the fetch-ahead distance — and therefore peak memory — to ~MaxConcurrency
// segments, and guarantees the writer's next-needed segment is always allowed
// to run (an unordered permit pool could let far-ahead fetches starve the
// writer's next segment and deadlock). Each write feeds the progress reporter,
// so progress is smooth and incremental instead of bursty.
func (h *HLSDownloader) downloadSegmentsConcurrent(ctx context.Context, segments []hlsSegment, w io.Writer) error {
	cfg := normalizeTransportConfig(h.Transport)
	window := cfg.MaxConcurrency
	if window < 1 {
		window = 1
	}

	n := len(segments)
	type result struct {
		body []byte
		err  error
	}
	slots := make([]result, n)
	done := make([]chan struct{}, n)
	gate := make([]chan struct{}, n)
	for i := range done {
		done[i] = make(chan struct{})
		gate[i] = make(chan struct{})
	}
	// Open the gate for the first `window` segments so they start immediately.
	for i := 0; i < window && i < n; i++ {
		close(gate[i])
	}

	// parentCtx is the caller's context; ctx is cancelled on the first failure
	// to stop sibling fetches — distinguishing the two lets a real segment error
	// surface instead of the internal cancellation it triggers.
	parentCtx := ctx
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// realErr records the lowest-indexed genuine fetch failure (as opposed to the
	// context.Canceled that failure induces on in-flight siblings). Without it,
	// the in-order writer could reach a canceled earlier-indexed sibling first
	// and report its "context canceled" instead of the real root cause.
	var (
		realErrMu  sync.Mutex
		realErrIdx = -1
		realErrVal error
	)
	recordReal := func(i int, err error) {
		realErrMu.Lock()
		if realErrVal == nil || i < realErrIdx {
			realErrIdx, realErrVal = i, err
		}
		realErrMu.Unlock()
	}
	getReal := func() (int, error) {
		realErrMu.Lock()
		defer realErrMu.Unlock()
		return realErrIdx, realErrVal
	}

	var wg sync.WaitGroup
	for i, seg := range segments {
		wg.Add(1)
		i, seg := i, seg
		go func() {
			defer wg.Done()
			select {
			case <-gate[i]:
			case <-ctx.Done():
				slots[i] = result{err: ctx.Err()}
				close(done[i])
				return
			}
			body, err := h.fetchSegmentBody(ctx, seg)
			slots[i] = result{body: body, err: err}
			close(done[i])
			if err != nil {
				if !errors.Is(err, context.Canceled) {
					recordReal(i, err)
				}
				cancel()
			}
		}()
	}

	// Consume fetched segments in order; after writing segment i, open the gate
	// for segment i+window so fetching stays at most `window` ahead.
	writeErr := func() error {
		for i, seg := range segments {
			select {
			case <-done[i]:
			case <-parentCtx.Done():
				cancel()
				return parentCtx.Err()
			}

			r := slots[i]
			if r.err != nil {
				cancel()
				if parentCtx.Err() != nil {
					return parentCtx.Err()
				}
				// Prefer the genuine root-cause failure over the context.Canceled
				// it may have induced on this (earlier-indexed) sibling.
				if ridx, rerr := getReal(); rerr != nil {
					return fmt.Errorf("failed to download segment seq=%d: %w", segments[ridx].Seq, rerr)
				}
				return fmt.Errorf("failed to download segment seq=%d: %w", seg.Seq, r.err)
			}

			// Init (EXT-X-MAP) segment is written before its media segment and
			// only when the referenced init URI changes.
			if seg.Map != nil && seg.Map.URI != h.writtenInitURI {
				if err := h.downloadInitSegment(ctx, *seg.Map, w); err != nil {
					cancel()
					return fmt.Errorf("failed to download init segment for seq=%d: %w", seg.Seq, err)
				}
				h.writtenInitURI = seg.Map.URI
			}

			if _, err := w.Write(r.body); err != nil {
				cancel()
				return err
			}
			slots[i].body = nil // release buffered segment
			h.lastSeq = seg.Seq
			h.seenSegments = trackSeen(h.seenSegments, seg.URL)
			h.reportSegment(seg.Duration)

			if next := i + window; next < n {
				close(gate[next])
			}
		}
		return nil
	}()
	wg.Wait()
	return writeErr
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

// parseExtInf extracts the segment duration (seconds) from an #EXTINF line of
// the form "#EXTINF:<duration>,<optional title>". Returns 0 when unparseable.
func parseExtInf(line string) float64 {
	v := strings.TrimPrefix(line, "#EXTINF:")
	if i := strings.IndexByte(v, ','); i >= 0 {
		v = v[:i]
	}
	d, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
	if err != nil || d < 0 {
		return 0
	}
	return d
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
