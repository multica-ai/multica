package lark

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestHTTPConnectionTokenFetcherCallbackEndpointSuccess exercises the
// happy path of the official /callback/ws/endpoint bootstrap: AppID +
// AppSecret in the body, no tenant_access_token bearer, response
// shape mirrors the SDK's EndpointResp + ClientConfig.
func TestHTTPConnectionTokenFetcherCallbackEndpointSuccess(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/callback/ws/endpoint" {
			t.Errorf("path = %q; want /callback/ws/endpoint", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("method = %q; want POST", r.Method)
		}
		if r.Header.Get("Authorization") != "" {
			t.Errorf("Authorization header should be empty for callback bootstrap")
		}
		var body bootstrapRequest
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.AppID != "cli_app_x" || body.AppSecret != "secret-xyz" {
			t.Errorf("body = %+v", body)
		}
		_, _ = io.WriteString(w, `{
			"code":0,
			"data":{
				"URL":"wss://lark.example/ws/foo?device_id=dev-1&service_id=42",
				"ClientConfig":{
					"ReconnectCount":-1,
					"ReconnectInterval":120,
					"ReconnectNonce":30,
					"PingInterval":120
				}
			}
		}`)
	}))
	defer srv.Close()

	f, err := NewHTTPConnectionTokenFetcher(HTTPConnectionTokenConfig{
		BaseURL: srv.URL,
		Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("constructor: %v", err)
	}
	ep, err := f.Endpoint(context.Background(), InstallationCredentials{AppID: "cli_app_x", AppSecret: "secret-xyz"})
	if err != nil {
		t.Fatalf("Endpoint: %v", err)
	}
	if ep.URL != "wss://lark.example/ws/foo?device_id=dev-1&service_id=42" {
		t.Errorf("URL = %q", ep.URL)
	}
	if ep.ServiceID != 42 {
		t.Errorf("ServiceID = %d; want 42", ep.ServiceID)
	}
	if ep.PingInterval != 120*time.Second {
		t.Errorf("PingInterval = %s; want 120s", ep.PingInterval)
	}
	if ep.ReconnectInterval != 120*time.Second {
		t.Errorf("ReconnectInterval = %s", ep.ReconnectInterval)
	}
	if ep.ReconnectNonce != 30*time.Second {
		t.Errorf("ReconnectNonce = %s", ep.ReconnectNonce)
	}
	if ep.ReconnectCount != -1 {
		t.Errorf("ReconnectCount = %d", ep.ReconnectCount)
	}
}

// TestHTTPConnectionTokenFetcherSurfacesLarkErrorCode confirms that
// app-type / auth errors from Lark (e.g. PersonalAgent not eligible)
// surface verbatim so the Hub backoff loop logs them.
func TestHTTPConnectionTokenFetcherSurfacesLarkErrorCode(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"code":403,"msg":"app type not supported"}`)
	}))
	defer srv.Close()

	f, _ := NewHTTPConnectionTokenFetcher(HTTPConnectionTokenConfig{
		BaseURL: srv.URL,
	})
	_, err := f.Endpoint(context.Background(), InstallationCredentials{AppID: "a", AppSecret: "s"})
	if err == nil {
		t.Fatal("expected error for non-zero Lark code")
	}
	if !contains(err.Error(), "app type not supported") {
		t.Errorf("error should surface Lark msg: %v", err)
	}
}

func TestHTTPConnectionTokenFetcherRejectsHTTPErrorStatus(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `boom`)
	}))
	defer srv.Close()

	f, _ := NewHTTPConnectionTokenFetcher(HTTPConnectionTokenConfig{
		BaseURL: srv.URL,
	})
	_, err := f.Endpoint(context.Background(), InstallationCredentials{AppID: "a", AppSecret: "s"})
	if err == nil {
		t.Fatal("expected error on 5xx")
	}
}

func TestHTTPConnectionTokenFetcherRejectsMissingCredentials(t *testing.T) {
	t.Parallel()
	f, _ := NewHTTPConnectionTokenFetcher(HTTPConnectionTokenConfig{BaseURL: "http://unused"})
	if _, err := f.Endpoint(context.Background(), InstallationCredentials{AppID: ""}); err == nil {
		t.Fatal("expected error on missing app_id")
	}
	if _, err := f.Endpoint(context.Background(), InstallationCredentials{AppID: "a"}); err == nil {
		t.Fatal("expected error on missing app_secret")
	}
}

func TestHTTPConnectionTokenFetcherRejectsURLWithoutServiceID(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Response URL is well-formed but missing the service_id
		// query param — the connector can't address outbound frames
		// without it, so the fetcher must reject the bootstrap.
		_, _ = io.WriteString(w, `{"code":0,"data":{"URL":"wss://lark.example/ws?device_id=dev-1","ClientConfig":{"PingInterval":120}}}`)
	}))
	defer srv.Close()

	f, _ := NewHTTPConnectionTokenFetcher(HTTPConnectionTokenConfig{BaseURL: srv.URL})
	_, err := f.Endpoint(context.Background(), InstallationCredentials{AppID: "a", AppSecret: "s"})
	if err == nil {
		t.Fatal("expected error when service_id is missing from response URL")
	}
}

func contains(s, sub string) bool {
	return s != "" && sub != "" && (s == sub || (len(s) > len(sub) && indexOf(s, sub) >= 0))
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
