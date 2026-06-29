package downloader

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

type DASHDownloader struct {
	Client           *http.Client
	ManifestURL      string
	RepresentationID string
	Headers          http.Header
	Transport        TransportConfig

	// State
	seenSegments     map[string]bool
	lastSeq          int64
	skippedFragments int
}

func NewDASHDownloader(client *http.Client, manifestURL, representationID string) *DASHDownloader {
	return &DASHDownloader{
		Client:           client,
		ManifestURL:      manifestURL,
		RepresentationID: representationID,
		seenSegments:     make(map[string]bool),
		lastSeq:          -1,
	}
}

func (d *DASHDownloader) WithRequestHeaders(headers http.Header) *DASHDownloader {
	d.Headers = cloneHeader(headers)
	return d
}

func (d *DASHDownloader) WithTransportConfig(cfg TransportConfig) *DASHDownloader {
	d.Transport = cfg
	return d
}

// ... helper structs (dashMPD, dashPeriod, etc. as defined before) ...
type dashMPD struct {
	XMLName                   xml.Name     `xml:"MPD"`
	Type                      string       `xml:"type,attr"`
	MinimumUpdatePeriod       string       `xml:"minimumUpdatePeriod,attr"`
	AvailabilityStartTime     string       `xml:"availabilityStartTime,attr"`
	MediaPresentationDuration string       `xml:"mediaPresentationDuration,attr"`
	MinBufferTime             string       `xml:"minBufferTime,attr"`
	BaseURL                   string       `xml:"BaseURL"`
	Period                    []dashPeriod `xml:"Period"`
}

type dashPeriod struct {
	AdaptationSet []dashAdaptationSet `xml:"AdaptationSet"`
}

type dashAdaptationSet struct {
	MimeType        string               `xml:"mimeType,attr"`
	Representation  []dashRepresentation `xml:"Representation"`
	SegmentTemplate *dashSegmentTemplate `xml:"SegmentTemplate"`
}

type dashRepresentation struct {
	ID              string               `xml:"id,attr"`
	Bandwidth       int                  `xml:"bandwidth,attr"`
	BaseURL         string               `xml:"BaseURL"`
	SegmentTemplate *dashSegmentTemplate `xml:"SegmentTemplate"`
}

type dashSegmentTemplate struct {
	Timescale       int64                `xml:"timescale,attr"`
	Initialization  string               `xml:"initialization,attr"`
	Media           string               `xml:"media,attr"`
	StartNumber     int64                `xml:"startNumber,attr"`
	SegmentTimeline *dashSegmentTimeline `xml:"SegmentTimeline"`
}

type dashSegmentTimeline struct {
	S []dashS `xml:"S"`
}

type dashS struct {
	T *int64 `xml:"t,attr"` // Pointer to distinguish missing attribute
	D int64  `xml:"d,attr"`
	R int64  `xml:"r,attr"`
}

type dashSegment struct {
	URL string
	Seq int64
}

func (d *DASHDownloader) Download(ctx context.Context, w io.Writer) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		manifest, err := d.fetchManifest(ctx)
		if err != nil {
			return err
		}

		mpd, err := parseDASH(manifest)
		if err != nil {
			return err
		}

		segments, timeout, err := d.extractSegments(mpd)
		if err != nil {
			return err
		}

		isDynamic := mpd.Type == "dynamic"
		if !isDynamic && len(segments) > 1 && normalizeTransportConfig(d.Transport).MaxConcurrency > 1 {
			if err := d.downloadSegmentsConcurrent(ctx, segments, w); err != nil {
				return err
			}
			return nil
		}

		// Download new segments
		for _, seg := range segments {
			if seg.Seq <= d.lastSeq && d.lastSeq != -1 {
				continue
			}
			if d.seenSegments[seg.URL] {
				continue
			}

			if err := d.downloadSegment(ctx, seg, w); err != nil {
				if isDynamic && shouldSkipFragmentError(err, d.Transport) {
					d.skippedFragments++
					if limit := d.Transport.MaxSkippedFragments; limit > 0 && d.skippedFragments > limit {
						return fmt.Errorf("failed to download segment seq=%d (skip limit exceeded): %w", seg.Seq, err)
					}
					d.lastSeq = seg.Seq
					d.seenSegments = trackSeen(d.seenSegments, seg.URL)
					continue
				}
				return err
			}

			d.lastSeq = seg.Seq
			d.seenSegments = trackSeen(d.seenSegments, seg.URL)
		}

		if !isDynamic {
			return nil
		}

		// Wait
		sleepTime := timeout
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

// maxConcurrentDASHSegments caps the number of segments buffered in memory
// during concurrent DASH downloads to prevent OOM on large static streams.
const maxConcurrentDASHSegments = 64

func (d *DASHDownloader) downloadSegmentsConcurrent(ctx context.Context, segments []dashSegment, w io.Writer) error {
	for start := 0; start < len(segments); start += maxConcurrentDASHSegments {
		end := start + maxConcurrentDASHSegments
		if end > len(segments) {
			end = len(segments)
		}
		if err := d.downloadSegmentBatchConcurrent(ctx, segments[start:end], w); err != nil {
			return err
		}
	}
	return nil
}

func (d *DASHDownloader) downloadSegmentBatchConcurrent(ctx context.Context, segments []dashSegment, w io.Writer) error {
	type item struct {
		index int
		seq   int64
		url   string
		body  []byte
		err   error
	}
	cfg := normalizeTransportConfig(d.Transport)
	concurrency := cfg.MaxConcurrency
	if concurrency > len(segments) {
		concurrency = len(segments)
	}
	sem := make(chan struct{}, concurrency)
	out := make([]item, len(segments))
	var wg sync.WaitGroup
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	for i, seg := range segments {
		wg.Add(1)
		i, seg := i, seg
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-sem }()
			body, err := doGETBytesWithRetry(ctx, d.Client, seg.URL, d.Headers, d.Transport)
			out[i] = item{
				index: i,
				seq:   seg.Seq,
				url:   seg.URL,
				body:  body,
				err:   err,
			}
			if err != nil {
				cancel()
			}
		}()
	}
	wg.Wait()

	for _, it := range out {
		if it.err != nil {
			return fmt.Errorf("failed to download segment seq=%d: %w", it.seq, it.err)
		}
		if _, err := w.Write(it.body); err != nil {
			return err
		}
		d.lastSeq = it.seq
		d.seenSegments[it.url] = true
	}
	return nil
}

func (d *DASHDownloader) fetchManifest(ctx context.Context) ([]byte, error) {
	return doGETBytesWithRetry(ctx, d.Client, d.ManifestURL, d.Headers, d.Transport)
}

func parseDASH(data []byte) (*dashMPD, error) {
	var mpd dashMPD
	if err := xml.Unmarshal(data, &mpd); err != nil {
		return nil, err
	}
	return &mpd, nil
}

func (d *DASHDownloader) extractSegments(mpd *dashMPD) ([]dashSegment, time.Duration, error) {
	// Find Representation
	var rep *dashRepresentation
	var adapt *dashAdaptationSet

	found := false
	for _, p := range mpd.Period {
		for i, a := range p.AdaptationSet {
			for j, r := range a.Representation {
				if r.ID == d.RepresentationID {
					rep = &p.AdaptationSet[i].Representation[j]
					adapt = &p.AdaptationSet[i]
					found = true
					break
				}
			}
			if found {
				break
			}
		}
		if found {
			break
		}
	}

	if !found {
		return nil, 0, fmt.Errorf("representation %s not found", d.RepresentationID)
	}

	// Resolve Template
	tmpl := rep.SegmentTemplate
	if tmpl == nil {
		tmpl = adapt.SegmentTemplate
	}
	if tmpl == nil {
		return nil, 0, fmt.Errorf("SegmentTemplate not found for representation %s", d.RepresentationID)
	}

	// Resolve BaseURL
	baseURL := mpd.BaseURL
	if rep.BaseURL != "" {
		baseURL = rep.BaseURL // Overrides? Or appends? DASH standard says ... complex. Assuming override or relative.
		// YouTube usually puts BaseURL in Rep usually? Or MPD level.
	}

	// Timeline processing
	if tmpl.SegmentTimeline == nil {
		// Number based template?
		return nil, 0, fmt.Errorf("SegmentTimeline missing (Number-based template not implemented)")
	}

	var segments []dashSegment
	currentTime := int64(0)
	currentSeq := tmpl.StartNumber // Defaults to 1?
	if currentSeq == 0 {
		currentSeq = 1
	}

	for _, s := range tmpl.SegmentTimeline.S {
		if s.T != nil {
			currentTime = *s.T
		}

		// Cap repeat count for safety. The DASH spec defines r=-1 as
		// "repeat until the next S element", but we don't have the next S
		// boundary here. Cap at a reasonable maximum and treat r=-1 as
		// a single occurrence (the caller will re-fetch for dynamic manifests).
		const maxSegmentRepeatCount = 10000
		repeatCount := s.R
		if repeatCount < 0 {
			repeatCount = 0 // r=-1: generate one segment; dynamic manifests re-fetch
		}
		if repeatCount > maxSegmentRepeatCount {
			repeatCount = maxSegmentRepeatCount
		}
		count := repeatCount + 1
		for i := int64(0); i < count; i++ {
			// Generate URL
			urlStr := strings.ReplaceAll(tmpl.Media, "$RepresentationID$", d.RepresentationID)
			urlStr = strings.ReplaceAll(urlStr, "$Number$", fmt.Sprintf("%d", currentSeq))
			urlStr = strings.ReplaceAll(urlStr, "$Time$", fmt.Sprintf("%d", currentTime))
			urlStr = strings.ReplaceAll(urlStr, "$Bandwidth$", fmt.Sprintf("%d", rep.Bandwidth))

			fullURL := resolveURL(d.ManifestURL, baseURL+urlStr)

			segments = append(segments, dashSegment{
				URL: fullURL,
				Seq: currentSeq,
			})

			currentTime += s.D
			currentSeq++
		}
	}

	// Duration calculation (minimumUpdatePeriod)
	timeout := 5 * time.Second
	if mpd.MinimumUpdatePeriod != "" {
		if d, err := parseDuration(mpd.MinimumUpdatePeriod); err == nil {
			timeout = d
		}
	}

	return segments, timeout, nil
}

func (d *DASHDownloader) downloadSegment(ctx context.Context, seg dashSegment, w io.Writer) error {
	body, err := doGETBytesWithRetry(ctx, d.Client, seg.URL, d.Headers, d.Transport)
	if err != nil {
		return err
	}
	_, err = w.Write(body)
	return err
}

func parseDuration(s string) (time.Duration, error) {
	return parseISO8601Duration(s)
}

// parseISO8601Duration parses a subset of ISO 8601 durations used in DASH
// manifests: forms like "PT1S", "PT1H30M", "PT2.5S", "PT0S", and the optional
// leading date component "P1DT2H". The previous implementation lowercased the
// string and then did a case-sensitive ReplaceAll of "PT", which never
// matched, so every DASH refresh interval was mis-parsed.
func parseISO8601Duration(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty duration")
	}
	if !strings.HasPrefix(s, "P") {
		return 0, fmt.Errorf("invalid ISO 8601 duration: %q", s)
	}
	body := s[1:]
	var d time.Duration
	consumed := false
	// Optional days: split on "T" if a date portion precedes the time portion.
	if idx := strings.Index(body, "T"); idx >= 0 {
		datePart := body[:idx]
		timePart := body[idx+1:]
		if err := scanDurationComponents(datePart, map[byte]time.Duration{'D': 24 * time.Hour, 'W': 7 * 24 * time.Hour, 'M': 30 * 24 * time.Hour, 'Y': 365 * 24 * time.Hour}, &d, &consumed); err != nil {
			return 0, err
		}
		if err := scanDurationComponents(timePart, map[byte]time.Duration{'H': time.Hour, 'M': time.Minute, 'S': time.Second}, &d, &consumed); err != nil {
			return 0, err
		}
	} else {
		// No time portion: only date units are valid.
		if err := scanDurationComponents(body, map[byte]time.Duration{'D': 24 * time.Hour, 'W': 7 * 24 * time.Hour, 'M': 30 * 24 * time.Hour, 'Y': 365 * 24 * time.Hour}, &d, &consumed); err != nil {
			return 0, err
		}
	}
	// "P" or "PT" with no components is not a valid duration.
	if !consumed {
		return 0, fmt.Errorf("invalid ISO 8601 duration: %q", s)
	}
	return d, nil
}

// scanDurationComponents scans a sequence like "1H30M" or "2.5S", adding each
// value (scaled by its unit) into d. units maps the unit suffix byte to its
// duration scale.
func scanDurationComponents(part string, units map[byte]time.Duration, d *time.Duration, consumed *bool) error {
	i := 0
	for i < len(part) {
		// Read the numeric value (integer or fractional).
		j := i
		for j < len(part) && (part[j] == '.' || (part[j] >= '0' && part[j] <= '9')) {
			j++
		}
		if j == i {
			return fmt.Errorf("invalid ISO 8601 duration segment: %q", part)
		}
		scale, ok := units[part[j]]
		if !ok {
			return fmt.Errorf("unknown ISO 8601 duration unit %q in %q", string(part[j]), part)
		}
		f, err := strconv.ParseFloat(part[i:j], 64)
		if err != nil {
			return fmt.Errorf("invalid ISO 8601 duration value %q: %w", part[i:j], err)
		}
		*d += time.Duration(f * float64(scale))
		*consumed = true
		i = j + 1
	}
	return nil
}
