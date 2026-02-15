package types

import "context"

type contextKey string

const (
	// ClientNameKey is the context key for the client name (e.g. "android", "web").
	ClientNameKey contextKey = "clientName"
)

// WithClientName returns a new context with the client name added.
func WithClientName(ctx context.Context, name string) context.Context {
	return context.WithValue(ctx, ClientNameKey, name)
}

// ClientNameFromContext returns the client name from the context.
func ClientNameFromContext(ctx context.Context) (string, bool) {
	name, ok := ctx.Value(ClientNameKey).(string)
	return name, ok
}
