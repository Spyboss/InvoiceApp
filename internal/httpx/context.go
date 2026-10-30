// Package httpx holds the transport concerns that every handler shares:
// middleware, request identity and response helpers. It knows nothing about
// the domain.
package httpx

import "context"

type ctxKey int

const (
	ctxRequestID ctxKey = iota
	ctxClientIP
)

// RequestID is the correlation id for the current request.
func RequestID(ctx context.Context) string {
	id, _ := ctx.Value(ctxRequestID).(string)
	return id
}

// ClientIP is the address the request came from, as seen through the edge
// proxy.
func ClientIP(ctx context.Context) string {
	ip, _ := ctx.Value(ctxClientIP).(string)
	return ip
}
