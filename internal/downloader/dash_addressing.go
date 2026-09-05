package downloader

import (
	"context"
	"fmt"
	"io"
	"math"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

func (d *DASHDownloader) extractSegments(mpd *dashMPD) ([]dashSegment, time.Duration, error) {
	timeout := 5 * time.Second
	if mpd.MinimumUpdatePeriod != "" {
		var err error
		timeout, err = parseDuration(mpd.MinimumUpdatePeriod)
		if err != nil {
			return nil, 0, err
		}
		if timeout <= 0 {
			timeout = time.Second
		}
	}
	var total time.Duration
	if mpd.MediaPresentationDuration != "" {
		var err error
		total, err = parseDuration(mpd.MediaPresentationDuration)
		if err != nil {
			return nil, 0, err
		}
	}
	var segments []dashSegment
	found := false
	periodStart := time.Duration(0)
	for pi, p := range mpd.Period {
		periodMatched := false
		if p.Start != "" {
			var err error
			periodStart, err = parseDuration(p.Start)
			if err != nil {
				return nil, 0, err
			}
		}
		periodDuration := time.Duration(0)
		if p.Duration != "" {
			var err error
			periodDuration, err = parseDuration(p.Duration)
			if err != nil {
				return nil, 0, err
			}
		} else if pi+1 < len(mpd.Period) && mpd.Period[pi+1].Start != "" {
			next, err := parseDuration(mpd.Period[pi+1].Start)
			if err != nil {
				return nil, 0, err
			}
			periodDuration = next - periodStart
		} else if total > periodStart {
			periodDuration = total - periodStart
		}
		periodKey := p.ID
		if periodKey == "" {
			periodKey = fmt.Sprintf("start:%d", periodStart)
		}
		for _, a := range p.AdaptationSet {
			for _, rep := range a.Representation {
				if rep.ID != d.RepresentationID {
					continue
				}
				found = true
				periodMatched = true
				base := d.ManifestURL
				for _, part := range []string{mpd.BaseURL, p.BaseURL, a.BaseURL, rep.BaseURL} {
					if strings.TrimSpace(part) != "" {
						base = resolveURL(base, strings.TrimSpace(part))
					}
				}
				tmpl := mergeDASHTemplate(mergeDASHTemplate(p.SegmentTemplate, a.SegmentTemplate), rep.SegmentTemplate)
				if tmpl == nil {
					if rep.BaseURL == "" && a.BaseURL == "" {
						return nil, 0, fmt.Errorf("representation %s has no media addressing", rep.ID)
					}
					segments = append(segments, dashSegment{URL: base, Seq: 1, Period: pi, PeriodKey: periodKey})
					continue
				}
				if tmpl.Timescale < 0 || tmpl.Duration < 0 || tmpl.PresentationTimeOffset < 0 {
					return nil, 0, fmt.Errorf("invalid DASH template timing")
				}
				scale := tmpl.Timescale
				if scale <= 0 {
					scale = 1
				}
				number := int64(1)
				if tmpl.StartNumber != nil {
					number = *tmpl.StartNumber
				}
				if number < 0 {
					return nil, 0, fmt.Errorf("negative DASH startNumber")
				}
				initURL := ""
				if tmpl.Initialization != "" {
					v, err := expandDASHTemplate(tmpl.Initialization, rep, number, 0)
					if err != nil {
						return nil, 0, err
					}
					initURL = resolveURL(base, v)
				}
				if tmpl.Media == "" {
					return nil, 0, fmt.Errorf("missing DASH media template")
				}
				boundary := int64(0)
				if periodDuration > 0 {
					v := periodDuration.Seconds() * float64(scale)
					if v >= float64(math.MaxInt64-tmpl.PresentationTimeOffset) {
						return nil, 0, fmt.Errorf("DASH duration overflow")
					}
					boundary = int64(math.Ceil(v)) + tmpl.PresentationTimeOffset
				}
				liveEnd := int64(0)
				if mpd.Type == "dynamic" && mpd.AvailabilityStartTime != "" {
					started, err := time.Parse(time.RFC3339Nano, mpd.AvailabilityStartTime)
					if err != nil {
						return nil, 0, err
					}
					elapsed := time.Since(started) - periodStart
					if elapsed > 0 {
						v := elapsed.Seconds() * float64(scale)
						if v >= float64(math.MaxInt64-tmpl.PresentationTimeOffset) {
							return nil, 0, fmt.Errorf("DASH live clock overflow")
						}
						liveEnd = int64(v) + tmpl.PresentationTimeOffset
					}
					if boundary == 0 || liveEnd < boundary {
						boundary = liveEnd
					}
				}
				timeline := tmpl.SegmentTimeline
				if timeline == nil {
					if tmpl.Duration <= 0 || boundary <= 0 {
						return nil, 0, fmt.Errorf("DASH number template requires duration and a presentation boundary")
					}
					start := tmpl.PresentationTimeOffset
					timeline = &dashSegmentTimeline{S: []dashS{{T: &start, D: tmpl.Duration, R: -1}}}
				}
				stamp := int64(0)
				for si, entry := range timeline.S {
					if entry.T != nil {
						stamp = *entry.T
					}
					if entry.D <= 0 || entry.R < -1 || entry.R == math.MaxInt64 || stamp < 0 {
						return nil, 0, fmt.Errorf("invalid DASH timeline entry")
					}
					count := entry.R + 1
					if entry.R == -1 {
						end := boundary
						if si+1 < len(timeline.S) && timeline.S[si+1].T != nil {
							end = *timeline.S[si+1].T
						}
						if end <= stamp {
							return nil, 0, fmt.Errorf("unbounded or invalid DASH negative repeat")
						}
						span := end - stamp
						count = span / entry.D
						if span%entry.D != 0 {
							count++
						}
						if mpd.Type == "dynamic" && liveEnd > 0 && end == liveEnd {
							count = span / entry.D
						}
					}
					const maxManifestSegments = 100000
					if count > maxManifestSegments || int64(len(segments))+count > maxManifestSegments {
						return nil, 0, fmt.Errorf("DASH manifest exceeds %d segments", maxManifestSegments)
					}
					for n := int64(0); n < count; n++ {
						v, err := expandDASHTemplate(tmpl.Media, rep, number, stamp)
						if err != nil {
							return nil, 0, err
						}
						segments = append(segments, dashSegment{URL: resolveURL(base, v), Seq: number, Period: pi, PeriodKey: periodKey, InitURL: initURL})
						if stamp > math.MaxInt64-entry.D || number == math.MaxInt64 {
							return nil, 0, fmt.Errorf("DASH timeline overflow")
						}
						stamp += entry.D
						number++
					}
				}
			}
		}
		if !periodMatched {
			return nil, 0, fmt.Errorf("representation %s missing in period %d", d.RepresentationID, pi)
		}
		periodStart += periodDuration
	}
	if !found {
		return nil, 0, fmt.Errorf("representation %s not found", d.RepresentationID)
	}
	return segments, timeout, nil
}

func mergeDASHTemplate(parent, child *dashSegmentTemplate) *dashSegmentTemplate {
	if parent == nil {
		if child == nil {
			return nil
		}
		out := *child
		return &out
	}
	out := *parent
	if child == nil {
		return &out
	}
	if child.Timescale != 0 {
		out.Timescale = child.Timescale
	}
	if child.Duration != 0 {
		out.Duration = child.Duration
	}
	if child.offsetSet || child.PresentationTimeOffset != 0 {
		out.PresentationTimeOffset = child.PresentationTimeOffset
	}
	if child.Initialization != "" {
		out.Initialization = child.Initialization
	}
	if child.Media != "" {
		out.Media = child.Media
	}
	if child.StartNumber != nil {
		out.StartNumber = child.StartNumber
	}
	if child.SegmentTimeline != nil {
		out.SegmentTimeline = child.SegmentTimeline
	}
	return &out
}

var dashTemplateToken = regexp.MustCompile("^(RepresentationID|Number|Time|Bandwidth)(%0?([0-9]+)d)?$")

func expandDASHTemplate(raw string, rep dashRepresentation, number, stamp int64) (string, error) {
	var out strings.Builder
	for i := 0; i < len(raw); {
		if raw[i] != '$' {
			out.WriteByte(raw[i])
			i++
			continue
		}
		if i+1 < len(raw) && raw[i+1] == '$' {
			out.WriteByte('$')
			i += 2
			continue
		}
		end := strings.IndexByte(raw[i+1:], '$')
		if end < 0 {
			return "", fmt.Errorf("unterminated DASH template")
		}
		end += i + 1
		match := dashTemplateToken.FindStringSubmatch(raw[i+1 : end])
		if match == nil {
			return "", fmt.Errorf("unsupported DASH template token %q", raw[i+1:end])
		}
		if match[1] == "RepresentationID" {
			if match[2] != "" {
				return "", fmt.Errorf("formatted representation ID unsupported")
			}
			out.WriteString(url.PathEscape(rep.ID))
		} else {
			value := number
			switch match[1] {
			case "Time":
				value = stamp
			case "Bandwidth":
				value = int64(rep.Bandwidth)
			}
			width := 0
			if match[3] != "" {
				var err error
				width, err = strconv.Atoi(match[3])
				if err != nil || width > 32 {
					return "", fmt.Errorf("invalid DASH template width")
				}
			}
			out.WriteString(fmt.Sprintf("%0*d", width, value))
		}
		i = end + 1
	}
	return out.String(), nil
}

func (d *DASHDownloader) writeSegment(ctx context.Context, seg dashSegment, body []byte, w io.Writer) error {
	initKey := fmt.Sprintf("%s|%s", dashPeriodKey(seg), seg.InitURL)
	if seg.InitURL != "" && initKey != d.writtenInit {
		init, err := doGETBytesWithRetry(ctx, d.Client, seg.InitURL, d.Headers, d.Transport)
		if err != nil {
			return err
		}
		if len(init) == 0 {
			return fmt.Errorf("empty DASH initialization segment")
		}
		if _, err = w.Write(init); err != nil {
			return err
		}
		d.writtenInit = initKey
	}
	if len(body) == 0 {
		return fmt.Errorf("empty DASH segment")
	}
	_, err := w.Write(body)
	return err
}
func dashSegmentKey(seg dashSegment) string { return fmt.Sprintf("%s|%s", dashPeriodKey(seg), seg.URL) }

func dashPeriodKey(seg dashSegment) string {
	if seg.PeriodKey != "" {
		return seg.PeriodKey
	}
	return strconv.Itoa(seg.Period)
}
