package formats

import "testing"

func TestAudit20260906DASHDRMUnmarked(t *testing.T) {
	fs, e := ParseDASHManifest(`<MPD><Period><AdaptationSet mimeType="video/mp4"><ContentProtection schemeIdUri="urn:mpeg:dash:mp4protection:2011" value="cenc"/><Representation id="137"><BaseURL>encrypted.mp4</BaseURL></Representation></AdaptationSet></Period></MPD>`, "https://example.test/list.mpd")
	if e != nil || len(fs) != 1 || !fs[0].IsDRM {
		t.Fatalf("regression: %+v %v", fs, e)
	}

}
