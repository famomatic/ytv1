package client

import (
	"strings"

	"github.com/famomatic/ytv1/internal/innertube"
)

func filterFormatsByPoTokenPolicy(formats []FormatInfo, cfg Config) ([]FormatInfo, []FormatSkipReason) {
	if len(formats) == 0 {
		return nil, nil
	}

	hasProvider := cfg.PoTokenProvider != nil
	kept := make([]FormatInfo, 0, len(formats))
	skips := make([]FormatSkipReason, 0)

	for _, f := range formats {
		protocol := protocolFromFormat(f)
		policy := effectivePoTokenFetchPolicy(protocol, cfg.PoTokenFetchPolicy)
		if policy == innertube.PoTokenFetchPolicyRequired && !hasProvider {
			skips = append(skips, FormatSkipReason{
				Itag:     f.Itag,
				Protocol: string(protocol),
				Reason:   "missing_po_token_provider",
			})
			continue
		}
		kept = append(kept, f)
	}

	return kept, skips
}

func protocolFromFormat(f FormatInfo) innertube.VideoStreamingProtocol {
	switch strings.ToLower(strings.TrimSpace(f.Protocol)) {
	case string(innertube.StreamingProtocolUnknown):
		return innertube.StreamingProtocolUnknown
	case string(innertube.StreamingProtocolDASH):
		return innertube.StreamingProtocolDASH
	case string(innertube.StreamingProtocolHLS):
		return innertube.StreamingProtocolHLS
	default:
		return innertube.StreamingProtocolHTTPS
	}
}

func effectivePoTokenFetchPolicy(protocol innertube.VideoStreamingProtocol, override map[innertube.VideoStreamingProtocol]innertube.PoTokenFetchPolicy) innertube.PoTokenFetchPolicy {
	if override != nil {
		if p, ok := override[protocol]; ok {
			return normalizePoTokenFetchPolicy(p)
		}
	}

	// Default non-blocking behavior for compatibility; callers can override to required.
	switch protocol {
	case innertube.StreamingProtocolHTTPS, innertube.StreamingProtocolDASH, innertube.StreamingProtocolHLS:
		return innertube.PoTokenFetchPolicyRecommended
	default:
		return innertube.PoTokenFetchPolicyNever
	}
}

func normalizePoTokenFetchPolicy(p innertube.PoTokenFetchPolicy) innertube.PoTokenFetchPolicy {
	switch innertube.PoTokenFetchPolicy(strings.ToLower(strings.TrimSpace(string(p)))) {
	case innertube.PoTokenFetchPolicyRequired:
		return innertube.PoTokenFetchPolicyRequired
	case innertube.PoTokenFetchPolicyRecommended:
		return innertube.PoTokenFetchPolicyRecommended
	case innertube.PoTokenFetchPolicyNever:
		return innertube.PoTokenFetchPolicyNever
	default:
		return innertube.PoTokenFetchPolicyRecommended
	}
}
