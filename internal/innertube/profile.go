package innertube

import "net/http"

// VideoStreamingProtocol represents the protocol used for video streaming.
type VideoStreamingProtocol string

const (
	StreamingProtocolUnknown VideoStreamingProtocol = "unknown"
	StreamingProtocolHTTPS   VideoStreamingProtocol = "https"
	StreamingProtocolDASH    VideoStreamingProtocol = "dash"
	StreamingProtocolHLS     VideoStreamingProtocol = "hls"
)

// PoTokenPolicy defines the policy for Proof of Origin (PO) Tokens.
type PoTokenPolicy struct {
	Required                   bool
	Recommended                bool
	NotRequiredForPremium      bool
	NotRequiredWithPlayerToken bool
}

// PlayerPoTokenPolicy mirrors yt-dlp's PLAYER_PO_TOKEN_POLICY: the PoT policy
// for the /player API request itself, independent of GVS streaming policies.
// A nil value means the player request policy is derived from the GVS map.
type PlayerPoTokenPolicy struct {
	Required    bool
	Recommended bool
}

// PoTokenFetchPolicy controls how strictly POT fetching is enforced.
type PoTokenFetchPolicy string

const (
	PoTokenFetchPolicyRequired    PoTokenFetchPolicy = "required"
	PoTokenFetchPolicyRecommended PoTokenFetchPolicy = "recommended"
	PoTokenFetchPolicyNever       PoTokenFetchPolicy = "never"
)

type ClientProfile struct {
	// ID is the registry/client alias used for policy and diagnostics
	// (e.g. "web_safari"), distinct from Innertube clientName ("WEB").
	ID                        string
	Name                      string
	Version                   string
	APIKey                    string
	UserAgent                 string
	ContextNameID             int
	RequireJSPlayer           bool
	SupportsCookies           bool
	SupportsAdPlaybackContext bool
	RequiresAuth              bool
	Host                      string
	Headers                   http.Header
	Screen                    string // e.g. "EMBED"
	PlayerParams              string

	// PoTokenPolicy map keyed by protocol (https, dash, hls).
	PoTokenPolicy map[VideoStreamingProtocol]PoTokenPolicy

	// PlayerPoTokenPolicy overrides the GVS-derived policy for player API
	// requests (e.g. android/ios mark it recommended-only upstream).
	PlayerPoTokenPolicy *PlayerPoTokenPolicy
}

type Registry interface {
	Get(name string) (ClientProfile, bool)
	All() []ClientProfile
}
