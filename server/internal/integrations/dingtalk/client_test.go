package dingtalk

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchAccessToken_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != accessTokenPath {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"accessToken":"tok-123","expireIn":7200}`))
	}))
	defer srv.Close()

	tok, ttl, err := fetchAccessToken(context.Background(), nil, srv.URL, "k", "s")
	if err != nil {
		t.Fatalf("fetchAccessToken: %v", err)
	}
	if tok != "tok-123" || ttl != 7200 {
		t.Errorf("got %q / %d, want tok-123 / 7200", tok, ttl)
	}
}

func TestFetchAccessToken_ErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"code":"InvalidAuthentication","message":"bad creds"}`))
	}))
	defer srv.Close()

	if _, _, err := fetchAccessToken(context.Background(), nil, srv.URL, "k", "s"); err == nil {
		t.Fatal("expected an error on a 400 response")
	}
}

func TestFetchAccessToken_MissingToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"expireIn":7200}`))
	}))
	defer srv.Close()

	if _, _, err := fetchAccessToken(context.Background(), nil, srv.URL, "k", "s"); err == nil {
		t.Fatal("expected an error when the response carries no accessToken")
	}
}
