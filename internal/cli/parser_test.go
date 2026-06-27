package cli

import (
	"context"
	"flag"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/famomatic/ytv1/internal/muxer"
)

func TestToClientConfig_StaticPoTokenProvider(t *testing.T) {
	cfg, err := ToClientConfig(Options{
		PoToken: "token-abc",
	})
	if err != nil {
		t.Fatalf("ToClientConfig() error = %v", err)
	}
	if cfg.PoTokenProvider == nil {
		t.Fatalf("expected PoTokenProvider to be configured")
	}
	token, err := cfg.PoTokenProvider.GetToken(context.Background(), "web")
	if err != nil {
		t.Fatalf("PoTokenProvider.GetToken() error = %v", err)
	}
	if token != "token-abc" {
		t.Fatalf("token = %q, want %q", token, "token-abc")
	}
}

func TestToClientConfig_EmptyPoTokenDoesNotConfigureProvider(t *testing.T) {
	cfg, err := ToClientConfig(Options{
		PoToken: "   ",
	})
	if err != nil {
		t.Fatalf("ToClientConfig() error = %v", err)
	}
	if cfg.PoTokenProvider != nil {
		t.Fatalf("expected PoTokenProvider to be nil for empty override")
	}
}

func TestToClientConfig_RetryOverrides(t *testing.T) {
	cfg, err := ToClientConfig(Options{
		DownloadRetries: 4,
		RetrySleepMS:    750,
	})
	if err != nil {
		t.Fatalf("ToClientConfig() error = %v", err)
	}
	if cfg.DownloadTransport.MaxRetries != 4 || cfg.MetadataTransport.MaxRetries != 4 {
		t.Fatalf("retry overrides not applied: download=%d metadata=%d", cfg.DownloadTransport.MaxRetries, cfg.MetadataTransport.MaxRetries)
	}
	wantBackoff := 750 * time.Millisecond
	if cfg.DownloadTransport.InitialBackoff != wantBackoff || cfg.MetadataTransport.InitialBackoff != wantBackoff {
		t.Fatalf("backoff overrides not applied: download=%s metadata=%s", cfg.DownloadTransport.InitialBackoff, cfg.MetadataTransport.InitialBackoff)
	}
}

func TestToClientConfig_FragmentRetriesOverrideGenericRetries(t *testing.T) {
	cfg, err := ToClientConfig(Options{
		DownloadRetries:    4,
		FragmentRetries:    7,
		FragmentRetriesSet: true,
	})
	if err != nil {
		t.Fatalf("ToClientConfig() error = %v", err)
	}
	if cfg.DownloadTransport.MaxRetries != 7 {
		t.Fatalf("download MaxRetries=%d, want 7", cfg.DownloadTransport.MaxRetries)
	}
	if cfg.MetadataTransport.MaxRetries != 4 {
		t.Fatalf("metadata MaxRetries=%d, want 4", cfg.MetadataTransport.MaxRetries)
	}
}

func TestToClientConfig_ExtractorRetriesOverrideGenericRetries(t *testing.T) {
	cfg, err := ToClientConfig(Options{
		DownloadRetries:     4,
		ExtractorRetries:    5,
		ExtractorRetriesSet: true,
	})
	if err != nil {
		t.Fatalf("ToClientConfig() error = %v", err)
	}
	if cfg.DownloadTransport.MaxRetries != 4 {
		t.Fatalf("download MaxRetries=%d, want 4", cfg.DownloadTransport.MaxRetries)
	}
	if cfg.MetadataTransport.MaxRetries != 5 {
		t.Fatalf("metadata MaxRetries=%d, want 5", cfg.MetadataTransport.MaxRetries)
	}
}

func TestToClientConfig_FileAccessRetries(t *testing.T) {
	cfg, err := ToClientConfig(Options{
		FileAccessRetries:    6,
		FileAccessRetriesSet: true,
	})
	if err != nil {
		t.Fatalf("ToClientConfig() error = %v", err)
	}
	if cfg.DownloadTransport.FileAccessRetries != 6 {
		t.Fatalf("FileAccessRetries=%d, want 6", cfg.DownloadTransport.FileAccessRetries)
	}
}

func TestToClientConfig_RetrySleepTypes(t *testing.T) {
	cfg, err := ToClientConfig(Options{
		RetrySleepMS:           500,
		HTTPRetrySleep:         1500 * time.Millisecond,
		HTTPRetrySleepSet:      true,
		FragmentRetrySleep:     2 * time.Second,
		FragmentRetrySleepSet:  true,
		ExtractorRetrySleep:    3 * time.Second,
		ExtractorRetrySleepSet: true,
	})
	if err != nil {
		t.Fatalf("ToClientConfig() error = %v", err)
	}
	if cfg.DownloadTransport.InitialBackoff != 2*time.Second {
		t.Fatalf("download InitialBackoff=%s, want 2s", cfg.DownloadTransport.InitialBackoff)
	}
	if cfg.MetadataTransport.InitialBackoff != 3*time.Second {
		t.Fatalf("metadata InitialBackoff=%s, want 3s", cfg.MetadataTransport.InitialBackoff)
	}
}

func TestToClientConfig_SocketTimeout(t *testing.T) {
	cfg, err := ToClientConfig(Options{SocketTimeout: 1500 * time.Millisecond})
	if err != nil {
		t.Fatalf("ToClientConfig() error = %v", err)
	}
	if cfg.RequestTimeout != 1500*time.Millisecond {
		t.Fatalf("RequestTimeout=%s, want 1.5s", cfg.RequestTimeout)
	}
}

func TestToClientConfig_SourceAddress(t *testing.T) {
	cfg, err := ToClientConfig(Options{SourceAddress: "127.0.0.1"})
	if err != nil {
		t.Fatalf("ToClientConfig() error = %v", err)
	}
	if cfg.SourceAddress != "127.0.0.1" {
		t.Fatalf("SourceAddress=%q, want 127.0.0.1", cfg.SourceAddress)
	}
}

func TestToClientConfig_NoCheckCertificate(t *testing.T) {
	cfg, err := ToClientConfig(Options{NoCheckCertificate: true})
	if err != nil {
		t.Fatalf("ToClientConfig() error = %v", err)
	}
	if !cfg.InsecureSkipVerify {
		t.Fatalf("InsecureSkipVerify=false, want true")
	}
}

func TestToClientConfig_RequestHeaders(t *testing.T) {
	cfg, err := ToClientConfig(Options{
		UserAgent:  "agent/1.0",
		Referer:    "https://example.com/watch",
		AddHeaders: []string{"X-Test: one", "X-Test: two", "bad-header"},
	})
	if err != nil {
		t.Fatalf("ToClientConfig() error = %v", err)
	}
	if got := cfg.RequestHeaders.Get("User-Agent"); got != "agent/1.0" {
		t.Fatalf("User-Agent=%q, want agent/1.0", got)
	}
	if got := cfg.RequestHeaders.Get("Referer"); got != "https://example.com/watch" {
		t.Fatalf("Referer=%q, want https://example.com/watch", got)
	}
	got := cfg.RequestHeaders.Values("X-Test")
	if len(got) != 2 || got[0] != "one" || got[1] != "two" {
		t.Fatalf("X-Test=%v, want [one two]", got)
	}
	if got := cfg.RequestHeaders.Get("bad-header"); got != "" {
		t.Fatalf("bad-header=%q, want empty", got)
	}
}

func TestToClientConfig_LimitRate(t *testing.T) {
	cfg, err := ToClientConfig(Options{LimitRateBytesPerSecond: 50 * 1024})
	if err != nil {
		t.Fatalf("ToClientConfig() error = %v", err)
	}
	if cfg.DownloadTransport.RateLimitBytesPerSecond != 50*1024 {
		t.Fatalf("RateLimitBytesPerSecond=%d, want %d", cfg.DownloadTransport.RateLimitBytesPerSecond, 50*1024)
	}
}

func TestToClientConfig_BufferAndHTTPChunkSize(t *testing.T) {
	cfg, err := ToClientConfig(Options{
		BufferSizeBytes:    16 * 1024,
		HTTPChunkSizeBytes: 10 * 1024 * 1024,
	})
	if err != nil {
		t.Fatalf("ToClientConfig() error = %v", err)
	}
	if !cfg.DownloadTransport.EnableChunked {
		t.Fatalf("EnableChunked=false, want true")
	}
	if cfg.DownloadTransport.ChunkSize != 10*1024*1024 {
		t.Fatalf("ChunkSize=%d, want 10MiB", cfg.DownloadTransport.ChunkSize)
	}
}

func TestToClientConfig_FragmentControls(t *testing.T) {
	cfg, err := ToClientConfig(Options{
		ConcurrentFragments:      8,
		SkipUnavailableFragments: true,
	})
	if err != nil {
		t.Fatalf("ToClientConfig() error = %v", err)
	}
	if cfg.DownloadTransport.MaxConcurrency != 8 {
		t.Fatalf("MaxConcurrency=%d, want 8", cfg.DownloadTransport.MaxConcurrency)
	}
	if !cfg.DownloadTransport.SkipUnavailableFragments {
		t.Fatalf("SkipUnavailableFragments=false, want true")
	}
}

func TestToClientConfig_PostprocessorArgsForFFmpegMuxer(t *testing.T) {
	cfg, err := ToClientConfig(Options{
		PostprocessorArgs: []string{
			"Merger+ffmpeg:-movflags +faststart",
			"Metadata:-ignored yes",
			`--flag "two words"`,
		},
	})
	if err != nil {
		t.Fatalf("ToClientConfig() error = %v", err)
	}
	ffmpegMuxer, ok := cfg.Muxer.(*muxer.FFmpegMuxer)
	if !ok {
		t.Fatalf("Muxer=%T, want *muxer.FFmpegMuxer", cfg.Muxer)
	}
	got := strings.Join(ffmpegMuxer.ExtraArgs, "\x00")
	want := strings.Join([]string{"-movflags", "+faststart", "--flag", "two words"}, "\x00")
	if got != want {
		t.Fatalf("ExtraArgs=%q, want %q", got, want)
	}
}

func TestToClientConfig_SubtitlePolicyFromFlags(t *testing.T) {
	cfg, err := ToClientConfig(Options{
		SubLangs:      "ko, en ,ko",
		WriteAutoSubs: true,
	})
	if err != nil {
		t.Fatalf("ToClientConfig() error = %v", err)
	}
	if cfg.SubtitlePolicy.PreferredLanguageCode != "ko" {
		t.Fatalf("preferred language=%q, want ko", cfg.SubtitlePolicy.PreferredLanguageCode)
	}
	if len(cfg.SubtitlePolicy.FallbackLanguageCodes) != 1 || cfg.SubtitlePolicy.FallbackLanguageCodes[0] != "en" {
		t.Fatalf("fallback languages=%v, want [en]", cfg.SubtitlePolicy.FallbackLanguageCodes)
	}
	if !cfg.SubtitlePolicy.PreferAutoGenerated {
		t.Fatalf("PreferAutoGenerated=%v, want true", cfg.SubtitlePolicy.PreferAutoGenerated)
	}
}

func TestParseFlags_ShortJEnablesPrintJSON(t *testing.T) {
	origArgs := os.Args
	origFlagSet := flag.CommandLine
	defer func() {
		os.Args = origArgs
		flag.CommandLine = origFlagSet
	}()

	os.Args = []string{"ytv1", "-J", "jNQXAC9IVRw"}
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	flag.CommandLine.SetOutput(io.Discard)

	opts := ParseFlags()
	if !opts.PrintJSON {
		t.Fatalf("PrintJSON=%v, want true", opts.PrintJSON)
	}
	if len(opts.URLs) != 1 || opts.URLs[0] != "jNQXAC9IVRw" {
		t.Fatalf("URLs=%v, want [jNQXAC9IVRw]", opts.URLs)
	}
}

func TestParseFlags_YTDLPCompatibilityAliases(t *testing.T) {
	origArgs := os.Args
	origFlagSet := flag.CommandLine
	defer func() {
		os.Args = origArgs
		flag.CommandLine = origFlagSet
	}()

	os.Args = []string{"ytv1", "--flat-playlist", "--extract-flat", "--no-playlist", "--yes-playlist", "--ignore-errors", "--continue", "-j", "--dump-json", "jNQXAC9IVRw"}
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	flag.CommandLine.SetOutput(io.Discard)

	opts := ParseFlags()
	if !opts.FlatPlaylist {
		t.Fatalf("FlatPlaylist=%v, want true", opts.FlatPlaylist)
	}
	if opts.NoPlaylist {
		t.Fatalf("NoPlaylist=%v, want false because --yes-playlist overrides it", opts.NoPlaylist)
	}
	if !opts.YesPlaylist {
		t.Fatalf("YesPlaylist=%v, want true", opts.YesPlaylist)
	}
	if opts.AbortOnError {
		t.Fatalf("AbortOnError=%v, want false because --ignore-errors overrides it", opts.AbortOnError)
	}
	if opts.NoContinue {
		t.Fatalf("NoContinue=%v, want false", opts.NoContinue)
	}
	if !opts.PrintJSON {
		t.Fatalf("PrintJSON=%v, want true from -j/--dump-json", opts.PrintJSON)
	}
	if len(opts.URLs) != 1 || opts.URLs[0] != "jNQXAC9IVRw" {
		t.Fatalf("URLs=%v, want [jNQXAC9IVRw]", opts.URLs)
	}
}

func TestParseFlags_SubFormatSingleDashCompatibility(t *testing.T) {
	origArgs := os.Args
	origFlagSet := flag.CommandLine
	defer func() {
		os.Args = origArgs
		flag.CommandLine = origFlagSet
	}()

	os.Args = []string{"ytv1", "-sub-format", "vtt", "jNQXAC9IVRw"}
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	flag.CommandLine.SetOutput(io.Discard)

	opts := ParseFlags()
	if opts.SubFormat != "vtt" {
		t.Fatalf("SubFormat=%q, want vtt", opts.SubFormat)
	}
	if len(opts.URLs) != 1 || opts.URLs[0] != "jNQXAC9IVRw" {
		t.Fatalf("URLs=%v, want [jNQXAC9IVRw]", opts.URLs)
	}
}

func TestParseFlags_ConvertSubsAliases(t *testing.T) {
	for _, flagName := range []string{"--convert-subs", "--convert-sub", "--convert-subtitles"} {
		t.Run(flagName, func(t *testing.T) {
			origArgs := os.Args
			origFlagSet := flag.CommandLine
			defer func() {
				os.Args = origArgs
				flag.CommandLine = origFlagSet
			}()

			os.Args = []string{"ytv1", flagName, "srt", "jNQXAC9IVRw"}
			flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
			flag.CommandLine.SetOutput(io.Discard)

			opts := ParseFlags()
			if opts.SubFormat != "srt" {
				t.Fatalf("SubFormat=%q, want srt", opts.SubFormat)
			}
			if len(opts.URLs) != 1 || opts.URLs[0] != "jNQXAC9IVRw" {
				t.Fatalf("URLs=%v, want [jNQXAC9IVRw]", opts.URLs)
			}
		})
	}
}

func TestParseFlags_ExtractAudio(t *testing.T) {
	cases := []struct {
		name        string
		args        []string
		wantExtract bool
		wantFormat  string
	}{
		{
			name:        "long",
			args:        []string{"ytv1", "--extract-audio", "jNQXAC9IVRw"},
			wantExtract: true,
			wantFormat:  "best",
		},
		{
			name:        "short mp3",
			args:        []string{"ytv1", "-x", "--audio-format", "mp3", "jNQXAC9IVRw"},
			wantExtract: true,
			wantFormat:  "mp3",
		},
		{
			name:        "equals format",
			args:        []string{"ytv1", "--extract-audio", "--audio-format=m4a", "jNQXAC9IVRw"},
			wantExtract: true,
			wantFormat:  "m4a",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			origArgs := os.Args
			origFlagSet := flag.CommandLine
			defer func() {
				os.Args = origArgs
				flag.CommandLine = origFlagSet
			}()

			os.Args = tc.args
			flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
			flag.CommandLine.SetOutput(io.Discard)

			opts := ParseFlags()
			if opts.ExtractAudio != tc.wantExtract {
				t.Fatalf("ExtractAudio=%v, want %v", opts.ExtractAudio, tc.wantExtract)
			}
			if opts.AudioFormat != tc.wantFormat {
				t.Fatalf("AudioFormat=%q, want %q", opts.AudioFormat, tc.wantFormat)
			}
			if len(opts.URLs) != 1 || opts.URLs[0] != "jNQXAC9IVRw" {
				t.Fatalf("URLs=%v, want [jNQXAC9IVRw]", opts.URLs)
			}
		})
	}
}

func TestParseFlags_AudioQualityLastFlagWins(t *testing.T) {
	origArgs := os.Args
	origFlagSet := flag.CommandLine
	defer func() {
		os.Args = origArgs
		flag.CommandLine = origFlagSet
	}()

	os.Args = []string{"ytv1", "-x", "--audio-format", "mp3", "--audio-quality", "0", "--audio-quality=192K", "jNQXAC9IVRw"}
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	flag.CommandLine.SetOutput(io.Discard)

	opts := ParseFlags()
	if opts.AudioQuality != "192K" {
		t.Fatalf("AudioQuality=%q, want 192K", opts.AudioQuality)
	}
	if len(opts.URLs) != 1 || opts.URLs[0] != "jNQXAC9IVRw" {
		t.Fatalf("URLs=%v, want [jNQXAC9IVRw]", opts.URLs)
	}
}

func TestParseFlags_EmbedMetadataLastFlagWins(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want bool
	}{
		{
			name: "embed alias enables",
			args: []string{"ytv1", "--add-metadata", "jNQXAC9IVRw"},
			want: true,
		},
		{
			name: "negative disables",
			args: []string{"ytv1", "--embed-metadata", "--no-embed-metadata", "jNQXAC9IVRw"},
			want: false,
		},
		{
			name: "later positive re-enables",
			args: []string{"ytv1", "--no-add-metadata", "--embed-metadata", "jNQXAC9IVRw"},
			want: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			origArgs := os.Args
			origFlagSet := flag.CommandLine
			defer func() {
				os.Args = origArgs
				flag.CommandLine = origFlagSet
			}()

			os.Args = tc.args
			flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
			flag.CommandLine.SetOutput(io.Discard)

			opts := ParseFlags()
			if opts.EmbedMetadata != tc.want {
				t.Fatalf("EmbedMetadata=%v, want %v", opts.EmbedMetadata, tc.want)
			}
			if len(opts.URLs) != 1 || opts.URLs[0] != "jNQXAC9IVRw" {
				t.Fatalf("URLs=%v, want [jNQXAC9IVRw]", opts.URLs)
			}
		})
	}
}

func TestParseFlags_PostprocessorArgs(t *testing.T) {
	origArgs := os.Args
	origFlagSet := flag.CommandLine
	defer func() {
		os.Args = origArgs
		flag.CommandLine = origFlagSet
	}()

	os.Args = []string{"ytv1", "--postprocessor-args", "Merger+ffmpeg:-movflags +faststart", "--ppa=FFmpeg:-hide_banner", "jNQXAC9IVRw"}
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	flag.CommandLine.SetOutput(io.Discard)

	opts := ParseFlags()
	got := strings.Join(opts.PostprocessorArgs, "\x00")
	want := strings.Join([]string{"Merger+ffmpeg:-movflags +faststart", "FFmpeg:-hide_banner"}, "\x00")
	if got != want {
		t.Fatalf("PostprocessorArgs=%q, want %q", got, want)
	}
	if len(opts.URLs) != 1 || opts.URLs[0] != "jNQXAC9IVRw" {
		t.Fatalf("URLs=%v, want [jNQXAC9IVRw]", opts.URLs)
	}
}

func TestParseFlags_MergeOutputFormat(t *testing.T) {
	origArgs := os.Args
	origFlagSet := flag.CommandLine
	defer func() {
		os.Args = origArgs
		flag.CommandLine = origFlagSet
	}()

	os.Args = []string{"ytv1", "--merge-output-format", "webm", "--merge-output-format=mkv", "jNQXAC9IVRw"}
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	flag.CommandLine.SetOutput(io.Discard)

	opts := ParseFlags()
	if opts.MergeOutputFormat != "mkv" {
		t.Fatalf("MergeOutputFormat=%q, want mkv", opts.MergeOutputFormat)
	}
	if len(opts.URLs) != 1 || opts.URLs[0] != "jNQXAC9IVRw" {
		t.Fatalf("URLs=%v, want [jNQXAC9IVRw]", opts.URLs)
	}
}

func TestParseFlags_RemuxVideo(t *testing.T) {
	origArgs := os.Args
	origFlagSet := flag.CommandLine
	defer func() {
		os.Args = origArgs
		flag.CommandLine = origFlagSet
	}()

	os.Args = []string{"ytv1", "--remux-video", "mp4", "--remux-video=mkv", "jNQXAC9IVRw"}
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	flag.CommandLine.SetOutput(io.Discard)

	opts := ParseFlags()
	if opts.RemuxVideo != "mkv" {
		t.Fatalf("RemuxVideo=%q, want mkv", opts.RemuxVideo)
	}
	if len(opts.URLs) != 1 || opts.URLs[0] != "jNQXAC9IVRw" {
		t.Fatalf("URLs=%v, want [jNQXAC9IVRw]", opts.URLs)
	}
}

func TestParseFlags_KeepVideoLastFlagWins(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want bool
	}{
		{
			name: "short enables",
			args: []string{"ytv1", "-k", "jNQXAC9IVRw"},
			want: true,
		},
		{
			name: "negative disables",
			args: []string{"ytv1", "--keep-video", "--no-keep-video", "jNQXAC9IVRw"},
			want: false,
		},
		{
			name: "later positive reenables",
			args: []string{"ytv1", "--no-keep-video", "--keep-video", "jNQXAC9IVRw"},
			want: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			origArgs := os.Args
			origFlagSet := flag.CommandLine
			defer func() {
				os.Args = origArgs
				flag.CommandLine = origFlagSet
			}()

			os.Args = tc.args
			flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
			flag.CommandLine.SetOutput(io.Discard)

			opts := ParseFlags()
			if opts.KeepVideo != tc.want {
				t.Fatalf("KeepVideo=%v, want %v", opts.KeepVideo, tc.want)
			}
			if len(opts.URLs) != 1 || opts.URLs[0] != "jNQXAC9IVRw" {
				t.Fatalf("URLs=%v, want [jNQXAC9IVRw]", opts.URLs)
			}
		})
	}
}

func TestParseFlags_PostOverwritesLastFlagWins(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want bool
	}{
		{
			name: "disable post overwrites",
			args: []string{"ytv1", "--no-post-overwrites", "jNQXAC9IVRw"},
			want: true,
		},
		{
			name: "later post overwrites reenables",
			args: []string{"ytv1", "--no-post-overwrites", "--post-overwrites", "jNQXAC9IVRw"},
			want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			origArgs := os.Args
			origFlagSet := flag.CommandLine
			defer func() {
				os.Args = origArgs
				flag.CommandLine = origFlagSet
			}()

			os.Args = tc.args
			flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
			flag.CommandLine.SetOutput(io.Discard)

			opts := ParseFlags()
			if opts.NoPostOverwrites != tc.want {
				t.Fatalf("NoPostOverwrites=%v, want %v", opts.NoPostOverwrites, tc.want)
			}
			if len(opts.URLs) != 1 || opts.URLs[0] != "jNQXAC9IVRw" {
				t.Fatalf("URLs=%v, want [jNQXAC9IVRw]", opts.URLs)
			}
		})
	}
}

func TestParseFlags_LimitRate(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want int64
	}{
		{
			name: "short",
			args: []string{"ytv1", "-r", "50K", "jNQXAC9IVRw"},
			want: 50 * 1024,
		},
		{
			name: "long decimal",
			args: []string{"ytv1", "--limit-rate", "1M", "jNQXAC9IVRw"},
			want: 1024 * 1024,
		},
		{
			name: "alias binary",
			args: []string{"ytv1", "--rate-limit=100KiB", "jNQXAC9IVRw"},
			want: 100 * 1024,
		},
		{
			name: "last wins invalid disables",
			args: []string{"ytv1", "--limit-rate", "1M", "--rate-limit", "bad", "jNQXAC9IVRw"},
			want: 0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			origArgs := os.Args
			origFlagSet := flag.CommandLine
			defer func() {
				os.Args = origArgs
				flag.CommandLine = origFlagSet
			}()

			os.Args = tc.args
			flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
			flag.CommandLine.SetOutput(io.Discard)

			opts := ParseFlags()
			if opts.LimitRateBytesPerSecond != tc.want {
				t.Fatalf("LimitRateBytesPerSecond=%d, want %d", opts.LimitRateBytesPerSecond, tc.want)
			}
			if len(opts.URLs) != 1 || opts.URLs[0] != "jNQXAC9IVRw" {
				t.Fatalf("URLs=%v, want [jNQXAC9IVRw]", opts.URLs)
			}
		})
	}
}

func TestParseFlags_SocketTimeout(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want time.Duration
	}{
		{
			name: "seconds",
			args: []string{"ytv1", "--socket-timeout", "1.5", "jNQXAC9IVRw"},
			want: 1500 * time.Millisecond,
		},
		{
			name: "equals",
			args: []string{"ytv1", "--socket-timeout=2", "jNQXAC9IVRw"},
			want: 2 * time.Second,
		},
		{
			name: "invalid ignored",
			args: []string{"ytv1", "--socket-timeout", "bad", "jNQXAC9IVRw"},
			want: 0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			origArgs := os.Args
			origFlagSet := flag.CommandLine
			defer func() {
				os.Args = origArgs
				flag.CommandLine = origFlagSet
			}()

			os.Args = tc.args
			flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
			flag.CommandLine.SetOutput(io.Discard)

			opts := ParseFlags()
			if opts.SocketTimeout != tc.want {
				t.Fatalf("SocketTimeout=%s, want %s", opts.SocketTimeout, tc.want)
			}
			if len(opts.URLs) != 1 || opts.URLs[0] != "jNQXAC9IVRw" {
				t.Fatalf("URLs=%v, want [jNQXAC9IVRw]", opts.URLs)
			}
		})
	}
}

func TestParseFlags_SourceAddress(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "source address",
			args: []string{"ytv1", "--source-address", "127.0.0.1", "jNQXAC9IVRw"},
			want: "127.0.0.1",
		},
		{
			name: "force ipv4",
			args: []string{"ytv1", "-4", "jNQXAC9IVRw"},
			want: "0.0.0.0",
		},
		{
			name: "force ipv6",
			args: []string{"ytv1", "--force-ipv6", "jNQXAC9IVRw"},
			want: "::",
		},
		{
			name: "last wins",
			args: []string{"ytv1", "--source-address", "127.0.0.1", "-6", "-4", "jNQXAC9IVRw"},
			want: "0.0.0.0",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			origArgs := os.Args
			origFlagSet := flag.CommandLine
			defer func() {
				os.Args = origArgs
				flag.CommandLine = origFlagSet
			}()

			os.Args = tc.args
			flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
			flag.CommandLine.SetOutput(io.Discard)

			opts := ParseFlags()
			if opts.SourceAddress != tc.want {
				t.Fatalf("SourceAddress=%q, want %q", opts.SourceAddress, tc.want)
			}
			if len(opts.URLs) != 1 || opts.URLs[0] != "jNQXAC9IVRw" {
				t.Fatalf("URLs=%v, want [jNQXAC9IVRw]", opts.URLs)
			}
		})
	}
}

func TestParseFlags_NoCheckCertificates(t *testing.T) {
	origArgs := os.Args
	origFlagSet := flag.CommandLine
	defer func() {
		os.Args = origArgs
		flag.CommandLine = origFlagSet
	}()

	os.Args = []string{"ytv1", "--no-check-certificates", "jNQXAC9IVRw"}
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	flag.CommandLine.SetOutput(io.Discard)

	opts := ParseFlags()
	if !opts.NoCheckCertificate {
		t.Fatalf("NoCheckCertificate=false, want true")
	}
	if len(opts.URLs) != 1 || opts.URLs[0] != "jNQXAC9IVRw" {
		t.Fatalf("URLs=%v, want [jNQXAC9IVRw]", opts.URLs)
	}
}

func TestParseFlags_RequestHeaders(t *testing.T) {
	origArgs := os.Args
	origFlagSet := flag.CommandLine
	defer func() {
		os.Args = origArgs
		flag.CommandLine = origFlagSet
	}()

	os.Args = []string{
		"ytv1",
		"--user-agent", "agent/1.0",
		"--referer=https://example.com/watch",
		"--add-headers", "X-Test: one",
		"--add-headers=X-Test: two",
		"jNQXAC9IVRw",
	}
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	flag.CommandLine.SetOutput(io.Discard)

	opts := ParseFlags()
	if opts.UserAgent != "agent/1.0" {
		t.Fatalf("UserAgent=%q, want agent/1.0", opts.UserAgent)
	}
	if opts.Referer != "https://example.com/watch" {
		t.Fatalf("Referer=%q, want https://example.com/watch", opts.Referer)
	}
	if len(opts.AddHeaders) != 2 || opts.AddHeaders[0] != "X-Test: one" || opts.AddHeaders[1] != "X-Test: two" {
		t.Fatalf("AddHeaders=%v, want [X-Test: one X-Test: two]", opts.AddHeaders)
	}
	if len(opts.URLs) != 1 || opts.URLs[0] != "jNQXAC9IVRw" {
		t.Fatalf("URLs=%v, want [jNQXAC9IVRw]", opts.URLs)
	}
}

func TestParseFlags_BufferAndHTTPChunkSize(t *testing.T) {
	origArgs := os.Args
	origFlagSet := flag.CommandLine
	defer func() {
		os.Args = origArgs
		flag.CommandLine = origFlagSet
	}()

	os.Args = []string{
		"ytv1",
		"--buffer-size", "16K",
		"--http-chunk-size=10M",
		"--resize-buffer",
		"--no-resize-buffer",
		"jNQXAC9IVRw",
	}
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	flag.CommandLine.SetOutput(io.Discard)

	opts := ParseFlags()
	if opts.BufferSizeBytes != 16*1024 {
		t.Fatalf("BufferSizeBytes=%d, want 16KiB", opts.BufferSizeBytes)
	}
	if opts.HTTPChunkSizeBytes != 10*1024*1024 {
		t.Fatalf("HTTPChunkSizeBytes=%d, want 10MiB", opts.HTTPChunkSizeBytes)
	}
	if !opts.NoResizeBuffer {
		t.Fatalf("NoResizeBuffer=false, want true")
	}
	if len(opts.URLs) != 1 || opts.URLs[0] != "jNQXAC9IVRw" {
		t.Fatalf("URLs=%v, want [jNQXAC9IVRw]", opts.URLs)
	}
}

func TestParseFlags_FragmentControls(t *testing.T) {
	cases := []struct {
		name             string
		args             []string
		wantN            int
		wantSkip         bool
		wantER           int
		wantERSet        bool
		wantFAR          int
		wantFARSet       bool
		wantFR           int
		wantFRSet        bool
		wantHTTPRetry    time.Duration
		wantHTTPSet      bool
		wantFragSleep    time.Duration
		wantFragSleepSet bool
		wantExtSleep     time.Duration
		wantExtSleepSet  bool
	}{
		{
			name:     "concurrency and default skip",
			args:     []string{"ytv1", "-N", "8", "jNQXAC9IVRw"},
			wantN:    8,
			wantSkip: true,
		},
		{
			name:     "long concurrency",
			args:     []string{"ytv1", "--concurrent-fragments", "3", "jNQXAC9IVRw"},
			wantN:    3,
			wantSkip: true,
		},
		{
			name:     "abort disables skip",
			args:     []string{"ytv1", "--skip-unavailable-fragments", "--abort-on-unavailable-fragments", "jNQXAC9IVRw"},
			wantSkip: false,
		},
		{
			name:     "later skip reenables",
			args:     []string{"ytv1", "--no-skip-unavailable-fragments", "--no-abort-on-unavailable-fragments", "jNQXAC9IVRw"},
			wantSkip: true,
		},
		{
			name:      "fragment retries",
			args:      []string{"ytv1", "--retries", "3", "--fragment-retries", "7", "jNQXAC9IVRw"},
			wantSkip:  true,
			wantFR:    7,
			wantFRSet: true,
		},
		{
			name:      "extractor retries",
			args:      []string{"ytv1", "--retries", "3", "--extractor-retries", "5", "jNQXAC9IVRw"},
			wantSkip:  true,
			wantER:    5,
			wantERSet: true,
		},
		{
			name:       "file access retries",
			args:       []string{"ytv1", "--file-access-retries", "6", "jNQXAC9IVRw"},
			wantSkip:   true,
			wantFAR:    6,
			wantFARSet: true,
		},
		{
			name:     "invalid fragment retries ignored",
			args:     []string{"ytv1", "--fragment-retries", "bad", "jNQXAC9IVRw"},
			wantSkip: true,
		},
		{
			name:     "invalid file access retries ignored",
			args:     []string{"ytv1", "--file-access-retries", "bad", "jNQXAC9IVRw"},
			wantSkip: true,
		},
		{
			name:     "invalid extractor retries ignored",
			args:     []string{"ytv1", "--extractor-retries", "bad", "jNQXAC9IVRw"},
			wantSkip: true,
		},
		{
			name:             "retry sleep types",
			args:             []string{"ytv1", "--retry-sleep", "1.5", "--retry-sleep", "fragment:exp=2:20", "--retry-sleep", "extractor:linear=3::2", "jNQXAC9IVRw"},
			wantSkip:         true,
			wantHTTPRetry:    1500 * time.Millisecond,
			wantHTTPSet:      true,
			wantFragSleep:    2 * time.Second,
			wantFragSleepSet: true,
			wantExtSleep:     3 * time.Second,
			wantExtSleepSet:  true,
		},
		{
			name:     "invalid retry sleep ignored",
			args:     []string{"ytv1", "--retry-sleep", "fragment:bad", "--retry-sleep", "file_access:1", "jNQXAC9IVRw"},
			wantSkip: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			origArgs := os.Args
			origFlagSet := flag.CommandLine
			defer func() {
				os.Args = origArgs
				flag.CommandLine = origFlagSet
			}()

			os.Args = tc.args
			flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
			flag.CommandLine.SetOutput(io.Discard)

			opts := ParseFlags()
			if opts.ConcurrentFragments != tc.wantN {
				t.Fatalf("ConcurrentFragments=%d, want %d", opts.ConcurrentFragments, tc.wantN)
			}
			if opts.SkipUnavailableFragments != tc.wantSkip {
				t.Fatalf("SkipUnavailableFragments=%v, want %v", opts.SkipUnavailableFragments, tc.wantSkip)
			}
			if opts.ExtractorRetries != tc.wantER {
				t.Fatalf("ExtractorRetries=%d, want %d", opts.ExtractorRetries, tc.wantER)
			}
			if opts.ExtractorRetriesSet != tc.wantERSet {
				t.Fatalf("ExtractorRetriesSet=%v, want %v", opts.ExtractorRetriesSet, tc.wantERSet)
			}
			if opts.FileAccessRetries != tc.wantFAR {
				t.Fatalf("FileAccessRetries=%d, want %d", opts.FileAccessRetries, tc.wantFAR)
			}
			if opts.FileAccessRetriesSet != tc.wantFARSet {
				t.Fatalf("FileAccessRetriesSet=%v, want %v", opts.FileAccessRetriesSet, tc.wantFARSet)
			}
			if opts.FragmentRetries != tc.wantFR {
				t.Fatalf("FragmentRetries=%d, want %d", opts.FragmentRetries, tc.wantFR)
			}
			if opts.FragmentRetriesSet != tc.wantFRSet {
				t.Fatalf("FragmentRetriesSet=%v, want %v", opts.FragmentRetriesSet, tc.wantFRSet)
			}
			if opts.HTTPRetrySleep != tc.wantHTTPRetry {
				t.Fatalf("HTTPRetrySleep=%s, want %s", opts.HTTPRetrySleep, tc.wantHTTPRetry)
			}
			if opts.HTTPRetrySleepSet != tc.wantHTTPSet {
				t.Fatalf("HTTPRetrySleepSet=%v, want %v", opts.HTTPRetrySleepSet, tc.wantHTTPSet)
			}
			if opts.FragmentRetrySleep != tc.wantFragSleep {
				t.Fatalf("FragmentRetrySleep=%s, want %s", opts.FragmentRetrySleep, tc.wantFragSleep)
			}
			if opts.FragmentRetrySleepSet != tc.wantFragSleepSet {
				t.Fatalf("FragmentRetrySleepSet=%v, want %v", opts.FragmentRetrySleepSet, tc.wantFragSleepSet)
			}
			if opts.ExtractorRetrySleep != tc.wantExtSleep {
				t.Fatalf("ExtractorRetrySleep=%s, want %s", opts.ExtractorRetrySleep, tc.wantExtSleep)
			}
			if opts.ExtractorRetrySleepSet != tc.wantExtSleepSet {
				t.Fatalf("ExtractorRetrySleepSet=%v, want %v", opts.ExtractorRetrySleepSet, tc.wantExtSleepSet)
			}
			if len(opts.URLs) != 1 || opts.URLs[0] != "jNQXAC9IVRw" {
				t.Fatalf("URLs=%v, want [jNQXAC9IVRw]", opts.URLs)
			}
		})
	}
}

func TestParseFlags_SleepIntervals(t *testing.T) {
	origArgs := os.Args
	origFlagSet := flag.CommandLine
	defer func() {
		os.Args = origArgs
		flag.CommandLine = origFlagSet
	}()

	os.Args = []string{
		"ytv1",
		"--sleep-requests", "0.75",
		"--sleep-interval", "1.5",
		"--min-sleep-interval=2",
		"--max-sleep-interval", "3.25",
		"--sleep-subtitles", "0.5",
		"jNQXAC9IVRw",
	}
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	flag.CommandLine.SetOutput(io.Discard)

	opts := ParseFlags()
	if opts.SleepRequests != 750*time.Millisecond {
		t.Fatalf("SleepRequests=%s, want 750ms", opts.SleepRequests)
	}
	if opts.SleepInterval != 2*time.Second {
		t.Fatalf("SleepInterval=%s, want 2s", opts.SleepInterval)
	}
	if opts.MaxSleepInterval != 3250*time.Millisecond {
		t.Fatalf("MaxSleepInterval=%s, want 3.25s", opts.MaxSleepInterval)
	}
	if opts.SleepSubtitles != 500*time.Millisecond {
		t.Fatalf("SleepSubtitles=%s, want 500ms", opts.SleepSubtitles)
	}
	if len(opts.URLs) != 1 || opts.URLs[0] != "jNQXAC9IVRw" {
		t.Fatalf("URLs=%v, want [jNQXAC9IVRw]", opts.URLs)
	}
}

func TestParseFlags_SubFormatLastFlagWins(t *testing.T) {
	origArgs := os.Args
	origFlagSet := flag.CommandLine
	defer func() {
		os.Args = origArgs
		flag.CommandLine = origFlagSet
	}()

	os.Args = []string{"ytv1", "--convert-subs", "srt", "--sub-format", "vtt", "jNQXAC9IVRw"}
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	flag.CommandLine.SetOutput(io.Discard)

	opts := ParseFlags()
	if opts.SubFormat != "vtt" {
		t.Fatalf("SubFormat=%q, want vtt", opts.SubFormat)
	}
}

func TestParseFlags_SubLangsAliasSingleDashCompatibility(t *testing.T) {
	origArgs := os.Args
	origFlagSet := flag.CommandLine
	defer func() {
		os.Args = origArgs
		flag.CommandLine = origFlagSet
	}()

	os.Args = []string{"ytv1", "-sub-langs", "ko,en", "jNQXAC9IVRw"}
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	flag.CommandLine.SetOutput(io.Discard)

	opts := ParseFlags()
	if opts.SubLangs != "ko,en" {
		t.Fatalf("SubLangs=%q, want ko,en", opts.SubLangs)
	}
	if len(opts.URLs) != 1 || opts.URLs[0] != "jNQXAC9IVRw" {
		t.Fatalf("URLs=%v, want [jNQXAC9IVRw]", opts.URLs)
	}
}

func TestParseFlags_WriteSRTAliasForcesSRTOutput(t *testing.T) {
	origArgs := os.Args
	origFlagSet := flag.CommandLine
	defer func() {
		os.Args = origArgs
		flag.CommandLine = origFlagSet
	}()

	os.Args = []string{"ytv1", "-write-srt", "-sub-format", "vtt", "jNQXAC9IVRw"}
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	flag.CommandLine.SetOutput(io.Discard)

	opts := ParseFlags()
	if !opts.WriteSubs {
		t.Fatalf("WriteSubs=%v, want true", opts.WriteSubs)
	}
	if opts.SubFormat != "srt" {
		t.Fatalf("SubFormat=%q, want srt", opts.SubFormat)
	}
	if len(opts.URLs) != 1 || opts.URLs[0] != "jNQXAC9IVRw" {
		t.Fatalf("URLs=%v, want [jNQXAC9IVRw]", opts.URLs)
	}
}

func TestParseFlags_DumpSingleJSON(t *testing.T) {
	origArgs := os.Args
	origFlagSet := flag.CommandLine
	defer func() {
		os.Args = origArgs
		flag.CommandLine = origFlagSet
	}()

	os.Args = []string{"ytv1", "--dump-single-json", "jNQXAC9IVRw"}
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	flag.CommandLine.SetOutput(io.Discard)

	opts := ParseFlags()
	if !opts.DumpSingleJSON {
		t.Fatalf("DumpSingleJSON=%v, want true", opts.DumpSingleJSON)
	}
	if len(opts.URLs) != 1 || opts.URLs[0] != "jNQXAC9IVRw" {
		t.Fatalf("URLs=%v, want [jNQXAC9IVRw]", opts.URLs)
	}
}

func TestParseFlags_WriteInfoJSON(t *testing.T) {
	origArgs := os.Args
	origFlagSet := flag.CommandLine
	defer func() {
		os.Args = origArgs
		flag.CommandLine = origFlagSet
	}()

	os.Args = []string{"ytv1", "--write-info-json", "--skip-download", "jNQXAC9IVRw"}
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	flag.CommandLine.SetOutput(io.Discard)

	opts := ParseFlags()
	if !opts.WriteInfoJSON {
		t.Fatalf("WriteInfoJSON=%v, want true", opts.WriteInfoJSON)
	}
	if !opts.SkipDownload {
		t.Fatalf("SkipDownload=%v, want true", opts.SkipDownload)
	}
	if len(opts.URLs) != 1 || opts.URLs[0] != "jNQXAC9IVRw" {
		t.Fatalf("URLs=%v, want [jNQXAC9IVRw]", opts.URLs)
	}
}

func TestParseFlags_NoWriteInfoJSONLastFlagWins(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want bool
	}{
		{
			name: "disable after write",
			args: []string{"ytv1", "--write-info-json", "--no-write-info-json", "jNQXAC9IVRw"},
			want: false,
		},
		{
			name: "reenable after no write",
			args: []string{"ytv1", "--no-write-info-json", "--write-info-json", "jNQXAC9IVRw"},
			want: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			origArgs := os.Args
			origFlagSet := flag.CommandLine
			defer func() {
				os.Args = origArgs
				flag.CommandLine = origFlagSet
			}()

			os.Args = tc.args
			flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
			flag.CommandLine.SetOutput(io.Discard)

			opts := ParseFlags()
			if opts.WriteInfoJSON != tc.want {
				t.Fatalf("WriteInfoJSON=%v, want %v", opts.WriteInfoJSON, tc.want)
			}
			if len(opts.URLs) != 1 || opts.URLs[0] != "jNQXAC9IVRw" {
				t.Fatalf("URLs=%v, want [jNQXAC9IVRw]", opts.URLs)
			}
		})
	}
}

func TestParseFlags_NoDownloadAlias(t *testing.T) {
	for _, flagName := range []string{"--no-download", "--simulate", "-s"} {
		t.Run(flagName, func(t *testing.T) {
			origArgs := os.Args
			origFlagSet := flag.CommandLine
			defer func() {
				os.Args = origArgs
				flag.CommandLine = origFlagSet
			}()

			os.Args = []string{"ytv1", flagName, "jNQXAC9IVRw"}
			flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
			flag.CommandLine.SetOutput(io.Discard)

			opts := ParseFlags()
			if !opts.SkipDownload {
				t.Fatalf("SkipDownload=%v, want true", opts.SkipDownload)
			}
			if len(opts.URLs) != 1 || opts.URLs[0] != "jNQXAC9IVRw" {
				t.Fatalf("URLs=%v, want [jNQXAC9IVRw]", opts.URLs)
			}
		})
	}
}

func TestParseFlags_QuietAliases(t *testing.T) {
	for _, flagName := range []string{"--quiet", "-q"} {
		t.Run(flagName, func(t *testing.T) {
			origArgs := os.Args
			origFlagSet := flag.CommandLine
			defer func() {
				os.Args = origArgs
				flag.CommandLine = origFlagSet
			}()

			os.Args = []string{"ytv1", flagName, "jNQXAC9IVRw"}
			flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
			flag.CommandLine.SetOutput(io.Discard)

			opts := ParseFlags()
			if !opts.Quiet {
				t.Fatalf("Quiet=%v, want true", opts.Quiet)
			}
			if len(opts.URLs) != 1 || opts.URLs[0] != "jNQXAC9IVRw" {
				t.Fatalf("URLs=%v, want [jNQXAC9IVRw]", opts.URLs)
			}
		})
	}
}

func TestParseFlags_BatchFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "urls.txt")
	if err := os.WriteFile(path, []byte("# comment\n\nfirst\n second \n"), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	origArgs := os.Args
	origFlagSet := flag.CommandLine
	defer func() {
		os.Args = origArgs
		flag.CommandLine = origFlagSet
	}()

	os.Args = []string{"ytv1", "--batch-file", path, "third"}
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	flag.CommandLine.SetOutput(io.Discard)

	opts := ParseFlags()
	if opts.BatchFile != path {
		t.Fatalf("BatchFile=%q, want %q", opts.BatchFile, path)
	}
	if strings.Join(opts.URLs, ",") != "first,second,third" {
		t.Fatalf("URLs=%v, want batch URLs before positional URLs", opts.URLs)
	}
}

func TestParseFlags_NoBatchFileLastFlagWins(t *testing.T) {
	path := filepath.Join(t.TempDir(), "urls.txt")
	if err := os.WriteFile(path, []byte("first\n"), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	origArgs := os.Args
	origFlagSet := flag.CommandLine
	defer func() {
		os.Args = origArgs
		flag.CommandLine = origFlagSet
	}()

	os.Args = []string{"ytv1", "-a", path, "--no-batch-file", "second"}
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	flag.CommandLine.SetOutput(io.Discard)

	opts := ParseFlags()
	if opts.BatchFile != "" {
		t.Fatalf("BatchFile=%q, want empty", opts.BatchFile)
	}
	if strings.Join(opts.URLs, ",") != "second" {
		t.Fatalf("URLs=%v, want only positional URL", opts.URLs)
	}
}

func TestParseFlags_SimulateLastFlagWins(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want bool
	}{
		{
			name: "disable after simulate",
			args: []string{"ytv1", "--simulate", "--no-simulate", "jNQXAC9IVRw"},
			want: false,
		},
		{
			name: "reenable after no simulate",
			args: []string{"ytv1", "--no-simulate", "-s", "jNQXAC9IVRw"},
			want: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			origArgs := os.Args
			origFlagSet := flag.CommandLine
			defer func() {
				os.Args = origArgs
				flag.CommandLine = origFlagSet
			}()

			os.Args = tc.args
			flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
			flag.CommandLine.SetOutput(io.Discard)

			opts := ParseFlags()
			if opts.SkipDownload != tc.want {
				t.Fatalf("SkipDownload=%v, want %v", opts.SkipDownload, tc.want)
			}
			if len(opts.URLs) != 1 || opts.URLs[0] != "jNQXAC9IVRw" {
				t.Fatalf("URLs=%v, want [jNQXAC9IVRw]", opts.URLs)
			}
		})
	}
}

func TestParseFlags_QuietLastFlagWins(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want bool
	}{
		{
			name: "disable after quiet",
			args: []string{"ytv1", "--quiet", "--no-quiet", "jNQXAC9IVRw"},
			want: false,
		},
		{
			name: "reenable after no quiet",
			args: []string{"ytv1", "--no-quiet", "-q", "jNQXAC9IVRw"},
			want: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			origArgs := os.Args
			origFlagSet := flag.CommandLine
			defer func() {
				os.Args = origArgs
				flag.CommandLine = origFlagSet
			}()

			os.Args = tc.args
			flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
			flag.CommandLine.SetOutput(io.Discard)

			opts := ParseFlags()
			if opts.Quiet != tc.want {
				t.Fatalf("Quiet=%v, want %v", opts.Quiet, tc.want)
			}
			if len(opts.URLs) != 1 || opts.URLs[0] != "jNQXAC9IVRw" {
				t.Fatalf("URLs=%v, want [jNQXAC9IVRw]", opts.URLs)
			}
		})
	}
}

func TestParseFlags_Version(t *testing.T) {
	origArgs := os.Args
	origFlagSet := flag.CommandLine
	defer func() {
		os.Args = origArgs
		flag.CommandLine = origFlagSet
	}()

	os.Args = []string{"ytv1", "--version"}
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	flag.CommandLine.SetOutput(io.Discard)

	opts := ParseFlags()
	if !opts.Version {
		t.Fatalf("Version=%v, want true", opts.Version)
	}
}

func TestParseFlags_ProgressControls(t *testing.T) {
	cases := []struct {
		name           string
		args           []string
		wantNoProgress bool
		wantProgress   bool
		wantNewline    bool
	}{
		{
			name:           "no progress",
			args:           []string{"ytv1", "--no-progress", "jNQXAC9IVRw"},
			wantNoProgress: true,
		},
		{
			name:         "progress",
			args:         []string{"ytv1", "--progress", "jNQXAC9IVRw"},
			wantProgress: true,
		},
		{
			name:         "last progress wins",
			args:         []string{"ytv1", "--no-progress", "--progress", "jNQXAC9IVRw"},
			wantProgress: true,
		},
		{
			name:           "last no progress wins",
			args:           []string{"ytv1", "--progress", "--no-progress", "jNQXAC9IVRw"},
			wantNoProgress: true,
		},
		{
			name:        "newline accepted",
			args:        []string{"ytv1", "--newline", "jNQXAC9IVRw"},
			wantNewline: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			origArgs := os.Args
			origFlagSet := flag.CommandLine
			defer func() {
				os.Args = origArgs
				flag.CommandLine = origFlagSet
			}()

			os.Args = tc.args
			flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
			flag.CommandLine.SetOutput(io.Discard)

			opts := ParseFlags()
			if opts.NoProgress != tc.wantNoProgress {
				t.Fatalf("NoProgress=%v, want %v", opts.NoProgress, tc.wantNoProgress)
			}
			if opts.Progress != tc.wantProgress {
				t.Fatalf("Progress=%v, want %v", opts.Progress, tc.wantProgress)
			}
			if opts.NewlineProgress != tc.wantNewline {
				t.Fatalf("NewlineProgress=%v, want %v", opts.NewlineProgress, tc.wantNewline)
			}
			if len(opts.URLs) != 1 || opts.URLs[0] != "jNQXAC9IVRw" {
				t.Fatalf("URLs=%v, want [jNQXAC9IVRw]", opts.URLs)
			}
		})
	}
}

func TestParseFlags_WriteDescription(t *testing.T) {
	origArgs := os.Args
	origFlagSet := flag.CommandLine
	defer func() {
		os.Args = origArgs
		flag.CommandLine = origFlagSet
	}()

	os.Args = []string{"ytv1", "--write-description", "--skip-download", "jNQXAC9IVRw"}
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	flag.CommandLine.SetOutput(io.Discard)

	opts := ParseFlags()
	if !opts.WriteDescription {
		t.Fatalf("WriteDescription=%v, want true", opts.WriteDescription)
	}
	if !opts.SkipDownload {
		t.Fatalf("SkipDownload=%v, want true", opts.SkipDownload)
	}
	if len(opts.URLs) != 1 || opts.URLs[0] != "jNQXAC9IVRw" {
		t.Fatalf("URLs=%v, want [jNQXAC9IVRw]", opts.URLs)
	}
}

func TestParseFlags_NoWriteDescriptionLastFlagWins(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want bool
	}{
		{
			name: "disable after write",
			args: []string{"ytv1", "--write-description", "--no-write-description", "jNQXAC9IVRw"},
			want: false,
		},
		{
			name: "reenable after no write",
			args: []string{"ytv1", "--no-write-description", "--write-description", "jNQXAC9IVRw"},
			want: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			origArgs := os.Args
			origFlagSet := flag.CommandLine
			defer func() {
				os.Args = origArgs
				flag.CommandLine = origFlagSet
			}()

			os.Args = tc.args
			flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
			flag.CommandLine.SetOutput(io.Discard)

			opts := ParseFlags()
			if opts.WriteDescription != tc.want {
				t.Fatalf("WriteDescription=%v, want %v", opts.WriteDescription, tc.want)
			}
			if len(opts.URLs) != 1 || opts.URLs[0] != "jNQXAC9IVRw" {
				t.Fatalf("URLs=%v, want [jNQXAC9IVRw]", opts.URLs)
			}
		})
	}
}

func TestParseFlags_NoWritePlaylistMetafilesLastFlagWins(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want bool
	}{
		{
			name: "disable playlist metafiles",
			args: []string{"ytv1", "--write-playlist-metafiles", "--no-write-playlist-metafiles", "PL1234567890"},
			want: true,
		},
		{
			name: "reenable playlist metafiles",
			args: []string{"ytv1", "--no-write-playlist-metafiles", "--write-playlist-metafiles", "PL1234567890"},
			want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			origArgs := os.Args
			origFlagSet := flag.CommandLine
			defer func() {
				os.Args = origArgs
				flag.CommandLine = origFlagSet
			}()

			os.Args = tc.args
			flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
			flag.CommandLine.SetOutput(io.Discard)

			opts := ParseFlags()
			if opts.NoWritePlaylistMetafiles != tc.want {
				t.Fatalf("NoWritePlaylistMetafiles=%v, want %v", opts.NoWritePlaylistMetafiles, tc.want)
			}
			if len(opts.URLs) != 1 || opts.URLs[0] != "PL1234567890" {
				t.Fatalf("URLs=%v, want [PL1234567890]", opts.URLs)
			}
		})
	}
}

func TestParseFlags_WriteThumbnail(t *testing.T) {
	origArgs := os.Args
	origFlagSet := flag.CommandLine
	defer func() {
		os.Args = origArgs
		flag.CommandLine = origFlagSet
	}()

	os.Args = []string{"ytv1", "--write-thumbnail", "--skip-download", "jNQXAC9IVRw"}
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	flag.CommandLine.SetOutput(io.Discard)

	opts := ParseFlags()
	if !opts.WriteThumbnail {
		t.Fatalf("WriteThumbnail=%v, want true", opts.WriteThumbnail)
	}
	if !opts.SkipDownload {
		t.Fatalf("SkipDownload=%v, want true", opts.SkipDownload)
	}
	if len(opts.URLs) != 1 || opts.URLs[0] != "jNQXAC9IVRw" {
		t.Fatalf("URLs=%v, want [jNQXAC9IVRw]", opts.URLs)
	}
}

func TestParseFlags_WriteURLLink(t *testing.T) {
	for _, flagName := range []string{"--write-link", "--write-url-link"} {
		t.Run(flagName, func(t *testing.T) {
			origArgs := os.Args
			origFlagSet := flag.CommandLine
			defer func() {
				os.Args = origArgs
				flag.CommandLine = origFlagSet
			}()

			os.Args = []string{"ytv1", flagName, "--skip-download", "jNQXAC9IVRw"}
			flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
			flag.CommandLine.SetOutput(io.Discard)

			opts := ParseFlags()
			if !opts.WriteURLLink {
				t.Fatalf("WriteURLLink=%v, want true", opts.WriteURLLink)
			}
			if !opts.SkipDownload {
				t.Fatalf("SkipDownload=%v, want true", opts.SkipDownload)
			}
			if len(opts.URLs) != 1 || opts.URLs[0] != "jNQXAC9IVRw" {
				t.Fatalf("URLs=%v, want [jNQXAC9IVRw]", opts.URLs)
			}
		})
	}
}

func TestParseFlags_WritePlatformLinks(t *testing.T) {
	cases := []struct {
		flagName string
		check    func(Options) bool
	}{
		{"--write-webloc-link", func(opts Options) bool { return opts.WriteWeblocLink }},
		{"--write-desktop-link", func(opts Options) bool { return opts.WriteDesktopLink }},
	}
	for _, tc := range cases {
		t.Run(tc.flagName, func(t *testing.T) {
			origArgs := os.Args
			origFlagSet := flag.CommandLine
			defer func() {
				os.Args = origArgs
				flag.CommandLine = origFlagSet
			}()

			os.Args = []string{"ytv1", tc.flagName, "--skip-download", "jNQXAC9IVRw"}
			flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
			flag.CommandLine.SetOutput(io.Discard)

			opts := ParseFlags()
			if !tc.check(opts) {
				t.Fatalf("%s was not enabled: %+v", tc.flagName, opts)
			}
			if !opts.SkipDownload {
				t.Fatalf("SkipDownload=%v, want true", opts.SkipDownload)
			}
			if len(opts.URLs) != 1 || opts.URLs[0] != "jNQXAC9IVRw" {
				t.Fatalf("URLs=%v, want [jNQXAC9IVRw]", opts.URLs)
			}
		})
	}
}

func TestParseFlags_GetThumbnail(t *testing.T) {
	origArgs := os.Args
	origFlagSet := flag.CommandLine
	defer func() {
		os.Args = origArgs
		flag.CommandLine = origFlagSet
	}()

	os.Args = []string{"ytv1", "--get-thumbnail", "jNQXAC9IVRw"}
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	flag.CommandLine.SetOutput(io.Discard)

	opts := ParseFlags()
	if !opts.GetThumbnail {
		t.Fatalf("GetThumbnail=%v, want true", opts.GetThumbnail)
	}
	if len(opts.URLs) != 1 || opts.URLs[0] != "jNQXAC9IVRw" {
		t.Fatalf("URLs=%v, want [jNQXAC9IVRw]", opts.URLs)
	}
}

func TestParseFlags_MetadataGetFlagsPreserveOrder(t *testing.T) {
	origArgs := os.Args
	origFlagSet := flag.CommandLine
	defer func() {
		os.Args = origArgs
		flag.CommandLine = origFlagSet
	}()

	os.Args = []string{"ytv1", "--get-id", "-e", "--get-filename", "-g", "--get-format", "--get-duration", "--get-description", "jNQXAC9IVRw"}
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	flag.CommandLine.SetOutput(io.Discard)

	opts := ParseFlags()
	if !opts.GetID || !opts.GetTitle || !opts.GetFilename || !opts.GetURL || !opts.GetFormat || !opts.GetDuration || !opts.GetDescription {
		t.Fatalf("get flags not all enabled: %+v", opts)
	}
	if strings.Join(opts.GetMetadataOrder, ",") != "id,title,filename,url,format,duration,description" {
		t.Fatalf("GetMetadataOrder=%v", opts.GetMetadataOrder)
	}
	if len(opts.URLs) != 1 || opts.URLs[0] != "jNQXAC9IVRw" {
		t.Fatalf("URLs=%v, want [jNQXAC9IVRw]", opts.URLs)
	}
}

func TestParseFlags_PrintPreservesOrderAndImpliesQuietSimulate(t *testing.T) {
	origArgs := os.Args
	origFlagSet := flag.CommandLine
	defer func() {
		os.Args = origArgs
		flag.CommandLine = origFlagSet
	}()

	os.Args = []string{"ytv1", "--print", "title", "-O", "url", "--get-id", "jNQXAC9IVRw"}
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	flag.CommandLine.SetOutput(io.Discard)

	opts := ParseFlags()
	if !opts.SkipDownload || !opts.Quiet {
		t.Fatalf("SkipDownload=%v Quiet=%v, want both true", opts.SkipDownload, opts.Quiet)
	}
	if strings.Join(opts.PrintTemplates, ",") != "title,url" {
		t.Fatalf("PrintTemplates=%v", opts.PrintTemplates)
	}
	if strings.Join(opts.GetMetadataOrder, ",") != "print:title,print:url,id" {
		t.Fatalf("GetMetadataOrder=%v", opts.GetMetadataOrder)
	}
	if len(opts.URLs) != 1 || opts.URLs[0] != "jNQXAC9IVRw" {
		t.Fatalf("URLs=%v, want [jNQXAC9IVRw]", opts.URLs)
	}
}

func TestParseFlags_PrintNoSimulateNoQuietOverrides(t *testing.T) {
	origArgs := os.Args
	origFlagSet := flag.CommandLine
	defer func() {
		os.Args = origArgs
		flag.CommandLine = origFlagSet
	}()

	os.Args = []string{"ytv1", "--print", "title", "--no-simulate", "--no-quiet", "jNQXAC9IVRw"}
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	flag.CommandLine.SetOutput(io.Discard)

	opts := ParseFlags()
	if opts.SkipDownload || opts.Quiet {
		t.Fatalf("SkipDownload=%v Quiet=%v, want both false", opts.SkipDownload, opts.Quiet)
	}
}

func TestParseFlags_PrintToFilePreservesOrderAndURLs(t *testing.T) {
	origArgs := os.Args
	origFlagSet := flag.CommandLine
	defer func() {
		os.Args = origArgs
		flag.CommandLine = origFlagSet
	}()

	os.Args = []string{"ytv1", "--print-to-file", "title", "out.txt", "--get-id", "jNQXAC9IVRw"}
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	flag.CommandLine.SetOutput(io.Discard)

	opts := ParseFlags()
	if !opts.SkipDownload || !opts.Quiet {
		t.Fatalf("SkipDownload=%v Quiet=%v, want both true", opts.SkipDownload, opts.Quiet)
	}
	if len(opts.PrintToFile) != 1 || opts.PrintToFile[0].Template != "title" || opts.PrintToFile[0].File != "out.txt" {
		t.Fatalf("PrintToFile=%+v", opts.PrintToFile)
	}
	if strings.Join(opts.GetMetadataOrder, ",") != "printfile:title\x00out.txt,id" {
		t.Fatalf("GetMetadataOrder=%v", opts.GetMetadataOrder)
	}
	if len(opts.URLs) != 1 || opts.URLs[0] != "jNQXAC9IVRw" {
		t.Fatalf("URLs=%v, want [jNQXAC9IVRw]", opts.URLs)
	}
}

func TestParseFlags_NoWriteThumbnailLastFlagWins(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want bool
	}{
		{
			name: "disable after write",
			args: []string{"ytv1", "--write-thumbnail", "--no-write-thumbnail", "jNQXAC9IVRw"},
			want: false,
		},
		{
			name: "reenable after disable",
			args: []string{"ytv1", "--no-write-thumbnail", "--write-thumbnail", "jNQXAC9IVRw"},
			want: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			origArgs := os.Args
			origFlagSet := flag.CommandLine
			defer func() {
				os.Args = origArgs
				flag.CommandLine = origFlagSet
			}()

			os.Args = tc.args
			flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
			flag.CommandLine.SetOutput(io.Discard)

			opts := ParseFlags()
			if opts.WriteThumbnail != tc.want {
				t.Fatalf("WriteThumbnail=%v, want %v", opts.WriteThumbnail, tc.want)
			}
			if len(opts.URLs) != 1 || opts.URLs[0] != "jNQXAC9IVRw" {
				t.Fatalf("URLs=%v, want [jNQXAC9IVRw]", opts.URLs)
			}
		})
	}
}

func TestParseFlags_SubtitleCompatibilityAliases(t *testing.T) {
	origArgs := os.Args
	origFlagSet := flag.CommandLine
	defer func() {
		os.Args = origArgs
		flag.CommandLine = origFlagSet
	}()

	os.Args = []string{"ytv1", "--write-automatic-subs", "--srt-langs", "ko,en", "jNQXAC9IVRw"}
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	flag.CommandLine.SetOutput(io.Discard)

	opts := ParseFlags()
	if !opts.WriteAutoSubs {
		t.Fatalf("WriteAutoSubs=%v, want true", opts.WriteAutoSubs)
	}
	if opts.SubLangs != "ko,en" {
		t.Fatalf("SubLangs=%q, want ko,en", opts.SubLangs)
	}
	if len(opts.URLs) != 1 || opts.URLs[0] != "jNQXAC9IVRw" {
		t.Fatalf("URLs=%v, want [jNQXAC9IVRw]", opts.URLs)
	}
}

func TestParseFlags_ListSubs(t *testing.T) {
	origArgs := os.Args
	origFlagSet := flag.CommandLine
	defer func() {
		os.Args = origArgs
		flag.CommandLine = origFlagSet
	}()

	os.Args = []string{"ytv1", "--list-subs", "jNQXAC9IVRw"}
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	flag.CommandLine.SetOutput(io.Discard)

	opts := ParseFlags()
	if !opts.ListSubs {
		t.Fatalf("ListSubs=%v, want true", opts.ListSubs)
	}
	if len(opts.URLs) != 1 || opts.URLs[0] != "jNQXAC9IVRw" {
		t.Fatalf("URLs=%v, want [jNQXAC9IVRw]", opts.URLs)
	}
}

func TestParseFlags_AllSubs(t *testing.T) {
	origArgs := os.Args
	origFlagSet := flag.CommandLine
	defer func() {
		os.Args = origArgs
		flag.CommandLine = origFlagSet
	}()

	os.Args = []string{"ytv1", "--all-subs", "--write-subs", "jNQXAC9IVRw"}
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	flag.CommandLine.SetOutput(io.Discard)

	opts := ParseFlags()
	if !opts.AllSubs {
		t.Fatalf("AllSubs=%v, want true", opts.AllSubs)
	}
	if !opts.WriteSubs {
		t.Fatalf("WriteSubs=%v, want true", opts.WriteSubs)
	}
	if len(opts.URLs) != 1 || opts.URLs[0] != "jNQXAC9IVRw" {
		t.Fatalf("URLs=%v, want [jNQXAC9IVRw]", opts.URLs)
	}
}

func TestParseFlags_NegativeSubtitleAliasesLastFlagWins(t *testing.T) {
	cases := []struct {
		name          string
		args          []string
		wantSubs      bool
		wantAuto      bool
		wantSubFormat string
	}{
		{
			name:          "disable write srt",
			args:          []string{"ytv1", "--write-srt", "--no-write-srt", "jNQXAC9IVRw"},
			wantSubs:      false,
			wantSubFormat: "best",
		},
		{
			name:          "reenable write srt",
			args:          []string{"ytv1", "--no-write-subs", "--write-srt", "jNQXAC9IVRw"},
			wantSubs:      true,
			wantSubFormat: "srt",
		},
		{
			name:          "disable auto subs",
			args:          []string{"ytv1", "--write-automatic-subs", "--no-write-automatic-subs", "jNQXAC9IVRw"},
			wantAuto:      false,
			wantSubFormat: "best",
		},
		{
			name:          "reenable auto subs",
			args:          []string{"ytv1", "--no-write-auto-subs", "--write-auto-subs", "jNQXAC9IVRw"},
			wantAuto:      true,
			wantSubFormat: "best",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			origArgs := os.Args
			origFlagSet := flag.CommandLine
			defer func() {
				os.Args = origArgs
				flag.CommandLine = origFlagSet
			}()

			os.Args = tc.args
			flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
			flag.CommandLine.SetOutput(io.Discard)

			opts := ParseFlags()
			if opts.WriteSubs != tc.wantSubs {
				t.Fatalf("WriteSubs=%v, want %v", opts.WriteSubs, tc.wantSubs)
			}
			if opts.WriteAutoSubs != tc.wantAuto {
				t.Fatalf("WriteAutoSubs=%v, want %v", opts.WriteAutoSubs, tc.wantAuto)
			}
			if opts.SubFormat != tc.wantSubFormat {
				t.Fatalf("SubFormat=%q, want %q", opts.SubFormat, tc.wantSubFormat)
			}
			if len(opts.URLs) != 1 || opts.URLs[0] != "jNQXAC9IVRw" {
				t.Fatalf("URLs=%v, want [jNQXAC9IVRw]", opts.URLs)
			}
		})
	}
}

func TestParseFlags_PlaylistItems(t *testing.T) {
	origArgs := os.Args
	origFlagSet := flag.CommandLine
	defer func() {
		os.Args = origArgs
		flag.CommandLine = origFlagSet
	}()

	os.Args = []string{"ytv1", "--playlist-items", "1,3:5,:10", "PLabc"}
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	flag.CommandLine.SetOutput(io.Discard)

	opts := ParseFlags()
	if opts.PlaylistItems != "1,3:5,:10" {
		t.Fatalf("PlaylistItems=%q, want 1,3:5,:10", opts.PlaylistItems)
	}
	if len(opts.URLs) != 1 || opts.URLs[0] != "PLabc" {
		t.Fatalf("URLs=%v, want [PLabc]", opts.URLs)
	}
}

func TestParseFlags_PlaylistItemsShortAlias(t *testing.T) {
	origArgs := os.Args
	origFlagSet := flag.CommandLine
	defer func() {
		os.Args = origArgs
		flag.CommandLine = origFlagSet
	}()

	os.Args = []string{"ytv1", "-I", "2:4", "PLabc"}
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	flag.CommandLine.SetOutput(io.Discard)

	opts := ParseFlags()
	if opts.PlaylistItems != "2:4" {
		t.Fatalf("PlaylistItems=%q, want 2:4", opts.PlaylistItems)
	}
	if len(opts.URLs) != 1 || opts.URLs[0] != "PLabc" {
		t.Fatalf("URLs=%v, want [PLabc]", opts.URLs)
	}
}

func TestParseFlags_PlaylistStartEnd(t *testing.T) {
	origArgs := os.Args
	origFlagSet := flag.CommandLine
	defer func() {
		os.Args = origArgs
		flag.CommandLine = origFlagSet
	}()

	os.Args = []string{"ytv1", "--playlist-start", "2", "--playlist-end", "5", "PLabc"}
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	flag.CommandLine.SetOutput(io.Discard)

	opts := ParseFlags()
	if opts.PlaylistStart != 2 || opts.PlaylistEnd != 5 {
		t.Fatalf("playlist range=%d:%d, want 2:5", opts.PlaylistStart, opts.PlaylistEnd)
	}
	if len(opts.URLs) != 1 || opts.URLs[0] != "PLabc" {
		t.Fatalf("URLs=%v, want [PLabc]", opts.URLs)
	}
}

func TestParseFlags_PlaylistReverse(t *testing.T) {
	origArgs := os.Args
	origFlagSet := flag.CommandLine
	defer func() {
		os.Args = origArgs
		flag.CommandLine = origFlagSet
	}()

	os.Args = []string{"ytv1", "--playlist-reverse", "PLabc"}
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	flag.CommandLine.SetOutput(io.Discard)

	opts := ParseFlags()
	if !opts.PlaylistReverse {
		t.Fatalf("PlaylistReverse=%v, want true", opts.PlaylistReverse)
	}
	if len(opts.URLs) != 1 || opts.URLs[0] != "PLabc" {
		t.Fatalf("URLs=%v, want [PLabc]", opts.URLs)
	}
}

func TestParseFlags_PlaylistRandom(t *testing.T) {
	origArgs := os.Args
	origFlagSet := flag.CommandLine
	defer func() {
		os.Args = origArgs
		flag.CommandLine = origFlagSet
	}()

	os.Args = []string{"ytv1", "--playlist-random", "PLabc"}
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	flag.CommandLine.SetOutput(io.Discard)

	opts := ParseFlags()
	if !opts.PlaylistRandom {
		t.Fatalf("PlaylistRandom=%v, want true", opts.PlaylistRandom)
	}
	if len(opts.URLs) != 1 || opts.URLs[0] != "PLabc" {
		t.Fatalf("URLs=%v, want [PLabc]", opts.URLs)
	}
}

func TestParseFlags_SkipPlaylistAfterErrors(t *testing.T) {
	origArgs := os.Args
	origFlagSet := flag.CommandLine
	defer func() {
		os.Args = origArgs
		flag.CommandLine = origFlagSet
	}()

	os.Args = []string{"ytv1", "--skip-playlist-after-errors", "2", "PLabc"}
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	flag.CommandLine.SetOutput(io.Discard)

	opts := ParseFlags()
	if opts.SkipPlaylistAfterErrors != 2 {
		t.Fatalf("SkipPlaylistAfterErrors=%d, want 2", opts.SkipPlaylistAfterErrors)
	}
	if len(opts.URLs) != 1 || opts.URLs[0] != "PLabc" {
		t.Fatalf("URLs=%v, want [PLabc]", opts.URLs)
	}
}

func TestParseFlags_MaxDownloads(t *testing.T) {
	origArgs := os.Args
	origFlagSet := flag.CommandLine
	defer func() {
		os.Args = origArgs
		flag.CommandLine = origFlagSet
	}()

	os.Args = []string{"ytv1", "--max-downloads", "2", "jNQXAC9IVRw"}
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	flag.CommandLine.SetOutput(io.Discard)

	opts := ParseFlags()
	if opts.MaxDownloads != 2 {
		t.Fatalf("MaxDownloads=%d, want 2", opts.MaxDownloads)
	}
	if len(opts.URLs) != 1 || opts.URLs[0] != "jNQXAC9IVRw" {
		t.Fatalf("URLs=%v, want [jNQXAC9IVRw]", opts.URLs)
	}
}

func TestParseFlags_BreakOnExisting(t *testing.T) {
	origArgs := os.Args
	origFlagSet := flag.CommandLine
	defer func() {
		os.Args = origArgs
		flag.CommandLine = origFlagSet
	}()

	os.Args = []string{"ytv1", "--break-on-existing", "jNQXAC9IVRw"}
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	flag.CommandLine.SetOutput(io.Discard)

	opts := ParseFlags()
	if !opts.BreakOnExisting {
		t.Fatalf("BreakOnExisting=%v, want true", opts.BreakOnExisting)
	}
	if len(opts.URLs) != 1 || opts.URLs[0] != "jNQXAC9IVRw" {
		t.Fatalf("URLs=%v, want [jNQXAC9IVRw]", opts.URLs)
	}
}

func TestParseFlags_ForceWriteArchiveAliases(t *testing.T) {
	for _, flagName := range []string{"--force-write-archive", "--force-write-download-archive", "--force-download-archive"} {
		t.Run(flagName, func(t *testing.T) {
			origArgs := os.Args
			origFlagSet := flag.CommandLine
			defer func() {
				os.Args = origArgs
				flag.CommandLine = origFlagSet
			}()

			os.Args = []string{"ytv1", flagName, "jNQXAC9IVRw"}
			flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
			flag.CommandLine.SetOutput(io.Discard)

			opts := ParseFlags()
			if !opts.ForceWriteArchive {
				t.Fatalf("ForceWriteArchive=%v, want true", opts.ForceWriteArchive)
			}
			if len(opts.URLs) != 1 || opts.URLs[0] != "jNQXAC9IVRw" {
				t.Fatalf("URLs=%v, want [jNQXAC9IVRw]", opts.URLs)
			}
		})
	}
}

func TestParseFlags_NoDownloadArchiveLastFlagWins(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "disable after archive",
			args: []string{"ytv1", "--download-archive", "archive.txt", "--no-download-archive", "jNQXAC9IVRw"},
			want: "",
		},
		{
			name: "archive after disable",
			args: []string{"ytv1", "--no-download-archive", "--download-archive", "archive.txt", "jNQXAC9IVRw"},
			want: "archive.txt",
		},
		{
			name: "equals form",
			args: []string{"ytv1", "--download-archive=first.txt", "--no-download-archive", "--download-archive=second.txt", "jNQXAC9IVRw"},
			want: "second.txt",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			origArgs := os.Args
			origFlagSet := flag.CommandLine
			defer func() {
				os.Args = origArgs
				flag.CommandLine = origFlagSet
			}()

			os.Args = tc.args
			flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
			flag.CommandLine.SetOutput(io.Discard)

			opts := ParseFlags()
			if opts.DownloadArchive != tc.want {
				t.Fatalf("DownloadArchive=%q, want %q", opts.DownloadArchive, tc.want)
			}
			if len(opts.URLs) != 1 || opts.URLs[0] != "jNQXAC9IVRw" {
				t.Fatalf("URLs=%v, want [jNQXAC9IVRw]", opts.URLs)
			}
		})
	}
}

func TestParseFlags_LastAliasWinsForFormatAndOutput(t *testing.T) {
	origArgs := os.Args
	origFlagSet := flag.CommandLine
	defer func() {
		os.Args = origArgs
		flag.CommandLine = origFlagSet
	}()

	os.Args = []string{"ytv1", "-f", "bestaudio", "--format", "bestvideo", "-o", "first.%(ext)s", "--output", "second.%(ext)s", "jNQXAC9IVRw"}
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	flag.CommandLine.SetOutput(io.Discard)

	opts := ParseFlags()
	if opts.FormatSelector != "bestvideo" {
		t.Fatalf("FormatSelector=%q, want bestvideo", opts.FormatSelector)
	}
	if opts.OutputTemplate != "second.%(ext)s" {
		t.Fatalf("OutputTemplate=%q, want second.%%(ext)s", opts.OutputTemplate)
	}
}

func TestParseFlags_OutputPaths(t *testing.T) {
	absDir := filepath.Join(t.TempDir(), "out")
	cases := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "short",
			args: []string{"ytv1", "-P", "out", "jNQXAC9IVRw"},
			want: "out",
		},
		{
			name: "home prefix",
			args: []string{"ytv1", "--paths", "home:downloads", "jNQXAC9IVRw"},
			want: "downloads",
		},
		{
			name: "last wins",
			args: []string{"ytv1", "--paths", "first", "-P", "second", "jNQXAC9IVRw"},
			want: "second",
		},
		{
			name: "absolute path",
			args: []string{"ytv1", "--paths", absDir, "jNQXAC9IVRw"},
			want: absDir,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			origArgs := os.Args
			origFlagSet := flag.CommandLine
			defer func() {
				os.Args = origArgs
				flag.CommandLine = origFlagSet
			}()

			os.Args = tc.args
			flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
			flag.CommandLine.SetOutput(io.Discard)

			opts := ParseFlags()
			if opts.OutputPathDir != tc.want {
				t.Fatalf("OutputPathDir=%q, want %q", opts.OutputPathDir, tc.want)
			}
			if len(opts.URLs) != 1 || opts.URLs[0] != "jNQXAC9IVRw" {
				t.Fatalf("URLs=%v, want [jNQXAC9IVRw]", opts.URLs)
			}
		})
	}
}

func TestParseFlags_OutputIDShortcut(t *testing.T) {
	cases := []struct {
		name         string
		args         []string
		wantUseID    bool
		wantTemplate string
	}{
		{
			name:      "id shortcut",
			args:      []string{"ytv1", "--id", "jNQXAC9IVRw"},
			wantUseID: true,
		},
		{
			name:         "later output overrides id",
			args:         []string{"ytv1", "--id", "-o", "%(title)s.%(ext)s", "jNQXAC9IVRw"},
			wantUseID:    false,
			wantTemplate: "%(title)s.%(ext)s",
		},
		{
			name:      "later id overrides output",
			args:      []string{"ytv1", "-o", "%(title)s.%(ext)s", "--id", "jNQXAC9IVRw"},
			wantUseID: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			origArgs := os.Args
			origFlagSet := flag.CommandLine
			defer func() {
				os.Args = origArgs
				flag.CommandLine = origFlagSet
			}()

			os.Args = tc.args
			flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
			flag.CommandLine.SetOutput(io.Discard)

			opts := ParseFlags()
			if opts.OutputUseID != tc.wantUseID {
				t.Fatalf("OutputUseID=%v, want %v", opts.OutputUseID, tc.wantUseID)
			}
			if opts.OutputTemplate != tc.wantTemplate {
				t.Fatalf("OutputTemplate=%q, want %q", opts.OutputTemplate, tc.wantTemplate)
			}
			if len(opts.URLs) != 1 || opts.URLs[0] != "jNQXAC9IVRw" {
				t.Fatalf("URLs=%v, want [jNQXAC9IVRw]", opts.URLs)
			}
		})
	}
}

func TestParseFlags_RestrictFilenamesLastFlagWins(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want bool
	}{
		{
			name: "enable",
			args: []string{"ytv1", "--restrict-filenames", "jNQXAC9IVRw"},
			want: true,
		},
		{
			name: "disable after enable",
			args: []string{"ytv1", "--restrict-filenames", "--no-restrict-filenames", "jNQXAC9IVRw"},
			want: false,
		},
		{
			name: "enable after disable",
			args: []string{"ytv1", "--no-restrict-filenames", "--restrict-filenames", "jNQXAC9IVRw"},
			want: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			origArgs := os.Args
			origFlagSet := flag.CommandLine
			defer func() {
				os.Args = origArgs
				flag.CommandLine = origFlagSet
			}()

			os.Args = tc.args
			flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
			flag.CommandLine.SetOutput(io.Discard)

			opts := ParseFlags()
			if opts.RestrictFilenames != tc.want {
				t.Fatalf("RestrictFilenames=%v, want %v", opts.RestrictFilenames, tc.want)
			}
			if len(opts.URLs) != 1 || opts.URLs[0] != "jNQXAC9IVRw" {
				t.Fatalf("URLs=%v, want [jNQXAC9IVRw]", opts.URLs)
			}
		})
	}
}

func TestParseFlags_TrimFilenames(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want int
	}{
		{
			name: "space form",
			args: []string{"ytv1", "--trim-filenames", "8", "jNQXAC9IVRw"},
			want: 8,
		},
		{
			name: "equals form",
			args: []string{"ytv1", "--trim-filenames=12", "jNQXAC9IVRw"},
			want: 12,
		},
		{
			name: "alias space form",
			args: []string{"ytv1", "--trim-file-names", "9", "jNQXAC9IVRw"},
			want: 9,
		},
		{
			name: "alias equals form",
			args: []string{"ytv1", "--trim-file-names=10", "jNQXAC9IVRw"},
			want: 10,
		},
		{
			name: "last spelling wins",
			args: []string{"ytv1", "--trim-filenames", "8", "--trim-file-names", "11", "jNQXAC9IVRw"},
			want: 11,
		},
		{
			name: "non positive disables",
			args: []string{"ytv1", "--trim-filenames", "0", "jNQXAC9IVRw"},
			want: 0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			origArgs := os.Args
			origFlagSet := flag.CommandLine
			defer func() {
				os.Args = origArgs
				flag.CommandLine = origFlagSet
			}()

			os.Args = tc.args
			flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
			flag.CommandLine.SetOutput(io.Discard)

			opts := ParseFlags()
			if opts.TrimFilenames != tc.want {
				t.Fatalf("TrimFilenames=%d, want %d", opts.TrimFilenames, tc.want)
			}
			if len(opts.URLs) != 1 || opts.URLs[0] != "jNQXAC9IVRw" {
				t.Fatalf("URLs=%v, want [jNQXAC9IVRw]", opts.URLs)
			}
		})
	}
}

func TestParseFlags_OverwritePolicyLastFlagWins(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want bool
	}{
		{
			name: "no overwrites",
			args: []string{"ytv1", "--no-overwrites", "jNQXAC9IVRw"},
			want: true,
		},
		{
			name: "short no overwrites",
			args: []string{"ytv1", "-w", "jNQXAC9IVRw"},
			want: true,
		},
		{
			name: "force after no overwrites",
			args: []string{"ytv1", "--no-overwrites", "--force-overwrites", "jNQXAC9IVRw"},
			want: false,
		},
		{
			name: "yes overwrites alias",
			args: []string{"ytv1", "--no-overwrites", "--yes-overwrites", "jNQXAC9IVRw"},
			want: false,
		},
		{
			name: "no overwrites after force",
			args: []string{"ytv1", "--force-overwrites", "--no-overwrites", "jNQXAC9IVRw"},
			want: true,
		},
		{
			name: "no force restores default",
			args: []string{"ytv1", "--no-overwrites", "--no-force-overwrites", "jNQXAC9IVRw"},
			want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			origArgs := os.Args
			origFlagSet := flag.CommandLine
			defer func() {
				os.Args = origArgs
				flag.CommandLine = origFlagSet
			}()

			os.Args = tc.args
			flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
			flag.CommandLine.SetOutput(io.Discard)

			opts := ParseFlags()
			if opts.NoOverwrites != tc.want {
				t.Fatalf("NoOverwrites=%v, want %v", opts.NoOverwrites, tc.want)
			}
			if len(opts.URLs) != 1 || opts.URLs[0] != "jNQXAC9IVRw" {
				t.Fatalf("URLs=%v, want [jNQXAC9IVRw]", opts.URLs)
			}
		})
	}
}

func TestParseFlags_ForceOverwritesImpliesNoContinue(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want bool
	}{
		{
			name: "force implies no continue",
			args: []string{"ytv1", "--force-overwrites", "jNQXAC9IVRw"},
			want: true,
		},
		{
			name: "yes alias implies no continue",
			args: []string{"ytv1", "--yes-overwrites", "--continue", "jNQXAC9IVRw"},
			want: true,
		},
		{
			name: "no force restores continue",
			args: []string{"ytv1", "--force-overwrites", "--no-force-overwrites", "jNQXAC9IVRw"},
			want: false,
		},
		{
			name: "explicit no continue remains",
			args: []string{"ytv1", "--force-overwrites", "--no-force-overwrites", "--no-continue", "jNQXAC9IVRw"},
			want: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			origArgs := os.Args
			origFlagSet := flag.CommandLine
			defer func() {
				os.Args = origArgs
				flag.CommandLine = origFlagSet
			}()

			os.Args = tc.args
			flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
			flag.CommandLine.SetOutput(io.Discard)

			opts := ParseFlags()
			if opts.NoContinue != tc.want {
				t.Fatalf("NoContinue=%v, want %v", opts.NoContinue, tc.want)
			}
			if len(opts.URLs) != 1 || opts.URLs[0] != "jNQXAC9IVRw" {
				t.Fatalf("URLs=%v, want [jNQXAC9IVRw]", opts.URLs)
			}
		})
	}
}

func TestParseFlags_PartLastFlagWins(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want bool
	}{
		{
			name: "no part",
			args: []string{"ytv1", "--no-part", "jNQXAC9IVRw"},
			want: true,
		},
		{
			name: "part after no part",
			args: []string{"ytv1", "--no-part", "--part", "jNQXAC9IVRw"},
			want: false,
		},
		{
			name: "no part after part",
			args: []string{"ytv1", "--part", "--no-part", "jNQXAC9IVRw"},
			want: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			origArgs := os.Args
			origFlagSet := flag.CommandLine
			defer func() {
				os.Args = origArgs
				flag.CommandLine = origFlagSet
			}()

			os.Args = tc.args
			flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
			flag.CommandLine.SetOutput(io.Discard)

			opts := ParseFlags()
			if opts.NoPart != tc.want {
				t.Fatalf("NoPart=%v, want %v", opts.NoPart, tc.want)
			}
			if len(opts.URLs) != 1 || opts.URLs[0] != "jNQXAC9IVRw" {
				t.Fatalf("URLs=%v, want [jNQXAC9IVRw]", opts.URLs)
			}
		})
	}
}

func TestParseFlags_MTimeLastFlagWins(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want bool
	}{
		{
			name: "mtime",
			args: []string{"ytv1", "--mtime", "jNQXAC9IVRw"},
			want: true,
		},
		{
			name: "no mtime after mtime",
			args: []string{"ytv1", "--mtime", "--no-mtime", "jNQXAC9IVRw"},
			want: false,
		},
		{
			name: "mtime after no mtime",
			args: []string{"ytv1", "--no-mtime", "--mtime", "jNQXAC9IVRw"},
			want: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			origArgs := os.Args
			origFlagSet := flag.CommandLine
			defer func() {
				os.Args = origArgs
				flag.CommandLine = origFlagSet
			}()

			os.Args = tc.args
			flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
			flag.CommandLine.SetOutput(io.Discard)

			opts := ParseFlags()
			if opts.UpdateMTime != tc.want {
				t.Fatalf("UpdateMTime=%v, want %v", opts.UpdateMTime, tc.want)
			}
			if len(opts.URLs) != 1 || opts.URLs[0] != "jNQXAC9IVRw" {
				t.Fatalf("URLs=%v, want [jNQXAC9IVRw]", opts.URLs)
			}
		})
	}
}

func TestParseFlags_LastBooleanAliasWins(t *testing.T) {
	origArgs := os.Args
	origFlagSet := flag.CommandLine
	defer func() {
		os.Args = origArgs
		flag.CommandLine = origFlagSet
	}()

	os.Args = []string{"ytv1", "--continue", "--no-continue", "--break-on-existing", "--no-break-on-existing", "--ignore-errors", "--abort-on-error", "--yes-playlist", "--no-playlist", "jNQXAC9IVRw"}
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	flag.CommandLine.SetOutput(io.Discard)

	opts := ParseFlags()
	if !opts.NoContinue {
		t.Fatalf("NoContinue=%v, want true because later --no-continue should win", opts.NoContinue)
	}
	if !opts.AbortOnError {
		t.Fatalf("AbortOnError=%v, want true because later --abort-on-error should win", opts.AbortOnError)
	}
	if opts.IgnoreErrors {
		t.Fatalf("IgnoreErrors=%v, want false because later --abort-on-error should win", opts.IgnoreErrors)
	}
	if !opts.NoPlaylist {
		t.Fatalf("NoPlaylist=%v, want true because later --no-playlist should win", opts.NoPlaylist)
	}
	if opts.YesPlaylist {
		t.Fatalf("YesPlaylist=%v, want false because later --no-playlist should win", opts.YesPlaylist)
	}
	if opts.BreakOnExisting {
		t.Fatalf("BreakOnExisting=%v, want false because later --no-break-on-existing should win", opts.BreakOnExisting)
	}
}
