package client

import (
	"mime"
	"net/url"
	"strconv"
	"strings"
)

// YTDLPDumpSingleJSON is a yt-dlp-style single video JSON payload.
type YTDLPDumpSingleJSON struct {
	RequestedFormats []YTDLPFormatEntry `json:"requested_formats,omitempty"`
	ID               string             `json:"id"`
	Title            string             `json:"title,omitempty"`
	WebpageURL       string             `json:"webpage_url,omitempty"`
	OriginalURL      string             `json:"original_url,omitempty"`
	Extractor        string             `json:"extractor,omitempty"`
	ExtractorKey     string             `json:"extractor_key,omitempty"`
	URL              string             `json:"url,omitempty"`
	Ext              string             `json:"ext,omitempty"`
	Formats          []YTDLPFormatEntry `json:"formats,omitempty"`
}

// YTDLPPlaylistInfoJSON is a yt-dlp-style playlist info JSON payload.
type YTDLPPlaylistInfoJSON struct {
	ID           string                   `json:"id"`
	Title        string                   `json:"title,omitempty"`
	WebpageURL   string                   `json:"webpage_url,omitempty"`
	OriginalURL  string                   `json:"original_url,omitempty"`
	Extractor    string                   `json:"extractor,omitempty"`
	ExtractorKey string                   `json:"extractor_key,omitempty"`
	PlaylistID   string                   `json:"playlist_id,omitempty"`
	Channel      string                   `json:"channel,omitempty"`
	ChannelID    string                   `json:"channel_id,omitempty"`
	Uploader     string                   `json:"uploader,omitempty"`
	UploaderID   string                   `json:"uploader_id,omitempty"`
	Entries      []YTDLPPlaylistItemEntry `json:"entries,omitempty"`
}

// YTDLPPlaylistItemEntry is one playlist entry in a yt-dlp-style payload.
type YTDLPPlaylistItemEntry struct {
	ID            string `json:"id"`
	Title         string `json:"title,omitempty"`
	URL           string `json:"url,omitempty"`
	WebpageURL    string `json:"webpage_url,omitempty"`
	ExtractorKey  string `json:"extractor_key,omitempty"`
	PlaylistIndex int    `json:"playlist_index,omitempty"`
	Duration      int64  `json:"duration,omitempty"`
	Uploader      string `json:"uploader,omitempty"`
}

// YTDLPFormatEntry is one format entry in a yt-dlp-style payload.
type YTDLPFormatEntry struct {
	HTTPHeaders map[string]string `json:"http_headers,omitempty"`
	FormatID    string            `json:"format_id,omitempty"`
	URL         string            `json:"url,omitempty"`
	Ext         string            `json:"ext,omitempty"`
	VCodec      string            `json:"vcodec,omitempty"`
	ACodec      string            `json:"acodec,omitempty"`
	Width       int               `json:"width,omitempty"`
	Height      int               `json:"height,omitempty"`
	FPS         int               `json:"fps,omitempty"`
	TBR         int               `json:"tbr,omitempty"`
	Protocol    string            `json:"protocol,omitempty"`
}

// BuildYTDLPPlaylistInfoJSON builds a yt-dlp-style playlist payload.
func BuildYTDLPPlaylistInfoJSON(playlist *PlaylistInfo) YTDLPPlaylistInfoJSON {
	if playlist == nil {
		return YTDLPPlaylistInfoJSON{Extractor: "youtube:playlist", ExtractorKey: "YoutubePlaylist"}
	}
	entries := make([]YTDLPPlaylistItemEntry, 0, len(playlist.Items))
	for i, item := range playlist.Items {
		index := item.PlaylistIndex
		if index <= 0 {
			index = i + 1
		}
		entries = append(entries, YTDLPPlaylistItemEntry{
			ID:            item.VideoID,
			Title:         item.Title,
			URL:           item.VideoID,
			WebpageURL:    CanonicalWatchURL(item.VideoID, item.VideoID),
			ExtractorKey:  "Youtube",
			PlaylistIndex: index,
			Duration:      item.DurationSec,
			Uploader:      item.Author,
		})
	}
	webURL := CanonicalPlaylistURL(playlist.ID)
	return YTDLPPlaylistInfoJSON{
		ID:           playlist.ID,
		Title:        playlist.Title,
		WebpageURL:   webURL,
		OriginalURL:  webURL,
		Extractor:    "youtube:playlist",
		ExtractorKey: "YoutubePlaylist",
		PlaylistID:   playlist.ID,
		Channel:      playlist.Channel,
		ChannelID:    playlist.ChannelID,
		Uploader:     playlist.Uploader,
		UploaderID:   playlist.UploaderID,
		Entries:      entries,
	}
}

// PlaylistInfoAsVideoInfo adapts playlist-level metadata for sidecar path
// rendering APIs that operate on VideoInfo-shaped template data.
func PlaylistInfoAsVideoInfo(playlist *PlaylistInfo) *VideoInfo {
	if playlist == nil {
		return &VideoInfo{}
	}
	author := firstNonEmpty(playlist.Uploader, playlist.Channel)
	return &VideoInfo{
		ID:        playlist.ID,
		Title:     playlist.Title,
		Author:    author,
		ChannelID: firstNonEmpty(playlist.ChannelID, playlist.UploaderID),
	}
}

// BuildYTDLPDumpSingleJSON builds a yt-dlp-style single video payload.
func BuildYTDLPDumpSingleJSON(input string, info *VideoInfo) YTDLPDumpSingleJSON {
	if info == nil {
		return YTDLPDumpSingleJSON{
			WebpageURL:   strings.TrimSpace(input),
			OriginalURL:  strings.TrimSpace(input),
			Extractor:    "youtube",
			ExtractorKey: "Youtube",
		}
	}
	webURL := CanonicalWatchURL(input, info.ID)
	extractor, key := "youtube", "Youtube"
	if info.SourceName != "" && info.SourceName != "youtube" {
		extractor = info.SourceName
		key = strings.ToUpper(extractor[:1]) + extractor[1:]
		webURL = firstNonEmpty(info.WebpageURL, input)
	}
	bestURL, bestExt := PickBestDirectFormatURL(info.Formats)
	formats := make([]YTDLPFormatEntry, 0, len(info.Formats))
	for _, f := range info.Formats {
		if strings.TrimSpace(f.URL) == "" {
			continue
		}
		formats = append(formats, YTDLPFormatEntry{
			FormatID: strconv.Itoa(f.Itag),
			URL:      f.URL,
			Ext:      mimeExt(f.MimeType),
			VCodec:   codecLabel(f.HasVideo),
			ACodec:   codecLabel(f.HasAudio),
			Width:    f.Width,
			Height:   f.Height,
			FPS:      f.FPS,
			TBR:      f.Bitrate / 1000,
			Protocol: f.Protocol,
		})
	}
	return YTDLPDumpSingleJSON{
		ID:           info.ID,
		Title:        info.Title,
		WebpageURL:   webURL,
		OriginalURL:  strings.TrimSpace(input),
		Extractor:    extractor,
		ExtractorKey: key,
		URL:          bestURL,
		Ext:          bestExt,
		Formats:      formats,
	}
}

// CanonicalWatchURL returns a canonical YouTube watch URL when videoID is known.
func CanonicalWatchURL(input string, videoID string) string {
	id := strings.TrimSpace(videoID)
	if id != "" {
		return "https://www.youtube.com/watch?v=" + id
	}
	return strings.TrimSpace(input)
}

// CanonicalPlaylistURL returns a canonical YouTube playlist URL.
func CanonicalPlaylistURL(playlistID string) string {
	id := strings.TrimSpace(playlistID)
	if id == "" {
		return ""
	}
	return "https://www.youtube.com/playlist?list=" + url.QueryEscape(id)
}

// PickBestDirectFormatURL returns the best direct URL from available formats.
func PickBestDirectFormatURL(formats []FormatInfo) (string, string) {
	bestIdx := -1
	for i, f := range formats {
		if strings.TrimSpace(f.URL) == "" {
			continue
		}
		if bestIdx == -1 {
			bestIdx = i
			continue
		}
		if CompareFormatQuality(f, formats[bestIdx]) > 0 {
			bestIdx = i
		}
	}
	if bestIdx < 0 {
		return "", ""
	}
	best := formats[bestIdx]
	return best.URL, mimeExt(best.MimeType)
}

// CompareFormatQuality compares formats for direct playback fallback quality.
func CompareFormatQuality(a, b FormatInfo) int {
	score := func(f FormatInfo) int64 {
		var s int64
		if f.HasAudio && f.HasVideo {
			s += 10_000_000_000
		} else if f.HasVideo {
			s += 5_000_000_000
		} else if f.HasAudio {
			s += 1_000_000_000
		}
		s += int64(f.Width*f.Height) * 1000
		s += int64(f.FPS) * 100
		s += int64(f.Bitrate)
		return s
	}
	as := score(a)
	bs := score(b)
	switch {
	case as > bs:
		return 1
	case as < bs:
		return -1
	default:
		return 0
	}
}

func codecLabel(enabled bool) string {
	if enabled {
		return "unknown"
	}
	return "none"
}

func mimeExt(mimeType string) string {
	mediaType, _, err := mime.ParseMediaType(mimeType)
	if err != nil {
		return "unknown"
	}
	parts := strings.SplitN(mediaType, "/", 2)
	if len(parts) != 2 || strings.TrimSpace(parts[1]) == "" {
		return "unknown"
	}
	return parts[1]
}
