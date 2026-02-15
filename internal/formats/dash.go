package formats

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// DASHManifest represents a parsed DASH manifest.
type DASHManifest struct {
	RawContent string
	Formats    []Format
}

func FetchDASHManifest(ctx context.Context, client *http.Client, url string) (*DASHManifest, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch DASH manifest: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("bad status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read body: %w", err)
	}

	raw := string(body)
	parsedFormats, _ := ParseDASHManifest(raw, url)

	return &DASHManifest{
		RawContent: raw,
		Formats:    parsedFormats,
	}, nil
}

type dashMPD struct {
	XMLName xml.Name      `xml:"MPD"`
	BaseURL string        `xml:"BaseURL"`
	Periods []dashPeriod  `xml:"Period"`
}

type dashPeriod struct {
	AdaptationSets []dashAdaptationSet `xml:"AdaptationSet"`
}

type dashAdaptationSet struct {
	MimeType string               `xml:"mimeType,attr"`
	Codecs   string               `xml:"codecs,attr"`
	Rep      []dashRepresentation `xml:"Representation"`
}

type dashRepresentation struct {
	ID               string `xml:"id,attr"`
	Bandwidth        int    `xml:"bandwidth,attr"`
	Width            int    `xml:"width,attr"`
	Height           int    `xml:"height,attr"`
	FrameRate        string `xml:"frameRate,attr"`
	MimeType         string `xml:"mimeType,attr"`
	Codecs           string `xml:"codecs,attr"`
	AudioSamplingRate string `xml:"audioSamplingRate,attr"`
	BaseURL          string `xml:"BaseURL"`
}

// ParseDASHManifest parses DASH MPD into normalized formats.
func ParseDASHManifest(raw, manifestURL string) ([]Format, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var mpd dashMPD
	if err := xml.Unmarshal([]byte(raw), &mpd); err != nil {
		return nil, err
	}

	formats := make([]Format, 0, 16)
	base := strings.TrimSpace(mpd.BaseURL)
	for _, period := range mpd.Periods {
		for _, adp := range period.AdaptationSets {
			for _, rep := range adp.Rep {
				uri := strings.TrimSpace(rep.BaseURL)
				if uri == "" {
					continue
				}
				absURL := resolveManifestRefURL(manifestURL, base, uri)
				mimeType := firstNonEmpty(strings.TrimSpace(rep.MimeType), strings.TrimSpace(adp.MimeType))
				codecsRaw := firstNonEmpty(strings.TrimSpace(rep.Codecs), strings.TrimSpace(adp.Codecs))
				if mimeType != "" && codecsRaw != "" && !strings.Contains(mimeType, "codecs=") {
					mimeType = mimeType + `; codecs="` + codecsRaw + `"`
				}
				container, codecs := parseMimeDetails(mimeType)

				f := Format{
					Itag:            parseInt(rep.ID),
					URL:             absURL,
					MimeType:        mimeType,
					Container:       container,
					Codecs:          codecs,
					Bitrate:         rep.Bandwidth,
					Width:           rep.Width,
					Height:          rep.Height,
					FPS:             parseFrameRate(rep.FrameRate),
					AudioSampleRate: parseInt(rep.AudioSamplingRate),
					Protocol:        "dash",
				}
				f.HasAudio, f.HasVideo = deriveMediaFlags(f, true)
				formats = append(formats, f)
			}
		}
	}
	return formats, nil
}

func parseFrameRate(raw string) int {
	s := strings.TrimSpace(raw)
	if s == "" {
		return 0
	}
	if strings.Contains(s, "/") {
		parts := strings.SplitN(s, "/", 2)
		if len(parts) != 2 {
			return 0
		}
		num, err1 := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
		den, err2 := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
		if err1 != nil || err2 != nil || den == 0 {
			return 0
		}
		return int(num / den)
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return int(v)
}

func resolveManifestRefURL(manifestURL, manifestBase, ref string) string {
	candidates := []string{ref}
	if manifestBase != "" {
		candidates = append(candidates, manifestBase+ref)
	}
	for _, c := range candidates {
		u, err := url.Parse(c)
		if err == nil && u.IsAbs() {
			return u.String()
		}
	}
	base, err := url.Parse(manifestURL)
	if err != nil {
		return ref
	}
	if manifestBase != "" {
		if base2, err := base.Parse(manifestBase); err == nil {
			base = base2
		}
	}
	if out, err := base.Parse(ref); err == nil {
		return out.String()
	}
	return ref
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
