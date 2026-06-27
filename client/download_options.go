package client

import (
	"path/filepath"
	"strconv"
	"strings"
)

// DefaultOutputTemplate is the yt-dlp-style package default media filename.
const DefaultOutputTemplate = "%(title)s [%(id)s].%(ext)s"

// OutputTemplateOptions describes package-level output template shortcuts.
type OutputTemplateOptions struct {
	OutputTemplate string
	OutputUseID    bool
	OutputPathDir  string
}

// EffectiveOutputTemplate applies common output-template shortcuts and output
// directory composition.
func EffectiveOutputTemplate(opts OutputTemplateOptions) string {
	outputTemplate := strings.TrimSpace(opts.OutputTemplate)
	if opts.OutputUseID {
		outputTemplate = "%(id)s.%(ext)s"
	}
	outputDir := strings.TrimSpace(opts.OutputPathDir)
	if outputDir == "" {
		return outputTemplate
	}
	if outputTemplate == "" {
		return filepath.Join(outputDir, DefaultOutputTemplate)
	}
	if filepath.IsAbs(outputTemplate) {
		return outputTemplate
	}
	return filepath.Join(outputDir, outputTemplate)
}

// DownloadOptionsRequest captures user-facing selection and file-output choices
// without depending on the CLI parser package.
type DownloadOptionsRequest struct {
	FormatSelector        string
	OutputTemplate        string
	OutputUseID           bool
	OutputPathDir         string
	ExtractAudio          bool
	AudioFormat           string
	KeepIntermediateFiles bool
	Resume                bool
	UsePartFiles          bool
	AudioQuality          string
	EmbedMetadata         bool
	MergeOutputFormat     string
	RemuxVideo            string
}

// BuildDownloadOptions maps common yt-dlp-style selection aliases into package
// download options.
func BuildDownloadOptions(req DownloadOptionsRequest) DownloadOptions {
	downloadOpts := DownloadOptions{
		Mode:                  SelectionModeBest,
		OutputPath:            EffectiveOutputTemplate(OutputTemplateOptions{OutputTemplate: req.OutputTemplate, OutputUseID: req.OutputUseID, OutputPathDir: req.OutputPathDir}),
		MergeOutput:           true,
		KeepIntermediateFiles: req.KeepIntermediateFiles,
		Resume:                req.Resume,
		UsePartFiles:          req.UsePartFiles,
		AudioQuality:          strings.TrimSpace(req.AudioQuality),
		NoEmbedMetadata:       !req.EmbedMetadata,
		MergeOutputFormat:     EffectiveMergeOutputExt(req.MergeOutputFormat, req.RemuxVideo),
	}

	raw := strings.TrimSpace(req.FormatSelector)
	if req.ExtractAudio && (raw == "" || strings.EqualFold(raw, "best")) {
		switch strings.ToLower(strings.TrimSpace(req.AudioFormat)) {
		case "mp3":
			raw = "mp3"
		default:
			raw = "bestaudio"
		}
	}
	lower := strings.ToLower(raw)
	switch lower {
	case "", "best":
		return downloadOpts
	case "bestvideo+bestaudio":
		downloadOpts.FormatSelector = "bestvideo+bestaudio/best"
		return downloadOpts
	case "bestaudio", "audioonly":
		downloadOpts.Mode = SelectionModeAudioOnly
		return downloadOpts
	case "bestvideo", "videoonly":
		downloadOpts.Mode = SelectionModeVideoOnly
		return downloadOpts
	case "mp4":
		downloadOpts.Mode = SelectionModeMP4AV
		return downloadOpts
	case "mp3":
		downloadOpts.Mode = SelectionModeMP3
		return downloadOpts
	}

	if itag, err := strconv.Atoi(lower); err == nil && itag > 0 {
		downloadOpts.Itag = itag
		return downloadOpts
	}

	downloadOpts.FormatSelector = raw
	return downloadOpts
}
