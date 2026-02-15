package innertube

// PlayerResponse is the top-level response from the /player endpoint.
type PlayerResponse struct {
	PlayabilityStatus PlayabilityStatus `json:"playabilityStatus"`
	StreamingData     StreamingData     `json:"streamingData"`
	VideoDetails      VideoDetails      `json:"videoDetails"`
	Microformat       Microformat       `json:"microformat"`
	Captions          Captions          `json:"captions"`
}

type BrowseResponse struct {
	Contents                    Contents                    `json:"contents"`
	OnResponseReceivedActions   []OnResponseReceivedAction  `json:"onResponseReceivedActions"`
	OnResponseReceivedEndpoints []OnResponseReceivedEndpoint `json:"onResponseReceivedEndpoints"`
}

type OnResponseReceivedAction struct {
	AppendContinuationItemsAction *AppendContinuationItemsAction `json:"appendContinuationItemsAction"`
	ReloadContinuationItemsCommand *ReloadContinuationItemsCommand `json:"reloadContinuationItemsCommand"`
}

type OnResponseReceivedEndpoint struct {
	AppendContinuationItemsAction *AppendContinuationItemsAction `json:"appendContinuationItemsAction"`
	ReloadContinuationItemsCommand *ReloadContinuationItemsCommand `json:"reloadContinuationItemsCommand"`
}

type AppendContinuationItemsAction struct {
	ContinuationItems []ContinuationItem `json:"continuationItems"`
}

type ReloadContinuationItemsCommand struct {
	ContinuationItems []ContinuationItem `json:"continuationItems"`
}

type Contents struct {
	TwoColumnBrowseResultsRenderer *TwoColumnBrowseResultsRenderer `json:"twoColumnBrowseResultsRenderer"`
}

type TwoColumnBrowseResultsRenderer struct {
	Tabs []Tab `json:"tabs"`
}

type Tab struct {
	TabRenderer *TabRenderer `json:"tabRenderer"`
}

type TabRenderer struct {
	Content *TabContent `json:"content"`
}

type TabContent struct {
	SectionListRenderer *SectionListRenderer `json:"sectionListRenderer"`
}

type SectionListRenderer struct {
	Contents []SectionListContent `json:"contents"`
}

type SectionListContent struct {
	ItemSectionRenderer *ItemSectionRenderer `json:"itemSectionRenderer"`
	ContinuationItemRenderer *ContinuationItemRenderer `json:"continuationItemRenderer"`
}

type ItemSectionRenderer struct {
	Contents []ItemSectionContent `json:"contents"`
}

type ItemSectionContent struct {
	PlaylistVideoRenderer *PlaylistVideoRenderer `json:"playlistVideoRenderer"`
}

type ContinuationItem struct {
	ContinuationItemRenderer *ContinuationItemRenderer `json:"continuationItemRenderer"`
	PlaylistVideoRenderer    *PlaylistVideoRenderer    `json:"playlistVideoRenderer"`
}

type ContinuationItemRenderer struct {
	ContinuationEndpoint ContinuationEndpoint `json:"continuationEndpoint"`
}

type ContinuationEndpoint struct {
	ContinuationCommand ContinuationCommand `json:"continuationCommand"`
}

type ContinuationCommand struct {
	Token string `json:"token"`
}

type PlaylistVideoRenderer struct {
	VideoID    string     `json:"videoId"`
	Title      LangText   `json:"title"`
	ShortBylineText LangText `json:"shortBylineText"`
	LengthText LangText   `json:"lengthText"`
}

type PlayabilityStatus struct {
	Status            string             `json:"status"`
	Reason            string             `json:"reason"`
	PlayableInEmbed   bool               `json:"playableInEmbed"`
	LiveStreamability *LiveStreamability `json:"liveStreamability"`
}

func (p *PlayabilityStatus) IsOK() bool {
	return p.Status == "OK"
}

func (p *PlayabilityStatus) IsLive() bool {
	return p.LiveStreamability != nil
}

type LiveStreamability struct {
	LiveStreamabilityRenderer LiveStreamabilityRenderer `json:"liveStreamabilityRenderer"`
}

type LiveStreamabilityRenderer struct {
	VideoId     string `json:"videoId"`
	PollDelayMs string `json:"pollDelayMs"`
}

type StreamingData struct {
	ExpiresInSeconds string   `json:"expiresInSeconds"`
	Formats          []Format `json:"formats"`
	AdaptiveFormats  []Format `json:"adaptiveFormats"`
	DashManifestURL  string   `json:"dashManifestUrl"`
	HlsManifestURL   string   `json:"hlsManifestUrl"`
}

type Format struct {
	Itag             int    `json:"itag"`
	URL              string `json:"url"`
	MimeType         string `json:"mimeType"`
	Bitrate          int    `json:"bitrate"`
	Width            int    `json:"width"`
	Height           int    `json:"height"`
	FPS              int    `json:"fps"`
	InitRange        *Range `json:"initRange"`
	IndexRange       *Range `json:"indexRange"`
	LastModified     string `json:"lastModified"`
	ContentLength    string `json:"contentLength"`
	Quality          string `json:"quality"`
	QualityLabel     string `json:"qualityLabel"`
	ProjectionType   string `json:"projectionType"`
	AverageBitrate   int    `json:"averageBitrate"`
	AudioQuality     string `json:"audioQuality"`
	ApproxDurationMs string `json:"approxDurationMs"`
	AudioSampleRate  string `json:"audioSampleRate"`
	AudioChannels    int    `json:"audioChannels"`
	SignatureCipher  string `json:"signatureCipher"`
	Cipher           string `json:"cipher"` // Legacy
}

type Range struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

type VideoDetails struct {
	VideoID           string           `json:"videoId"`
	Title             string           `json:"title"`
	LengthSeconds     string           `json:"lengthSeconds"`
	Keywords          []string         `json:"keywords"`
	ChannelID         string           `json:"channelId"`
	IsOwnerViewing    bool             `json:"isOwnerViewing"`
	ShortDescription  string           `json:"shortDescription"`
	IsCrawlable       bool             `json:"isCrawlable"`
	Thumbnail         ThumbnailDetails `json:"thumbnail"`
	AllowRatings      bool             `json:"allowRatings"`
	ViewCount         string           `json:"viewCount"`
	Author            string           `json:"author"`
	IsPrivate         bool             `json:"isPrivate"`
	IsUnpluggedCorpus bool             `json:"isUnpluggedCorpus"`
	IsLiveContent     bool             `json:"isLiveContent"`
}

type ThumbnailDetails struct {
	Thumbnails []Thumbnail `json:"thumbnails"`
}

type Thumbnail struct {
	URL    string `json:"url"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

type Microformat struct {
	PlayerMicroformatRenderer PlayerMicroformatRenderer `json:"playerMicroformatRenderer"`
}

type PlayerMicroformatRenderer struct {
	Thumbnail          ThumbnailDetails `json:"thumbnail"`
	Embed              Embed            `json:"embed"`
	Title              SimpleText       `json:"title"`
	Description        SimpleText       `json:"description"`
	LengthSeconds      string           `json:"lengthSeconds"`
	OwnerProfileUrl    string           `json:"ownerProfileUrl"`
	ExternalChannelId  string           `json:"externalChannelId"`
	IsFamilySafe       bool             `json:"isFamilySafe"`
	AvailableCountries []string         `json:"availableCountries"`
	IsUnlisted         bool             `json:"isUnlisted"`
	HasYpcMetadata     bool             `json:"hasYpcMetadata"`
	ViewCount          string           `json:"viewCount"`
	Category           string           `json:"category"`
	PublishDate        string           `json:"publishDate"`
	OwnerChannelName   string           `json:"ownerChannelName"`
	UploadDate         string           `json:"uploadDate"`
}

type Embed struct {
	IframeUrl string `json:"iframeUrl"`
	FlashUrl  string `json:"flashUrl"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
}

type SimpleText struct {
	SimpleText string `json:"simpleText"`
}

type Captions struct {
	PlayerCaptionsTracklistRenderer PlayerCaptionsTracklistRenderer `json:"playerCaptionsTracklistRenderer"`
}

type PlayerCaptionsTracklistRenderer struct {
	CaptionTracks []CaptionTrack `json:"captionTracks"`
}

type CaptionTrack struct {
	BaseURL      string   `json:"baseUrl"`
	Name         LangText `json:"name"`
	VssID        string   `json:"vssId"`
	LanguageCode string   `json:"languageCode"`
	Kind         string   `json:"kind,omitempty"`
}

type LangText struct {
	SimpleText string    `json:"simpleText"`
	Runs       []TextRun `json:"runs"`
}

type TextRun struct {
	Text string `json:"text"`
}
