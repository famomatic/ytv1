package soop

import (
	"bufio"
	"strings"
	"testing"
)

func TestFrameRoundtrip(t *testing.T) {
	cases := [][]byte{
		[]byte(""),
		[]byte("hi"),
		[]byte(`{"SVC":4,"RESULT":0,"DATA":{"HLS_PORT":2935}}`),
		make([]byte, 200),   // 2-byte extended length
		make([]byte, 70000), // 8-byte extended length
	}
	for i, payload := range cases {
		frame, err := encodeClientFrame(wsOpText, payload)
		if err != nil {
			t.Fatalf("case %d encode: %v", i, err)
		}
		op, got, err := readServerFrame(bufio.NewReader(strings.NewReader(string(frame))))
		if err != nil {
			t.Fatalf("case %d decode: %v", i, err)
		}
		if op != wsOpText {
			t.Errorf("case %d opcode = %d, want %d", i, op, wsOpText)
		}
		if string(got) != string(payload) {
			t.Errorf("case %d payload mismatch: got %d bytes want %d", i, len(got), len(payload))
		}
	}
}

func TestFieldHelpers(t *testing.T) {
	m := map[string]any{
		"s":     "text",
		"empty": "",
		"n":     float64(2935),
		"ns":    "1080",
	}
	if v, ok := stringField(m, "s"); !ok || v != "text" {
		t.Errorf("stringField s = %q,%v", v, ok)
	}
	if _, ok := stringField(m, "empty"); ok {
		t.Error("empty string should report absent")
	}
	if v, ok := intField(m, "n"); !ok || v != 2935 {
		t.Errorf("intField n = %d,%v", v, ok)
	}
	if v, ok := intField(m, "ns"); !ok || v != 1080 {
		t.Errorf("intField ns = %d,%v", v, ok)
	}
	if asInt("18000") != 18000 || asInt("") != 0 {
		t.Error("asInt mismatch")
	}
	if orString("", "fb") != "fb" || orString("a", "fb") != "a" {
		t.Error("orString mismatch")
	}
}
