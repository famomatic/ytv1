package innertube

var (
	defaultInnertubeAPIKey = "AIzaSyAMfDpyiHtLq81UCmkNk0q5zY0ongtTTDn"

	// WebClient is the standard web client (Desktop).
	WebClient = ClientProfile{
		Name:            "WEB",
		Version:         "2.20260114.08.00",
		ContextNameID:   1,
		UserAgent:       "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
		SupportsCookies: true,
		Host:            "www.youtube.com",
		APIKey:          defaultInnertubeAPIKey,
		PoTokenPolicy: map[VideoStreamingProtocol]PoTokenPolicy{
			StreamingProtocolHTTPS: {
				Required:                   true,
				Recommended:                true,
				NotRequiredForPremium:      true,
				NotRequiredWithPlayerToken: false,
			},
			StreamingProtocolDASH: {
				Required:                   true,
				Recommended:                true,
				NotRequiredForPremium:      true,
				NotRequiredWithPlayerToken: false,
			},
			StreamingProtocolHLS: {
				Required:    false,
				Recommended: true,
			},
		},
	}

	// WebEmbeddedClient is for embedded players.
	WebEmbeddedClient = ClientProfile{
		Name:            "WEB_EMBEDDED_PLAYER",
		Version:         "1.20260115.01.00",
		ContextNameID:   56,
		UserAgent:       WebClient.UserAgent,
		APIKey:          defaultInnertubeAPIKey,
		SupportsCookies: true,
		Host:            "www.youtube.com",
		Screen:          "EMBED",
	}

	// WebSafariClient mirrors yt-dlp's "web_safari" strategy using WEB clientName
	// with a Safari UA profile.
	WebSafariClient = ClientProfile{
		Name:            "WEB",
		Version:         "2.20260114.08.00",
		ContextNameID:   1,
		UserAgent:       "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/15.5 Safari/605.1.15,gzip(gfe)",
		SupportsCookies: true,
		Host:            "www.youtube.com",
		APIKey:          defaultInnertubeAPIKey,
		PoTokenPolicy:   WebClient.PoTokenPolicy,
	}

	// MWebClient represents the mobile web client.
	MWebClient = ClientProfile{
		Name:            "MWEB",
		Version:         "2.20260115.01.00",
		ContextNameID:   2,
		UserAgent:       "Mozilla/5.0 (iPad; CPU OS 16_7_10 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/16.6 Mobile/15E148 Safari/604.1,gzip(gfe)",
		APIKey:          defaultInnertubeAPIKey,
		Host:            "www.youtube.com",
		SupportsCookies: true,
		PoTokenPolicy: map[VideoStreamingProtocol]PoTokenPolicy{
			StreamingProtocolHTTPS: {
				Required:                   true,
				Recommended:                true,
				NotRequiredForPremium:      true,
				NotRequiredWithPlayerToken: false,
			},
			StreamingProtocolDASH: {
				Required:                   true,
				Recommended:                true,
				NotRequiredForPremium:      true,
				NotRequiredWithPlayerToken: false,
			},
			StreamingProtocolHLS: {
				Required:    false,
				Recommended: true,
			},
		},
	}

	// AndroidClient mimics the official Android app.
	AndroidClient = ClientProfile{
		Name:          "ANDROID",
		Version:       "21.02.35",
		ContextNameID: 3,
		UserAgent:     "com.google.android.youtube/21.02.35 (Linux; U; Android 11) gzip",
		APIKey:        defaultInnertubeAPIKey,
		Host:          "www.youtube.com",
		PoTokenPolicy: map[VideoStreamingProtocol]PoTokenPolicy{
			StreamingProtocolHTTPS: {
				Required:                   true,
				Recommended:                true,
				NotRequiredWithPlayerToken: true,
			},
			StreamingProtocolDASH: {
				Required:                   true,
				Recommended:                true,
				NotRequiredWithPlayerToken: true,
			},
			StreamingProtocolHLS: {
				Required:                   false,
				Recommended:                true,
				NotRequiredWithPlayerToken: true,
			},
		},
	}

	// iOSClient mimics the official iOS app.
	iOSClient = ClientProfile{
		Name:          "IOS",
		Version:       "21.02.3",
		ContextNameID: 5,
		UserAgent:     "com.google.ios.youtube/21.02.3 (iPhone16,2; U; CPU iOS 18_3_2 like Mac OS X;)",
		APIKey:        defaultInnertubeAPIKey,
		Host:          "www.youtube.com",
		PoTokenPolicy: map[VideoStreamingProtocol]PoTokenPolicy{
			StreamingProtocolHTTPS: {
				Required:                   true,
				Recommended:                true,
				NotRequiredWithPlayerToken: true,
			},
			StreamingProtocolHLS: {
				Required:                   true,
				Recommended:                true,
				NotRequiredWithPlayerToken: true,
			},
		},
	}

	// TVClient is for Smart TV interactions.
	TVClient = ClientProfile{
		Name:            "TVHTML5",
		Version:         "7.20260114.12.00",
		ContextNameID:   7,
		UserAgent:       "Mozilla/5.0 (ChromiumStylePlatform) Cobalt/25.lts.30.1034943-gold (unlike Gecko), Unknown_TV_Unknown_0/Unknown (Unknown, Unknown)",
		APIKey:          defaultInnertubeAPIKey,
		SupportsCookies: true,
		Host:            "www.youtube.com",
	}

	// AndroidVRClient matches yt-dlp's preferred no-auth mobile app fallback.
	AndroidVRClient = ClientProfile{
		Name:          "ANDROID_VR",
		Version:       "1.71.26",
		ContextNameID: 28,
		UserAgent:     "com.google.android.apps.youtube.vr.oculus/1.71.26 (Linux; U; Android 12L; eureka-user Build/SQ3A.220605.009.A1) gzip",
		APIKey:        defaultInnertubeAPIKey,
		Host:          "www.youtube.com",
	}
)
