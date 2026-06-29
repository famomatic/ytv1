package downloader

import (
	"testing"
	"time"
)

func TestParseISO8601Duration(t *testing.T) {
	cases := []struct {
		in   string
		want time.Duration
	}{
		{"PT1S", 1 * time.Second},
		{"PT1H30M", time.Hour + 30*time.Minute},
		{"PT0S", 0},
		{"PT2.5S", 2500 * time.Millisecond},
		{"PT1H", time.Hour},
		{"P1DT2H", 26 * time.Hour},
		{"PT1M30S", 90 * time.Second},
	}
	for _, c := range cases {
		got, err := parseDuration(c.in)
		if err != nil {
			t.Errorf("parseDuration(%q) error = %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("parseDuration(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestParseISO8601Duration_Invalid(t *testing.T) {
	for _, in := range []string{"", "abc", "P", "PT"} {
		if _, err := parseDuration(in); err == nil {
			t.Errorf("parseDuration(%q) expected error, got nil", in)
		}
	}
}

