package downloader

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type DASHDownloader struct {
	Client           *http.Client
	ManifestURL      string
	RepresentationID string

	// State
	seenSegments map[string]bool
	lastSeq      int64
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

		// Download new segments
		for _, seg := range segments {
			if seg.Seq <= d.lastSeq && d.lastSeq != -1 {
				continue
			}
			if d.seenSegments[seg.URL] {
				continue
			}

			if err := d.downloadSegment(ctx, seg, w); err != nil {
				return err
			}

			d.lastSeq = seg.Seq
			d.seenSegments[seg.URL] = true
		}

		if mpd.Type != "dynamic" {
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

func (d *DASHDownloader) fetchManifest(ctx context.Context) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", d.ManifestURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := d.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("DASH manifest fetch failed: %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
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
	req, err := http.NewRequestWithContext(ctx, "GET", seg.URL, nil)
	if err != nil {
		return err
	}
	resp, err := d.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("segment fetch failed: %d", resp.StatusCode)
	}
	_, err = io.Copy(w, resp.Body)
	return err
}

func parseDuration(s string) (time.Duration, error) {
	// ISO 8601 duration parser (PT1S)
	// Go doesn't have native ISO duration parser.
	// Simple approximation for PT#S
	return time.ParseDuration(strings.ToLower(strings.ReplaceAll(s, "PT", "")))
}
