package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientMetadataExtractsHeaders(t *testing.T) {
	var gotPlatform, gotVersion, gotOS string
	handler := ClientMetadata(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		gotPlatform, gotVersion, gotOS = ClientMetadataFromContext(r.Context())
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(HeaderClientPlatform, "desktop")
	req.Header.Set(HeaderClientVersion, "1.2.3")
	req.Header.Set(HeaderClientOS, "macos")

	handler.ServeHTTP(httptest.NewRecorder(), req)

	if gotPlatform != "desktop" {
		t.Errorf("platform: got %q, want desktop", gotPlatform)
	}
	if gotVersion != "1.2.3" {
		t.Errorf("version: got %q, want 1.2.3", gotVersion)
	}
	if gotOS != "macos" {
		t.Errorf("os: got %q, want macos", gotOS)
	}
}

func TestClientMetadataMissingHeadersReturnEmpty(t *testing.T) {
	var gotPlatform, gotVersion, gotOS string
	handler := ClientMetadata(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		gotPlatform, gotVersion, gotOS = ClientMetadataFromContext(r.Context())
	}))

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	if gotPlatform != "" || gotVersion != "" || gotOS != "" {
		t.Errorf("expected empty metadata, got (%q,%q,%q)", gotPlatform, gotVersion, gotOS)
	}
}

func TestSetClientMetadataAttachesValues(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	ctx := SetClientMetadata(req.Context(), "cli", "0.5.1", "linux")

	platform, version, os := ClientMetadataFromContext(ctx)
	if platform != "cli" || version != "0.5.1" || os != "linux" {
		t.Errorf("got (%q,%q,%q), want (cli,0.5.1,linux)", platform, version, os)
	}
}
