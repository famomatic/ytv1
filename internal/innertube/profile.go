package innertube

import "net/http"

// VideoStreamingProtocol represents the protocol used for video streaming.
type VideoStreamingProtocol string

const (
	StreamingProtocolHTTPS VideoStreamingProtocol = "https"
	StreamingProtocolDASH  VideoStreamingProtocol = "dash"
	StreamingProtocolHLS   VideoStreamingProtocol = "hls"
)

// PoTokenPolicy defines the policy for Proof of Origin (PO) Tokens.
type PoTokenPolicy struct {
	Required                   bool
	Recommended                bool
	NotRequiredForPremium      bool
	NotRequiredWithPlayerToken bool
}

// PoTokenFetchPolicy controls how strictly POT fetching is enforced.
type PoTokenFetchPolicy string

const (
	PoTokenFetchPolicyRequired    PoTokenFetchPolicy = "required"
	PoTokenFetchPolicyRecommended PoTokenFetchPolicy = "recommended"
	PoTokenFetchPolicyNever       PoTokenFetchPolicy = "never"
)

type ClientProfile struct {
	Name            string
	Version         string
	APIKey          string
	UserAgent       string
	ContextNameID   int
	RequireJSPlayer bool
	SupportsCookies bool
	RequiresAuth    bool
	Host            string
	Headers         http.Header
	Screen          string // e.g. "EMBED"

	// PoTokenPolicy map keyed by protocol (https, dash, hls).
	PoTokenPolicy map[VideoStreamingProtocol]PoTokenPolicy
}

type Registry interface {
	Get(name string) (ClientProfile, bool)
	All() []ClientProfile
}
