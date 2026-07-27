package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/famomatic/ytv1/client"
	"github.com/famomatic/ytv1/internal/cli"
	"github.com/famomatic/ytv1/internal/source/soop"
)

var verboseLifecyclePrinter *lifecyclePrinter
var activeProgressPrinter *cliProgressPrinter
var activeDownloadArchive *client.DownloadArchive
var activeDownloadLimit *downloadLimit
var sleepBeforeRequestFunc = time.Sleep
var sleepBeforeDownloadFunc = time.Sleep
var sleepBeforeSubtitleFunc = time.Sleep
var latestReleaseURL = "https://api.github.com/repos/famomatic/ytv1/releases/latest"
var currentVersion = "dev"
var httpGetLatestRelease = defaultHTTPGetLatestRelease

// statusOverride, when non-nil, redirects all human-readable
// status/progress/log output away from os.Stdout. It is set to os.Stderr when
// media is streamed to stdout (`-o -`) so the piped media is never interleaved
// with status text (mirrors yt-dlp, which routes logs to stderr for `-o -`).
var statusOverride io.Writer

// statusW returns the sink for human-readable status/progress/log output: the
// override when set, else the current os.Stdout (read live so tests that swap
// os.Stdout still capture it).
func statusW() io.Writer {
	if statusOverride != nil {
		return statusOverride
	}
	return os.Stdout
}

var errBreakOnExisting = errors.New("break on existing archive entry")
var errMaxDownloadsReached = errors.New("maximum number of downloads reached")

const (
	exitCodeSuccess             = 0
	exitCodeGenericFailure      = 1
	exitCodeInvalidInput        = 2
	exitCodeLoginRequired       = 3
	exitCodeUnavailable         = 4
	exitCodeNoPlayableFormats   = 5
	exitCodeChallengeUnresolved = 6
	exitCodeAllClientsFailed    = 7
	exitCodeDownloadFailed      = 8
	exitCodeMP3ConfigRequired   = 9
	exitCodeTranscriptParse     = 10
)

func main() {
	// Hidden subcommand: run the detached SOOP agent stream server (spawned by the
	// soop source so the loopback URL outlives a `-J` resolve; see agentserve.go).
	if len(os.Args) >= 3 && os.Args[1] == "soopserve" {
		if err := soop.RunAgentServe(os.Args[2]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	opts := cli.ParseFlags()
	os.Exit(run(opts))
}

func run(opts cli.Options) int {
	// When media is streamed to stdout (`-o -`), all status/progress/log text
	// must go to stderr so it does not corrupt the piped media.
	if opts.OutputTemplate == "-" {
		statusOverride = os.Stderr
	}
	// The SOOP agent's loopback server must outlive this process only when the run
	// resolves a URL and exits (so MPV's ytdl_hook / an external player can play it
	// afterwards). An actual download serves it in-process so it dies with us — a
	// detached daemon there could survive a wedged consumer and keep pulling from
	// the local agent indefinitely.
	soop.SetDetachServe(opts.PrintJSON || opts.DumpSingleJSON || opts.SkipDownload || opts.GetURL || len(opts.PrintTemplates) > 0)
	if opts.Version {
		fmt.Println(versionString())
		return exitCodeSuccess
	}
	if len(opts.URLs) == 0 {
		err := fmt.Errorf("%w: no input URLs provided", client.ErrInvalidInput)
		if opts.PrintJSON {
			emitJSONFailure("", err, exitCodeInvalidInput)
		} else {
			fmt.Println("Usage: ytv1 [OPTIONS] URL [URL...]")
		}
		return exitCodeInvalidInput
	}

	cfg, err := cli.ToClientConfig(opts)
	if err != nil {
		return handleStartupError(opts, fmt.Errorf("failed to initialize config: %w", err))
	}
	if strings.TrimSpace(opts.DownloadArchive) != "" {
		archive, err := newDownloadArchive(opts.DownloadArchive)
		if err != nil {
			return handleStartupError(opts, fmt.Errorf("failed to initialize download archive: %w", err))
		}
		activeDownloadArchive = archive
		defer func() {
			if err := archive.Close(); err != nil {
				warnf(opts, "failed to close download archive: %v", err)
			}
		}()
	}
	if opts.MaxDownloads > 0 {
		activeDownloadLimit = &downloadLimit{Max: opts.MaxDownloads}
		defer func() {
			activeDownloadLimit = nil
		}()
	}
	checkAndPrintOutdated(opts)
	attachLifecycleHandlers(&cfg, opts)
	c := client.New(cfg)
	ctx := context.Background()
	return processInputsWithExitCode(ctx, c, opts.URLs, opts, processURL)
}

func handleStartupError(opts cli.Options, err error) int {
	code := classifyExitCode(err)
	if opts.PrintJSON {
		emitJSONFailure("", err, code)
	} else {
		log.Printf("Error: %v", err)
	}
	return code
}

func processInputs(
	ctx context.Context,
	c *client.Client,
	urls []string,
	opts cli.Options,
	processor func(context.Context, *client.Client, string, cli.Options) error,
) bool {
	return processInputsWithExitCode(ctx, c, urls, opts, processor) != exitCodeSuccess
}

func processInputsWithExitCode(
	ctx context.Context,
	c *client.Client,
	urls []string,
	opts cli.Options,
	processor func(context.Context, *client.Client, string, cli.Options) error,
) int {
	exitCode := exitCodeSuccess
	for _, url := range urls {
		if err := processor(ctx, c, url, opts); err != nil {
			if errors.Is(err, errBreakOnExisting) || errors.Is(err, errMaxDownloadsReached) {
				break
			}
			code := classifyExitCode(err)
			if code > exitCode {
				exitCode = code
			}
			if opts.PrintJSON {
				emitJSONFailure(url, err, code)
			} else {
				log.Printf("Error processing %s: %v", url, err)
			}
			if (opts.OverrideDiagnostics || opts.Verbose) && !opts.PrintJSON {
				printAttemptDiagnostics(err)
			}
			if opts.AbortOnError {
				break
			}
		}
	}
	if activeProgressPrinter != nil {
		resetConsoleTitle()
	}
	return exitCode
}

func attachLifecycleHandlers(cfg *client.Config, opts cli.Options) {
	lp := newLifecyclePrinter(time.Now)
	verboseLifecyclePrinter = lp
	activeProgressPrinter = nil
	if shouldPrintHumanText(opts) && !opts.NoWarnings {
		cfg.Logger = cliLogger{}
	}
	if shouldPrintProgressText(opts) {
		activeProgressPrinter = newCLIProgressPrinter(opts)
		cfg.OnDownloadProgress = activeProgressPrinter.Print
	}
	if !shouldPrintHumanText(opts) {
		return
	}
	cfg.OnExtractionEvent = func(evt client.ExtractionEvent) {
		if opts.Verbose {
			finishActiveProgressLine()
			fmt.Fprintln(statusW(), lp.formatExtractionEvent(evt))
			return
		}
		if isBasicExtractionEvent(evt) {
			finishActiveProgressLine()
			fmt.Fprintln(statusW(), formatExtractionEvent(evt))
		}
	}
	cfg.OnDownloadEvent = func(evt client.DownloadEvent) {
		if opts.Verbose || isBasicDownloadEvent(evt) {
			finishActiveProgressLine()
			fmt.Fprintln(statusW(), lp.formatDownloadEvent(evt))
		}
	}
}

type cliLogger struct{}

func (cliLogger) Warnf(format string, args ...any) {
	finishActiveProgressLine()
	fmt.Fprintf(statusW(), "[warn] "+format+"\n", args...)
}

func finishActiveProgressLine() {
	if activeProgressPrinter != nil {
		activeProgressPrinter.Finish()
	}
}

func isBasicExtractionEvent(evt client.ExtractionEvent) bool {
	if evt.Stage != "player_api_json" {
		return false
	}
	return evt.Phase == "start" || evt.Phase == "success" || evt.Phase == "failure"
}

func isBasicDownloadEvent(evt client.DownloadEvent) bool {
	return evt.Phase == "destination" || evt.Phase == "failure"
}

func versionString() string {
	return "ytv1 " + strings.TrimSpace(currentVersion)
}

func checkAndPrintOutdated(opts cli.Options) {
	if !shouldPrintHumanText(opts) || opts.NoWarnings {
		return
	}
	if _, ok := parseReleaseTag(currentVersion); !ok {
		return
	}
	latest, err := httpGetLatestRelease(context.Background())
	if err != nil || strings.TrimSpace(latest) == "" {
		return
	}
	if compareReleaseTags(currentVersion, latest) < 0 {
		fmt.Fprintf(statusW(), "[warn] ytv1 is outdated: current=%s latest=%s\n", currentVersion, latest)
	}
}

func defaultHTTPGetLatestRelease(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, latestReleaseURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "ytv1")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("github release check failed: status=%d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	var payload struct {
		TagName string `json:"tag_name"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", err
	}
	return strings.TrimSpace(payload.TagName), nil
}

func compareReleaseTags(current, latest string) int {
	c, cok := parseReleaseTag(current)
	l, lok := parseReleaseTag(latest)
	if !cok || !lok {
		return 0
	}
	for i := 0; i < len(c) && i < len(l); i++ {
		if c[i] < l[i] {
			return -1
		}
		if c[i] > l[i] {
			return 1
		}
	}
	return 0
}

func parseReleaseTag(tag string) ([3]int, bool) {
	var out [3]int
	s := strings.TrimSpace(strings.TrimPrefix(tag, "v"))
	if s == "" || s == "dev" {
		return out, false
	}
	re := regexp.MustCompile(`^(\d+)(?:\.(\d+))?(?:\.(\d+))?`)
	matches := re.FindStringSubmatch(s)
	if len(matches) == 0 {
		return out, false
	}
	for i := 1; i <= 3; i++ {
		if matches[i] == "" {
			continue
		}
		v, err := strconv.Atoi(matches[i])
		if err != nil {
			return out, false
		}
		out[i-1] = v
	}
	return out, true
}
