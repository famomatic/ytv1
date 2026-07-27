package cli

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/famomatic/ytv1/client"
	"github.com/famomatic/ytv1/internal/cookies"
	"github.com/famomatic/ytv1/internal/muxer"
)

// Options holds all command-line options.
type Options struct {
	// Input
	URLs      []string
	BatchFile string // -a, --batch-file

	// General
	Help    bool
	Version bool

	// Network
	ProxyURL           string
	CookiesFile        string        // --cookies
	SocketTimeout      time.Duration // --socket-timeout
	SourceAddress      string        // --source-address, -4, -6
	NoCheckCertificate bool          // --no-check-certificates
	UserAgent          string        // --user-agent
	Referer            string        // --referer
	AddHeaders         []string      // --add-headers

	// Video Selection
	FormatSelector string // -f, --format
	ListFormats    bool   // -F, --list-formats

	// Download / Filesystem
	OutputTemplate           string        // -o, --output
	OutputPathDir            string        // -P, --paths
	OutputUseID              bool          // --id
	RestrictFilenames        bool          // --restrict-filenames
	TrimFilenames            int           // --trim-filenames
	NoOverwrites             bool          // --no-overwrites
	NoPostOverwrites         bool          // --no-post-overwrites
	DownloadArchive          string        // --download-archive
	SkipDownload             bool          // --skip-download
	ForceWriteArchive        bool          // --force-write-archive
	NoWarnings               bool          // --no-warnings
	NoContinue               bool          // --no-continue
	NoPart                   bool          // --no-part
	UpdateMTime              bool          // --mtime
	KeepVideo                bool          // -k, --keep-video
	BreakOnExisting          bool          // --break-on-existing
	AbortOnError             bool          // --abort-on-error
	IgnoreErrors             bool          // -i, --ignore-errors
	DownloadRetries          int           // --retries
	FileAccessRetries        int           // --file-access-retries
	FileAccessRetriesSet     bool          // true when --file-access-retries was valid
	ExtractorRetries         int           // --extractor-retries
	ExtractorRetriesSet      bool          // true when --extractor-retries was valid
	RetrySleepMS             int           // --retry-sleep-ms
	FragmentRetries          int           // --fragment-retries
	FragmentRetriesSet       bool          // true when --fragment-retries was valid
	HTTPRetrySleep           time.Duration // --retry-sleep http/default
	HTTPRetrySleepSet        bool          // true when --retry-sleep http/default was valid
	FragmentRetrySleep       time.Duration // --retry-sleep fragment
	FragmentRetrySleepSet    bool          // true when --retry-sleep fragment was valid
	ExtractorRetrySleep      time.Duration // --retry-sleep extractor
	ExtractorRetrySleepSet   bool          // true when --retry-sleep extractor was valid
	LimitRateBytesPerSecond  int64         // -r, --limit-rate
	BufferSizeBytes          int64         // --buffer-size
	HTTPChunkSizeBytes       int64         // --http-chunk-size
	NoResizeBuffer           bool          // --no-resize-buffer
	ConcurrentFragments      int           // -N, --concurrent-fragments
	SkipUnavailableFragments bool          // --skip-unavailable-fragments
	SleepRequests            time.Duration // --sleep-requests
	SleepInterval            time.Duration // --sleep-interval
	MaxSleepInterval         time.Duration // --max-sleep-interval
	SleepSubtitles           time.Duration // --sleep-subtitles
	MaxDownloads             int           // --max-downloads
	WriteSubs                bool          // --write-subs
	WriteAutoSubs            bool          // --write-auto-subs
	ListSubs                 bool          // --list-subs
	AllSubs                  bool          // --all-subs
	WriteInfoJSON            bool          // --write-info-json
	WriteDescription         bool          // --write-description
	NoWritePlaylistMetafiles bool          // --no-write-playlist-metafiles
	WriteThumbnail           bool          // --write-thumbnail
	WriteURLLink             bool          // --write-link, --write-url-link
	WriteWeblocLink          bool          // --write-webloc-link
	WriteDesktopLink         bool          // --write-desktop-link
	GetThumbnail             bool          // --get-thumbnail
	SubLangs                 string        // --sub-lang
	SubFormat                string        // --sub-format
	FlatPlaylist             bool          // --flat-playlist
	NoPlaylist               bool          // --no-playlist
	YesPlaylist              bool          // --yes-playlist
	PlaylistItems            string        // --playlist-items
	PlaylistStart            int           // --playlist-start
	PlaylistEnd              int           // --playlist-end
	PlaylistReverse          bool          // --playlist-reverse
	PlaylistRandom           bool          // --playlist-random
	SkipPlaylistAfterErrors  int           // --skip-playlist-after-errors

	// Post-processing
	MergeOutput       bool     // --merge-output-format (implied true in ytv1 currently, but we can make it explicit or toggle)
	MergeOutputFormat string   // --merge-output-format
	RemuxVideo        string   // --remux-video
	ExtractAudio      bool     // -x, --extract-audio
	AudioFormat       string   // --audio-format
	AudioQuality      string   // --audio-quality
	EmbedMetadata     bool     // --embed-metadata, --add-metadata
	PostprocessorArgs []string // --postprocessor-args, --ppa

	// Advanced / Debug
	ClientsOverrides    string // --clients
	OverrideAppend      bool   // --override-append-fallback
	OverrideDiagnostics bool   // --override-diagnostics
	VisitorData         string // --visitor-data
	PoToken             string // --po-token
	FFmpegLocation      string // --ffmpeg-location
	ClientHedgeMS       int    // --client-hedge-ms

	// Verbosity / Debug
	Verbose          bool
	Quiet            bool // -q, --quiet
	NoProgress       bool // --no-progress
	Progress         bool // --progress
	NewlineProgress  bool // --newline
	PrintJSON        bool // --print-json
	DumpSingleJSON   bool // --dump-single-json
	GetTitle         bool // -e, --get-title
	GetID            bool // --get-id
	GetDescription   bool // --get-description
	GetDuration      bool // --get-duration
	GetFormat        bool // --get-format
	GetFilename      bool // --get-filename
	GetURL           bool // --get-url
	GetMetadataOrder []string
	PrintTemplates   []string // -O, --print
	PrintToFile      []PrintToFileSpec
	PlayerJSURLOnly  bool // --playerjs (legacy/debug)
}

type PrintToFileSpec struct {
	Template string
	File     string
}

// ParseFlags parses command-line arguments into Options.
func ParseFlags() Options {
	opts := Options{}
	rawArgs := append([]string(nil), os.Args[1:]...)
	parseArgs := normalizePrintToFileArgs(rawArgs)

	// Helper to bind multiple flags to one variable
	var formatShort, formatLong string
	var outputShort, outputLong string
	var listFormatsShort, listFormatsLong bool

	flag.StringVar(&formatShort, "f", "best", "Video format code")
	flag.StringVar(&formatLong, "format", "best", "Video format code")

	flag.StringVar(&opts.BatchFile, "batch-file", "", "File containing URLs to download, one URL per line")
	flag.StringVar(&opts.BatchFile, "a", "", "Alias of --batch-file (yt-dlp compatibility)")
	noBatchFile := false
	flag.BoolVar(&noBatchFile, "no-batch-file", false, "Do not read URLs from a batch file")

	flag.StringVar(&outputShort, "o", "", "Output filename template")
	flag.StringVar(&outputLong, "output", "", "Output filename template")
	var pathsShort, pathsLong string
	flag.StringVar(&pathsShort, "P", "", "Output path directory")
	flag.StringVar(&pathsLong, "paths", "", "Output path directory")
	flag.BoolVar(&opts.OutputUseID, "id", false, "Use only video ID in file name")
	flag.BoolVar(&opts.RestrictFilenames, "restrict-filenames", false, "Restrict filenames to ASCII-safe characters")
	flag.BoolVar(&opts.RestrictFilenames, "no-restrict-filenames", false, "Do not restrict filenames")
	flag.IntVar(&opts.TrimFilenames, "trim-filenames", 0, "Limit filename basename length")
	flag.IntVar(&opts.TrimFilenames, "trim-file-names", 0, "Alias of --trim-filenames")
	flag.BoolVar(&opts.NoOverwrites, "no-overwrites", false, "Do not overwrite existing files")
	flag.BoolVar(&opts.NoOverwrites, "w", false, "Alias of --no-overwrites")
	flag.BoolVar(&opts.NoOverwrites, "force-overwrites", false, "Overwrite existing files")
	flag.BoolVar(&opts.NoOverwrites, "yes-overwrites", false, "Alias of --force-overwrites")
	flag.BoolVar(&opts.NoOverwrites, "no-force-overwrites", false, "Use default overwrite policy")
	flag.BoolVar(&opts.NoPostOverwrites, "post-overwrites", false, "Overwrite post-processed files")
	flag.BoolVar(&opts.NoPostOverwrites, "no-post-overwrites", false, "Do not overwrite post-processed files")

	flag.BoolVar(&listFormatsShort, "F", false, "List available formats")
	flag.BoolVar(&listFormatsLong, "list-formats", false, "List available formats")

	flag.StringVar(&opts.ProxyURL, "proxy", "", "Use the specified HTTP/HTTPS/SOCKS proxy")
	flag.StringVar(&opts.CookiesFile, "cookies", "", "Netscape formatted cookies file")
	flag.StringVar(&opts.UserAgent, "user-agent", "", "User-Agent header value")
	flag.StringVar(&opts.Referer, "referer", "", "Referer header value")
	addHeaders := multiStringFlag{}
	flag.Var(&addHeaders, "add-headers", "Additional request header FIELD:VALUE")
	var socketTimeoutRaw string
	flag.StringVar(&socketTimeoutRaw, "socket-timeout", "", "Time to wait before giving up, in seconds")
	flag.BoolVar(&opts.NoCheckCertificate, "no-check-certificates", false, "Suppress HTTPS certificate validation")
	var sourceAddressRaw string
	var forceIPv4Short, forceIPv4Long, forceIPv6Short, forceIPv6Long bool
	flag.StringVar(&sourceAddressRaw, "source-address", "", "Client-side IP address to bind to")
	flag.BoolVar(&forceIPv4Long, "force-ipv4", false, "Make all connections via IPv4")
	flag.BoolVar(&forceIPv4Short, "4", false, "Alias of --force-ipv4")
	flag.BoolVar(&forceIPv6Long, "force-ipv6", false, "Make all connections via IPv6")
	flag.BoolVar(&forceIPv6Short, "6", false, "Alias of --force-ipv6")

	flag.BoolVar(&opts.SkipDownload, "skip-download", false, "Do not download the video")
	flag.BoolVar(&opts.SkipDownload, "no-download", false, "Alias of --skip-download (yt-dlp compatibility)")
	flag.BoolVar(&opts.SkipDownload, "simulate", false, "Alias of --skip-download (yt-dlp compatibility)")
	flag.BoolVar(&opts.SkipDownload, "s", false, "Alias of --simulate (yt-dlp compatibility)")
	flag.BoolVar(&opts.SkipDownload, "no-simulate", false, "Do not simulate, download normally (yt-dlp compatibility)")
	flag.BoolVar(&opts.ForceWriteArchive, "force-write-archive", false, "Write download archive entries even for successful simulated/no-download runs")
	flag.BoolVar(&opts.ForceWriteArchive, "force-write-download-archive", false, "Alias of --force-write-archive")
	flag.BoolVar(&opts.ForceWriteArchive, "force-download-archive", false, "Alias of --force-write-archive")
	flag.BoolVar(&opts.NoWarnings, "no-warnings", false, "Suppress non-critical warning messages")
	flag.StringVar(&opts.DownloadArchive, "download-archive", "", "File to store downloaded video IDs for idempotent reruns")
	noDownloadArchive := false
	flag.BoolVar(&noDownloadArchive, "no-download-archive", false, "Do not use a download archive")
	flag.BoolVar(&opts.NoContinue, "no-continue", false, "Do not resume partially downloaded files")
	continueDownloads := true
	flag.BoolVar(&continueDownloads, "continue", true, "Resume partially downloaded files (yt-dlp compatibility alias)")
	flag.BoolVar(&opts.NoPart, "no-part", false, "Do not use temporary .part files")
	flag.BoolVar(&opts.NoPart, "part", false, "Use temporary .part files")
	flag.BoolVar(&opts.UpdateMTime, "mtime", false, "Set output file modification time from media metadata")
	flag.BoolVar(&opts.UpdateMTime, "no-mtime", false, "Do not set output file modification time")
	flag.BoolVar(&opts.KeepVideo, "keep-video", false, "Keep intermediate video/audio files after post-processing")
	flag.BoolVar(&opts.KeepVideo, "k", false, "Alias of --keep-video")
	flag.BoolVar(&opts.KeepVideo, "no-keep-video", false, "Delete intermediate video/audio files after post-processing")
	flag.BoolVar(&opts.BreakOnExisting, "break-on-existing", false, "Stop processing when a video is already in the download archive")
	flag.BoolVar(&opts.BreakOnExisting, "no-break-on-existing", false, "Continue after download archive hits")
	flag.BoolVar(&opts.AbortOnError, "abort-on-error", false, "Abort batch processing on first error")
	flag.BoolVar(&opts.AbortOnError, "no-ignore-errors", false, "Abort on download error (yt-dlp compatibility alias)")
	flag.BoolVar(&opts.IgnoreErrors, "ignore-errors", false, "Continue on download errors (yt-dlp compatibility alias)")
	flag.BoolVar(&opts.IgnoreErrors, "i", false, "Alias of --ignore-errors (yt-dlp compatibility)")
	flag.IntVar(&opts.DownloadRetries, "retries", -1, "Download retry count override (-1 keeps defaults)")
	var fileAccessRetriesRaw string
	flag.StringVar(&fileAccessRetriesRaw, "file-access-retries", "", "File access retry count override")
	var extractorRetriesRaw string
	flag.StringVar(&extractorRetriesRaw, "extractor-retries", "", "Extractor retry count override")
	flag.IntVar(&opts.RetrySleepMS, "retry-sleep-ms", -1, "Download retry initial backoff in milliseconds (-1 keeps defaults)")
	var retrySleepRaw string
	flag.StringVar(&retrySleepRaw, "retry-sleep", "", "Retry sleep expression")
	var fragmentRetriesRaw string
	flag.StringVar(&fragmentRetriesRaw, "fragment-retries", "", "Fragment retry count override")
	var limitRateShort, limitRateLong, rateLimitLong string
	flag.StringVar(&limitRateShort, "r", "", "Maximum download rate")
	flag.StringVar(&limitRateLong, "limit-rate", "", "Maximum download rate")
	flag.StringVar(&rateLimitLong, "rate-limit", "", "Alias of --limit-rate")
	var bufferSizeRaw, httpChunkSizeRaw string
	flag.StringVar(&bufferSizeRaw, "buffer-size", "", "Download buffer size")
	flag.StringVar(&httpChunkSizeRaw, "http-chunk-size", "", "HTTP chunk size for direct downloads")
	flag.BoolVar(&opts.NoResizeBuffer, "resize-buffer", false, "Automatically resize download buffer")
	flag.BoolVar(&opts.NoResizeBuffer, "no-resize-buffer", false, "Do not automatically resize download buffer")
	flag.IntVar(&opts.ConcurrentFragments, "concurrent-fragments", 0, "Number of fragments to download concurrently")
	flag.IntVar(&opts.ConcurrentFragments, "N", 0, "Alias of --concurrent-fragments")
	flag.BoolVar(&opts.SkipUnavailableFragments, "skip-unavailable-fragments", true, "Skip unavailable fragments")
	flag.BoolVar(&opts.SkipUnavailableFragments, "no-abort-on-unavailable-fragments", true, "Alias of --skip-unavailable-fragments")
	flag.BoolVar(&opts.SkipUnavailableFragments, "abort-on-unavailable-fragments", true, "Abort when a fragment is unavailable")
	flag.BoolVar(&opts.SkipUnavailableFragments, "no-skip-unavailable-fragments", true, "Alias of --abort-on-unavailable-fragments")
	var sleepIntervalRaw, minSleepIntervalRaw, maxSleepIntervalRaw, sleepSubtitlesRaw string
	var sleepRequestsRaw string
	flag.StringVar(&sleepRequestsRaw, "sleep-requests", "", "Seconds to sleep before extraction requests")
	flag.StringVar(&sleepIntervalRaw, "sleep-interval", "", "Seconds to sleep before each download")
	flag.StringVar(&minSleepIntervalRaw, "min-sleep-interval", "", "Alias of --sleep-interval")
	flag.StringVar(&maxSleepIntervalRaw, "max-sleep-interval", "", "Maximum seconds to sleep before each download")
	flag.StringVar(&sleepSubtitlesRaw, "sleep-subtitles", "", "Seconds to sleep before subtitle downloads")
	flag.IntVar(&opts.MaxDownloads, "max-downloads", 0, "Abort after downloading NUMBER files")
	writeSRT := false
	noWriteSRT := false
	flag.BoolVar(&writeSRT, "write-srt", false, "Alias of --write-subs that forces SRT output (yt-dlp compatibility)")
	flag.BoolVar(&noWriteSRT, "no-write-srt", false, "Disable --write-srt/--write-subs (yt-dlp compatibility)")
	flag.BoolVar(&opts.WriteSubs, "write-subs", false, "Write subtitle file")
	flag.BoolVar(&opts.WriteSubs, "no-write-subs", false, "Do not write subtitle files")
	flag.BoolVar(&opts.WriteAutoSubs, "write-auto-subs", false, "Write automatically generated subtitle file")
	flag.BoolVar(&opts.WriteAutoSubs, "write-automatic-subs", false, "Alias of --write-auto-subs (yt-dlp compatibility)")
	flag.BoolVar(&opts.WriteAutoSubs, "no-write-auto-subs", false, "Do not write automatically generated subtitle files")
	flag.BoolVar(&opts.WriteAutoSubs, "no-write-automatic-subs", false, "Alias of --no-write-auto-subs (yt-dlp compatibility)")
	flag.BoolVar(&opts.ListSubs, "list-subs", false, "List available subtitles and exit")
	flag.BoolVar(&opts.AllSubs, "all-subs", false, "Download all available subtitles")
	flag.BoolVar(&opts.WriteInfoJSON, "write-info-json", false, "Write video metadata to a .info.json sidecar file")
	flag.BoolVar(&opts.WriteInfoJSON, "no-write-info-json", false, "Do not write video metadata sidecar files")
	flag.BoolVar(&opts.WriteDescription, "write-description", false, "Write video description to a .description sidecar file")
	flag.BoolVar(&opts.WriteDescription, "no-write-description", false, "Do not write video description sidecar files")
	flag.BoolVar(&opts.NoWritePlaylistMetafiles, "write-playlist-metafiles", false, "Write playlist metadata sidecars when writing metadata files")
	flag.BoolVar(&opts.NoWritePlaylistMetafiles, "no-write-playlist-metafiles", false, "Do not write playlist metadata sidecars")
	flag.BoolVar(&opts.WriteThumbnail, "write-thumbnail", false, "Write video thumbnail to an image sidecar file")
	flag.BoolVar(&opts.WriteThumbnail, "no-write-thumbnail", false, "Do not write video thumbnail sidecar files")
	flag.BoolVar(&opts.WriteURLLink, "write-link", false, "Write an internet shortcut sidecar")
	flag.BoolVar(&opts.WriteURLLink, "write-url-link", false, "Write a Windows .url internet shortcut sidecar")
	flag.BoolVar(&opts.WriteWeblocLink, "write-webloc-link", false, "Write a macOS .webloc internet shortcut sidecar")
	flag.BoolVar(&opts.WriteDesktopLink, "write-desktop-link", false, "Write a Linux .desktop internet shortcut sidecar")
	flag.BoolVar(&opts.GetThumbnail, "get-thumbnail", false, "Print the selected thumbnail URL and exit")
	flag.StringVar(&opts.SubLangs, "sub-lang", "en", "Languages of the subtitles to download (optional) separated by commas")
	flag.StringVar(&opts.SubLangs, "sub-langs", "en", "Alias of --sub-lang (yt-dlp compatibility)")
	flag.StringVar(&opts.SubLangs, "srt-langs", "en", "Alias of --sub-langs (yt-dlp compatibility)")
	flag.StringVar(&opts.SubFormat, "sub-format", "best", "Subtitle format preference (e.g. vtt/srt, best)")
	flag.StringVar(&opts.SubFormat, "convert-subs", "best", "Alias of --sub-format (yt-dlp compatibility)")
	flag.StringVar(&opts.SubFormat, "convert-sub", "best", "Alias of --convert-subs")
	flag.StringVar(&opts.SubFormat, "convert-subtitles", "best", "Alias of --convert-subs")
	flag.BoolVar(&opts.FlatPlaylist, "flat-playlist", false, "Do not resolve and download playlist items, emit flat entries only")
	flag.BoolVar(&opts.FlatPlaylist, "extract-flat", false, "Alias of --flat-playlist (yt-dlp compatibility)")
	flag.BoolVar(&opts.NoPlaylist, "no-playlist", false, "Download only the video, if the URL refers to a video and a playlist")
	flag.BoolVar(&opts.YesPlaylist, "yes-playlist", false, "Download the playlist, if the URL refers to a video and a playlist")
	flag.StringVar(&opts.PlaylistItems, "playlist-items", "", "Comma-separated playlist item indexes/ranges to process, e.g. 1,3:5,:10")
	flag.StringVar(&opts.PlaylistItems, "I", "", "Alias of --playlist-items (yt-dlp compatibility)")
	flag.IntVar(&opts.PlaylistStart, "playlist-start", 0, "Playlist item to start at (1-based, yt-dlp compatibility)")
	flag.IntVar(&opts.PlaylistEnd, "playlist-end", 0, "Playlist item to end at (1-based, yt-dlp compatibility)")
	flag.BoolVar(&opts.PlaylistReverse, "playlist-reverse", false, "Download playlist videos in reverse order")
	flag.BoolVar(&opts.PlaylistReverse, "no-playlist-reverse", false, "Download playlist videos in normal order")
	flag.BoolVar(&opts.PlaylistRandom, "playlist-random", false, "Download playlist videos in random order")
	flag.IntVar(&opts.SkipPlaylistAfterErrors, "skip-playlist-after-errors", 0, "Number of allowed failures until the rest of the playlist is skipped")

	flag.BoolVar(&opts.ExtractAudio, "extract-audio", false, "Convert/download audio-only output")
	flag.BoolVar(&opts.ExtractAudio, "x", false, "Alias of --extract-audio")
	flag.StringVar(&opts.AudioFormat, "audio-format", "best", "Audio format for --extract-audio")
	flag.StringVar(&opts.AudioQuality, "audio-quality", "", "Audio quality for --extract-audio transcodes")
	flag.StringVar(&opts.MergeOutputFormat, "merge-output-format", "", "Container extension for merged video+audio output")
	flag.StringVar(&opts.RemuxVideo, "remux-video", "", "Remux merged output into a target container")
	flag.BoolVar(&opts.EmbedMetadata, "embed-metadata", false, "Add metadata to merged media files")
	flag.BoolVar(&opts.EmbedMetadata, "add-metadata", false, "Alias of --embed-metadata")
	flag.BoolVar(&opts.EmbedMetadata, "no-embed-metadata", false, "Do not add metadata to merged media files")
	flag.BoolVar(&opts.EmbedMetadata, "no-add-metadata", false, "Alias of --no-embed-metadata")
	postprocessorArgs := multiStringFlag{}
	flag.Var(&postprocessorArgs, "postprocessor-args", "Arguments to pass to supported postprocessors")
	flag.Var(&postprocessorArgs, "ppa", "Alias of --postprocessor-args")

	flag.BoolVar(&opts.PrintJSON, "print-json", false, "Be quiet and print the video information as JSON")
	flag.BoolVar(&opts.Quiet, "quiet", false, "Activate quiet mode")
	flag.BoolVar(&opts.Quiet, "q", false, "Alias of --quiet (yt-dlp compatibility)")
	flag.BoolVar(&opts.Quiet, "no-quiet", false, "Deactivate quiet mode (yt-dlp compatibility)")
	flag.BoolVar(&opts.NoProgress, "no-progress", false, "Do not print progress/status output")
	flag.BoolVar(&opts.Progress, "progress", false, "Print progress/status output")
	flag.BoolVar(&opts.NewlineProgress, "newline", false, "Output progress as new lines")
	flag.BoolVar(&opts.PrintJSON, "J", false, "Alias of --print-json (yt-dlp compatibility)")
	flag.BoolVar(&opts.PrintJSON, "j", false, "Alias of --print-json (yt-dlp compatibility)")
	flag.BoolVar(&opts.PrintJSON, "dump-json", false, "Alias of --print-json (yt-dlp compatibility)")
	flag.BoolVar(&opts.DumpSingleJSON, "dump-single-json", false, "Print a yt-dlp compatible single-entry JSON payload")
	flag.BoolVar(&opts.GetTitle, "e", false, "Alias of --get-title (yt-dlp compatibility)")
	flag.BoolVar(&opts.GetTitle, "get-title", false, "Print title and exit")
	flag.BoolVar(&opts.GetID, "get-id", false, "Print video ID and exit")
	flag.BoolVar(&opts.GetDescription, "get-description", false, "Print description and exit")
	flag.BoolVar(&opts.GetDuration, "get-duration", false, "Print duration and exit")
	flag.BoolVar(&opts.GetFilename, "get-filename", false, "Print output filename and exit")
	flag.BoolVar(&opts.GetFormat, "get-format", false, "Print selected format and exit")
	flag.BoolVar(&opts.GetURL, "get-url", false, "Print selected direct media URL(s) and exit")
	flag.BoolVar(&opts.GetURL, "g", false, "Alias of --get-url (yt-dlp compatibility)")
	printValues := multiStringFlag{}
	flag.Var(&printValues, "print", "Print a metadata field or output template and exit")
	flag.Var(&printValues, "O", "Alias of --print (yt-dlp compatibility)")
	printToFileValues := multiStringFlag{}
	flag.Var(&printToFileValues, "print-to-file", "Append a metadata field or output template to a file")
	flag.BoolVar(&opts.PlayerJSURLOnly, "playerjs", false, "Print player base.js URL only (debug)")

	flag.BoolVar(&opts.Verbose, "verbose", false, "Print various debugging information")
	flag.BoolVar(&opts.Version, "version", false, "Print version information and exit")

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

	// Move option flags ahead of positional arguments before parsing. Go's
	// flag package stops at the first non-flag token, so `ytv1 URL --proxy ...`
	// would otherwise silently drop every flag after the URL. For --proxy that
	// is a dangerous footgun: the user believes traffic is tunneled while it
	// actually goes direct, leaking their real IP. Permuting (as yt-dlp does)
	// lets flags appear anywhere on the command line.
	parseArgs = reorderArgs(flag.CommandLine, parseArgs)

	_ = flag.CommandLine.Parse(parseArgs)

	// Consolidate aliases
	opts.FormatSelector = pickValue(formatShort, formatLong, "best")
	if v, ok := findLastStringFlag(rawArgs, "-f", "--format"); ok {
		opts.FormatSelector = v
	}
	if v, ok := findLastBatchFileFlag(rawArgs); ok {
		opts.BatchFile = v
	} else if noBatchFile {
		opts.BatchFile = ""
	}
	opts.OutputTemplate = pickValue(outputShort, outputLong, "")
	if v, ok := findLastStringFlag(rawArgs, "-o", "--output"); ok {
		opts.OutputTemplate = v
	}
	if kind, value, ok := findLastOutputNamingFlag(rawArgs); ok {
		switch kind {
		case "id":
			opts.OutputUseID = true
			opts.OutputTemplate = ""
		case "template":
			opts.OutputUseID = false
			opts.OutputTemplate = value
		}
	}
	opts.OutputPathDir = normalizeOutputPathDir(pickValue(pathsShort, pathsLong, ""))
	if v, ok := findLastStringFlag(rawArgs, "-P", "--paths"); ok {
		opts.OutputPathDir = normalizeOutputPathDir(v)
	}
	if v, ok := findLastBoolFlag(rawArgs, map[string]bool{
		"--restrict-filenames":    true,
		"--no-restrict-filenames": false,
	}); ok {
		opts.RestrictFilenames = v
	}
	if v, ok := findLastIntFlag(rawArgs, "--trim-filenames", "--trim-file-names"); ok && v > 0 {
		opts.TrimFilenames = v
	}
	forceOverwritesFinal := false
	if noOverwrites, forceOverwrites, ok := findLastOverwritePolicyFlag(rawArgs); ok {
		opts.NoOverwrites = noOverwrites
		forceOverwritesFinal = forceOverwrites
	}
	if v, ok := findLastBoolFlag(rawArgs, map[string]bool{
		"--post-overwrites":    false,
		"--no-post-overwrites": true,
	}); ok {
		opts.NoPostOverwrites = v
	}
	if v, ok := findLastStringFlag(rawArgs, "--sub-format", "-sub-format", "--convert-subs", "--convert-sub", "--convert-subtitles"); ok {
		opts.SubFormat = v
	}
	if v, ok := findLastStringFlag(rawArgs, "--audio-format"); ok {
		opts.AudioFormat = v
	}
	if v, ok := findLastStringFlag(rawArgs, "--audio-quality"); ok {
		opts.AudioQuality = v
	}
	if v, ok := findLastStringFlag(rawArgs, "--socket-timeout"); ok {
		opts.SocketTimeout = parseSecondsDuration(v)
	} else {
		opts.SocketTimeout = parseSecondsDuration(socketTimeoutRaw)
	}
	if v, ok := findLastSourceAddressFlag(rawArgs); ok {
		opts.SourceAddress = v
	} else if sourceAddressRaw != "" {
		opts.SourceAddress = sourceAddressRaw
	} else if forceIPv6Short || forceIPv6Long {
		opts.SourceAddress = "::"
	} else if forceIPv4Short || forceIPv4Long {
		opts.SourceAddress = "0.0.0.0"
	}
	if v, ok := findLastStringFlag(rawArgs, "--merge-output-format"); ok {
		opts.MergeOutputFormat = v
	}
	if v, ok := findLastStringFlag(rawArgs, "--remux-video"); ok {
		opts.RemuxVideo = v
	}
	if v, ok := findLastStringFlag(rawArgs, "-r", "--limit-rate", "--rate-limit"); ok {
		opts.LimitRateBytesPerSecond = parseByteRate(v)
	} else {
		opts.LimitRateBytesPerSecond = firstPositiveInt64(parseByteRate(limitRateShort), parseByteRate(limitRateLong), parseByteRate(rateLimitLong))
	}
	retrySleepValues := findStringFlags(rawArgs, "--retry-sleep")
	if len(retrySleepValues) == 0 && retrySleepRaw != "" {
		retrySleepValues = append(retrySleepValues, retrySleepRaw)
	}
	for _, raw := range retrySleepValues {
		kind, d, ok := parseRetrySleep(raw)
		if !ok {
			continue
		}
		switch kind {
		case "http":
			opts.HTTPRetrySleep = d
			opts.HTTPRetrySleepSet = true
		case "fragment":
			opts.FragmentRetrySleep = d
			opts.FragmentRetrySleepSet = true
		case "extractor":
			opts.ExtractorRetrySleep = d
			opts.ExtractorRetrySleepSet = true
		}
	}
	if v, ok := findLastStringFlag(rawArgs, "--extractor-retries"); ok {
		if retries, ok := parseRetryCount(v); ok {
			opts.ExtractorRetries = retries
			opts.ExtractorRetriesSet = true
		}
	} else if retries, ok := parseRetryCount(extractorRetriesRaw); ok {
		opts.ExtractorRetries = retries
		opts.ExtractorRetriesSet = true
	}
	if v, ok := findLastStringFlag(rawArgs, "--file-access-retries"); ok {
		if retries, ok := parseRetryCount(v); ok {
			opts.FileAccessRetries = retries
			opts.FileAccessRetriesSet = true
		}
	} else if retries, ok := parseRetryCount(fileAccessRetriesRaw); ok {
		opts.FileAccessRetries = retries
		opts.FileAccessRetriesSet = true
	}
	if v, ok := findLastStringFlag(rawArgs, "--fragment-retries"); ok {
		if retries, ok := parseRetryCount(v); ok {
			opts.FragmentRetries = retries
			opts.FragmentRetriesSet = true
		}
	} else if retries, ok := parseRetryCount(fragmentRetriesRaw); ok {
		opts.FragmentRetries = retries
		opts.FragmentRetriesSet = true
	}
	if v, ok := findLastStringFlag(rawArgs, "--buffer-size"); ok {
		opts.BufferSizeBytes = parseByteRate(v)
	} else {
		opts.BufferSizeBytes = parseByteRate(bufferSizeRaw)
	}
	if v, ok := findLastStringFlag(rawArgs, "--http-chunk-size"); ok {
		opts.HTTPChunkSizeBytes = parseByteRate(v)
	} else {
		opts.HTTPChunkSizeBytes = parseByteRate(httpChunkSizeRaw)
	}
	if v, ok := findLastBoolFlag(rawArgs, map[string]bool{
		"--resize-buffer":    false,
		"--no-resize-buffer": true,
	}); ok {
		opts.NoResizeBuffer = v
	}
	if v, ok := findLastIntFlag(rawArgs, "-N", "--concurrent-fragments"); ok && v > 0 {
		opts.ConcurrentFragments = v
	}
	if v, ok := findLastBoolFlag(rawArgs, map[string]bool{
		"--skip-unavailable-fragments":        true,
		"--no-abort-on-unavailable-fragments": true,
		"--abort-on-unavailable-fragments":    false,
		"--no-skip-unavailable-fragments":     false,
	}); ok {
		opts.SkipUnavailableFragments = v
	}
	if v, ok := findLastStringFlag(rawArgs, "--sleep-requests"); ok {
		opts.SleepRequests = parseSecondsDuration(v)
	} else {
		opts.SleepRequests = parseSecondsDuration(sleepRequestsRaw)
	}
	if v, ok := findLastStringFlag(rawArgs, "--sleep-interval", "--min-sleep-interval"); ok {
		opts.SleepInterval = parseSecondsDuration(v)
	} else {
		opts.SleepInterval = firstPositiveDuration(parseSecondsDuration(sleepIntervalRaw), parseSecondsDuration(minSleepIntervalRaw))
	}
	if v, ok := findLastStringFlag(rawArgs, "--max-sleep-interval"); ok {
		opts.MaxSleepInterval = parseSecondsDuration(v)
	} else {
		opts.MaxSleepInterval = parseSecondsDuration(maxSleepIntervalRaw)
	}
	if v, ok := findLastStringFlag(rawArgs, "--sleep-subtitles"); ok {
		opts.SleepSubtitles = parseSecondsDuration(v)
	} else {
		opts.SleepSubtitles = parseSecondsDuration(sleepSubtitlesRaw)
	}
	if v, ok := findLastBoolFlag(rawArgs, map[string]bool{
		"--embed-metadata":    true,
		"--add-metadata":      true,
		"--no-embed-metadata": false,
		"--no-add-metadata":   false,
	}); ok {
		opts.EmbedMetadata = v
	}
	opts.PostprocessorArgs = append([]string(nil), postprocessorArgs...)
	opts.AddHeaders = append([]string(nil), addHeaders...)
	if v, ok := findLastDownloadArchiveFlag(rawArgs); ok {
		opts.DownloadArchive = v
	} else if noDownloadArchive {
		opts.DownloadArchive = ""
	}
	opts.ListFormats = listFormatsShort || listFormatsLong
	if v, ok := findLastBoolFlag(rawArgs, map[string]bool{
		"--skip-download": true,
		"--no-download":   true,
		"--simulate":      true,
		"-s":              true,
		"--no-simulate":   false,
	}); ok {
		opts.SkipDownload = v
	}
	opts.PrintTemplates = append([]string(nil), printValues...)
	opts.PrintToFile = findPrintToFileSpecs(rawArgs)
	if (len(opts.PrintTemplates) > 0 || len(opts.PrintToFile) > 0) && !hasBoolFlagAfterLastPrint(rawArgs, "--no-simulate") {
		opts.SkipDownload = true
	}
	if v, ok := findLastBoolFlag(rawArgs, map[string]bool{
		"--quiet":    true,
		"-q":         true,
		"--no-quiet": false,
	}); ok {
		opts.Quiet = v
	}
	if v, ok := findLastBoolFlag(rawArgs, map[string]bool{
		"--no-progress": true,
		"--progress":    false,
	}); ok {
		opts.NoProgress = v
		opts.Progress = !v
	}
	if (len(opts.PrintTemplates) > 0 || len(opts.PrintToFile) > 0) && !hasBoolFlagAfterLastPrint(rawArgs, "--no-quiet") {
		opts.Quiet = true
	}
	if v, ok := findLastBoolFlag(rawArgs, map[string]bool{
		"--no-continue": true,
		"--continue":    false,
	}); ok {
		opts.NoContinue = v
	} else if !continueDownloads {
		opts.NoContinue = true
	}
	if forceOverwritesFinal {
		opts.NoContinue = true
	}
	if v, ok := findLastBoolFlag(rawArgs, map[string]bool{
		"--no-part": true,
		"--part":    false,
	}); ok {
		opts.NoPart = v
	}
	if v, ok := findLastBoolFlag(rawArgs, map[string]bool{
		"--mtime":    true,
		"--no-mtime": false,
	}); ok {
		opts.UpdateMTime = v
	}
	if v, ok := findLastBoolFlag(rawArgs, map[string]bool{
		"--keep-video":    true,
		"-k":              true,
		"--no-keep-video": false,
	}); ok {
		opts.KeepVideo = v
	}
	if v, ok := findLastBoolFlag(rawArgs, map[string]bool{
		"--break-on-existing":    true,
		"--no-break-on-existing": false,
	}); ok {
		opts.BreakOnExisting = v
	}
	if v, ok := findLastBoolFlag(rawArgs, map[string]bool{
		"--abort-on-error":   true,
		"--no-ignore-errors": true,
		"--ignore-errors":    false,
		"-i":                 false,
	}); ok {
		opts.AbortOnError = v
		opts.IgnoreErrors = !v
	} else if opts.IgnoreErrors {
		opts.AbortOnError = false
	}
	if v, ok := findLastBoolFlag(rawArgs, map[string]bool{
		"--no-playlist":  true,
		"--yes-playlist": false,
	}); ok {
		opts.NoPlaylist = v
		opts.YesPlaylist = !v
	} else if opts.YesPlaylist {
		opts.NoPlaylist = false
	}
	if writeSubs, forceSRT, ok := findLastSubtitleWriteFlag(rawArgs); ok {
		opts.WriteSubs = writeSubs
		if forceSRT {
			opts.SubFormat = "srt"
		}
	} else if writeSRT {
		opts.WriteSubs = true
		opts.SubFormat = "srt"
	} else if noWriteSRT {
		opts.WriteSubs = false
	}
	if v, ok := findLastBoolFlag(rawArgs, map[string]bool{
		"--write-auto-subs":         true,
		"--write-automatic-subs":    true,
		"--no-write-auto-subs":      false,
		"--no-write-automatic-subs": false,
	}); ok {
		opts.WriteAutoSubs = v
	}
	if v, ok := findLastBoolFlag(rawArgs, map[string]bool{
		"--write-info-json":    true,
		"--no-write-info-json": false,
	}); ok {
		opts.WriteInfoJSON = v
	}
	if v, ok := findLastBoolFlag(rawArgs, map[string]bool{
		"--write-description":    true,
		"--no-write-description": false,
	}); ok {
		opts.WriteDescription = v
	}
	if v, ok := findLastBoolFlag(rawArgs, map[string]bool{
		"--write-playlist-metafiles":    false,
		"--no-write-playlist-metafiles": true,
	}); ok {
		opts.NoWritePlaylistMetafiles = v
	}
	if v, ok := findLastBoolFlag(rawArgs, map[string]bool{
		"--write-thumbnail":    true,
		"--no-write-thumbnail": false,
	}); ok {
		opts.WriteThumbnail = v
	}
	opts.GetMetadataOrder = findMetadataGetFlagOrder(rawArgs)

	opts.URLs = append(batchFileURLs(opts.BatchFile, os.Stdin), flag.Args()...)
	return opts
}

type multiStringFlag []string

func (m *multiStringFlag) String() string {
	if m == nil {
		return ""
	}
	return strings.Join(*m, ",")
}

func (m *multiStringFlag) Set(value string) error {
	*m = append(*m, value)
	return nil
}

func ffmpegMergerPostprocessorArgs(values []string) []string {
	out := make([]string, 0)
	for _, value := range values {
		key, rawArgs := splitPostprocessorArgSpec(value)
		if !isFFmpegMergerPostprocessorKey(key) {
			continue
		}
		args, err := splitShellFields(rawArgs)
		if err != nil {
			args = strings.Fields(rawArgs)
		}
		out = append(out, args...)
	}
	return out
}

func splitPostprocessorArgSpec(value string) (string, string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", ""
	}
	if i := strings.Index(value, ":"); i >= 0 {
		return strings.TrimSpace(value[:i]), strings.TrimSpace(value[i+1:])
	}
	return "default-compat", value
}

func isFFmpegMergerPostprocessorKey(key string) bool {
	key = strings.ToLower(strings.TrimSpace(key))
	if key == "" {
		key = "default-compat"
	}
	key = strings.TrimSuffix(key, "_i")
	key = strings.TrimSuffix(key, "_o")
	for len(key) > 0 {
		last := key[len(key)-1]
		if last < '0' || last > '9' {
			break
		}
		key = key[:len(key)-1]
	}
	switch key {
	case "default", "default-compat", "ffmpeg", "merger", "merger+ffmpeg":
		return true
	default:
		return false
	}
}

func splitShellFields(value string) ([]string, error) {
	fields := make([]string, 0)
	var b strings.Builder
	var quote rune
	escaped := false
	for _, r := range value {
		switch {
		case escaped:
			b.WriteRune(r)
			escaped = false
		case r == '\\':
			escaped = true
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				b.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote = r
		case r == ' ' || r == '\t' || r == '\n' || r == '\r':
			if b.Len() > 0 {
				fields = append(fields, b.String())
				b.Reset()
			}
		default:
			b.WriteRune(r)
		}
	}
	if escaped {
		b.WriteRune('\\')
	}
	if quote != 0 {
		return nil, fmt.Errorf("unterminated quote")
	}
	if b.Len() > 0 {
		fields = append(fields, b.String())
	}
	return fields, nil
}

func parseByteRate(raw string) int64 {
	s := strings.TrimSpace(raw)
	if s == "" {
		return 0
	}
	lower := strings.ToLower(s)
	multiplier := float64(1)
	suffixes := []struct {
		suffix string
		value  float64
	}{
		{"kib", 1024},
		{"mib", 1024 * 1024},
		{"gib", 1024 * 1024 * 1024},
		{"kb", 1000},
		{"mb", 1000 * 1000},
		{"gb", 1000 * 1000 * 1000},
		{"k", 1024},
		{"m", 1024 * 1024},
		{"g", 1024 * 1024 * 1024},
		{"b", 1},
	}
	for _, suffix := range suffixes {
		if strings.HasSuffix(lower, suffix.suffix) {
			multiplier = suffix.value
			s = strings.TrimSpace(s[:len(s)-len(suffix.suffix)])
			break
		}
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || v <= 0 {
		return 0
	}
	return int64(v * multiplier)
}

func firstPositiveInt64(values ...int64) int64 {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func parseRetryCount(raw string) (int, bool) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return 0, false
	}
	v, err := strconv.Atoi(s)
	if err != nil || v < 0 {
		return 0, false
	}
	return v, true
}

func parseRetrySleep(raw string) (string, time.Duration, bool) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", 0, false
	}
	kind := "http"
	if before, after, ok := strings.Cut(s, ":"); ok {
		switch before {
		case "http", "fragment", "extractor":
			kind = before
			s = strings.TrimSpace(after)
		case "file_access":
			return "", 0, false
		}
	}
	seconds, ok := retrySleepStartSeconds(s)
	if !ok {
		return "", 0, false
	}
	return kind, time.Duration(seconds * float64(time.Second)), true
}

func retrySleepStartSeconds(expr string) (float64, bool) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return 0, false
	}
	if rest, ok := strings.CutPrefix(expr, "linear="); ok {
		expr = rest
	} else if rest, ok := strings.CutPrefix(expr, "exp="); ok {
		expr = rest
	}
	first := expr
	if before, _, ok := strings.Cut(expr, ":"); ok {
		first = before
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(first), 64)
	if err != nil || v < 0 {
		return 0, false
	}
	return v, true
}

func buildRequestHeaders(opts Options) http.Header {
	headers := make(http.Header)
	if strings.TrimSpace(opts.UserAgent) != "" {
		headers.Set("User-Agent", strings.TrimSpace(opts.UserAgent))
	}
	if strings.TrimSpace(opts.Referer) != "" {
		headers.Set("Referer", strings.TrimSpace(opts.Referer))
	}
	for _, raw := range opts.AddHeaders {
		name, value, ok := parseHeaderSpec(raw)
		if !ok {
			continue
		}
		headers.Add(name, value)
	}
	if len(headers) == 0 {
		return nil
	}
	return headers
}

func parseHeaderSpec(raw string) (string, string, bool) {
	name, value, ok := strings.Cut(raw, ":")
	name = strings.TrimSpace(name)
	if !ok || name == "" {
		return "", "", false
	}
	return name, strings.TrimSpace(value), true
}

func parseSecondsDuration(raw string) time.Duration {
	s := strings.TrimSpace(raw)
	if s == "" {
		return 0
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || v <= 0 {
		return 0
	}
	return time.Duration(v * float64(time.Second))
}

func firstPositiveDuration(values ...time.Duration) time.Duration {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func parsePrintToFileSpecs(values []string) []PrintToFileSpec {
	specs := make([]PrintToFileSpec, 0, len(values))
	for _, value := range values {
		fields := strings.Fields(value)
		if len(fields) < 2 {
			continue
		}
		specs = append(specs, PrintToFileSpec{
			Template: strings.Join(fields[:len(fields)-1], " "),
			File:     fields[len(fields)-1],
		})
	}
	return specs
}

// boolFlag matches flag values that take no argument (e.g. bool flags), so
// reorderArgs knows not to pull the following token along as a value.
type boolFlag interface {
	IsBoolFlag() bool
}

// reorderArgs permutes args so all option flags (and their values) precede
// positional arguments, mirroring GNU getopt / yt-dlp behavior. This is
// required because flag.FlagSet.Parse stops at the first positional argument;
// without it, `ytv1 URL --proxy p` would parse zero flags and silently ignore
// --proxy. A "--" token terminates flag scanning: everything after it is
// treated as positional.
func reorderArgs(fs *flag.FlagSet, args []string) []string {
	flags := make([]string, 0, len(args))
	positionals := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			// Explicit end-of-flags marker: keep it in place and treat the rest
			// as positional so downstream Parse also stops here.
			flags = append(flags, arg)
			positionals = append(positionals, args[i+1:]...)
			break
		}
		// Not a flag token ("-" alone, "" , or anything not starting with '-').
		if len(arg) < 2 || arg[0] != '-' {
			positionals = append(positionals, arg)
			continue
		}
		flags = append(flags, arg)
		// "--name=value" / "-name=value" carry their value inline.
		if strings.ContainsRune(arg, '=') {
			continue
		}
		name := strings.TrimLeft(arg, "-")
		f := fs.Lookup(name)
		if f == nil {
			// Unknown flag: let Parse report it. Don't consume the next token.
			continue
		}
		if bf, ok := f.Value.(boolFlag); ok && bf.IsBoolFlag() {
			// Boolean flag: takes no value.
			continue
		}
		// Value-taking flag in "-name value" form: pull the value along.
		if i+1 < len(args) {
			flags = append(flags, args[i+1])
			i++
		}
	}
	return append(flags, positionals...)
}

func normalizePrintToFileArgs(args []string) []string {
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--print-to-file" && i+2 < len(args) {
			out = append(out, "--print-to-file="+args[i+1]+" "+args[i+2])
			i += 2
			continue
		}
		out = append(out, arg)
	}
	return out
}

func findPrintToFileSpecs(args []string) []PrintToFileSpec {
	specs := make([]PrintToFileSpec, 0)
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--print-to-file":
			if i+2 < len(args) {
				specs = append(specs, PrintToFileSpec{Template: args[i+1], File: args[i+2]})
				i += 2
			}
		case strings.HasPrefix(arg, "--print-to-file="):
			specs = append(specs, parsePrintToFileSpecs([]string{strings.TrimPrefix(arg, "--print-to-file=")})...)
		}
	}
	return specs
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

func normalizeOutputPathDir(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if filepath.VolumeName(raw) != "" {
		return raw
	}
	if before, after, ok := strings.Cut(raw, ":"); ok {
		switch strings.ToLower(strings.TrimSpace(before)) {
		case "", "home":
			return strings.TrimSpace(after)
		default:
			// Unknown location-type prefix (temp:, subtitle:, ...): strip the
			// type and use the path rather than silently discarding the whole
			// value (which sent output to the cwd).
			return strings.TrimSpace(after)
		}
	}
	return raw
}

func findLastOutputNamingFlag(args []string) (string, string, bool) {
	var (
		lastKind  string
		lastValue string
		found     bool
	)
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--id":
			lastKind = "id"
			lastValue = ""
			found = true
		case arg == "-o", arg == "--output":
			if i+1 < len(args) {
				lastKind = "template"
				lastValue = args[i+1]
				found = true
			}
		case strings.HasPrefix(arg, "-o="):
			lastKind = "template"
			lastValue = strings.TrimPrefix(arg, "-o=")
			found = true
		case strings.HasPrefix(arg, "--output="):
			lastKind = "template"
			lastValue = strings.TrimPrefix(arg, "--output=")
			found = true
		}
	}
	return lastKind, lastValue, found
}

func findLastOverwritePolicyFlag(args []string) (bool, bool, bool) {
	const (
		policyNoOverwrite = "no"
		policyForce       = "force"
		policyDefault     = "default"
	)
	last := ""
	for _, arg := range args {
		switch arg {
		case "--no-overwrites", "-w":
			last = policyNoOverwrite
		case "--force-overwrites", "--yes-overwrites":
			last = policyForce
		case "--no-force-overwrites":
			last = policyDefault
		}
	}
	switch last {
	case policyNoOverwrite:
		return true, false, true
	case policyForce:
		return false, true, true
	case policyDefault:
		return false, false, true
	default:
		return false, false, false
	}
}

func findLastStringFlag(args []string, names ...string) (string, bool) {
	var (
		last  string
		found bool
	)
	for i := 0; i < len(args); i++ {
		arg := args[i]
		for _, name := range names {
			if arg == name {
				if i+1 < len(args) {
					last = args[i+1]
					found = true
				}
				break
			}
			prefix := name + "="
			if strings.HasPrefix(arg, prefix) {
				last = strings.TrimPrefix(arg, prefix)
				found = true
				break
			}
		}
	}
	return last, found
}

func findStringFlags(args []string, names ...string) []string {
	var values []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		for _, name := range names {
			if arg == name {
				if i+1 < len(args) {
					values = append(values, args[i+1])
				}
				break
			}
			prefix := name + "="
			if strings.HasPrefix(arg, prefix) {
				values = append(values, strings.TrimPrefix(arg, prefix))
				break
			}
		}
	}
	return values
}

func findLastSourceAddressFlag(args []string) (string, bool) {
	var (
		last  string
		found bool
	)
	for i := 0; i < len(args); i++ {
		switch arg := args[i]; {
		case arg == "--source-address":
			if i+1 < len(args) {
				last = args[i+1]
				found = true
			}
		case strings.HasPrefix(arg, "--source-address="):
			last = strings.TrimPrefix(arg, "--source-address=")
			found = true
		case arg == "-4", arg == "--force-ipv4":
			last = "0.0.0.0"
			found = true
		case arg == "-6", arg == "--force-ipv6":
			last = "::"
			found = true
		}
	}
	return last, found
}

func findLastIntFlag(args []string, names ...string) (int, bool) {
	var (
		last  int
		found bool
	)
	for i := 0; i < len(args); i++ {
		arg := args[i]
		for _, name := range names {
			if arg == name {
				if i+1 < len(args) {
					if v, err := strconv.Atoi(args[i+1]); err == nil {
						last = v
						found = true
					}
				}
				break
			}
			prefix := name + "="
			if strings.HasPrefix(arg, prefix) {
				if v, err := strconv.Atoi(strings.TrimPrefix(arg, prefix)); err == nil {
					last = v
					found = true
				}
				break
			}
		}
	}
	return last, found
}

func findLastBatchFileFlag(args []string) (string, bool) {
	var (
		last  string
		found bool
	)
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--batch-file", arg == "-a":
			if i+1 < len(args) {
				last = args[i+1]
				found = true
			}
		case strings.HasPrefix(arg, "--batch-file="):
			last = strings.TrimPrefix(arg, "--batch-file=")
			found = true
		case strings.HasPrefix(arg, "-a="):
			last = strings.TrimPrefix(arg, "-a=")
			found = true
		case arg == "--no-batch-file":
			last = ""
			found = true
		case strings.HasPrefix(arg, "--no-batch-file="):
			parsed, err := strconv.ParseBool(strings.TrimPrefix(arg, "--no-batch-file="))
			if err == nil && parsed {
				last = ""
				found = true
			}
		}
	}
	return last, found
}

func batchFileURLs(path string, stdin io.Reader) []string {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	var r io.Reader
	var f *os.File
	if path == "-" {
		r = stdin
	} else {
		file, err := os.Open(path)
		if err != nil {
			return nil
		}
		f = file
		r = file
	}
	if f != nil {
		defer f.Close()
	}
	scanner := bufio.NewScanner(r)
	urls := make([]string, 0)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		urls = append(urls, line)
	}
	return urls
}

func findLastBoolFlag(args []string, values map[string]bool) (bool, bool) {
	var (
		last  bool
		found bool
	)
	for _, arg := range args {
		for name, fallback := range values {
			if arg == name {
				last = fallback
				found = true
				break
			}
			prefix := name + "="
			if strings.HasPrefix(arg, prefix) {
				parsed, err := strconv.ParseBool(strings.TrimPrefix(arg, prefix))
				if err != nil {
					break
				}
				if fallback {
					last = parsed
				} else {
					last = !parsed
				}
				found = true
				break
			}
		}
	}
	return last, found
}

func findLastDownloadArchiveFlag(args []string) (string, bool) {
	var (
		last  string
		found bool
	)
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--download-archive":
			if i+1 < len(args) {
				last = args[i+1]
				found = true
			}
		case strings.HasPrefix(arg, "--download-archive="):
			last = strings.TrimPrefix(arg, "--download-archive=")
			found = true
		case arg == "--no-download-archive":
			last = ""
			found = true
		case strings.HasPrefix(arg, "--no-download-archive="):
			parsed, err := strconv.ParseBool(strings.TrimPrefix(arg, "--no-download-archive="))
			if err == nil && parsed {
				last = ""
				found = true
			}
		}
	}
	return last, found
}

func findLastSubtitleWriteFlag(args []string) (write bool, forceSRT bool, found bool) {
	for _, arg := range args {
		name := arg
		if idx := strings.Index(name, "="); idx >= 0 {
			name = name[:idx]
		}
		switch name {
		case "--write-subs":
			write = true
			forceSRT = false
			found = true
		case "--write-srt":
			write = true
			forceSRT = true
			found = true
		case "--no-write-subs", "--no-write-srt":
			write = false
			forceSRT = false
			found = true
		}
	}
	return write, forceSRT, found
}

func findMetadataGetFlagOrder(args []string) []string {
	order := make([]string, 0, 4)
	for i := 0; i < len(args); i++ {
		arg := args[i]
		name := arg
		if idx := strings.Index(name, "="); idx >= 0 {
			name = name[:idx]
		}
		switch name {
		case "-e", "--get-title":
			order = append(order, "title")
		case "--get-id":
			order = append(order, "id")
		case "--get-description":
			order = append(order, "description")
		case "--get-duration":
			order = append(order, "duration")
		case "--get-filename":
			order = append(order, "filename")
		case "--get-format":
			order = append(order, "format")
		case "-g", "--get-url":
			order = append(order, "url")
		case "-O", "--print":
			if i+1 < len(args) {
				order = append(order, "print:"+args[i+1])
				i++
			}
		case "--print-to-file":
			if i+2 < len(args) {
				order = append(order, "printfile:"+args[i+1]+"\x00"+args[i+2])
				i += 2
			}
		case "--print=", "-O=":
			// unreachable with the name extraction above; kept for readability.
		default:
			if strings.HasPrefix(arg, "--print=") {
				order = append(order, "print:"+strings.TrimPrefix(arg, "--print="))
			} else if strings.HasPrefix(arg, "-O=") {
				order = append(order, "print:"+strings.TrimPrefix(arg, "-O="))
			} else if strings.HasPrefix(arg, "--print-to-file=") {
				specs := parsePrintToFileSpecs([]string{strings.TrimPrefix(arg, "--print-to-file=")})
				if len(specs) > 0 {
					order = append(order, "printfile:"+specs[0].Template+"\x00"+specs[0].File)
				}
			}
		}
	}
	return order
}

func hasBoolFlagAfterLastPrint(args []string, flagName string) bool {
	lastPrint := -1
	for i, arg := range args {
		switch {
		case arg == "--print", arg == "-O", arg == "--print-to-file", strings.HasPrefix(arg, "--print="), strings.HasPrefix(arg, "-O="), strings.HasPrefix(arg, "--print-to-file="):
			lastPrint = i
		}
	}
	if lastPrint < 0 {
		return false
	}
	for _, arg := range args[lastPrint+1:] {
		if arg == flagName {
			return true
		}
		if strings.HasPrefix(arg, flagName+"=") {
			parsed, err := strconv.ParseBool(strings.TrimPrefix(arg, flagName+"="))
			return err == nil && parsed
		}
	}
	return false
}

// ToClientConfig converts Options to client.Config.
// ToClientConfig converts Options to client.Config.
func ToClientConfig(opts Options) (client.Config, error) {
	// Fail loud on an unusable --proxy value. A proxy that cannot be honored
	// must abort here, before any request: silently connecting directly would
	// leak the user's real IP after they explicitly asked to tunnel.
	if err := client.ValidateProxyURL(opts.ProxyURL); err != nil {
		return client.Config{}, err
	}
	cfg := client.Config{
		ProxyURL:           opts.ProxyURL,
		SourceAddress:      opts.SourceAddress,
		InsecureSkipVerify: opts.NoCheckCertificate,
		VisitorData:        opts.VisitorData,
	}
	cfg.RequestHeaders = buildRequestHeaders(opts)
	if opts.SocketTimeout > 0 {
		cfg.RequestTimeout = opts.SocketTimeout
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
	if opts.ExtractorRetriesSet {
		cfg.MetadataTransport.MaxRetries = opts.ExtractorRetries
	}
	if opts.RetrySleepMS >= 0 {
		backoff := time.Duration(opts.RetrySleepMS) * time.Millisecond
		cfg.DownloadTransport.InitialBackoff = backoff
		cfg.MetadataTransport.InitialBackoff = backoff
	}
	if opts.HTTPRetrySleepSet {
		cfg.DownloadTransport.InitialBackoff = opts.HTTPRetrySleep
	}
	if opts.ExtractorRetrySleepSet {
		cfg.MetadataTransport.InitialBackoff = opts.ExtractorRetrySleep
	}
	if opts.FragmentRetrySleepSet {
		cfg.DownloadTransport.InitialBackoff = opts.FragmentRetrySleep
	}
	if opts.FragmentRetriesSet {
		cfg.DownloadTransport.MaxRetries = opts.FragmentRetries
	}
	if opts.FileAccessRetriesSet {
		cfg.DownloadTransport.FileAccessRetries = opts.FileAccessRetries
	}
	if opts.LimitRateBytesPerSecond > 0 {
		cfg.DownloadTransport.RateLimitBytesPerSecond = opts.LimitRateBytesPerSecond
	}
	if opts.BufferSizeBytes > 0 {
		cfg.DownloadTransport.ChunkSize = opts.BufferSizeBytes
	}
	if opts.HTTPChunkSizeBytes > 0 {
		cfg.DownloadTransport.EnableChunked = true
		cfg.DownloadTransport.ChunkSize = opts.HTTPChunkSizeBytes
	}
	if opts.ConcurrentFragments > 0 {
		cfg.DownloadTransport.MaxConcurrency = opts.ConcurrentFragments
	}
	cfg.DownloadTransport.SkipUnavailableFragments = opts.SkipUnavailableFragments

	// Muxer: puremux (pure Go; reads WebM/MKV/MP4 input, writes WebM/MKV) is
	// the primary muxer. FFmpeg handles MP4 output, metadata embedding, and
	// any puremux failure; FFmpegLocation/postprocessor-args configure it.
	ffmpegFallback := muxer.NewFFmpegMuxerWithExtraArgs(opts.FFmpegLocation, ffmpegMergerPostprocessorArgs(opts.PostprocessorArgs))
	cfg.Muxer = muxer.NewPureMuxMuxer(ffmpegFallback)

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
