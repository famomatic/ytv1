// Package source defines the multi-source extraction seam for ytv1.
//
// Each supported site (YouTube, SOOP, ...) can be modeled as a Source: it
// recognizes its own input shapes (Matches) and turns an input into a
// site-neutral Media result (Extract) carrying normalized formats plus any
// extra HTTP headers the media/manifest requests require.
//
// Sources self-register from their package init via Register; the client builds
// the active set with Build. This keeps site-specific extraction out of the
// generic client/download/mux pipeline, so adding a new site is a matter of
// adding one package under internal/source/<name> and blank-importing it.
package source

import (
	"context"
	"net/http"

	"github.com/famomatic/ytv1/internal/types"
)

// Media is the site-neutral result of extracting an input. It is the common
// currency between a Source and the generic download pipeline.
type Media struct {
	// ID is a stable, source-scoped identifier (e.g. a SOOP title/broadcast
	// number). Combined with the source name it keys caches and output names.
	ID string

	Title        string
	Author       string
	Description  string
	DurationSec  int64
	ThumbnailURL string
	IsLive       bool
	UploadDate   string
	WebpageURL   string

	// Formats are the normalized, selectable streams. For HLS sources these are
	// the master-playlist variants (highest first is not required; the caller
	// selects by quality).
	Formats []types.FormatInfo

	// MediaHeaders are extra HTTP headers that must accompany manifest and
	// segment requests for this media (e.g. Referer/Origin for CDN auth). They
	// override any defaults the download pipeline would otherwise set.
	MediaHeaders http.Header
}

// Source extracts media from a specific site.
type Source interface {
	// Name is a short, stable identifier for the source (e.g. "soop"). It tags
	// formats and namespaces cache/output identifiers.
	Name() string

	// Matches reports whether this source recognizes the input (URL/ID shape).
	// It must not perform network I/O.
	Matches(input string) bool

	// Extract resolves the input into site-neutral Media, including normalized
	// formats and any required media request headers.
	Extract(ctx context.Context, input string) (*Media, error)
}

// Deps carries shared dependencies handed to a Source constructor.
type Deps struct {
	// HTTPClient is the client the source should use for its API/manifest calls.
	// It carries the caller's proxy, cookie jar, and transport configuration.
	HTTPClient *http.Client
}

// Constructor builds a Source from shared deps. Returning nil disables the
// source (e.g. when a required dependency is missing).
type Constructor func(Deps) Source

var registry []Constructor

// Register adds a Source constructor to the global registry. Intended to be
// called from a source package's init().
func Register(c Constructor) {
	if c != nil {
		registry = append(registry, c)
	}
}

// Build instantiates every registered source with the given deps, skipping any
// that construct to nil. The returned slice preserves registration order.
func Build(d Deps) []Source {
	out := make([]Source, 0, len(registry))
	for _, c := range registry {
		if s := c(d); s != nil {
			out = append(out, s)
		}
	}
	return out
}

// Match returns the first source in the set that recognizes the input, or nil.
func Match(sources []Source, input string) Source {
	for _, s := range sources {
		if s != nil && s.Matches(input) {
			return s
		}
	}
	return nil
}
