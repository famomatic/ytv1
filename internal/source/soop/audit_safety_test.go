package soop

import (
	"bufio"
	"io"
	"net"
	"testing"
	"time"
)

func TestWebSocketWritesAfterHandshakeDeadline(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, e := ln.Accept()
		if e != nil {
			return
		}
		defer conn.Close()
		br := bufio.NewReader(conn)
		for {
			line, e := br.ReadString('\n')
			if e != nil {
				return
			}
			if line == "\r\n" {
				break
			}
		}
		io.WriteString(conn, "HTTP/1.1 101 Switching Protocols\r\n\r\n")
		io.Copy(io.Discard, conn)
	}()
	ws, err := wsDial(ln.Addr().String(), "/Websocket", 200*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { ws.conn.Close(); <-done }()
	<-time.After(250 * time.Millisecond)
	ws.conn.SetReadDeadline(time.Now().Add(time.Second))
	err = ws.writeText([]byte("heartbeat"))
	if err != nil {
		t.Fatalf("heartbeat after handshake deadline: %v", err)
	}
}
