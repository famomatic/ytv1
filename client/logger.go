package client

// Logger is an optional package logger used for non-fatal warnings.
type Logger interface {
	Warnf(format string, args ...any)
}

type nopLogger struct{}

func (nopLogger) Warnf(string, ...any) {}
