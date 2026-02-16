package orchestrator

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/famomatic/ytv1/internal/innertube"
)

type selectorStub struct {
	clients []innertube.ClientProfile
}

func (s selectorStub) Select(string) []innertube.ClientProfile { return s.clients }
func (s selectorStub) Registry() innertube.Registry            { return innertube.NewRegistry() }

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type poTokenProviderStub struct {
	token    string
	err      error
	called   int32
	clientID string
}

func (p *poTokenProviderStub) GetToken(_ context.Context, clientID string) (string, error) {
	atomic.AddInt32(&p.called, 1)
	p.clientID = clientID
	return p.token, p.err
}

func TestEngineFallsBackToEmbeddedPhase(t *testing.T) {
	android := innertube.AndroidClient
	embedded := innertube.WebEmbeddedClient

	tr := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(r.Body)
		payload := string(body)
		var response string
		switch {
		case strings.Contains(payload, `"clientName":"ANDROID"`):
			response = `{"playabilityStatus":{"status":"LOGIN_REQUIRED","reason":"Sign in to confirm your age"}}`
		case strings.Contains(payload, `"clientName":"WEB_EMBEDDED_PLAYER"`):
			response = `{"playabilityStatus":{"status":"OK"},"videoDetails":{"videoId":"jNQXAC9IVRw","title":"ok","author":"yt"}}`
		default:
			response = `{"playabilityStatus":{"status":"UNPLAYABLE","reason":"unexpected"}}`
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewBufferString(response)),
			Header:     make(http.Header),
		}, nil
	})

	engine := NewEngine(
		selectorStub{clients: []innertube.ClientProfile{android, embedded}},
		innertube.Config{HTTPClient: &http.Client{Transport: tr}},
	)

	resp, err := engine.GetVideoInfo(context.Background(), "jNQXAC9IVRw")
	if err != nil {
		t.Fatalf("GetVideoInfo() error = %v", err)
	}
	if resp == nil || resp.PlayabilityStatus.Status != "OK" {
		t.Fatalf("expected OK response from fallback phase")
	}
}

func TestEngineSkipsFallbackOnHTTPFailureOnly(t *testing.T) {
	mweb := innertube.MWebClient
	embedded := innertube.WebEmbeddedClient
	var embeddedCalls int32

	tr := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(r.Body)
		payload := string(body)
		switch {
		case strings.Contains(payload, `"clientName":"MWEB"`):
			return &http.Response{
				StatusCode: http.StatusInternalServerError,
				Body:       io.NopCloser(bytes.NewBufferString(`server error`)),
				Header:     make(http.Header),
			}, nil
		case strings.Contains(payload, `"clientName":"WEB_EMBEDDED_PLAYER"`):
			atomic.AddInt32(&embeddedCalls, 1)
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewBufferString(`{"playabilityStatus":{"status":"OK"}}`)),
				Header:     make(http.Header),
			}, nil
		default:
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewBufferString(`{"playabilityStatus":{"status":"UNPLAYABLE"}}`)),
				Header:     make(http.Header),
			}, nil
		}
	})

	engine := NewEngine(
		selectorStub{clients: []innertube.ClientProfile{mweb, embedded}},
		innertube.Config{HTTPClient: &http.Client{Transport: tr}},
	)

	_, err := engine.GetVideoInfo(context.Background(), "jNQXAC9IVRw")
	if err == nil {
		t.Fatalf("expected error when only http status failure occurs")
	}
	if atomic.LoadInt32(&embeddedCalls) != 0 {
		t.Fatalf("embedded fallback should be skipped for non-playability failures")
	}
}

func TestEngineInjectsPoTokenWhenProviderConfigured(t *testing.T) {
	web := innertube.WebClient
	provider := &poTokenProviderStub{token: "po-token-123"}
	var sawPoToken int32

	tr := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(r.Body)
		payload := string(body)
		if strings.Contains(payload, `"poToken":"po-token-123"`) {
			atomic.StoreInt32(&sawPoToken, 1)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewBufferString(`{"playabilityStatus":{"status":"OK"},"videoDetails":{"videoId":"jNQXAC9IVRw","title":"ok","author":"yt"}}`)),
			Header:     make(http.Header),
		}, nil
	})

	engine := NewEngine(
		selectorStub{clients: []innertube.ClientProfile{web}},
		innertube.Config{
			HTTPClient:      &http.Client{Transport: tr},
			PoTokenProvider: provider,
		},
	)

	_, err := engine.GetVideoInfo(context.Background(), "jNQXAC9IVRw")
	if err != nil {
		t.Fatalf("GetVideoInfo() error = %v", err)
	}
	if atomic.LoadInt32(&provider.called) == 0 {
		t.Fatalf("expected PoTokenProvider to be called")
	}
	if provider.clientID != web.Name {
		t.Fatalf("provider clientID = %q, want %q", provider.clientID, web.Name)
	}
	if atomic.LoadInt32(&sawPoToken) == 0 {
		t.Fatalf("expected poToken in request payload")
	}
}

func TestEngineInjectsVisitorDataFromConfig(t *testing.T) {
	web := innertube.WebClient
	var sawVisitorData int32

	tr := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(r.Body)
		payload := string(body)
		if strings.Contains(payload, `"visitorData":"visitor-abc"`) {
			atomic.StoreInt32(&sawVisitorData, 1)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewBufferString(`{"playabilityStatus":{"status":"OK"},"videoDetails":{"videoId":"jNQXAC9IVRw","title":"ok","author":"yt"}}`)),
			Header:     make(http.Header),
		}, nil
	})

	engine := NewEngine(
		selectorStub{clients: []innertube.ClientProfile{web}},
		innertube.Config{
			HTTPClient:  &http.Client{Transport: tr},
			VisitorData: "visitor-abc",
		},
	)

	_, err := engine.GetVideoInfo(context.Background(), "jNQXAC9IVRw")
	if err != nil {
		t.Fatalf("GetVideoInfo() error = %v", err)
	}
	if atomic.LoadInt32(&sawVisitorData) == 0 {
		t.Fatalf("expected visitorData in request payload")
	}
}

func TestEngineProceedsWithoutPoTokenWhenProviderMissing(t *testing.T) {
	web := innertube.WebClient
	var calls int32
	engine := NewEngine(
		selectorStub{clients: []innertube.ClientProfile{web}},
		innertube.Config{HTTPClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			atomic.AddInt32(&calls, 1)
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewBufferString(`{"playabilityStatus":{"status":"OK"},"videoDetails":{"videoId":"jNQXAC9IVRw","title":"ok","author":"yt"}}`)),
				Header:     make(http.Header),
			}, nil
		})}},
	)

	resp, err := engine.GetVideoInfo(context.Background(), "jNQXAC9IVRw")
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if resp == nil || resp.PlayabilityStatus.Status != "OK" {
		t.Fatalf("expected OK response")
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("expected exactly 1 http call")
	}
}

func TestEngineFailsWithoutPoTokenWhenPolicyOverrideRequired(t *testing.T) {
	web := innertube.WebClient
	engine := NewEngine(
		selectorStub{clients: []innertube.ClientProfile{web}},
		innertube.Config{
			HTTPClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(bytes.NewBufferString(`{"playabilityStatus":{"status":"OK"}}`)),
					Header:     make(http.Header),
				}, nil
			})},
			PoTokenFetchPolicy: map[innertube.VideoStreamingProtocol]innertube.PoTokenFetchPolicy{
				innertube.StreamingProtocolHTTPS: innertube.PoTokenFetchPolicyRequired,
			},
		},
	)

	_, err := engine.GetVideoInfo(context.Background(), "jNQXAC9IVRw")
	if err == nil {
		t.Fatalf("expected error for required POT without provider")
	}
	var allErr *AllClientsFailedError
	if !errors.As(err, &allErr) {
		t.Fatalf("expected AllClientsFailedError, got %T", err)
	}
	if len(allErr.Attempts) == 0 {
		t.Fatalf("expected at least one attempt error")
	}
	var poErr *PoTokenRequiredError
	if !errors.As(allErr.Attempts[0].Err, &poErr) {
		t.Fatalf("expected PoTokenRequiredError, got %T", allErr.Attempts[0].Err)
	}
	if poErr.Policy != innertube.PoTokenFetchPolicyRequired {
		t.Fatalf("policy = %q, want %q", poErr.Policy, innertube.PoTokenFetchPolicyRequired)
	}
	if poErr.ProviderAvailable {
		t.Fatalf("expected ProviderAvailable=false")
	}
	if len(poErr.Protocols) != 1 || poErr.Protocols[0] != innertube.StreamingProtocolHTTPS {
		t.Fatalf("unexpected protocols: %+v", poErr.Protocols)
	}
}

func TestEngineProceedsWithoutPoTokenWhenProviderFails(t *testing.T) {
	web := innertube.WebClient
	provider := &poTokenProviderStub{err: errors.New("provider down")}
	var calls int32

	engine := NewEngine(
		selectorStub{clients: []innertube.ClientProfile{web}},
		innertube.Config{
			HTTPClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				atomic.AddInt32(&calls, 1)
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(bytes.NewBufferString(`{"playabilityStatus":{"status":"OK"},"videoDetails":{"videoId":"jNQXAC9IVRw","title":"ok","author":"yt"}}`)),
					Header:     make(http.Header),
				}, nil
			})},
			PoTokenProvider: provider,
		},
	)

	resp, err := engine.GetVideoInfo(context.Background(), "jNQXAC9IVRw")
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if resp == nil || resp.PlayabilityStatus.Status != "OK" {
		t.Fatalf("expected OK response")
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("expected exactly 1 http call")
	}
}

func TestEngineAddsFallbackClientsWhenOverridesOmitThem(t *testing.T) {
	web := innertube.WebClient
	var embeddedCalls int32

	tr := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(r.Body)
		payload := string(body)
		switch {
		case strings.Contains(payload, `"clientName":"WEB"`):
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewBufferString(`{"playabilityStatus":{"status":"LOGIN_REQUIRED","reason":"Sign in to confirm your age"}}`)),
				Header:     make(http.Header),
			}, nil
		case strings.Contains(payload, `"clientName":"WEB_EMBEDDED_PLAYER"`):
			atomic.AddInt32(&embeddedCalls, 1)
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewBufferString(`{"playabilityStatus":{"status":"OK"},"videoDetails":{"videoId":"jNQXAC9IVRw","title":"ok","author":"yt"}}`)),
				Header:     make(http.Header),
			}, nil
		case strings.Contains(payload, `"clientName":"TVHTML5"`):
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewBufferString(`{"playabilityStatus":{"status":"UNPLAYABLE","reason":"restricted"}}`)),
				Header:     make(http.Header),
			}, nil
		default:
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewBufferString(`{"playabilityStatus":{"status":"UNPLAYABLE","reason":"unexpected"}}`)),
				Header:     make(http.Header),
			}, nil
		}
	})

	engine := NewEngine(
		selectorStub{clients: []innertube.ClientProfile{web}},
		innertube.Config{HTTPClient: &http.Client{Transport: tr}},
	)

	resp, err := engine.GetVideoInfo(context.Background(), "jNQXAC9IVRw")
	if err != nil {
		t.Fatalf("GetVideoInfo() error = %v", err)
	}
	if resp == nil || resp.PlayabilityStatus.Status != "OK" {
		t.Fatalf("expected OK response from fallback path")
	}
	if atomic.LoadInt32(&embeddedCalls) == 0 {
		t.Fatalf("expected embedded fallback client to be attempted")
	}
}

func TestEngineMixedFailureMatrixIncludesHTTPAndPlayability(t *testing.T) {
	web := innertube.WebClient
	mweb := innertube.MWebClient
	embedded := innertube.WebEmbeddedClient

	tr := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(r.Body)
		payload := string(body)
		switch {
		case strings.Contains(payload, `"clientName":"WEB"`):
			return &http.Response{
				StatusCode: http.StatusBadRequest,
				Body:       io.NopCloser(bytes.NewBufferString(`bad request`)),
				Header:     make(http.Header),
			}, nil
		case strings.Contains(payload, `"clientName":"MWEB"`):
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewBufferString(`{"playabilityStatus":{"status":"UNPLAYABLE","reason":"Blocked in your country"}}`)),
				Header:     make(http.Header),
			}, nil
		case strings.Contains(payload, `"clientName":"WEB_EMBEDDED_PLAYER"`):
			return &http.Response{
				StatusCode: http.StatusBadGateway,
				Body:       io.NopCloser(bytes.NewBufferString(`gateway error`)),
				Header:     make(http.Header),
			}, nil
		case strings.Contains(payload, `"clientName":"TVHTML5"`):
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewBufferString(`{"playabilityStatus":{"status":"UNPLAYABLE","reason":"tv restricted"}}`)),
				Header:     make(http.Header),
			}, nil
		default:
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewBufferString(`{"playabilityStatus":{"status":"UNPLAYABLE","reason":"unexpected"}}`)),
				Header:     make(http.Header),
			}, nil
		}
	})

	engine := NewEngine(
		selectorStub{clients: []innertube.ClientProfile{web, mweb, embedded}},
		innertube.Config{HTTPClient: &http.Client{Transport: tr}},
	)

	_, err := engine.GetVideoInfo(context.Background(), "jNQXAC9IVRw")
	if err == nil {
		t.Fatalf("expected error")
	}

	var allErr *AllClientsFailedError
	if !errors.As(err, &allErr) {
		t.Fatalf("expected AllClientsFailedError, got %T", err)
	}
	if len(allErr.Attempts) < 3 {
		t.Fatalf("expected at least 3 attempts, got %d", len(allErr.Attempts))
	}

	var hasHTTP, hasPlayability bool
	for _, attempt := range allErr.Attempts {
		var httpErr *HTTPStatusError
		if errors.As(attempt.Err, &httpErr) {
			hasHTTP = true
		}
		var playabilityErr *PlayabilityError
		if errors.As(attempt.Err, &playabilityErr) {
			hasPlayability = true
		}
	}
	if !hasHTTP || !hasPlayability {
		t.Fatalf("expected mixed failures (http=%v playability=%v)", hasHTTP, hasPlayability)
	}
}

func TestEnginePlayabilityErrorIncludesTypedDetail(t *testing.T) {
	web := innertube.WebClient
	tr := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body: io.NopCloser(bytes.NewBufferString(`{
				"playabilityStatus":{
					"status":"UNPLAYABLE",
					"reason":"This video is not available in your country",
					"errorScreen":{
						"playerErrorMessageRenderer":{
							"subreason":{"simpleText":"DRM protected stream"}
						}
					}
				},
				"microformat":{
					"playerMicroformatRenderer":{
						"availableCountries":["KR","US"]
					}
				}
			}`)),
			Header: make(http.Header),
		}, nil
	})
	engine := NewEngine(
		selectorStub{clients: []innertube.ClientProfile{web}},
		innertube.Config{HTTPClient: &http.Client{Transport: tr}},
	)

	_, err := engine.GetVideoInfo(context.Background(), "jNQXAC9IVRw")
	if err == nil {
		t.Fatalf("expected error")
	}

	var allErr *AllClientsFailedError
	if !errors.As(err, &allErr) {
		t.Fatalf("expected AllClientsFailedError, got %T", err)
	}
	if len(allErr.Attempts) == 0 {
		t.Fatalf("expected at least one attempt")
	}
	var playErr *PlayabilityError
	for _, attempt := range allErr.Attempts {
		if errors.As(attempt.Err, &playErr) {
			break
		}
	}
	if playErr == nil {
		t.Fatalf("expected a PlayabilityError in attempts")
	}
	if !playErr.IsGeoRestricted() {
		t.Fatalf("expected geo restricted detail")
	}
	if !playErr.IsDRMProtected() {
		t.Fatalf("expected drm protected detail")
	}
	if got := len(playErr.Detail.AvailableCountries); got != 2 {
		t.Fatalf("available countries count = %d, want 2", got)
	}
}

func TestPoTokenPolicyByProtocol(t *testing.T) {
	web := innertube.WebClient
	if !requiresPoToken(web, innertube.StreamingProtocolHTTPS) {
		t.Fatalf("web https should require po token")
	}
	if !requiresPoToken(web, innertube.StreamingProtocolDASH) {
		t.Fatalf("web dash should require po token")
	}
	if requiresPoToken(web, innertube.StreamingProtocolHLS) {
		t.Fatalf("web hls should not require po token")
	}
	if !recommendsPoToken(web, innertube.StreamingProtocolHLS) {
		t.Fatalf("web hls should recommend po token")
	}

	mweb := innertube.MWebClient
	if !requiresPoToken(mweb, innertube.StreamingProtocolHTTPS) {
		t.Fatalf("mweb https should require po token")
	}
	if !recommendsPoToken(mweb, innertube.StreamingProtocolHTTPS) {
		t.Fatalf("mweb https should recommend po token")
	}
}

func TestEngineResolvesAPIKeyFromWatchPageWhenEnabled(t *testing.T) {
	web := innertube.WebClient
	web.APIKey = "stale_fallback_key"

	var sawDynamicKey int32
	var sawVisitorData int32
	var sawAuthUser int32
	var sawPageID int32
	var sawSTS int32
	tr := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/watch":
			return &http.Response{
				StatusCode: http.StatusOK,
				Body: io.NopCloser(bytes.NewBufferString(
					`<script>ytcfg.set({"INNERTUBE_API_KEY":"dynamic_watch_key_789","VISITOR_DATA":"visitor_from_watch","SESSION_INDEX":4,"DELEGATED_SESSION_ID":"delegated_watch","USER_SESSION_ID":"user_watch","STS":20731});</script>`,
				)),
				Header: make(http.Header),
			}, nil
		case r.Method == http.MethodPost && r.URL.Path == "/youtubei/v1/player":
			if r.URL.Query().Get("key") == "dynamic_watch_key_789" {
				atomic.StoreInt32(&sawDynamicKey, 1)
			}
			body, _ := io.ReadAll(r.Body)
			if strings.Contains(string(body), `"visitorData":"visitor_from_watch"`) {
				atomic.StoreInt32(&sawVisitorData, 1)
			}
			if strings.Contains(string(body), `"signatureTimestamp":20731`) {
				atomic.StoreInt32(&sawSTS, 1)
			}
			if r.Header.Get("X-Goog-AuthUser") == "4" {
				atomic.StoreInt32(&sawAuthUser, 1)
			}
			if r.Header.Get("X-Goog-PageId") == "delegated_watch" {
				atomic.StoreInt32(&sawPageID, 1)
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewBufferString(`{"playabilityStatus":{"status":"OK"},"videoDetails":{"videoId":"jNQXAC9IVRw","title":"ok","author":"yt"}}`)),
				Header:     make(http.Header),
			}, nil
		default:
			return &http.Response{
				StatusCode: http.StatusNotFound,
				Body:       io.NopCloser(bytes.NewBufferString(`not found`)),
				Header:     make(http.Header),
			}, nil
		}
	})

	engine := NewEngine(
		selectorStub{clients: []innertube.ClientProfile{web}},
		innertube.Config{
			HTTPClient:                    &http.Client{Transport: tr},
			EnableDynamicAPIKeyResolution: true,
		},
	)

	_, err := engine.GetVideoInfo(context.Background(), "jNQXAC9IVRw")
	if err != nil {
		t.Fatalf("GetVideoInfo() error = %v", err)
	}
	if atomic.LoadInt32(&sawDynamicKey) == 0 {
		t.Fatalf("expected player request to use dynamically resolved API key")
	}
	if atomic.LoadInt32(&sawVisitorData) == 0 {
		t.Fatalf("expected player request to use watch-page visitorData fallback")
	}
	if atomic.LoadInt32(&sawAuthUser) == 0 {
		t.Fatalf("expected player request to use watch-page session index in X-Goog-AuthUser")
	}
	if atomic.LoadInt32(&sawPageID) == 0 {
		t.Fatalf("expected player request to use watch-page delegated session id in X-Goog-PageId")
	}
	if atomic.LoadInt32(&sawSTS) == 0 {
		t.Fatalf("expected player request to use watch-page STS in playback context")
	}
}

func TestEngineAppliesRequestHeaders(t *testing.T) {
	web := innertube.WebClient
	var sawHeader int32
	tr := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("X-Test-Header") == "ytv1" {
			atomic.StoreInt32(&sawHeader, 1)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewBufferString(`{"playabilityStatus":{"status":"OK"},"videoDetails":{"videoId":"jNQXAC9IVRw","title":"ok","author":"yt"}}`)),
			Header:     make(http.Header),
		}, nil
	})
	engine := NewEngine(
		selectorStub{clients: []innertube.ClientProfile{web}},
		innertube.Config{
			HTTPClient: &http.Client{Transport: tr},
			RequestHeaders: http.Header{
				"X-Test-Header": []string{"ytv1"},
			},
		},
	)
	_, err := engine.GetVideoInfo(context.Background(), "jNQXAC9IVRw")
	if err != nil {
		t.Fatalf("GetVideoInfo() error = %v", err)
	}
	if atomic.LoadInt32(&sawHeader) == 0 {
		t.Fatalf("expected custom request header to be sent")
	}
}

func TestEngineAppliesInnertubeIdentityHeaders(t *testing.T) {
	web := innertube.WebClient
	var sawClientName int32
	var sawClientVersion int32
	var sawVisitor int32
	var sawXOrigin int32

	tr := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("X-YouTube-Client-Name") == "1" {
			atomic.StoreInt32(&sawClientName, 1)
		}
		if r.Header.Get("X-YouTube-Client-Version") == web.Version {
			atomic.StoreInt32(&sawClientVersion, 1)
		}
		if r.Header.Get("X-Goog-Visitor-Id") == "visitor-xyz" {
			atomic.StoreInt32(&sawVisitor, 1)
		}
		if r.Header.Get("X-Origin") == "https://www.youtube.com" {
			atomic.StoreInt32(&sawXOrigin, 1)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewBufferString(`{"playabilityStatus":{"status":"OK"},"videoDetails":{"videoId":"jNQXAC9IVRw","title":"ok","author":"yt"}}`)),
			Header:     make(http.Header),
		}, nil
	})

	engine := NewEngine(
		selectorStub{clients: []innertube.ClientProfile{web}},
		innertube.Config{
			HTTPClient:  &http.Client{Transport: tr},
			VisitorData: "visitor-xyz",
		},
	)
	_, err := engine.GetVideoInfo(context.Background(), "jNQXAC9IVRw")
	if err != nil {
		t.Fatalf("GetVideoInfo() error = %v", err)
	}
	if atomic.LoadInt32(&sawClientName) == 0 {
		t.Fatalf("expected X-YouTube-Client-Name header")
	}
	if atomic.LoadInt32(&sawClientVersion) == 0 {
		t.Fatalf("expected X-YouTube-Client-Version header")
	}
	if atomic.LoadInt32(&sawVisitor) == 0 {
		t.Fatalf("expected X-Goog-Visitor-Id header")
	}
	if atomic.LoadInt32(&sawXOrigin) == 0 {
		t.Fatalf("expected X-Origin header")
	}
}

func TestEngineAppliesAdPlaybackContextAndPlayerParams(t *testing.T) {
	web := innertube.WebClient
	web.PlayerParams = "test_player_params"

	var sawAdPlayback int32
	var sawParams int32
	tr := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method == http.MethodPost && r.URL.Path == "/youtubei/v1/player" {
			body, _ := io.ReadAll(r.Body)
			if strings.Contains(string(body), `"adPlaybackContext":{"pyv":true}`) {
				atomic.StoreInt32(&sawAdPlayback, 1)
			}
			if strings.Contains(string(body), `"params":"test_player_params"`) {
				atomic.StoreInt32(&sawParams, 1)
			}
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewBufferString(`{"playabilityStatus":{"status":"OK"},"videoDetails":{"videoId":"jNQXAC9IVRw","title":"ok","author":"yt"}}`)),
			Header:     make(http.Header),
		}, nil
	})

	engine := NewEngine(
		selectorStub{clients: []innertube.ClientProfile{web}},
		innertube.Config{
			HTTPClient:           &http.Client{Transport: tr},
			UseAdPlaybackContext: true,
		},
	)
	_, err := engine.GetVideoInfo(context.Background(), "jNQXAC9IVRw")
	if err != nil {
		t.Fatalf("GetVideoInfo() error = %v", err)
	}
	if atomic.LoadInt32(&sawAdPlayback) == 0 {
		t.Fatalf("expected adPlaybackContext for supported client")
	}
	if atomic.LoadInt32(&sawParams) == 0 {
		t.Fatalf("expected top-level player params to be sent")
	}
}

func TestEngineAppliesCookieAuthHeadersAndVisitorFallback(t *testing.T) {
	web := innertube.WebClient
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar.New() error = %v", err)
	}
	u, _ := url.Parse("https://www.youtube.com")
	jar.SetCookies(u, []*http.Cookie{
		{Name: "VISITOR_INFO1_LIVE", Value: "visitor-from-cookie", Domain: ".youtube.com", Path: "/"},
		{Name: "SAPISID", Value: "sid-value", Domain: ".youtube.com", Path: "/"},
		{Name: "LOGIN_INFO", Value: "logged-in", Domain: ".youtube.com", Path: "/"},
	})

	var sawVisitor int32
	var sawAuth int32
	var sawBootstrap int32

	tr := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("X-Goog-Visitor-Id") == "visitor-from-cookie" {
			atomic.StoreInt32(&sawVisitor, 1)
		}
		if strings.HasPrefix(r.Header.Get("Authorization"), "SAPISIDHASH ") {
			atomic.StoreInt32(&sawAuth, 1)
		}
		if r.Header.Get("X-Youtube-Bootstrap-Logged-In") == "true" {
			atomic.StoreInt32(&sawBootstrap, 1)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewBufferString(`{"playabilityStatus":{"status":"OK"},"videoDetails":{"videoId":"jNQXAC9IVRw","title":"ok","author":"yt"}}`)),
			Header:     make(http.Header),
		}, nil
	})

	engine := NewEngine(
		selectorStub{clients: []innertube.ClientProfile{web}},
		innertube.Config{
			HTTPClient: &http.Client{Transport: tr, Jar: jar},
		},
	)
	_, err = engine.GetVideoInfo(context.Background(), "jNQXAC9IVRw")
	if err != nil {
		t.Fatalf("GetVideoInfo() error = %v", err)
	}
	if atomic.LoadInt32(&sawVisitor) == 0 {
		t.Fatalf("expected visitor fallback from cookie")
	}
	if atomic.LoadInt32(&sawAuth) == 0 {
		t.Fatalf("expected cookie auth header")
	}
	if atomic.LoadInt32(&sawBootstrap) == 0 {
		t.Fatalf("expected bootstrap logged-in header")
	}
}

func TestEngineMetadataRetryOnTransientStatus(t *testing.T) {
	web := innertube.WebClient
	var calls int32
	tr := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		n := atomic.AddInt32(&calls, 1)
		if n == 1 {
			return &http.Response{
				StatusCode: http.StatusBadGateway,
				Body:       io.NopCloser(bytes.NewBufferString(`bad gateway`)),
				Header:     make(http.Header),
			}, nil
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewBufferString(`{"playabilityStatus":{"status":"OK"},"videoDetails":{"videoId":"jNQXAC9IVRw","title":"ok","author":"yt"}}`)),
			Header:     make(http.Header),
		}, nil
	})

	engine := NewEngine(
		selectorStub{clients: []innertube.ClientProfile{web}},
		innertube.Config{
			HTTPClient: &http.Client{Transport: tr},
			MetadataTransport: innertube.MetadataTransportConfig{
				MaxRetries:     1,
				InitialBackoff: time.Millisecond,
				MaxBackoff:     time.Millisecond,
			},
		},
	)
	_, err := engine.GetVideoInfo(context.Background(), "jNQXAC9IVRw")
	if err != nil {
		t.Fatalf("GetVideoInfo() error = %v", err)
	}
	if atomic.LoadInt32(&calls) != 2 {
		t.Fatalf("expected 2 calls with retry, got %d", atomic.LoadInt32(&calls))
	}
}

func TestEngineDisableFallbackClients(t *testing.T) {
	web := innertube.WebClient
	engine := NewEngine(
		selectorStub{clients: []innertube.ClientProfile{web}},
		innertube.Config{
			HTTPClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewBufferString(`{"playabilityStatus":{"status":"LOGIN_REQUIRED","reason":"Sign in"}}`)), Header: make(http.Header)}, nil
			})},
			DisableFallbackClients: true,
		},
	)

	_, err := engine.GetVideoInfo(context.Background(), "jNQXAC9IVRw")
	if err == nil {
		t.Fatalf("expected error")
	}
	var allErr *AllClientsFailedError
	if !errors.As(err, &allErr) {
		t.Fatalf("expected all-clients-failed error")
	}
	// With auto-append disabled and selector returning WEB only, fallback clients must not be added.
	if len(allErr.Attempts) != 1 {
		t.Fatalf("expected 1 attempt, got %d", len(allErr.Attempts))
	}
}

func TestEngineEmitsPlayerAPIEventsInDeterministicStartOrder(t *testing.T) {
	web := innertube.WebClient
	mweb := innertube.MWebClient

	var mu sync.Mutex
	var got []innertube.ExtractionEvent

	tr := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(r.Body)
		payload := string(body)
		if strings.Contains(payload, `"clientName":"MWEB"`) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewBufferString(`{"playabilityStatus":{"status":"OK"},"videoDetails":{"videoId":"jNQXAC9IVRw","title":"ok","author":"yt"}}`)),
				Header:     make(http.Header),
			}, nil
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewBufferString(`{"playabilityStatus":{"status":"UNPLAYABLE","reason":"blocked"}}`)),
			Header:     make(http.Header),
		}, nil
	})

	engine := NewEngine(
		selectorStub{clients: []innertube.ClientProfile{web, mweb}},
		innertube.Config{
			HTTPClient: &http.Client{Transport: tr},
			OnExtractionEvent: func(evt innertube.ExtractionEvent) {
				mu.Lock()
				defer mu.Unlock()
				got = append(got, evt)
			},
		},
	)

	_, err := engine.GetVideoInfo(context.Background(), "jNQXAC9IVRw")
	if err != nil {
		t.Fatalf("GetVideoInfo() error = %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	var starts []string
	var hasSuccess bool
	for _, evt := range got {
		if evt.Stage != "player_api_json" {
			continue
		}
		if evt.Phase == "start" {
			starts = append(starts, evt.Client)
		}
		if evt.Phase == "success" {
			hasSuccess = true
		}
	}
	wantStarts := []string{"web", "mweb"}
	if !reflect.DeepEqual(starts, wantStarts) {
		t.Fatalf("start events = %v, want %v", starts, wantStarts)
	}
	if !hasSuccess {
		t.Fatalf("expected at least one success event")
	}
}
