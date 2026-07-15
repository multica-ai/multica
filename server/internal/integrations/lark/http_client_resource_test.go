package lark

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHTTPClientDownloadMessageResource(t *testing.T) {
	var gotPath, gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/open-apis/auth/v3/tenant_access_token/internal":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":0,"tenant_access_token":"token","expire":7200}`))
		case "/open-apis/im/v1/messages/om_1/resources":
			gotPath, gotQuery = r.URL.Path, r.URL.RawQuery
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
	}, "om_1", "img_key", "image")
	if err != nil {
		t.Fatalf("DownloadMessageResource: %v", err)
	}
	if gotPath == "" || gotQuery != "file_key=img_key&type=image" {
		t.Fatalf("request = %s?%s", gotPath, gotQuery)
	}
	if string(got.Data) != "png" || got.Filename != "screen.png" || got.ContentType != "image/png" {
		t.Fatalf("resource = %+v", got)
	}
}
