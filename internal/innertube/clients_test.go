package innertube

import "testing"

// Version parity snapshot from yt-dlp 2026.08.19
// (yt_dlp/extractor/youtube/_base.py INNERTUBE_CLIENTS).
func TestClientVersionsMatchYTDLP20260819(t *testing.T) {
	cases := []struct {
		profile ClientProfile
		version string
	}{
		{WebClient, "2.20260708.00.00"},
		{WebSafariClient, "2.20260708.00.00"},
		{WebEmbeddedClient, "2.20260708.00.00"},
		{WebMusicClient, "1.20260707.12.00"},
		{WebCreatorClient, "1.20260708.06.00"},
		{AndroidClient, "21.26.364"},
		{AndroidVRClient, "1.65.10"},
		{iOSClient, "21.26.4"},
		{VisionOSClient, "1.02"},
		{MWebClient, "2.20260708.05.00"},
		{TVClient, "7.20260707.07.00"},
		{TVDowngradedClient, "5.20260707"},
		{TVSimplyClient, "1.0"},
	}
	for _, tc := range cases {
		if tc.profile.Version != tc.version {
			t.Errorf("client %q version = %q, want %q", tc.profile.ID, tc.profile.Version, tc.version)
		}
	}
}

func TestVisionOSClientProfile(t *testing.T) {
	if VisionOSClient.ContextNameID != 101 {
		t.Fatalf("visionos context name id = %d, want 101", VisionOSClient.ContextNameID)
	}
	if VisionOSClient.RequireJSPlayer {
		t.Fatalf("visionos must not require a JS player")
	}
	if len(VisionOSClient.PoTokenPolicy) != 0 {
		t.Fatalf("visionos must not declare GVS PoT policies upstream")
	}
}

func TestAndroidVRPoliciesMatchUpstream(t *testing.T) {
	if AndroidVRClient.PlayerPoTokenPolicy == nil || AndroidVRClient.PlayerPoTokenPolicy.Required {
		t.Fatalf("android_vr player PoT policy must be recommended-only")
	}
	for _, protocol := range []VideoStreamingProtocol{StreamingProtocolHTTPS, StreamingProtocolDASH} {
		p, ok := AndroidVRClient.PoTokenPolicy[protocol]
		if !ok || !p.Required || !p.Recommended || !p.NotRequiredWithPlayerToken {
			t.Fatalf("android_vr %s policy drift: %+v", protocol, p)
		}
	}
	hls, ok := AndroidVRClient.PoTokenPolicy[StreamingProtocolHLS]
	if !ok || hls.Required || !hls.Recommended || !hls.NotRequiredWithPlayerToken {
		t.Fatalf("android_vr hls policy drift: %+v", hls)
	}
}

func TestRegistryIncludesNewUpstreamClients(t *testing.T) {
	r := NewRegistry()
	for _, name := range []string{"visionos", "web_music", "tv_simply"} {
		if _, ok := r.Get(name); !ok {
			t.Fatalf("registry missing client %q", name)
		}
	}
	tvDowngraded, ok := r.Get("tv_downgraded")
	if !ok {
		t.Fatalf("registry missing tv_downgraded")
	}
	if tvDowngraded.Version != TVDowngradedClient.Version || tvDowngraded.UserAgent != TVDowngradedClient.UserAgent {
		t.Fatalf("tv_downgraded must map to its own profile, got %+v", tvDowngraded)
	}
}

func TestTVSamsungProfileMatchesUpstream(t *testing.T) {
	p, ok := NewRegistry().Get("tv_samsung")
	if !ok {
		t.Fatalf("registry missing tv_samsung")
	}
	if p.Name != "TVHTML5" || p.Version != "5.20260707" || p.ContextNameID != 7 || !p.SupportsCookies {
		t.Fatalf("tv_samsung profile drift: %+v", p)
	}
	req := NewPlayerRequest(p, "jNQXAC9IVRw")
	c := req.Context.Client
	if c.DeviceMake != "Samsung" || c.DeviceModel != "UKS9800" || c.OsName != "Tizen" || c.OsVersion != "2.4.0" {
		t.Fatalf("tv_samsung context drift: %+v", c)
	}
	if WebEmbeddedClient.UserAgent != WebSafariClient.UserAgent {
		t.Fatalf("web_embedded must use the Safari UA")
	}
}
