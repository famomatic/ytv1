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
	tests := []struct {
		name     string
		fixture  string
		input    string
		expected string
	}{
		{name: "v1", fixture: "synthetic_basejs_fixture.js", input: "abcdef", expected: "edabc"},
		{name: "v2", fixture: "synthetic_basejs_fixture_v2.js", input: "abcdef", expected: "acbd"},
		{name: "v3", fixture: "synthetic_basejs_fixture_v3.js", input: "abcdef", expected: "fedca"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			js := loadFixture(t, tt.fixture)
			d := NewDecipherer(js)
			got, err := d.DecipherSignature(tt.input)
			if err != nil {
				t.Fatalf("DecipherSignature() error = %v", err)
			}
			if got != tt.expected {
				t.Fatalf("DecipherSignature() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestDecipherN_WithFixture(t *testing.T) {
	tests := []struct {
		name     string
		fixture  string
		input    string
		expected string
	}{
		{name: "v1", fixture: "synthetic_basejs_fixture.js", input: "12345", expected: "2345"},
		{name: "v2", fixture: "synthetic_basejs_fixture_v2.js", input: "12345", expected: "345"},
		{name: "v3", fixture: "synthetic_basejs_fixture_v3.js", input: "12345", expected: "2345"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			js := loadFixture(t, tt.fixture)
			d := NewDecipherer(js)
			got, err := d.DecipherN(tt.input)
			if err != nil {
				t.Fatalf("DecipherN() error = %v", err)
			}
			if got != tt.expected {
				t.Fatalf("DecipherN() = %q, want %q", got, tt.expected)
			}
		})
	}
}
