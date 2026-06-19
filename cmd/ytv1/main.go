package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/famomatic/ytv1/client"
	"github.com/famomatic/ytv1/internal/cli"
)

var verboseLifecyclePrinter *lifecyclePrinter
var activeDownloadArchive *client.DownloadArchive
var activeDownloadLimit *downloadLimit
var sleepBeforeRequestFunc = time.Sleep
var sleepBeforeDownloadFunc = time.Sleep
var sleepBeforeSubtitleFunc = time.Sleep

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
	opts := cli.ParseFlags()
	os.Exit(run(opts))
}

func run(opts cli.Options) int {
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
	return exitCode
}

func attachLifecycleHandlers(cfg *client.Config, opts cli.Options) {
	if !opts.Verbose {
		return
	}
	lp := newLifecyclePrinter(time.Now)
	verboseLifecyclePrinter = lp
	cfg.Logger = verboseLogger{}
	cfg.OnExtractionEvent = func(evt client.ExtractionEvent) {
		fmt.Println(lp.formatExtractionEvent(evt))
	}
	cfg.OnDownloadEvent = func(evt client.DownloadEvent) {
		fmt.Println(lp.formatDownloadEvent(evt))
	}
}

type verboseLogger struct{}

func (verboseLogger) Warnf(format string, args ...any) {
	fmt.Printf("[warn] "+format+"\n", args...)
}
