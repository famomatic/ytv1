package client

import (
	"context"
	"net/url"
	"strconv"
	"strings"

	"github.com/famomatic/ytv1/internal/innertube"
	"github.com/famomatic/ytv1/internal/playerjs"
)

type challengeSolutions struct {
	n   map[string]string
	sig map[string]string
}

func (c *Client) primeChallengeSolutions(
	ctx context.Context,
	playerURL string,
	resp *innertube.PlayerResponse,
	dashManifestURL string,
	hlsManifestURL string,
) {
	if c == nil || playerURL == "" || resp == nil {
		return
	}

	nChallenges, sigChallenges := collectStreamChallenges(resp, dashManifestURL, hlsManifestURL)
	if len(nChallenges) == 0 && len(sigChallenges) == 0 {
		return
	}

	c.emitExtractionEvent("challenge", "start", "web", playerURL)

	decipherer, err := c.loadDecipherer(ctx, playerURL)
	if err != nil {
		c.emitExtractionEvent("challenge", "failure", "web", err.Error())
		return
	}

	failures := 0
	for challenge := range nChallenges {
		if _, ok := c.getChallengeN(playerURL, challenge); ok {
			continue
		}
		decoded, err := decipherer.DecipherN(challenge)
		if err != nil {
			failures++
			continue
		}
		c.setChallengeN(playerURL, challenge, decoded)
	}

	for challenge := range sigChallenges {
		if _, ok := c.getChallengeSig(playerURL, challenge); ok {
			continue
		}
		decoded, err := decipherer.DecipherSignature(challenge)
		if err != nil {
			failures++
			continue
		}
		c.setChallengeSig(playerURL, challenge, decoded)
	}

	if failures > 0 {
		c.emitExtractionEvent("challenge", "partial", "web", "unsolved="+itoa(failures))
		return
	}
	c.emitExtractionEvent("challenge", "success", "web", "n="+itoa(len(nChallenges))+",sig="+itoa(len(sigChallenges)))
}

func (c *Client) decodeNWithCache(ctx context.Context, playerURL, challenge string) (string, error) {
	if decoded, ok := c.getChallengeN(playerURL, challenge); ok {
		return decoded, nil
	}
	decipherer, err := c.loadDecipherer(ctx, playerURL)
	if err != nil {
		return "", err
	}
	decoded, err := decipherer.DecipherN(challenge)
	if err != nil {
		return "", err
	}
	c.setChallengeN(playerURL, challenge, decoded)
	return decoded, nil
}

func (c *Client) decodeSignatureWithCache(ctx context.Context, playerURL, challenge string) (string, error) {
	if decoded, ok := c.getChallengeSig(playerURL, challenge); ok {
		return decoded, nil
	}
	decipherer, err := c.loadDecipherer(ctx, playerURL)
	if err != nil {
		return "", err
	}
	decoded, err := decipherer.DecipherSignature(challenge)
	if err != nil {
		return "", err
	}
	c.setChallengeSig(playerURL, challenge, decoded)
	return decoded, nil
}

func (c *Client) loadDecipherer(ctx context.Context, playerURL string) (*playerjs.Decipherer, error) {
	c.emitExtractionEvent("player_js", "start", "web", playerURL)
	jsBody, err := c.playerJSResolver.GetPlayerJS(ctx, playerURL)
	if err != nil {
		c.emitExtractionEvent("player_js", "failure", "web", err.Error())
		return nil, err
	}
	c.emitExtractionEvent("player_js", "success", "web", playerURL)
	return playerjs.NewDecipherer(jsBody), nil
}

func (c *Client) getChallengeN(playerURL, challenge string) (string, bool) {
	c.challengesMu.RLock()
	defer c.challengesMu.RUnlock()
	s, ok := c.challenges[playerURL]
	if !ok || s.n == nil {
		return "", false
	}
	decoded, ok := s.n[challenge]
	return decoded, ok
}

func (c *Client) getChallengeSig(playerURL, challenge string) (string, bool) {
	c.challengesMu.RLock()
	defer c.challengesMu.RUnlock()
	s, ok := c.challenges[playerURL]
	if !ok || s.sig == nil {
		return "", false
	}
	decoded, ok := s.sig[challenge]
	return decoded, ok
}

func (c *Client) setChallengeN(playerURL, challenge, decoded string) {
	c.challengesMu.Lock()
	defer c.challengesMu.Unlock()
	if c.challenges == nil {
		c.challenges = make(map[string]challengeSolutions)
	}
	s := c.challenges[playerURL]
	if s.n == nil {
		s.n = make(map[string]string)
	}
	s.n[challenge] = decoded
	c.challenges[playerURL] = s
}

func (c *Client) setChallengeSig(playerURL, challenge, decoded string) {
	c.challengesMu.Lock()
	defer c.challengesMu.Unlock()
	if c.challenges == nil {
		c.challenges = make(map[string]challengeSolutions)
	}
	s := c.challenges[playerURL]
	if s.sig == nil {
		s.sig = make(map[string]string)
	}
	s.sig[challenge] = decoded
	c.challenges[playerURL] = s
}

func collectStreamChallenges(resp *innertube.PlayerResponse, dashManifestURL, hlsManifestURL string) (map[string]struct{}, map[string]struct{}) {
	nChallenges := make(map[string]struct{})
	sigChallenges := make(map[string]struct{})
	if resp == nil {
		return nChallenges, sigChallenges
	}
	all := make([]innertube.Format, 0, len(resp.StreamingData.Formats)+len(resp.StreamingData.AdaptiveFormats))
	all = append(all, resp.StreamingData.Formats...)
	all = append(all, resp.StreamingData.AdaptiveFormats...)

	for _, f := range all {
		collectNFromURL(f.URL, nChallenges)
		cipher := f.SignatureCipher
		if cipher == "" {
			cipher = f.Cipher
		}
		if strings.TrimSpace(cipher) == "" {
			continue
		}
		q, err := url.ParseQuery(cipher)
		if err != nil {
			continue
		}
		if s := strings.TrimSpace(q.Get("s")); s != "" {
			sigChallenges[s] = struct{}{}
		}
		collectNFromURL(q.Get("url"), nChallenges)
	}

	collectNFromURL(dashManifestURL, nChallenges)
	collectNFromURL(hlsManifestURL, nChallenges)

	return nChallenges, sigChallenges
}

func collectNFromURL(rawURL string, out map[string]struct{}) {
	if strings.TrimSpace(rawURL) == "" {
		return
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return
	}
	if n := strings.TrimSpace(u.Query().Get("n")); n != "" {
		out[n] = struct{}{}
	}
}

func itoa(v int) string {
	return strconv.Itoa(v)
}
