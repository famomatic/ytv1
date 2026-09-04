package downloader

import (
	"context"
	"io"
	"net/http"
	"strings"
	"time"
)

// HLSMediaSegment is one decrypted media segment plus its optional fMP4
// initialization segment. Sequence is the HLS media-sequence number and is
// used to align independently published video and audio renditions.
type HLSMediaSegment struct {
	Sequence int
	Duration time.Duration
	Data     []byte
	Init     []byte
}

// HLSMediaSegmentReader incrementally follows a media playlist and returns
// complete segment bodies. Unlike HLSDownloader it does not concatenate the
// bodies, allowing a caller to demux separate video/audio renditions and mux
// their packets into one live output stream.
type HLSMediaSegmentReader struct {
	dl          *HLSDownloader
	pending     []hlsSegment
	endList     bool
	target      time.Duration
	nextRefresh time.Time
	liveEdge    int
	initURI     string
	initData    []byte
}

func NewHLSMediaSegmentReader(client *http.Client, playlistURL string) *HLSMediaSegmentReader {
	return &HLSMediaSegmentReader{dl: NewHLSDownloader(client, playlistURL)}
}

func (r *HLSMediaSegmentReader) WithRequestHeaders(headers http.Header) *HLSMediaSegmentReader {
	r.dl.WithRequestHeaders(headers)
	return r
}

func (r *HLSMediaSegmentReader) WithTransportConfig(cfg TransportConfig) *HLSMediaSegmentReader {
	r.dl.WithTransportConfig(cfg)
	return r
}

// WithLiveEdgeSegments limits the first read of a live playlist to the newest
// count segments. This avoids replaying an entire DVR window when a caller is
// opening a real-time stream. VOD playlists and subsequent refreshes are not
// truncated.
func (r *HLSMediaSegmentReader) WithLiveEdgeSegments(count int) *HLSMediaSegmentReader {
	r.liveEdge = max(count, 0)
	return r
}

func (r *HLSMediaSegmentReader) Next(ctx context.Context) (*HLSMediaSegment, error) {
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if len(r.pending) > 0 {
			seg := r.pending[0]
			r.pending = r.pending[1:]
			body, err := r.dl.fetchSegmentBody(ctx, seg)
			if err != nil {
				return nil, err
			}
			if seg.Map != nil && seg.Map.URI != r.initURI {
				r.initData, err = doGETBytesWithRetry(ctx, r.dl.Client, seg.Map.URI, r.dl.Headers, r.dl.Transport)
				if err != nil {
					return nil, err
				}
				r.initURI = seg.Map.URI
			}
			r.dl.lastSeq = seg.Seq
			r.dl.seenSegments = trackSeen(r.dl.seenSegments, seg.URL)
			if len(r.pending) == 0 && !r.endList {
				r.nextRefresh = time.Now().Add(refreshDelay(r.target))
			}
			return &HLSMediaSegment{
				Sequence: seg.Seq,
				Duration: time.Duration(seg.Duration * float64(time.Second)),
				Data:     body,
				Init:     append([]byte(nil), r.initData...),
			}, nil
		}
		if r.endList && r.dl.lastSeq >= 0 {
			return nil, io.EOF
		}
		if wait := time.Until(r.nextRefresh); wait > 0 {
			timer := time.NewTimer(wait)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, ctx.Err()
			case <-timer.C:
			}
		}

		manifest, err := r.dl.fetchManifest(ctx, r.dl.PlaylistURL)
		if err != nil {
			return nil, err
		}
		segments, targetSeconds, err := r.dl.parseSegments(ctx, manifest, r.dl.PlaylistURL)
		if err != nil {
			return nil, err
		}
		r.endList = strings.Contains(manifest, "#EXT-X-ENDLIST")
		if targetSeconds > 0 {
			r.target = time.Duration(targetSeconds * float64(time.Second))
		}
		firstManifest := r.dl.lastSeq < 0
		r.pending = r.dl.filterNewSegments(segments)
		if firstManifest && !r.endList && r.liveEdge > 0 && len(r.pending) > r.liveEdge {
			r.pending = r.pending[len(r.pending)-r.liveEdge:]
		}
		if len(r.pending) == 0 {
			if r.endList {
				return nil, io.EOF
			}
			r.nextRefresh = time.Now().Add(refreshDelay(r.target))
			continue
		}
	}
}

func refreshDelay(target time.Duration) time.Duration {
	if target <= 0 {
		return time.Second
	}
	delay := target / 2
	return min(max(delay, 250*time.Millisecond), 5*time.Second)
}
