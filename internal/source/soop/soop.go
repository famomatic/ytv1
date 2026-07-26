// Package soop implements a ytv1 Source for SOOP (sooplive.co.kr, formerly
// AfreecaTV). It supports both VOD (vod.sooplive.co.kr/player/<id>) and live
// (play.sooplive.co.kr/<bjid>[/<bno>]) inputs.
//
// Extraction resolves the site's HLS master playlist, then exposes its variants
// as normalized formats so the generic pipeline can pick the highest quality
// and download it (in parallel for VOD) exactly like any other HLS stream.
//
// Behavior is ported from the lavalink-go SOOP source; the HTTP endpoints,
// user-agent discipline (mobile UA for the VOD API, desktop UA for the live
// endpoints) and the auth_master_playlist / ?aid= workaround match that
// reference.
package soop

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/famomatic/ytv1/internal/formats"
	"github.com/famomatic/ytv1/internal/iox"
	"github.com/famomatic/ytv1/internal/source"
	"github.com/famomatic/ytv1/internal/types"
)

const (
	sourceName = "soop"

	// User agents: the VOD mobile API and the live PC API expect different
	// client contexts (see reference implementation).
	mobileUserAgent  = "Mozilla/5.0 (Linux; Android 10; K) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/119.0.0.0 Mobile Safari/537.36"
	desktopUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"

	originHeader = "https://play.sooplive.co.kr"

	liveArtworkBase  = "https://liveimg.sooplive.co.kr/m/"
	maxAPIRespBytes  = 4 << 20 // 4 MiB
	maxManifestBytes = 5 << 20 // 5 MiB
	requestTimeout   = 15 * time.Second
)

// API endpoints. Declared as vars (not consts) so tests can redirect them to a
// local httptest server.
var (
	vodViewAPI      = "https://api.m.sooplive.co.kr/station/video/a/view"
	liveInfoAPI     = "https://live.sooplive.com/afreeca/player_live_api.php"
	streamAssignAPI = "https://livestream-manager.sooplive.com/broad_stream_assign.html"
)

var (
	// SOOP serves both the Korean (sooplive.co.kr) and global (sooplive.com)
	// domains; both hit the same backend APIs.
	// https://vod.sooplive.co.kr/player/177615269
	// https://vod.sooplive.com/player/201581319
	vodRegex = regexp.MustCompile(`^https?://(?:www\.)?vod\.sooplive\.(?:co\.kr|com)/player/(\d+)`)
	// https://play.sooplive.co.kr/b13246/290898914 (live w/ bno)
	// https://play.sooplive.co.kr/b13246          (live, bno resolved from bjid)
	liveRegex = regexp.MustCompile(`^https?://(?:www\.)?play\.sooplive\.(?:co\.kr|com)/([a-zA-Z0-9_-]+)(?:/(\d+))?`)
)

func init() {
	source.Register(func(d source.Deps) source.Source {
		return New(d.HTTPClient)
	})
}

// Source extracts SOOP VOD and live media.
type Source struct {
	http *http.Client
}

// New returns a SOOP source using the provided HTTP client. A default client is
// created when nil so the source is usable standalone.
func New(client *http.Client) *Source {
	if client == nil {
		client = &http.Client{Timeout: requestTimeout}
	}
	return &Source{http: client}
}

// Name implements source.Source.
func (s *Source) Name() string { return sourceName }

// Matches implements source.Source.
func (s *Source) Matches(input string) bool {
	in := strings.TrimSpace(input)
	return vodRegex.MatchString(in) || liveRegex.MatchString(in)
}

// Extract implements source.Source.
func (s *Source) Extract(ctx context.Context, input string) (*source.Media, error) {
	in := strings.TrimSpace(input)

	// Headers required by the SOOP CDN for manifest/segment requests. They
	// override the pipeline's default (YouTube) media headers. The Origin must
	// match the input's domain family (.com global vs .co.kr Korean).
	origin := originHeader
	if strings.Contains(in, "sooplive.com") {
		origin = "https://play.sooplive.com"
	}
	mediaHeaders := http.Header{}
	mediaHeaders.Set("User-Agent", desktopUserAgent)
	mediaHeaders.Set("Referer", in)
	mediaHeaders.Set("Origin", origin)

	if m := vodRegex.FindStringSubmatch(in); m != nil {
		return s.extractVOD(ctx, in, m[1], mediaHeaders)
	}
	if m := liveRegex.FindStringSubmatch(in); m != nil {
		return s.extractLive(ctx, in, m[1], m[2], mediaHeaders)
	}
	return nil, fmt.Errorf("soop: unsupported input %q", input)
}

// ---- VOD ----

func (s *Source) extractVOD(ctx context.Context, input, titleNo string, mediaHeaders http.Header) (*source.Media, error) {
	form := url.Values{}
	form.Set("nTitleNo", titleNo)

	body, err := s.postForm(ctx, vodViewAPI, form, mobileUserAgent)
	if err != nil {
		return nil, fmt.Errorf("soop: vod view api: %w", err)
	}

	var res struct {
		Result int `json:"result"`
		Data   struct {
			Title      string `json:"title"`
			WriterNick string `json:"writer_nick"`
			Files      []struct {
				File     string `json:"file"`
				Duration int64  `json:"duration"`
				Snapshot string `json:"snapshot"`
			} `json:"files"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return nil, fmt.Errorf("soop: decode vod response: %w", err)
	}
	if len(res.Data.Files) == 0 {
		return nil, fmt.Errorf("soop: no files in vod response for title %s", titleNo)
	}

	// A SOOP VOD is split across one or more "files" (parts), each its own HLS
	// playlist with an independent duration. Downloading only the first part
	// (as an earlier version did) silently truncated long VODs to a few minutes
	// while still reporting success. Resolve the variants of every part and
	// stitch them into per-quality multi-part formats so the full VOD is
	// downloaded and concatenated.
	partVariants := make([][]formats.Format, 0, len(res.Data.Files))
	var totalDurationMs int64
	var snapshot string
	for i, file := range res.Data.Files {
		if strings.TrimSpace(file.File) == "" {
			continue
		}
		vs, err := s.hlsVariants(ctx, file.File, mediaHeaders)
		if err != nil {
			return nil, fmt.Errorf("soop: vod part %d: %w", i, err)
		}
		if len(vs) == 0 {
			continue
		}
		partVariants = append(partVariants, vs)
		totalDurationMs += file.Duration
		if snapshot == "" {
			snapshot = file.Snapshot
		}
	}
	if len(partVariants) == 0 {
		return nil, fmt.Errorf("soop: empty vod stream url for title %s", titleNo)
	}

	return &source.Media{
		ID:           titleNo,
		Title:        res.Data.Title,
		Author:       res.Data.WriterNick,
		DurationSec:  totalDurationMs / 1000, // API reports milliseconds
		ThumbnailURL: snapshot,
		IsLive:       false,
		WebpageURL:   input,
		Formats:      buildMultiPartFormats(partVariants),
		MediaHeaders: mediaHeaders,
	}, nil
}

// buildMultiPartFormats stitches the per-part variant lists into per-quality
// formats. The first part defines the quality set; each of its variants is
// matched to the equivalent variant in every later part (by height, falling
// back to position) so a single quality resolves to an ordered list of
// per-part media-playlist URLs.
func buildMultiPartFormats(parts [][]formats.Format) []types.FormatInfo {
	base := parts[0]
	out := make([]types.FormatInfo, 0, len(base))
	for i, v := range base {
		info := formatToInfo(v, i)
		partURLs := make([]string, 0, len(parts))
		partURLs = append(partURLs, v.URL)
		for _, p := range parts[1:] {
			if u := matchVariant(p, v, i); u != "" {
				partURLs = append(partURLs, u)
			}
		}
		info.URL = partURLs[0]
		info.Parts = partURLs
		out = append(out, info)
	}
	return out
}

// matchVariant finds the media-playlist URL in a part that corresponds to the
// target variant: same height when known, else the variant at the same
// position, else the last available. Returns "" only for an empty part.
func matchVariant(part []formats.Format, target formats.Format, idx int) string {
	if target.Height > 0 {
		for _, f := range part {
			if f.Height == target.Height {
				return f.URL
			}
		}
	}
	if idx < len(part) {
		return part[idx].URL
	}
	if len(part) > 0 {
		return part[len(part)-1].URL
	}
	return ""
}

// ---- Live ----

func (s *Source) extractLive(ctx context.Context, input, bjID, bno string, mediaHeaders http.Header) (*source.Media, error) {
	// Route A: drive the local SOOP P2P grid agent for >540p (no login). Only
	// attempted when the agent is present; any failure falls through to the CDN
	// path below, so this never blocks extraction.
	if agentEnabled() && agentProbe() {
		if media, err := s.extractLiveViaAgent(ctx, input, bjID, bno); err == nil {
			return media, nil
		}
	}
	return s.extractLiveViaCDN(ctx, input, bjID, bno, mediaHeaders)
}

// extractLiveViaAgent drives the local P2P agent for the ORIGINAL/1080p stream and
// exposes it as a single loopback MPEG-TS format the pipeline downloads.
func (s *Source) extractLiveViaAgent(ctx context.Context, input, bjID, bno string) (*source.Media, error) {
	info, err := s.fetchLiveInfo(ctx, orString(bno, bjID), bno == "")
	if err != nil {
		return nil, err
	}
	if info.BjID == "" {
		info.BjID = bjID
	}

	// The agent gateway validates a fan_ticket from /broad/a/watch; it also
	// returns authoritative gateway/relay coordinates (preferred over the
	// player_live_api grid params when present).
	au := s.fetchAU(ctx, info.BjID)
	watch, err := s.fetchWatchInfo(ctx, info.BjID, info.BNO, au)
	if err != nil {
		return nil, err
	}
	params := agentStreamParams{
		BNO:         info.BNO,
		BjID:        info.BjID,
		GUID:        newAgentGUID(),
		FanTicket:   watch.FanTicket,
		AU:          au,
		GateWayIP:   orString(watch.GateWayIP, info.GateWayIP),
		GateWayPort: orString(watch.GateWayPort, info.GateWayPort),
		CenterIP:    orString(watch.RelayIP, info.CenterIP),
		CenterPort:  orString(watch.RelayPort, info.CenterPort),
		Category:    info.Category,
	}

	streamURL, err := s.serveAgentStream(params)
	if err != nil {
		return nil, err
	}
	return buildAgentMedia(input, streamURL, info), nil
}

// extractLiveViaCDN resolves the stream through the public CDN. With login
// cookies (carried by the shared HTTP client) the token is authenticated and
// exposes higher qualities (Route C); anonymously it is capped at 540p.
func (s *Source) extractLiveViaCDN(ctx context.Context, input, bjID, bno string, mediaHeaders http.Header) (*source.Media, error) {
	// A URL without an explicit broadcast number (play.sooplive.co.kr/<bjid>)
	// needs the current BNO resolved from the streamer id first.
	if bno == "" {
		resolved, err := s.resolveLiveBNO(ctx, bjID)
		if err != nil {
			return nil, err
		}
		bno = resolved
	}

	title, author, key, err := s.fetchLiveAuth(ctx, bno)
	if err != nil {
		return nil, err
	}

	masterURL, err := s.resolveLiveStreamURL(ctx, bno, key)
	if err != nil {
		return nil, err
	}

	formats, err := s.hlsFormats(ctx, masterURL, mediaHeaders)
	if err != nil {
		return nil, err
	}

	return &source.Media{
		ID:           bno,
		Title:        title,
		Author:       author,
		IsLive:       true,
		WebpageURL:   input,
		ThumbnailURL: liveArtworkBase + bno,
		Formats:      formats,
		MediaHeaders: mediaHeaders,
	}, nil
}

// resolveLiveBNO looks up the current broadcast number for a streamer id.
func (s *Source) resolveLiveBNO(ctx context.Context, bjID string) (string, error) {
	form := url.Values{}
	form.Set("bid", bjID)
	form.Set("type", "live")

	body, err := s.postForm(ctx, liveInfoAPI, form, desktopUserAgent)
	if err != nil {
		return "", fmt.Errorf("soop: resolve bno: %w", err)
	}
	var res struct {
		Channel struct {
			BNO json.Number `json:"BNO"`
		} `json:"CHANNEL"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return "", fmt.Errorf("soop: decode bno response: %w", err)
	}
	bno := res.Channel.BNO.String()
	if bno == "" || bno == "0" {
		return "", fmt.Errorf("soop: streamer %s is not live (no broadcast number); provide a full /<bjid>/<bno> URL", bjID)
	}
	return bno, nil
}

// fetchLiveAuth calls the PC live API to obtain the stream auth key.
func (s *Source) fetchLiveAuth(ctx context.Context, bno string) (title, author, key string, err error) {
	// stream_type/quality must match the -common-master-hls broad_key used by
	// resolveLiveStreamURL: an AID minted without quality=master is rejected by
	// the CDN (the master-playlist request then hangs instead of erroring).
	form := url.Values{}
	form.Set("bno", bno)
	form.Set("type", "aid")
	form.Set("stream_type", "common")
	form.Set("quality", "master")

	body, err := s.postForm(ctx, liveInfoAPI, form, desktopUserAgent)
	if err != nil {
		return "", "", "", fmt.Errorf("soop: live auth api: %w", err)
	}
	var res struct {
		Channel struct {
			Title         string `json:"TITLE"`
			BjNick        string `json:"BJNICK"`
			ColonyContent string `json:"COLONY_CONTENT"`
			Aid           string `json:"AID"`
		} `json:"CHANNEL"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return "", "", "", fmt.Errorf("soop: decode live auth: %w", err)
	}
	key = res.Channel.ColonyContent
	if key == "" {
		key = res.Channel.Aid
	}
	if key == "" {
		return "", "", "", fmt.Errorf("soop: live stream %s unavailable (no auth key)", bno)
	}
	return res.Channel.Title, res.Channel.BjNick, key, nil
}

// resolveLiveStreamURL resolves the CDN master playlist URL for a live stream.
func (s *Source) resolveLiveStreamURL(ctx context.Context, bno, key string) (string, error) {
	// cors_origin_url is deliberately omitted: sending it steers the assign API
	// to the regional (.co.kr) CDN, which is unreachable from some networks,
	// while omitting it (as yt-dlp does) yields the global CDN.
	q := url.Values{}
	q.Set("return_type", "gcp_cdn")
	q.Set("broad_key", bno+"-common-master-hls")

	reqURL := streamAssignAPI + "?" + q.Encode()
	body, err := s.get(ctx, reqURL, desktopUserAgent)
	if err != nil {
		return "", fmt.Errorf("soop: stream assign: %w", err)
	}
	var res struct {
		ViewURL string `json:"view_url"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return "", fmt.Errorf("soop: decode stream assign: %w", err)
	}
	if res.ViewURL == "" {
		return "", fmt.Errorf("soop: no view_url for live stream %s", bno)
	}
	// The API returns auth_playlist.m3u8 which is incompatible with type=aid
	// tokens; the master playlist (all qualities) must be used instead.
	viewURL := strings.Replace(res.ViewURL, "auth_playlist.m3u8", "auth_master_playlist.m3u8", 1)
	return fmt.Sprintf("%s?aid=%s", viewURL, url.QueryEscape(key)), nil
}

// ---- HLS variant materialization ----

// hlsFormats fetches an HLS master playlist and returns its variants as
// normalized formats. If the URL is already a media playlist (no variants), a
// single passthrough format wrapping the URL is returned so the HLS downloader
// can consume it directly.
func (s *Source) hlsFormats(ctx context.Context, manifestURL string, headers http.Header) ([]types.FormatInfo, error) {
	variants, err := s.hlsVariants(ctx, manifestURL, headers)
	if err != nil {
		return nil, err
	}
	out := make([]types.FormatInfo, 0, len(variants))
	for i, f := range variants {
		out = append(out, formatToInfo(f, i))
	}
	return out, nil
}

// hlsVariants fetches an HLS master playlist and returns its variants. If the
// URL is already a media playlist (no master variants), a single passthrough
// variant wrapping the URL is returned so callers can consume it directly.
func (s *Source) hlsVariants(ctx context.Context, manifestURL string, headers http.Header) ([]formats.Format, error) {
	body, err := s.getWithHeaders(ctx, manifestURL, headers)
	if err != nil {
		return nil, fmt.Errorf("soop: fetch hls manifest: %w", err)
	}
	raw := string(body)

	variants, _ := formats.ParseHLSManifest(raw, manifestURL)
	if len(variants) > 0 {
		return variants, nil
	}

	// No master variants: treat the URL as a directly-playable media playlist.
	if !strings.Contains(raw, "#EXTM3U") {
		return nil, fmt.Errorf("soop: response is not an HLS manifest")
	}
	return []formats.Format{{
		Itag:     1,
		URL:      manifestURL,
		MimeType: `video/mp4`,
		Protocol: "hls",
		HasAudio: true,
		HasVideo: true,
	}}, nil
}

// formatToInfo converts an internal formats.Format (from ParseHLSManifest) into
// the public types.FormatInfo, tagging it as a SOOP stream and giving it a
// stable synthetic itag and a human-readable quality label.
func formatToInfo(f formats.Format, index int) types.FormatInfo {
	itag := f.Itag
	if itag == 0 {
		if f.Height > 0 {
			itag = f.Height
		} else {
			itag = index + 1
		}
	}
	qualityLabel := f.QualityLabel
	if qualityLabel == "" && f.Height > 0 {
		qualityLabel = fmt.Sprintf("%dp", f.Height)
	}
	return types.FormatInfo{
		Itag:         itag,
		URL:          f.URL,
		MimeType:     f.MimeType,
		Protocol:     "hls",
		HasAudio:     f.HasAudio,
		HasVideo:     f.HasVideo,
		Bitrate:      f.Bitrate,
		Width:        f.Width,
		Height:       f.Height,
		FPS:          f.FPS,
		Quality:      f.Quality,
		QualityLabel: qualityLabel,
		SourceClient: sourceName,
	}
}

// ---- HTTP helpers ----

func (s *Source) postForm(ctx context.Context, endpoint string, form url.Values, userAgent string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", userAgent)
	return s.do(req)
}

func (s *Source) get(ctx context.Context, endpoint, userAgent string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	return s.do(req)
}

func (s *Source) getWithHeaders(ctx context.Context, endpoint string, headers http.Header) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	for k, vals := range headers {
		for _, v := range vals {
			req.Header.Set(k, v)
		}
	}
	return s.doLimited(req, maxManifestBytes)
}

func (s *Source) do(req *http.Request) ([]byte, error) {
	return s.doLimited(req, maxAPIRespBytes)
}

func (s *Source) doLimited(req *http.Request, limit int64) ([]byte, error) {
	resp, err := s.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d from %s", resp.StatusCode, req.URL.Host)
	}
	return iox.ReadAllLimit(resp.Body, limit)
}
