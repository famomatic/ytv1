package soop

import (
	"context"
	"errors"
	"net"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/famomatic/ytv1/internal/source/soop/p2p"
)

// TestP2PLive explicitly opts into network traffic. It resolves fresh endpoints
// and tickets, and never calls the installed SOOP agent.
func TestP2PLive(t *testing.T) {
	bj := os.Getenv("YTV1_SOOP_P2P_LIVE_BJID")
	if bj == "" {
		t.Skip("set YTV1_SOOP_P2P_LIVE_BJID to a currently live public broadcaster")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	s := New(nil)
	defer s.Close()
	info, err := s.fetchLiveInfo(ctx, bj, true)
	if err != nil {
		t.Fatal(err)
	}
	au := s.fetchAU(ctx, bj)
	w, err := s.fetchWatchInfo(ctx, bj, info.BNO, au)
	if err != nil {
		t.Fatal(err)
	}
	bno, _ := strconv.ParseUint(info.BNO, 10, 32)
	category, _ := strconv.ParseUint(info.Category, 10, 32)
	p := agentStreamParams{AU: au, GUID: newAgentGUID()}
	c := p2p.Config{Broadcast: uint32(bno), Broadcaster: bj, Category: uint32(category), GUID: p.GUID, FanTicket: w.FanTicket, JoinLog: p.joinlog("0"), GatewayAddress: net.JoinHostPort(orString(w.GateWayIP, info.GateWayIP), orString(w.GateWayPort, info.GateWayPort)), RelayAddress: net.JoinHostPort(orString(w.RelayIP, info.CenterIP), orString(w.RelayPort, info.CenterPort))}
	var size, video, audio int
	stop := errors.New("sample complete")
	var output *os.File
	if path := os.Getenv("YTV1_SOOP_P2P_RAW_OUT"); path != "" {
		output, err = os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		defer output.Close()
	}
	err = p2p.Stream(ctx, c, func(chunk []byte) error {
		if size == 0 {
			t.Log("first native media received")
		}
		size += len(chunk)
		switch classifyPayload(chunk[77:]) {
		case chunkVideo:
			video++
		case chunkAudio:
			audio++
		}
		if output != nil {
			if _, e := output.Write(chunk); e != nil {
				return e
			}
		}
		if size > 8<<20 && video > 100 && audio > 100 {
			return stop
		}
		return nil
	})
	t.Logf("broadcast=%s bytes=%d video=%d audio=%d", info.BNO, size, video, audio)
	if !errors.Is(err, stop) {
		t.Fatalf("native reception: %v", err)
	}
}
