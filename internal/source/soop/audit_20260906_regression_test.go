package soop

import (
	"io"
	"net"
	"net/http/httptest"
	"testing"
)

func TestAudit20260906AgentFailureLooksSuccessful(t *testing.T) {
	ln, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	old := agentServicePort
	agentServicePort = port
	defer func() { agentServicePort = old }()
	s := New(nil)
	defer s.Close()
	srv := httptest.NewServer(s.agentHandler(agentStreamParams{}, nil, false))
	defer srv.Close()
	r, e := srv.Client().Get(srv.URL)
	if e != nil {
		t.Fatal(e)
	}
	defer r.Body.Close()
	b, e := io.ReadAll(r.Body)
	if e != nil || r.StatusCode != 502 {
		t.Fatalf("regression: %d %q %v", r.StatusCode, b, e)
	}

}
