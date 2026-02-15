package client

import (
	"errors"
	"testing"
)

func TestRewriteURLParam(t *testing.T) {
	in := "https://example.com/manifest.mpd?foo=1&n=abc"
	out, err := rewriteURLParam(in, "n", func(v string) (string, error) {
		if v != "abc" {
			t.Fatalf("decoder input = %q, want abc", v)
		}
		return "xyz", nil
	})
	if err != nil {
		t.Fatalf("rewriteURLParam() error = %v", err)
	}
	if out != "https://example.com/manifest.mpd?foo=1&n=xyz" {
		t.Fatalf("rewriteURLParam() = %q", out)
	}
}

func TestRewriteURLParamDecoderError(t *testing.T) {
	in := "https://example.com/manifest.m3u8?n=abc"
	_, err := rewriteURLParam(in, "n", func(string) (string, error) {
		return "", errors.New("boom")
	})
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestHasQueryParam(t *testing.T) {
	if !hasQueryParam("https://example.com/x?n=1", "n") {
		t.Fatalf("expected n param")
	}
	if hasQueryParam("https://example.com/x?foo=1", "n") {
		t.Fatalf("did not expect n param")
	}
}

