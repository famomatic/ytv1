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
		if f.IsDRM {
			skips = append(skips, FormatSkipReason{
				Itag:     f.Itag,
				Protocol: f.Protocol,
				Reason:   "drm_protected",
			})
			continue
		}
		if f.IsDamaged {
			skips = append(skips, FormatSkipReason{
				Itag:     f.Itag,
				Protocol: f.Protocol,
				Reason:   "damaged_format",
			})
			continue
		}
		protocol := protocolFromFormat(f)
		policy := poTokenFetchPolicyForSourceClient(f.SourceClient, protocol, cfg.PoTokenFetchPolicy)
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

func poTokenFetchPolicyForSourceClient(
	sourceClient string,
	protocol innertube.VideoStreamingProtocol,
	override map[innertube.VideoStreamingProtocol]innertube.PoTokenFetchPolicy,
) innertube.PoTokenFetchPolicy {
	if override != nil {
		if p, ok := override[protocol]; ok {
			return normalizePoTokenFetchPolicy(p)
		}
	}
	if profile, ok := resolveSourceClientProfile(sourceClient); ok {
		if policy, exists := profile.PoTokenPolicy[protocol]; exists {
			if policy.Required || policy.Recommended {
				// Keep default fetch mode non-blocking unless explicit override
				// requests required behavior.
				return innertube.PoTokenFetchPolicyRecommended
			}
			return innertube.PoTokenFetchPolicyNever
		}
	}
	return effectivePoTokenFetchPolicy(protocol, override)
}

func resolveSourceClientProfile(sourceClient string) (innertube.ClientProfile, bool) {
	id := strings.ToLower(strings.TrimSpace(sourceClient))
	if id == "" {
		return innertube.ClientProfile{}, false
	}
	registry := innertube.NewRegistry()
	if profile, ok := registry.Get(id); ok {
		return profile, true
	}
	for _, profile := range registry.All() {
		if strings.EqualFold(profile.Name, sourceClient) {
			return profile, true
		}
	}
	return innertube.ClientProfile{}, false
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
