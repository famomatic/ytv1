package downloader

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestDoGETBytesWithRetry_RetriesOn429(t *testing.T) {
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) == 1 {
			w.Header().Set("Retry-After", "0")
			http.Error(w, "rate limited", http.StatusTooManyRequests)
			return
		}
		w.Write([]byte("ok"))
	}))
	defer server.Close()

	body, err := doGETBytesWithRetry(context.Background(), server.Client(), server.URL, nil, TransportConfig{
		MaxRetries:       1,
		InitialBackoff:   time.Millisecond,
		MaxBackoff:       time.Millisecond,
		RetryStatusCodes: []int{http.StatusTooManyRequests},
	})
	if err != nil {
		t.Fatalf("doGETBytesWithRetry() error = %v", err)
	}
	if got := string(body); got != "ok" {
		t.Fatalf("body=%q, want ok", got)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("call count=%d, want 2", got)
	}
}

func TestParseRetryAfter_SecondsAndHTTPDate(t *testing.T) {
	if d := parseRetryAfter("1"); d != time.Second {
		t.Fatalf("seconds parse mismatch: got=%v want=%v", d, time.Second)
	}
	when := time.Now().Add(2 * time.Second).UTC().Format(http.TimeFormat)
	if d := parseRetryAfter(when); d <= 0 {
		t.Fatalf("http-date parse mismatch: got=%v", d)
	}
}
