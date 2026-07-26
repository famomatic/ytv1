package soop

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestAgentStreamLive is a manual, opt-in end-to-end test: it drives the local
// SOOP agent and writes ~15s of remuxed MPEG-TS to $YTV1_SOOP_TS_OUT. Requires a
// running SOOPStreamer and a live broadcast. Gated by YTV1_SOOP_LIVE=1.
//
//	YTV1_SOOP_LIVE=1 YTV1_SOOP_FTK=<fanticket> YTV1_SOOP_TS_OUT=out.ts \
//	  go test ./internal/source/soop/ -run TestAgentStreamLive -v
func TestAgentStreamLive(t *testing.T) {
	if os.Getenv("YTV1_SOOP_LIVE") != "1" {
		t.Skip("set YTV1_SOOP_LIVE=1 to run the live agent-stream test")
	}
	outPath := os.Getenv("YTV1_SOOP_TS_OUT")
	if outPath == "" {
		outPath = "soop_agent.mp4"
	}
	f, err := os.Create(outPath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	p := agentStreamParams{
		BNO:         envOr("YTV1_SOOP_BNO", "295847533"),
		BjID:        envOr("YTV1_SOOP_BJID", "sdkels"),
		GUID:        "25631A3AB79CB882B26207735783A003",
		FanTicket:   os.Getenv("YTV1_SOOP_FTK"),
		AU:          os.Getenv("YTV1_SOOP_AU"),
		GateWayIP:   envOr("YTV1_SOOP_GWIP", "118.218.125.116"),
		GateWayPort: "3456",
		CenterIP:    envOr("YTV1_SOOP_CTIP", "110.10.76.216"),
		CenterPort:  "18000",
		Category:    envOr("YTV1_SOOP_CATE", "00740000"),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	src := New(nil)
	err = src.streamAgentMedia(ctx, p, f)
	if err != nil && err != context.DeadlineExceeded {
		t.Logf("stream ended: %v", err)
	}
	fi, _ := f.Stat()
	t.Logf("wrote %d bytes to %s", fi.Size(), outPath)
	if fi.Size() < 100000 {
		t.Fatalf("too little media (%d bytes)", fi.Size())
	}
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
