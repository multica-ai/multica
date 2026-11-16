package middleware

import (
	"net/http"
	"strings"
)

const cspBaseHeader = "default-src 'self'; " +
	"script-src 'self'; " +
	"style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' https: data:; " +
	"connect-src 'self' wss:; "

const cspHeader = cspBaseHeader +
	"frame-ancestors 'none'; " +
	"object-src 'none'; " +
	"base-uri 'self'; " +
	"form-action 'self'"

const attachmentPreviewCSPHeader = cspBaseHeader +
	"frame-ancestors 'self'; " +
	"object-src 'none'; " +
	"base-uri 'self'; " +
	"form-action 'self'"

func ContentSecurityPolicy(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", contentSecurityPolicyForRequest(r))
		next.ServeHTTP(w, r)
	})
}

func contentSecurityPolicyForRequest(r *http.Request) string {
	if isAttachmentPreviewDocumentPath(r.URL.Path) {
		return attachmentPreviewCSPHeader
	}
	return cspHeader
}

func isAttachmentPreviewDocumentPath(path string) bool {
	return strings.HasPrefix(path, "/api/attachments/") &&
		(strings.HasSuffix(path, "/download") || strings.HasSuffix(path, "/content"))
}
