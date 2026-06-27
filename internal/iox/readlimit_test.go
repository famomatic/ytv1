package iox

import (
	"errors"
	"strings"
	"testing"
)

func TestReadAllLimit_WithinLimit(t *testing.T) {
	input := strings.NewReader("hello world")
	data, err := ReadAllLimit(input, 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(data) != "hello world" {
		t.Fatalf("got %q, want %q", string(data), "hello world")
	}
}

func TestReadAllLimit_ExactLimit(t *testing.T) {
	input := strings.NewReader("hello")
	data, err := ReadAllLimit(input, 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(data) != "hello" {
		t.Fatalf("got %q, want %q", string(data), "hello")
	}
}

func TestReadAllLimit_ExceedsLimit(t *testing.T) {
	input := strings.NewReader("hello world")
	_, err := ReadAllLimit(input, 5)
	if !errors.Is(err, ErrBodyTooLarge) {
		t.Fatalf("got err=%v, want ErrBodyTooLarge", err)
	}
}

func TestReadAllLimit_ZeroLimit(t *testing.T) {
	input := strings.NewReader("x")
	_, err := ReadAllLimit(input, 0)
	if !errors.Is(err, ErrBodyTooLarge) {
		t.Fatalf("got err=%v, want ErrBodyTooLarge", err)
	}
}

func TestReadAllLimit_EmptyInput(t *testing.T) {
	input := strings.NewReader("")
	data, err := ReadAllLimit(input, 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(data) != 0 {
		t.Fatalf("got %d bytes, want 0", len(data))
	}
}

func TestReadAllLimit_ReadError(t *testing.T) {
	input := &errorReader{}
	_, err := ReadAllLimit(input, 100)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if errors.Is(err, ErrBodyTooLarge) {
		t.Fatal("should not be ErrBodyTooLarge for reader errors")
	}
}

type errorReader struct{}

func (r *errorReader) Read(_ []byte) (int, error) {
	return 0, errors.New("read failure")
}
