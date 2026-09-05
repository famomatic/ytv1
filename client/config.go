package client

import (
	"context"
	"net/http"
	"time"

	"github.com/famomatic/ytv1/internal/innertube"
	"github.com/famomatic/ytv1/internal/types"
)

// Config holds configuration for the YouTube client.
type Config struct {
	// HTTPClient is the client used for making requests.
	// If nil, http.DefaultClient is used.
	HTTPClient *http.Client

	// ProxyURL is the optional proxy URL to use for requests.
	// If HTTPClient is provided, this field is ignored.
	ProxyURL string

	// SourceAddress is the optional local IP address to bind outbound requests to.
	// If HTTPClient is provided, this field is ignored.
	SourceAddress string

	// InsecureSkipVerify disables TLS certificate verification for the default HTTP client.
	// If HTTPClient is provided, this field is ignored.
	InsecureSkipVerify bool

	// CookieJar is an optional cookie jar to use for requests.
	// Applied to HTTPClient if non-nil.
	CookieJar http.CookieJar

	// PoTokenProvider is the provider for PO Tokens.
	// If nil, PO Tokens will not be injected, which may cause throttling or errors.
	PoTokenProvider innertube.PoTokenProvider
	// PoTokenCacheTTL defaults to 5 minutes; negative delegates every call to the provider.
	PoTokenCacheTTL time.Duration

	// PoTokenFetchPolicy overrides POT enforcement per streaming protocol.
	// Supported values: required|recommended|never.
	PoTokenFetchPolicy map[innertube.VideoStreamingProtocol]innertube.PoTokenFetchPolicy

	// VisitorData is the "VISITOR_INFO1_LIVE" cookie value.
	// Use this to persist sessions or emulate a specific user context.
	VisitorData string

	// PlayerJSBaseURL overrides player JS fetch host (default: https://www.youtube.com).
	PlayerJSBaseURL string

	// PlayerJSUserAgent overrides player JS fetch User-Agent.
	// If empty, package fallback is used.
	PlayerJSUserAgent string

	// PlayerJSHeaders are additional headers for player JS fetches.
	PlayerJSHeaders http.Header

	// PlayerJSPreferredLocale controls canonical locale for player JS fetch path.
	// Default is "en_US". Fetch falls back to the original watch-page locale path.
	PlayerJSPreferredLocale string

	// ClientOverrides sets Innertube client trial order (e.g. "web", "ios", "android").
	// If empty, package defaults are used.
	ClientOverrides []string

	// AppendFallbackOnClientOverrides keeps fallback-client auto append enabled even
	// when ClientOverrides is explicitly provided.
	// Default is false: explicit override mode disables auto fallback append.
	AppendFallbackOnClientOverrides bool

	// DisableDynamicAPIKeyResolution disables watch-page ytcfg API key extraction.
	// Default is false (dynamic resolution enabled).
	DisableDynamicAPIKeyResolution bool

	// UseAdPlaybackContext enables `playbackContext.adPlaybackContext.pyv=true`
	// when the selected client supports ad playback context.
	UseAdPlaybackContext bool

	// ClientHedgeDelay delays lower-priority client requests during extraction.
	// Zero means immediate parallel start for all selected clients.
	ClientHedgeDelay time.Duration

	// RequestHeaders are applied to package-level outgoing HTTP requests.
	RequestHeaders http.Header

	// RequestTimeout applies a package-level timeout to outgoing operations.
	// If the caller context already has a shorter deadline, that deadline is preserved.
	// Zero means no additional timeout.
	RequestTimeout time.Duration

	// ClientSkip excludes specific Innertube clients from selection.
	ClientSkip []string

	// DisableFallbackClients disables automatic fallback-client append behavior.
	DisableFallbackClients bool

	// MetadataTransport configures retry/backoff for Innertube metadata requests.
	MetadataTransport MetadataTransportConfig

	// MP3Transcoder handles optional stream->mp3 conversion in Download(mode=mp3).
	// If nil, mp3 mode returns ErrMP3TranscoderNotConfigured.
	MP3Transcoder MP3Transcoder

	// DownloadTransport configures retry/backoff behavior for stream downloads.
	DownloadTransport DownloadTransportConfig

	// Muxer handles optional video+audio merging in Download(options.Merge=true).
	// If nil or unavailable, a selected merge returns ErrMuxerUnavailable.
	// Select a single muxed format explicitly to avoid requiring a muxer.
	Muxer Muxer

	// Logger receives non-fatal package warnings (optional).
	// If nil, warnings are suppressed.
	Logger Logger

	// OnExtractionEvent receives extraction lifecycle events (optional).
	// If nil, extraction events are suppressed.
	//
	// Concurrency contract: this callback may be invoked from multiple
	// goroutines concurrently (extraction races across client profiles).
	// Implementations must be goroutine-safe; the bundled CLI printer is.
	// Callbacks run synchronously and must not call extraction on this Client.
	OnExtractionEvent func(ExtractionEvent)

	// OnDownloadEvent receives download lifecycle events (optional).
	// If nil, download events are suppressed.
	//
	// Concurrency contract: this callback may be invoked from multiple
	// goroutines concurrently (parallel chunk/segment downloads). It must be
	// goroutine-safe; the bundled CLI printer is.
	// Callbacks run synchronously: avoid blocking and re-entering this operation.
	OnDownloadEvent func(DownloadEvent)

	// OnDownloadProgress receives byte-level direct media download progress (optional).
	// If nil, progress events are suppressed.
	//
	// Concurrency contract: this callback may be invoked from multiple
	// goroutines concurrently (parallel chunk downloads). It must be
	// goroutine-safe; the bundled CLI printer is.
	// Callbacks run synchronously: avoid blocking and re-entering this operation.
	OnDownloadProgress func(DownloadProgressEvent)

	// KeepIntermediateFiles keeps intermediate video/audio files after merge download.
	// Default is false (remove intermediates on successful/failed merge attempt).
	KeepIntermediateFiles bool

	// SessionCacheTTL expires in-memory video sessions after this duration.
	// When unset (zero), the package defaults to 6 hours to match YouTube's
	// googlevideo URL lifetime. Set to a positive value to override. Set to
	// a negative value (e.g. -1) to disable local TTL expiration; URL-expire
	// validation still applies regardless.
	SessionCacheTTL time.Duration

	// SessionCacheMaxEntries bounds in-memory video session count (LRU eviction).
	// Zero uses 256 entries; negative means unbounded.
	SessionCacheMaxEntries int

	// SubtitlePolicy controls default subtitle track selection behavior.
	SubtitlePolicy SubtitlePolicy

	// PlaylistContinuationMaxRequests bounds continuation browse requests in GetPlaylist.
	// Zero or negative uses package default.
	PlaylistContinuationMaxRequests int
}

// SubtitlePolicy controls subtitle selection when language is not explicitly specified.
type SubtitlePolicy struct {
	PreferredLanguageCode string
	FallbackLanguageCodes []string
	PreferAutoGenerated   bool
}

// Muxer defines the interface for media muxing operations.
type Muxer interface {
	Available() bool
	Merge(ctx context.Context, videoPath, audioPath, outputPath string, meta types.Metadata) error
}

// DownloadTransportConfig controls retry/backoff behavior for direct stream downloads.
type DownloadTransportConfig struct {
	MaxBufferedBytes            int64 // segmented downloads: default 512 MiB worker buffer budget
	MaxSegmentBytes             int64 // segmented downloads: default 100 MiB per fragment
	MaxRetries                  int
	InitialBackoff              time.Duration
	MaxBackoff                  time.Duration
	RetryStatusCodes            []int
	EnableChunked               bool
	ChunkSize                   int64
	MaxConcurrency              int
	SkipUnavailableFragments    bool
	MaxSkippedFragments         int
	RateLimitBytesPerSecond     int64
	ThrottledRateBytesPerSecond int64
	ThrottledRateMinDuration    time.Duration
	FileAccessRetries           int
	FileAccessBackoff           time.Duration
}

// MetadataTransportConfig controls retry/backoff for Innertube player metadata requests.
type MetadataTransportConfig struct {
	MaxRetries       int
	InitialBackoff   time.Duration
	MaxBackoff       time.Duration
	RetryStatusCodes []int
}

// ToInnerTubeConfig converts package-level Config into innertube.Config.
func (c Config) ToInnerTubeConfig() innertube.Config {
	disableFallback := c.DisableFallbackClients
	if !disableFallback && len(c.ClientOverrides) > 0 && !c.AppendFallbackOnClientOverrides {
		disableFallback = true
	}

	var extractionHandler innertube.ExtractionEventHandler
	if c.OnExtractionEvent != nil {
		extractionHandler = func(evt innertube.ExtractionEvent) {
			c.OnExtractionEvent(ExtractionEvent{
				Stage:  evt.Stage,
				Phase:  evt.Phase,
				Client: evt.Client,
				Detail: evt.Detail,
			})
		}
	}

	return innertube.Config{
		HTTPClient:                    c.HTTPClient,
		ProxyURL:                      c.ProxyURL,
		PoTokenProvider:               c.PoTokenProvider,
		PoTokenFetchPolicy:            c.PoTokenFetchPolicy,
		VisitorData:                   c.VisitorData,
		PlayerJSBaseURL:               c.PlayerJSBaseURL,
		PlayerJSUserAgent:             c.PlayerJSUserAgent,
		PlayerJSHeaders:               c.PlayerJSHeaders,
		PlayerJSPreferredLocale:       c.PlayerJSPreferredLocale,
		ClientOverrides:               c.ClientOverrides,
		ClientSkip:                    c.ClientSkip,
		RequestHeaders:                c.RequestHeaders,
		RequestTimeout:                c.RequestTimeout,
		DisableFallbackClients:        disableFallback,
		MetadataTransport:             innertube.MetadataTransportConfig(c.MetadataTransport),
		EnableDynamicAPIKeyResolution: !c.DisableDynamicAPIKeyResolution,
		UseAdPlaybackContext:          c.UseAdPlaybackContext,
		ClientHedgeDelay:              c.ClientHedgeDelay,
		OnExtractionEvent:             extractionHandler,
	}
}
