package downloader

import (
	"bytes"
	"testing"
)

func TestMalformedDurationReturnsError(t *testing.T) {
	for _, raw := range []string{"PT1", "P9", "PT2.5"} {
		if _, err := parseDuration(raw); err == nil {
			t.Errorf("%s: expected invalid duration", raw)
		}
	}
}
func TestHLSIVValidation(t *testing.T) {
	for _, raw := range []string{"0x", "0xz1", "0x" + string(bytes.Repeat([]byte("1"), 33))} {
		if _, err := parseKey(`METHOD=AES-128,URI="key",IV=`+raw, "https://example/"); err == nil {
			t.Errorf("accepted %s", raw)
		}
	}
	key, err := parseKey(`METHOD=AES-128,URI="key",IV=0x1`, "https://example/")
	if err != nil || len(key.IV) != 16 || key.IV[15] != 1 {
		t.Fatalf("short integer IV not normalized: %+v %v", key, err)
	}
}
