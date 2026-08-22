package httpapi

import (
	"context"
	"net/http"
)

type contextKey string

const requestIDKey contextKey = "requestID"

func withRequestID(r *http.Request) *http.Request {
	id := r.Header.Get("X-Request-ID")
	if id == "" {
		id = "local-request"
	}
	return r.WithContext(context.WithValue(r.Context(), requestIDKey, id))
}
func requestID(r *http.Request) string {
	if v, ok := r.Context().Value(requestIDKey).(string); ok {
		return v
	}
	return "local-request"
}
