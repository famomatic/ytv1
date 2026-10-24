package client

import (
	"context"
	"net/http"
)

type sourceOperationKey struct{}
type sourceOperation struct {
	id      string
	headers http.Header
}

func (c *Client) applySourceMediaHeadersContext(ctx context.Context, id string, base http.Header) http.Header {
	if op, ok := ctx.Value(sourceOperationKey{}).(sourceOperation); ok && op.id == id {
		if base == nil {
			base = make(http.Header)
		}
		for k, v := range op.headers {
			base[k] = append([]string(nil), v...)
		}
		return base
	}
	return c.applySourceMediaHeaders(id, base)
}
