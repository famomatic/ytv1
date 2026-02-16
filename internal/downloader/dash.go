package downloader

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
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
					d.seenSegments[seg.URL] = true
					continue
				}
				return err
			}

			d.lastSeq = seg.Seq
			d.seenSegments[seg.URL] = true
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

func (d *DASHDownloader) downloadSegmentsConcurrent(ctx context.Context, segments []dashSegment, w io.Writer) error {
	type item struct {
		index int
		seq   int64
		url   string
		body  []byte
		err   error
	}
	cfg := normalizeTransportConfig(d.Transport)
	sem := make(chan struct{}, cfg.MaxConcurrency)
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

		// Repeat 'r' times (r=0 means 1 occurrence total, r=1 means 2 total)
		// Spec says r is repeat count *after* the first one.
		// Usually r=-1 means repeat until next S.

		count := s.R + 1
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
	// ISO 8601 duration parser (PT1S)
	// Go doesn't have native ISO duration parser.
	// Simple approximation for PT#S
	return time.ParseDuration(strings.ToLower(strings.ReplaceAll(s, "PT", "")))
}
