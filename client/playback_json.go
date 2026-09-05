package client

import "context"

// ResolveVideoURLs returns an independent metadata copy with playback URLs
// resolved through the same signature, throttling and token policies as Download.
func (c *Client) ResolveVideoURLs(ctx context.Context, input string, info *VideoInfo) (*VideoInfo, error) {
	if info == nil {
		return nil, ErrNoPlayableFormats
	}
	out := cloneVideoInfo(info)
	id := info.ID
	if src := c.matchSource(input); src != nil {
		id = sourceSessionKey(src.Name(), info.ID)
	}
	for i := range out.Formats {
		u, err := c.resolveSelectedFormatURL(ctx, id, out.Formats[i])
		if err != nil {
			return nil, err
		}
		out.Formats[i].URL = u
		out.Formats[i].Ciphered = false
	}
	return out, nil
}

// PlaybackJSON applies format selection and resolves URLs for JSON consumers.
func (c *Client) PlaybackJSON(ctx context.Context, input string, info *VideoInfo, options DownloadOptions) (YTDLPDumpSingleJSON, error) {
	if info == nil {
		return YTDLPDumpSingleJSON{}, ErrNoPlayableFormats
	}
	selected, err := SelectFormatsForDownloadOptions(info.Formats, options)
	if err != nil {
		return YTDLPDumpSingleJSON{}, err
	}
	copy := cloneVideoInfo(info)
	copy.Formats = selected
	resolved, err := c.ResolveVideoURLs(ctx, input, copy)
	if err != nil {
		return YTDLPDumpSingleJSON{}, err
	}
	payload := BuildYTDLPDumpSingleJSON(input, resolved)
	id := info.ID
	if src := c.matchSource(input); src != nil {
		id = sourceSessionKey(src.Name(), info.ID)
	}
	for i, f := range resolved.Formats {
		headers := c.applySourceMediaHeaders(id, buildMediaRequestHeadersForSourceClient(c.config.RequestHeaders, id, f.SourceClient))
		payload.Formats[i].HTTPHeaders = make(map[string]string, len(headers))
		for k := range headers {
			payload.Formats[i].HTTPHeaders[k] = headers.Get(k)
		}
	}
	if len(selected) > 1 {
		payload.RequestedFormats = payload.Formats
		payload.URL = ""
	}
	return payload, nil
}
