package middleware

import (
	"context"
	"net/http"
)

// Client metadata context keys.
//
// Populated by ClientMetadata middleware from X-Client-Platform / X-Client-Version /
// X-Client-OS request headers. Sent by every first-party client (Web, Desktop, CLI,
// Daemon) so the server can split logs / metrics / gating decisions by caller
// without having to reverse-engineer User-Agent strings or upgrade payloads.
//
// All three values are best-effort: handlers must treat missing values as
// "unknown" and never make security decisions based on them — these headers
// are client-controlled and trivial to spoof.
type clientMetadataKey int

const (
	ctxKeyClientPlatform clientMetadataKey = iota
	ctxKeyClientVersion
	ctxKeyClientOS
)

// Header names — exported so other packages (request logger, realtime hub)
// can stay in sync without re-declaring magic strings.
const (
	HeaderClientPlatform = "X-Client-Platform"
	HeaderClientVersion  = "X-Client-Version"
	HeaderClientOS       = "X-Client-OS"
)

// ClientMetadata extracts X-Client-Platform / X-Client-Version / X-Client-OS
// from the request and stashes them in the request context so downstream
// handlers and the request logger can read them via ClientMetadataFromContext.
//
// Wired in router.go before route mounting so every authenticated and
// unauthenticated handler benefits from the same observability dimensions.
func ClientMetadata(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		if v := r.Header.Get(HeaderClientPlatform); v != "" {
			ctx = context.WithValue(ctx, ctxKeyClientPlatform, v)
		}
		if v := r.Header.Get(HeaderClientVersion); v != "" {
			ctx = context.WithValue(ctx, ctxKeyClientVersion, v)
		}
		if v := r.Header.Get(HeaderClientOS); v != "" {
			ctx = context.WithValue(ctx, ctxKeyClientOS, v)
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// ClientMetadataFromContext returns the platform/version/os captured from
// X-Client-* headers. Empty strings are returned for any value that wasn't
// sent — callers must treat missing values as "unknown" rather than failing.
func ClientMetadataFromContext(ctx context.Context) (platform, version, os string) {
	platform, _ = ctx.Value(ctxKeyClientPlatform).(string)
	version, _ = ctx.Value(ctxKeyClientVersion).(string)
	os, _ = ctx.Value(ctxKeyClientOS).(string)
	return platform, version, os
}

// SetClientMetadata explicitly attaches client metadata to a context. Used
// by the realtime hub, where metadata arrives via WS query parameters
// (`client_platform`, `client_version`, `client_os`) instead of headers.
func SetClientMetadata(ctx context.Context, platform, version, os string) context.Context {
	if platform != "" {
		ctx = context.WithValue(ctx, ctxKeyClientPlatform, platform)
	}
	if version != "" {
		ctx = context.WithValue(ctx, ctxKeyClientVersion, version)
	}
	if os != "" {
		ctx = context.WithValue(ctx, ctxKeyClientOS, os)
	}
	return ctx
}
