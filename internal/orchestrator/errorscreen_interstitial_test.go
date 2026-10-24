package orchestrator

import (
	"encoding/json"
	"testing"

	"github.com/famomatic/ytv1/internal/innertube"
)

func TestExtractPlayabilityDetailReadsInterstitialErrorScreen(t *testing.T) {
	var resp innertube.PlayerResponse
	raw := `{"playabilityStatus":{"status":"LOGIN_REQUIRED","errorScreen":{"playerInterstitialRenderer":{"content":{"interstitialViewModel":{"title":{"content":"Sign in to confirm"},"description":{"content":"Use cookies"}}}}}}}`
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		t.Fatal(err)
	}
	d := extractPlayabilityDetail(&resp)
	if d.Subreason != "Use cookies" || !d.LoginRequired {
		t.Fatalf("unexpected detail: %+v", d)
	}
}

func TestMergePlayerResponsesFillsMissingMetadata(t *testing.T) {
	var tv, web innertube.PlayerResponse
	if err := json.Unmarshal([]byte(`{"playabilityStatus":{"status":"OK"},"videoDetails":{"videoId":"x"}}`), &tv); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(`{"playabilityStatus":{"status":"OK"},"videoDetails":{"videoId":"x","title":"T","author":"A"},"microformat":{"playerMicroformatRenderer":{"lengthSeconds":"9"}}}`), &web); err != nil {
		t.Fatal(err)
	}
	m := mergePlayerResponses([]*innertube.PlayerResponse{&tv, &web})
	if m.VideoDetails.Title != "T" || m.VideoDetails.Author != "A" || m.Microformat.PlayerMicroformatRenderer.LengthSeconds != "9" {
		t.Fatalf("metadata not filled from later response: %+v", m)
	}
}
