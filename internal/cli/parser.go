package cli

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/famomatic/ytv1/client"
	"github.com/famomatic/ytv1/internal/cookies"
	"github.com/famomatic/ytv1/internal/muxer"
)

// Options holds all command-line options.
type Options struct {
	// Input
	URLs []string

	// General
	Help    bool
	Version bool

	// Network
	ProxyURL    string
	CookiesFile string // --cookies

	// Video Selection
	FormatSelector string // -f, --format
	ListFormats    bool   // -F, --list-formats

	// Download / Filesystem
	OutputTemplate  string // -o, --output
	DownloadArchive string // --download-archive
	SkipDownload    bool   // --skip-download
	NoContinue      bool   // --no-continue
	AbortOnError    bool   // --abort-on-error
	DownloadRetries int    // --retries
	RetrySleepMS    int    // --retry-sleep-ms
	WriteSubs       bool   // --write-subs
	WriteAutoSubs   bool   // --write-auto-subs
	SubLangs        string // --sub-lang

	// Post-processing
	MergeOutput bool // --merge-output-format (implied true in ytv1 currently, but we can make it explicit or toggle)

	// Advanced / Debug
	ClientsOverrides    string // --clients
	OverrideAppend      bool   // --override-append-fallback
	OverrideDiagnostics bool   // --override-diagnostics
	VisitorData         string // --visitor-data
	PoToken             string // --po-token
	FFmpegLocation      string // --ffmpeg-location
	ClientHedgeMS       int    // --client-hedge-ms

	// Verbosity / Debug
	Verbose         bool
	PrintJSON       bool // --print-json
	PlayerJSURLOnly bool // --playerjs (legacy/debug)
}

// ParseFlags parses command-line arguments into Options.
func ParseFlags() Options {
	opts := Options{}

	// Helper to bind multiple flags to one variable
	var formatShort, formatLong string
	var outputShort, outputLong string
	var listFormatsShort, listFormatsLong bool

	flag.StringVar(&formatShort, "f", "best", "Video format code")
	flag.StringVar(&formatLong, "format", "best", "Video format code")

	flag.StringVar(&outputShort, "o", "", "Output filename template")
	flag.StringVar(&outputLong, "output", "", "Output filename template")

	flag.BoolVar(&listFormatsShort, "F", false, "List available formats")
	flag.BoolVar(&listFormatsLong, "list-formats", false, "List available formats")

	flag.StringVar(&opts.ProxyURL, "proxy", "", "Use the specified HTTP/HTTPS/SOCKS proxy")
	flag.StringVar(&opts.CookiesFile, "cookies", "", "Netscape formatted cookies file")

	flag.BoolVar(&opts.SkipDownload, "skip-download", false, "Do not download the video")
	flag.StringVar(&opts.DownloadArchive, "download-archive", "", "File to store downloaded video IDs for idempotent reruns")
	flag.BoolVar(&opts.NoContinue, "no-continue", false, "Do not resume partially downloaded files")
	flag.BoolVar(&opts.AbortOnError, "abort-on-error", false, "Abort batch processing on first error")
	flag.IntVar(&opts.DownloadRetries, "retries", -1, "Download retry count override (-1 keeps defaults)")
	flag.IntVar(&opts.RetrySleepMS, "retry-sleep-ms", -1, "Download retry initial backoff in milliseconds (-1 keeps defaults)")
	flag.BoolVar(&opts.WriteSubs, "write-subs", false, "Write subtitle file")
	flag.BoolVar(&opts.WriteAutoSubs, "write-auto-subs", false, "Write automatically generated subtitle file")
	flag.StringVar(&opts.SubLangs, "sub-lang", "en", "Languages of the subtitles to download (optional) separated by commas")

	flag.BoolVar(&opts.PrintJSON, "print-json", false, "Be quiet and print the video information as JSON")
	flag.BoolVar(&opts.PlayerJSURLOnly, "playerjs", false, "Print player base.js URL only (debug)")

	flag.BoolVar(&opts.Verbose, "verbose", false, "Print various debugging information")

	// Advanced / Debug flags from original main.go
	flag.StringVar(&opts.ClientsOverrides, "clients", "", "Comma-separated Innertube client order override")
	flag.BoolVar(&opts.OverrideAppend, "override-append-fallback", false, "When -clients is set, keep fallback auto-append enabled")
	flag.BoolVar(&opts.OverrideDiagnostics, "override-diagnostics", false, "Print per-client attempt diagnostics on metadata failure")
	flag.StringVar(&opts.VisitorData, "visitor-data", "", "VISITOR_INFO1_LIVE value override")
	flag.StringVar(&opts.PoToken, "po-token", "", "Static PO token override (applied to POT-required requests)")
	flag.StringVar(&opts.FFmpegLocation, "ffmpeg-location", "", "Path to ffmpeg binary")
	flag.IntVar(&opts.ClientHedgeMS, "client-hedge-ms", 350, "Delay(ms) before launching lower-priority fallback clients")

	// Custom usage
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: ytv1 [OPTIONS] URL [URL...]\n\n")
		fmt.Fprintln(os.Stderr, "Options:")
		flag.PrintDefaults()
	}

	flag.Parse()

	// Consolidate aliases
	opts.FormatSelector = pickValue(formatShort, formatLong, "best")
	opts.OutputTemplate = pickValue(outputShort, outputLong, "")
	opts.ListFormats = listFormatsShort || listFormatsLong

	opts.URLs = flag.Args()
	return opts
}

func pickValue(v1, v2, def string) string {
	if v1 != def {
		return v1
	}
	if v2 != def {
		return v2
	}
	return def
}

// ToClientConfig converts Options to client.Config.
// ToClientConfig converts Options to client.Config.
func ToClientConfig(opts Options) (client.Config, error) {
	cfg := client.Config{
		ProxyURL:    opts.ProxyURL,
		VisitorData: opts.VisitorData,
	}
	langs := parseSubLangs(opts.SubLangs)
	if len(langs) > 0 {
		cfg.SubtitlePolicy.PreferredLanguageCode = langs[0]
		if len(langs) > 1 {
			cfg.SubtitlePolicy.FallbackLanguageCodes = append([]string(nil), langs[1:]...)
		}
	}
	cfg.SubtitlePolicy.PreferAutoGenerated = opts.WriteAutoSubs && !opts.WriteSubs
	if opts.ClientHedgeMS > 0 {
		cfg.ClientHedgeDelay = time.Duration(opts.ClientHedgeMS) * time.Millisecond
	}
	if strings.TrimSpace(opts.PoToken) != "" {
		cfg.PoTokenProvider = staticPoTokenProvider(strings.TrimSpace(opts.PoToken))
	}
	if opts.DownloadRetries >= 0 {
		cfg.DownloadTransport.MaxRetries = opts.DownloadRetries
		cfg.MetadataTransport.MaxRetries = opts.DownloadRetries
	}
	if opts.RetrySleepMS >= 0 {
		backoff := time.Duration(opts.RetrySleepMS) * time.Millisecond
		cfg.DownloadTransport.InitialBackoff = backoff
		cfg.MetadataTransport.InitialBackoff = backoff
	}

	// Muxer check (ffmpeg)
	cfg.Muxer = muxer.NewFFmpegMuxer(opts.FFmpegLocation)

	if opts.ClientsOverrides != "" {
		cfg.ClientOverrides = strings.Split(opts.ClientsOverrides, ",")
		// Trim spaces
		for i := range cfg.ClientOverrides {
			cfg.ClientOverrides[i] = strings.TrimSpace(cfg.ClientOverrides[i])
		}

		cfg.AppendFallbackOnClientOverrides = opts.OverrideAppend
		if !opts.OverrideAppend {
			cfg.DisableFallbackClients = true
		}
	}

	// Load Cookies
	if opts.CookiesFile != "" {
		f, err := os.Open(opts.CookiesFile)
		if err != nil {
			return cfg, fmt.Errorf("failed to open cookies file: %w", err)
		}
		defer f.Close()

		cookiesList, err := cookies.ParseNetscape(f)
		if err != nil {
			return cfg, fmt.Errorf("failed to parse cookies file: %w", err)
		}

		jar, err := cookiejar.New(nil)
		if err != nil {
			return cfg, fmt.Errorf("failed to create cookie jar: %w", err)
		}

		// Map by domain
		domainCookies := make(map[string][]*http.Cookie)
		for _, c := range cookiesList {
			domainCookies[c.Domain] = append(domainCookies[c.Domain], c)
		}

		for domain, cs := range domainCookies {
			// Construct a fake URL for the domain
			scheme := "http"
			// Check if any cookie is secure
			for _, c := range cs {
				if c.Secure {
					scheme = "https"
					break
				}
			}
			host := strings.TrimPrefix(domain, ".")
			u := &url.URL{Scheme: scheme, Host: host}
			jar.SetCookies(u, cs)
		}

		cfg.CookieJar = jar
	}

	return cfg, nil
}

func parseSubLangs(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		lang := strings.ToLower(strings.TrimSpace(part))
		if lang == "" {
			continue
		}
		if _, ok := seen[lang]; ok {
			continue
		}
		seen[lang] = struct{}{}
		out = append(out, lang)
	}
	return out
}

type staticPoTokenProvider string

func (p staticPoTokenProvider) GetToken(_ context.Context, _ string) (string, error) {
	return string(p), nil
}
