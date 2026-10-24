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
