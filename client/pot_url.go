package client

import (
	"context"
	"net/url"
	"strings"

	"github.com/famomatic/ytv1/internal/innertube"
	"github.com/famomatic/ytv1/internal/orchestrator"
)

func protocolFromURL(rawURL string) innertube.VideoStreamingProtocol {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return innertube.StreamingProtocolUnknown
	}
	switch strings.ToLower(strings.TrimSpace(u.Scheme)) {
	case "http", "https":
		return innertube.StreamingProtocolHTTPS
	case "dash":
		return innertube.StreamingProtocolDASH
	case "hls":
		return innertube.StreamingProtocolHLS
	default:
		return innertube.StreamingProtocolUnknown
	}
}

func hasPoTokenInURL(rawURL string) bool {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return false
	}
	if strings.TrimSpace(u.Query().Get("pot")) != "" {
		return true
	}
	return strings.Contains(u.Path, "/pot/")
}

func injectPoToken(rawURL string, token string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set("pot", strings.TrimSpace(token))
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func poTokenProviderClientID(sourceClient string) string {
	if profile, ok := resolveSourceClientProfile(sourceClient); ok {
		if name := strings.TrimSpace(profile.Name); name != "" {
			return name
		}
	}
	return strings.TrimSpace(sourceClient)
}

func (c *Client) applyPoTokenPolicyToURL(
	ctx context.Context,
	rawURL string,
	sourceClient string,
	protocol innertube.VideoStreamingProtocol,
) (string, error) {
	if strings.TrimSpace(rawURL) == "" || hasPoTokenInURL(rawURL) {
		return rawURL, nil
	}
	policy := poTokenFetchPolicyForSourceClient(sourceClient, protocol, c.config.PoTokenFetchPolicy)
	if policy == innertube.PoTokenFetchPolicyNever {
		return rawURL, nil
	}

	if c.config.PoTokenProvider == nil {
		if policy == innertube.PoTokenFetchPolicyRequired {
			return "", &orchestrator.PoTokenRequiredError{
				Client:            sourceClient,
				Cause:             "provider missing (required by policy)",
				Policy:            innertube.PoTokenFetchPolicyRequired,
				Protocols:         []innertube.VideoStreamingProtocol{protocol},
				ProviderAvailable: false,
			}
		}
		return rawURL, nil
	}

	clientID := poTokenProviderClientID(sourceClient)
	token, err := c.config.PoTokenProvider.GetToken(ctx, clientID)
	if err != nil {
		if policy == innertube.PoTokenFetchPolicyRequired {
			return "", &orchestrator.PoTokenRequiredError{
				Client:            sourceClient,
				Cause:             "provider error: " + err.Error(),
				Policy:            innertube.PoTokenFetchPolicyRequired,
				Protocols:         []innertube.VideoStreamingProtocol{protocol},
				ProviderAvailable: true,
			}
		}
		c.warnf("po token provider error; using url without pot (client=%s protocol=%s): %v", sourceClient, protocol, err)
		return rawURL, nil
	}
	if strings.TrimSpace(token) == "" {
		if policy == innertube.PoTokenFetchPolicyRequired {
			return "", &orchestrator.PoTokenRequiredError{
				Client:            sourceClient,
				Cause:             "empty token from provider",
				Policy:            innertube.PoTokenFetchPolicyRequired,
				Protocols:         []innertube.VideoStreamingProtocol{protocol},
				ProviderAvailable: true,
			}
		}
		return rawURL, nil
	}
	return injectPoToken(rawURL, token)
}
