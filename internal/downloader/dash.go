package downloader

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
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
	writtenInit      string
	skippedFragments int
}

func NewDASHDownloader(client *http.Client, manifestURL, representationID string) *DASHDownloader {
	if client == nil {
		client = http.DefaultClient
	}

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
	ID              string               `xml:"id,attr"`
	Start           string               `xml:"start,attr"`
	Duration        string               `xml:"duration,attr"`
	BaseURL         string               `xml:"BaseURL"`
	SegmentTemplate *dashSegmentTemplate `xml:"SegmentTemplate"`
	AdaptationSet   []dashAdaptationSet  `xml:"AdaptationSet"`
}

type dashAdaptationSet struct {
	BaseURL         string               `xml:"BaseURL"`
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
	offsetSet              bool
	Duration               int64                `xml:"duration,attr"`
	PresentationTimeOffset int64                `xml:"presentationTimeOffset,attr"`
	Timescale              int64                `xml:"timescale,attr"`
	Initialization         string               `xml:"initialization,attr"`
	Media                  string               `xml:"media,attr"`
	StartNumber            *int64               `xml:"startNumber,attr"`
	SegmentTimeline        *dashSegmentTimeline `xml:"SegmentTimeline"`
}

func (t *dashSegmentTemplate) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	type plain dashSegmentTemplate
	var v plain
	if err := d.DecodeElement(&v, &start); err != nil {
		return err
	}
	*t = dashSegmentTemplate(v)
	for _, a := range start.Attr {
		if a.Name.Local == "presentationTimeOffset" {
			t.offsetSet = true
		}
	}
	return nil
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
	PeriodKey string
	InitURL   string
	Period    int
	URL       string
	Seq       int64
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
		if isDynamic {
			next := make(map[string]bool, len(segments))
			for _, seg := range segments {
				k := dashSegmentKey(seg)
				if d.seenSegments[k] {
					next[k] = true
				}
			}
			d.seenSegments = next
		}
		if !isDynamic && len(segments) > 1 && normalizeTransportConfig(d.Transport).MaxConcurrency > 1 {
			if err := d.downloadSegmentsConcurrent(ctx, segments, w); err != nil {
				return err
			}
			return nil
		}

		// Download new segments.
		//
		// Dynamic (live) manifests are re-fetched on a sliding window and the
		// per-segment Seq is a synthetic position recomputed from StartNumber on
		// every refresh (see extractSegments), so it is NOT stable across
		// refreshes: a $Time$-based timeline keeps StartNumber constant while the
		// window advances, making the newest segment land at roughly the same
		// Seq as last refresh and the `seg.Seq <= lastSeq` guard silently skip
		// it. For dynamic manifests we therefore dedup by the stable segment URL
		// only. Static manifests keep the fast lastSeq short-circuit.
		for _, seg := range segments {

			if d.seenSegments[dashSegmentKey(seg)] {
				continue
			}

			if err := d.downloadSegment(ctx, seg, w); err != nil {
				if isDynamic && shouldSkipFragmentError(err, d.Transport) {
					d.skippedFragments++
					if limit := d.Transport.MaxSkippedFragments; limit > 0 && d.skippedFragments > limit {
						return fmt.Errorf("failed to download segment seq=%d (skip limit exceeded): %w", seg.Seq, err)
					}
					d.lastSeq = seg.Seq
					d.seenSegments[dashSegmentKey(seg)] = true
					continue
				}
				return err
			}

			d.lastSeq = seg.Seq
			d.seenSegments[dashSegmentKey(seg)] = true
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
	return orderedFetch(ctx, len(segments), fragmentWindow(d.Transport),
		func(ctx context.Context, i int) ([]byte, error) {
			seg := segments[i]
			body, err := doGETBytesWithRetry(ctx, d.Client, seg.URL, d.Headers, d.Transport)
			if err != nil {
				return nil, fmt.Errorf("failed to download segment seq=%d: %w", seg.Seq, err)
			}
			if len(body) == 0 {
				return nil, fmt.Errorf("empty DASH segment seq=%d", seg.Seq)
			}
			return body, nil
		}, func(ctx context.Context, i int, body []byte) error { return d.writeSegment(ctx, segments[i], body, w) })
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

func (d *DASHDownloader) downloadSegment(ctx context.Context, seg dashSegment, w io.Writer) error {
	body, err := doGETBytesWithRetry(ctx, d.Client, seg.URL, d.Headers, d.Transport)
	if err != nil {
		return err
	}
	if len(body) == 0 {
		return fmt.Errorf("segment seq=%d downloaded as empty body", seg.Seq)
	}
	return d.writeSegment(ctx, seg, body, w)
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
		if j == i || j == len(part) {
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
		value := f * float64(scale)
		if math.IsInf(value, 0) || math.IsNaN(value) || value >= float64(math.MaxInt64) || value < 0 || *d > time.Duration(math.MaxInt64)-time.Duration(value) {
			return fmt.Errorf("ISO 8601 duration overflow")
		}
		*d += time.Duration(value)
		*consumed = true
		i = j + 1
	}
	return nil
}
