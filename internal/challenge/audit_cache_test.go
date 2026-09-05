package challenge

import (
	"context"
	"testing"
	"time"
)

func TestTokenExpiryCallsProviderAgain(t *testing.T) {
	base := &poProviderStub{token: "old"}
	p := NewCachedPoTokenProvider(base).(*cachedPoTokenProvider)
	p.GetToken(context.Background(), "web")
	base.token = "new"
	p.expires["web"] = time.Now().Add(-time.Second)
	token, err := p.GetToken(context.Background(), "web")
	if err != nil || token != "new" {
		t.Fatalf("%q %v", token, err)
	}
}
