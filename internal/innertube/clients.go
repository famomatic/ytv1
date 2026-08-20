package innertube

var (
	defaultInnertubeAPIKey = "AIzaSyAMfDpyiHtLq81UCmkNk0q5zY0ongtTTDn"

	// WebClient is the standard web client (Desktop).
	WebClient = ClientProfile{
		ID:                        "web",
		Name:                      "WEB",
		Version:                   "2.20260708.00.00",
		ContextNameID:             1,
		UserAgent:                 "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
		SupportsCookies:           true,
		SupportsAdPlaybackContext: true,
		Host:                      "www.youtube.com",
		APIKey:                    defaultInnertubeAPIKey,
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
		ID:              "web_embedded",
		Name:            "WEB_EMBEDDED_PLAYER",
		Version:         "2.20260708.00.00",
		ContextNameID:   56,
		UserAgent:       WebClient.UserAgent,
		APIKey:          defaultInnertubeAPIKey,
		SupportsCookies: true,
		Host:            "www.youtube.com",
		Screen:          "EMBED",
	}

	// WebCreatorClient mirrors yt-dlp's "web_creator" profile used for
	// authenticated/premium fallbacks. This client now requires sign-in for
	// every video.
	WebCreatorClient = ClientProfile{
		ID:              "web_creator",
		Name:            "WEB_CREATOR",
		Version:         "1.20260708.06.00",
		ContextNameID:   62,
		UserAgent:       WebClient.UserAgent,
		APIKey:          defaultInnertubeAPIKey,
		SupportsCookies: true,
		RequiresAuth:    true,
		Host:            "studio.youtube.com",
		PoTokenPolicy: map[VideoStreamingProtocol]PoTokenPolicy{
			StreamingProtocolHTTPS: {
				Required:              true,
				Recommended:           true,
				NotRequiredForPremium: true,
			},
			StreamingProtocolDASH: {
				Required:              true,
				Recommended:           true,
				NotRequiredForPremium: true,
			},
			StreamingProtocolHLS: {
				Required:    false,
				Recommended: true,
			},
		},
	}

	// WebSafariClient mirrors yt-dlp's "web_safari" strategy using WEB clientName
	// with a Safari UA profile. Safari UA returns pre-merged video+audio HLS
	// formats; since 2026.07 those HLS formats are only returned with some
	// logged-in or "trusted" sessions.
	WebSafariClient = ClientProfile{
		ID:                        "web_safari",
		Name:                      "WEB",
		Version:                   "2.20260708.00.00",
		ContextNameID:             1,
		UserAgent:                 "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/15.5 Safari/605.1.15,gzip(gfe)",
		SupportsCookies:           true,
		SupportsAdPlaybackContext: true,
		Host:                      "www.youtube.com",
		APIKey:                    defaultInnertubeAPIKey,
		PoTokenPolicy:             WebClient.PoTokenPolicy,
	}

	// WebMusicClient mirrors yt-dlp's "web_music" profile (WEB_REMIX on
	// music.youtube.com).
	WebMusicClient = ClientProfile{
		ID:                        "web_music",
		Name:                      "WEB_REMIX",
		Version:                   "1.20260707.12.00",
		ContextNameID:             67,
		UserAgent:                 WebClient.UserAgent,
		APIKey:                    defaultInnertubeAPIKey,
		SupportsCookies:           true,
		SupportsAdPlaybackContext: true,
		Host:                      "music.youtube.com",
		PoTokenPolicy:             WebClient.PoTokenPolicy,
	}

	// MWebClient represents the mobile web client.
	MWebClient = ClientProfile{
		ID:                        "mweb",
		Name:                      "MWEB",
		Version:                   "2.20260708.05.00",
		ContextNameID:             2,
		UserAgent:                 "Mozilla/5.0 (iPad; CPU OS 16_7_10 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/16.6 Mobile/15E148 Safari/604.1,gzip(gfe)",
		APIKey:                    defaultInnertubeAPIKey,
		Host:                      "www.youtube.com",
		SupportsCookies:           true,
		SupportsAdPlaybackContext: true,
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
		ID:              "android",
		Name:            "ANDROID",
		Version:         "21.26.364",
		ContextNameID:   3,
		UserAgent:       "com.google.android.youtube/21.26.364 (Linux; U; Android 11) gzip",
		RequireJSPlayer: false,
		APIKey:          defaultInnertubeAPIKey,
		Host:            "www.youtube.com",
		PlayerPoTokenPolicy: &PlayerPoTokenPolicy{
			Recommended: true,
		},
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
		ID:              "ios",
		Name:            "IOS",
		Version:         "21.26.4",
		ContextNameID:   5,
		UserAgent:       "com.google.ios.youtube/21.26.4 (iPhone16,2; U; CPU iOS 18_3_2 like Mac OS X;)",
		RequireJSPlayer: false,
		APIKey:          defaultInnertubeAPIKey,
		Host:            "www.youtube.com",
		PlayerPoTokenPolicy: &PlayerPoTokenPolicy{
			Recommended: true,
		},
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

	// VisionOSClient mirrors yt-dlp's "visionos" profile, the current lead
	// JS-less default client. "Made for kids" videos are not available with
	// this client.
	VisionOSClient = ClientProfile{
		ID:              "visionos",
		Name:            "VISIONOS",
		Version:         "1.02",
		ContextNameID:   101,
		UserAgent:       "Mozilla/5.0 (Macintosh; Intel Mac OS X 15_7_3) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/26.0 Safari/605.1.15",
		RequireJSPlayer: false,
		APIKey:          defaultInnertubeAPIKey,
		Host:            "www.youtube.com",
	}

	// TVClient is for Smart TV interactions.
	TVClient = ClientProfile{
		ID:              "tv",
		Name:            "TVHTML5",
		Version:         "7.20260707.07.00",
		ContextNameID:   7,
		UserAgent:       "Mozilla/5.0 (ChromiumStylePlatform) Cobalt/25.lts.30.1034943-gold (unlike Gecko), Unknown_TV_Unknown_0/Unknown (Unknown, Unknown)",
		APIKey:          defaultInnertubeAPIKey,
		SupportsCookies: true,
		Host:            "www.youtube.com",
	}

	// TVDowngradedClient mirrors yt-dlp's "tv_downgraded" profile (older
	// TVHTML5 version with a generic Cobalt UA).
	TVDowngradedClient = ClientProfile{
		ID:              "tv_downgraded",
		Name:            "TVHTML5",
		Version:         "5.20260707",
		ContextNameID:   7,
		UserAgent:       "Mozilla/5.0 (ChromiumStylePlatform) Cobalt/Version",
		APIKey:          defaultInnertubeAPIKey,
		SupportsCookies: true,
		Host:            "www.youtube.com",
	}

	// TVSimplyClient mirrors yt-dlp's "tv_simply" profile (TVHTML5_SIMPLY).
	TVSimplyClient = ClientProfile{
		ID:            "tv_simply",
		Name:          "TVHTML5_SIMPLY",
		Version:       "1.0",
		ContextNameID: 75,
		APIKey:        defaultInnertubeAPIKey,
		Host:          "www.youtube.com",
		PoTokenPolicy: map[VideoStreamingProtocol]PoTokenPolicy{
			StreamingProtocolHTTPS: {
				Required:    true,
				Recommended: true,
			},
			StreamingProtocolDASH: {
				Required:    true,
				Recommended: true,
			},
			StreamingProtocolHLS: {
				Recommended: true,
			},
		},
	}

	// AndroidVRClient matches yt-dlp's ANDROID_VR profile. "Made for kids"
	// videos are not available with this client; clientVersion>1.65 may return
	// SABR streams only. Since 2026.07 intermittent/selective POT enforcement
	// has been observed for non-HLS formats, and since 2026.08.17 ALL formats
	// (including live HLS and itag 18) are 403'd with version 1.65.10, so it is
	// no longer part of default client chains.
	AndroidVRClient = ClientProfile{
		ID:              "android_vr",
		Name:            "ANDROID_VR",
		Version:         "1.65.10",
		ContextNameID:   28,
		UserAgent:       "com.google.android.apps.youtube.vr.oculus/1.65.10 (Linux; U; Android 12L; eureka-user Build/SQ3A.220605.009.A1) gzip",
		RequireJSPlayer: false,
		APIKey:          defaultInnertubeAPIKey,
		Host:            "www.youtube.com",
		PlayerPoTokenPolicy: &PlayerPoTokenPolicy{
			Recommended: true,
		},
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
				Recommended:                true,
				NotRequiredWithPlayerToken: true,
			},
		},
	}
)
