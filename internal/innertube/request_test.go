package innertube

import "testing"

func TestNewPlayerRequestAndroidContext(t *testing.T) {
	req := NewPlayerRequest(AndroidClient, "jNQXAC9IVRw")
	c := req.Context.Client
	if c.OsName != "Android" || c.DeviceModel == "" || c.AndroidSdkVersion == 0 {
		t.Fatalf("unexpected android context: %+v", c)
	}
}

func TestNewPlayerRequestEmbeddedContext(t *testing.T) {
	req := NewPlayerRequest(WebEmbeddedClient, "jNQXAC9IVRw")
	if req.Context.ThirdParty == nil {
		t.Fatalf("expected thirdParty embed context")
	}
	if req.Context.ThirdParty.EmbedUrl == "" {
		t.Fatalf("expected embed url")
	}
}

func TestNewPlayerRequestTVContext(t *testing.T) {
	req := NewPlayerRequest(TVClient, "jNQXAC9IVRw")
	c := req.Context.Client
	if c.OsName != "Cobalt" {
		t.Fatalf("expected Cobalt OS for TV client, got %q", c.OsName)
	}
}

func TestSetPoToken(t *testing.T) {
	req := NewPlayerRequest(WebClient, "jNQXAC9IVRw")
	req.SetPoToken("token-1")
	if req.ServiceIntegrityDimensions == nil {
		t.Fatalf("expected serviceIntegrityDimensions to be set")
	}
	if req.ServiceIntegrityDimensions.PoToken != "token-1" {
		t.Fatalf("unexpected poToken: %q", req.ServiceIntegrityDimensions.PoToken)
	}
}
