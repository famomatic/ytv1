package playerjs

import (
	"os"
	"path/filepath"
	"testing"
)

func loadFixture(t *testing.T, name string) string {
	t.Helper()
	p := filepath.Join("testdata", name)
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("failed to read fixture %s: %v", p, err)
	}
	return string(b)
}

func TestDecipherSignature_WithFixture(t *testing.T) {
	js := loadFixture(t, "synthetic_basejs_fixture.js")
	d := NewDecipherer(js)

	got, err := d.DecipherSignature("abcdef")
	if err != nil {
		t.Fatalf("DecipherSignature() error = %v", err)
	}
	if got != "edabc" {
		t.Fatalf("DecipherSignature() = %q, want %q", got, "edabc")
	}
}

func TestDecipherN_WithFixture(t *testing.T) {
	js := loadFixture(t, "synthetic_basejs_fixture.js")
	d := NewDecipherer(js)

	got, err := d.DecipherN("12345")
	if err != nil {
		t.Fatalf("DecipherN() error = %v", err)
	}
	if got != "2345" {
		t.Fatalf("DecipherN() = %q, want %q", got, "2345")
	}
}
