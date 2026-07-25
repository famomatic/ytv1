package soop

// Live channel info fetched from player_live_api.php with type=live. Unlike the
// type=aid call (which returns only the CDN auth token), type=live returns the
// P2P grid coordinates the local agent needs to join the mesh. These fields are
// returned for anonymous requests, so the agent path needs no login.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
)

// liveInfo holds the grid/session parameters for a broadcast.
type liveInfo struct {
	BNO    string
	BjID   string // streamer id (from the input URL / bid)
	Title  string
	Author string

	CenterIP    string // CTIP
	CenterPort  string // CTPT
	GateWayIP   string // GWIP
	GateWayPort string // GWPT
	Category    string // CATE
	FanTicket   string // FTK
	Ticket      string // TK (login/guest cookie ticket; empty when anonymous)
	ResourceMD  string // RMD
	Password    bool   // BPWD == "Y"
}

// fetchLiveInfo calls player_live_api.php (type=live) and returns the broadcast
// grid coordinates. bno may be empty, in which case the API resolves the
// streamer's current broadcast and the returned BNO reflects it.
func (s *Source) fetchLiveInfo(ctx context.Context, bjIDOrBNO string, byBJID bool) (*liveInfo, error) {
	form := url.Values{}
	if byBJID {
		form.Set("bid", bjIDOrBNO)
	} else {
		form.Set("bno", bjIDOrBNO)
	}
	form.Set("type", "live")

	body, err := s.postForm(ctx, liveInfoAPI, form, desktopUserAgent)
	if err != nil {
		return nil, fmt.Errorf("soop: live info api: %w", err)
	}

	var res struct {
		Channel struct {
			Result json.Number `json:"RESULT"`
			BNO    json.Number `json:"BNO"`
			Title  string      `json:"TITLE"`
			BjNick string      `json:"BJNICK"`
			CTIP   string      `json:"CTIP"`
			CTPT   json.Number `json:"CTPT"`
			GWIP   string      `json:"GWIP"`
			GWPT   json.Number `json:"GWPT"`
			CATE   string      `json:"CATE"`
			FTK    string      `json:"FTK"`
			TK     string      `json:"TK"`
			RMD    string      `json:"RMD"`
			BPWD   string      `json:"BPWD"`
		} `json:"CHANNEL"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return nil, fmt.Errorf("soop: decode live info: %w", err)
	}
	ch := res.Channel

	bno := ch.BNO.String()
	if bno == "" || bno == "0" {
		if byBJID {
			return nil, fmt.Errorf("soop: streamer %s is not live (no broadcast number); provide a full /<bjid>/<bno> URL", bjIDOrBNO)
		}
		bno = bjIDOrBNO
	}

	bjID := ""
	if byBJID {
		bjID = bjIDOrBNO
	}
	return &liveInfo{
		BNO:         bno,
		BjID:        bjID,
		Title:       ch.Title,
		Author:      ch.BjNick,
		CenterIP:    ch.CTIP,
		CenterPort:  ch.CTPT.String(),
		GateWayIP:   ch.GWIP,
		GateWayPort: ch.GWPT.String(),
		Category:    ch.CATE,
		FanTicket:   ch.FTK,
		Ticket:      ch.TK,
		ResourceMD:  ch.RMD,
		Password:    ch.BPWD == "Y",
	}, nil
}
