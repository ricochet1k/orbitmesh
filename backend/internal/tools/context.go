package tools

import "context"

type contextKey string

const (
	ctxSessionIDKey  contextKey = "session_id"
	ctxAPIBaseURLKey contextKey = "api_base_url"
)

func WithSessionID(ctx context.Context, sessionID string) context.Context {
	return context.WithValue(ctx, ctxSessionIDKey, sessionID)
}

func SessionIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	v, _ := ctx.Value(ctxSessionIDKey).(string)
	return v
}

func WithAPIBaseURL(ctx context.Context, baseURL string) context.Context {
	return context.WithValue(ctx, ctxAPIBaseURLKey, baseURL)
}

func APIBaseURLFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	v, _ := ctx.Value(ctxAPIBaseURLKey).(string)
	return v
}
