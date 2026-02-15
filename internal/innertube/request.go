package innertube

import "strings"

type PlayerRequest struct {
	Context       Context       `json:"context"`
	VideoID       string        `json:"videoId"`
	CPN           string        `json:"cpn,omitempty"`
	ContentCheckOk bool         `json:"contentCheckOk,omitempty"`
	RacyCheckOk    bool         `json:"racyCheckOk,omitempty"`
	PlaybackContext PlaybackContext `json:"playbackContext,omitempty"`
	ServiceIntegrityDimensions *ServiceIntegrityDimensions `json:"serviceIntegrityDimensions,omitempty"`
}

type Context struct {
	Client          ClientInfo      `json:"client"`
	User            UserContext     `json:"user,omitempty"`
	ThirdParty      *ThirdParty     `json:"thirdParty,omitempty"`
	Request         RequestContext  `json:"request,omitempty"`
}

type ClientInfo struct {
	ClientName      string `json:"clientName"`
	ClientVersion   string `json:"clientVersion"`
	DeviceMake      string `json:"deviceMake,omitempty"`
	DeviceModel     string `json:"deviceModel,omitempty"`
	UserAgent       string `json:"userAgent,omitempty"`
	OsName          string `json:"osName,omitempty"`
	OsVersion       string `json:"osVersion,omitempty"`
	AcceptLanguage  string `json:"hl"`
	TimeZone        string `json:"timeZone"`
	UtcOffsetMinutes int   `json:"utcOffsetMinutes"`
	AndroidSdkVersion int  `json:"androidSdkVersion,omitempty"`
}

type UserContext struct {
	LockedSafetyMode bool `json:"lockedSafetyMode,omitempty"`
}

type ThirdParty struct {
	EmbedUrl string `json:"embedUrl"`
}

type RequestContext struct {
	UseSsl                  bool `json:"useSsl"`
	InternalExperimentFlags []string `json:"internalExperimentFlags,omitempty"`
}

type PlaybackContext struct {
	ContentPlaybackContext ContentPlaybackContext `json:"contentPlaybackContext"`
}

type ContentPlaybackContext struct {
	Vis             int    `json:"vis"`
	Splay           bool   `json:"splay"`
	AutoCaptionsDefaultOn bool `json:"autoCaptionsDefaultOn"`
	Html5Preference string `json:"html5Preference"`
	Lact            int64  `json:"lact"`
	SignatureTimestamp int `json:"signatureTimestamp,omitempty"`
}

type ServiceIntegrityDimensions struct {
	PoToken string `json:"poToken,omitempty"`
}


func NewPlayerRequest(profile ClientProfile, videoID string) *PlayerRequest {
	clientInfo := ClientInfo{
		ClientName:       profile.Name,
		ClientVersion:    profile.Version,
		UserAgent:        profile.UserAgent,
		AcceptLanguage:   "en",
		TimeZone:         "UTC",
		UtcOffsetMinutes: 0,
	}
	applyClientContextDefaults(&clientInfo, profile)

	req := &PlayerRequest{
		VideoID: videoID,
		RacyCheckOk: true,
		ContentCheckOk: true,
		Context: Context{
			Client: clientInfo,
			Request: RequestContext{
				UseSsl: true,
			},
		},
		PlaybackContext: PlaybackContext{
			ContentPlaybackContext: ContentPlaybackContext{
				Vis: 0,
				Splay: false,
				Html5Preference: "HTML5_PREF_WANTS",
				Lact: 10000, // Dummy value
			},
		},
	}
	
	if profile.Screen == "EMBED" {
		req.Context.ThirdParty = &ThirdParty{
			EmbedUrl: "https://www.youtube.com/watch?v=" + videoID,
		}
	}

	return req
}

func (r *PlayerRequest) SetPoToken(token string) {
	if token == "" {
		return
	}
	r.ServiceIntegrityDimensions = &ServiceIntegrityDimensions{PoToken: token}
}

func applyClientContextDefaults(client *ClientInfo, profile ClientProfile) {
	switch strings.ToUpper(strings.TrimSpace(profile.Name)) {
	case "ANDROID":
		fallthrough
	case "ANDROID_VR":
		client.OsName = "Android"
		client.OsVersion = "11"
		client.DeviceMake = "Google"
		client.DeviceModel = "Pixel 5"
		client.AndroidSdkVersion = 30
	case "IOS":
		client.OsName = "iOS"
		client.OsVersion = "18.3.2.22D82"
		client.DeviceMake = "Apple"
		client.DeviceModel = "iPhone16,2"
	case "MWEB":
		client.OsName = "Android"
		client.OsVersion = "11"
		client.DeviceMake = "Google"
		client.DeviceModel = "Pixel 5"
	case "TVHTML5":
		client.OsName = "Cobalt"
		client.OsVersion = "25"
		client.DeviceMake = "Unknown"
		client.DeviceModel = "TV"
	default:
		client.OsName = "Windows"
		client.OsVersion = "10.0"
		client.DeviceMake = "Microsoft"
		client.DeviceModel = "Desktop"
	}
}
