package lark

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHTTPClientDownloadMessageResource(t *testing.T) {
	var gotPath, gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/open-apis/auth/v3/tenant_access_token/internal":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":0,"tenant_access_token":"token","expire":7200}`))
		case r.URL.EscapedPath() == "/open-apis/im/v1/messages/om_1/resources/img%2Fkey":
			gotPath, gotQuery = r.URL.EscapedPath(), r.URL.RawQuery
			w.Header().Set("Content-Type", "image/png")
			w.Header().Set("Content-Disposition", `attachment; filename="screen.png"`)
			_, _ = w.Write([]byte("png"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client := NewHTTPAPIClient(HTTPClientConfig{BaseURL: srv.URL}).(*httpAPIClient)
	got, err := client.DownloadMessageResource(context.Background(), InstallationCredentials{
		AppID: "cli", AppSecret: "secret",
	}, "om_1", "img/key", "image")
	if err != nil {
		t.Fatalf("DownloadMessageResource: %v", err)
	}
	if gotPath != "/open-apis/im/v1/messages/om_1/resources/img%2Fkey" || gotQuery != "type=image" {
		t.Fatalf("request = %s?%s", gotPath, gotQuery)
	}
	if string(got.Data) != "png" || got.Filename != "screen.png" || got.ContentType != "image/png" {
		t.Fatalf("resource = %+v", got)
	}
}

func TestHTTPClientDownloadMessageResourceParsesContentDisposition(t *testing.T) {
	tests := []struct {
		header string
		want   string
	}{
		{`Attachment ; filename = "screen shot.png"`, "screen shot.png"},
		{`attachment; filename="fallback.png"; filename*=UTF-8''screen%20shot.png`, "screen shot.png"},
	}
	for _, tc := range tests {
		t.Run(tc.header, func(t *testing.T) {
			srv := resourceTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "image/png")
				w.Header().Set("Content-Disposition", tc.header)
				_, _ = io.WriteString(w, "png")
			})
			defer srv.Close()
			client := NewHTTPAPIClient(HTTPClientConfig{BaseURL: srv.URL}).(*httpAPIClient)
			got, err := client.DownloadMessageResource(context.Background(), testCreds(), "om_1", "img_key", "image")
			if err != nil {
				t.Fatalf("DownloadMessageResource: %v", err)
			}
			if got.Filename != tc.want {
				t.Fatalf("Filename = %q, want %q", got.Filename, tc.want)
			}
		})
	}
}

func TestHTTPClientDownloadMessageResourceRejectsOversize(t *testing.T) {
	srv := resourceTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = io.WriteString(w, "12345")
	})
	defer srv.Close()

	client := NewHTTPAPIClient(HTTPClientConfig{BaseURL: srv.URL, MaxResourceBytes: 4}).(*httpAPIClient)
	_, err := client.DownloadMessageResource(context.Background(), testCreds(), "om_1", "img_key", "image")
	if err == nil || !strings.Contains(err.Error(), "exceeds 4 bytes") {
		t.Fatalf("expected size-limit error, got %v", err)
	}
}

func TestHTTPClientDownloadMessageResourceBusinessErrorInvalidatesToken(t *testing.T) {
	tokenCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/open-apis/auth/v3/tenant_access_token/internal" {
			tokenCalls++
			_, _ = io.WriteString(w, `{"code":0,"tenant_access_token":"token","expire":7200}`)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = io.WriteString(w, `{"code":99991663,"msg":"token expired"}`)
	}))
	defer srv.Close()

	client := NewHTTPAPIClient(HTTPClientConfig{BaseURL: srv.URL}).(*httpAPIClient)
	for i := 0; i < 2; i++ {
		_, err := client.DownloadMessageResource(context.Background(), testCreds(), "om_1", "img_key", "image")
		var apiErr *APIError
		if !errors.As(err, &apiErr) || apiErr.Code != codeTokenExpired {
			t.Fatalf("call %d: expected token APIError, got %v", i, err)
		}
	}
	if tokenCalls != 2 {
		t.Fatalf("invalid token must be evicted; token endpoint calls=%d want 2", tokenCalls)
	}
}

func resourceTestServer(t *testing.T, resource http.HandlerFunc) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/open-apis/auth/v3/tenant_access_token/internal":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"code":0,"tenant_access_token":"token","expire":7200}`)
		case "/open-apis/im/v1/messages/om_1/resources/img_key":
			resource(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
}
