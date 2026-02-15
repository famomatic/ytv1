package orchestrator

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"sync/atomic"
	"strings"
	"testing"

	"github.com/mjmst/ytv1/internal/innertube"
)

type selectorStub struct {
	clients []innertube.ClientProfile
}

func (s selectorStub) Select(string) []innertube.ClientProfile { return s.clients }
func (s selectorStub) Registry() innertube.Registry            { return innertube.NewRegistry() }

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

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
	android := innertube.AndroidClient
	embedded := innertube.WebEmbeddedClient
	var embeddedCalls int32

	tr := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(r.Body)
		payload := string(body)
		switch {
		case strings.Contains(payload, `"clientName":"ANDROID"`):
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
		selectorStub{clients: []innertube.ClientProfile{android, embedded}},
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
