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

func TestDecipherSignature_RuntimeFallback(t *testing.T) {
	js := `var _yt_player={};(function(g){
is=function(a,b){return b.split("").reverse().join("")};
g.Mj=function(u){this.u=u;this.get=function(k){if(k!=="n")return null;var m=this.u.match(/[?&]n=([^&]+)/);if(!m||!m[1])return null;var v=decodeURIComponent(m[1]);return v?v.split("").reverse().join(""):null;};};
ocx=function(b,R,h,K){for(const l of h){if(!l.url)continue;if(l.s){const a=is(16,decodeURIComponent(l.s));if(a){return a;}}}};
MzD=function(b){try{const R=(new g.Mj(b,!0)).get("n");if(R){const h=b.match(/\/n\/([^/]+)/);if(h&&h[1]&&h[1]!==R)return b.replace("/n/"+h[1],"/n/"+R)}}catch(R){}return b};
})(_yt_player);`
	d := NewDecipherer(js)

	got, err := d.DecipherSignature("abcdef")
	if err != nil {
		t.Fatalf("DecipherSignature() runtime fallback error = %v", err)
	}
	if got != "fedcba" {
		t.Fatalf("DecipherSignature() runtime fallback = %q, want %q", got, "fedcba")
	}
}

func TestDecipherN_RuntimeFallback(t *testing.T) {
	js := `var _yt_player={};(function(g){
is=function(a,b){return b.split("").reverse().join("")};
g.Mj=function(u){this.u=u;this.get=function(k){if(k!=="n")return null;var m=this.u.match(/[?&]n=([^&]+)/);if(!m||!m[1])return null;var v=decodeURIComponent(m[1]);return v?v.split("").reverse().join(""):null;};};
ocx=function(b,R,h,K){for(const l of h){if(!l.url)continue;if(l.s){const a=is(16,decodeURIComponent(l.s));if(a){return a;}}}};
MzD=function(b){try{const R=(new g.Mj(b,!0)).get("n");if(R){const h=b.match(/\/n\/([^/]+)/);if(h&&h[1]&&h[1]!==R)return b.replace("/n/"+h[1],"/n/"+R)}}catch(R){}return b};
})(_yt_player);`
	d := NewDecipherer(js)

	got, err := d.DecipherN("abcdef")
	if err != nil {
		t.Fatalf("DecipherN() runtime fallback error = %v", err)
	}
	if got != "fedcba" {
		t.Fatalf("DecipherN() runtime fallback = %q, want %q", got, "fedcba")
	}
}
