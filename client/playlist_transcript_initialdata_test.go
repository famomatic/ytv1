package client

import "testing"

func TestExtractYTInitialDataPrefersDedicatedScript(t *testing.T) {
	page := []byte(`<script id="yt-initial-data" type="application/json">{"a":1}</script><script>var ytInitialData = {"b":2};</script>`)
	got, err := extractYTInitialData(page)
	if err != nil || string(got) != `{"a":1}` {
		t.Fatalf("got %q err %v", got, err)
	}
	legacy := []byte(`<script>var ytInitialData = {"b":2};</script>`)
	got, err = extractYTInitialData(legacy)
	if err != nil || string(got) != `{"b":2}` {
		t.Fatalf("legacy got %q err %v", got, err)
	}
}
