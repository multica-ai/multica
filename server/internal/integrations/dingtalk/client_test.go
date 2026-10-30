package dingtalk

import (
	"context"
	"encoding/json"
	"fmt"
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

// downloadTestServer fakes the token + messageFiles/download endpoints and
// counts token mints so the 401-refresh path is observable.
func downloadTestServer(t *testing.T, onDownload func(w http.ResponseWriter, r *http.Request), tokenMints *int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case accessTokenPath:
			*tokenMints++
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(fmt.Sprintf(`{"accessToken":"tok-%d","expireIn":7200}`, *tokenMints)))
		case messageFilesDownloadPath:
			onDownload(w, r)
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func TestMessageFileDownloadURL(t *testing.T) {
	mints := 0
	srv := downloadTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("x-acs-dingtalk-access-token"); got != "tok-1" {
			t.Errorf("access token header = %q, want tok-1", got)
		}
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body["robotCode"] != "robot-1" || body["downloadCode"] != "dl-code" {
			t.Errorf("body = %v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"downloadUrl":"http://files.example/one"}`))
	}, &mints)
	defer srv.Close()

	c := NewClient(nil, srv.URL)
	url, err := c.messageFileDownloadURL(context.Background(), "k", "s", "robot-1", "dl-code")
	if err != nil {
		t.Fatalf("messageFileDownloadURL: %v", err)
	}
	if url != "http://files.example/one" {
		t.Errorf("url = %q", url)
	}
	if mints != 1 {
		t.Errorf("token mints = %d, want 1", mints)
	}
}

func TestMessageFileDownloadURL_RefreshOn401(t *testing.T) {
	mints := 0
	calls := 0
	srv := downloadTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if got := r.Header.Get("x-acs-dingtalk-access-token"); got != "tok-2" {
			t.Errorf("retry should carry the refreshed token, got %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"downloadUrl":"http://files.example/two"}`))
	}, &mints)
	defer srv.Close()

	c := NewClient(nil, srv.URL)
	url, err := c.messageFileDownloadURL(context.Background(), "k", "s", "robot-1", "dl-code")
	if err != nil {
		t.Fatalf("messageFileDownloadURL: %v", err)
	}
	if url != "http://files.example/two" {
		t.Errorf("url = %q", url)
	}
	if mints != 2 || calls != 2 {
		t.Errorf("mints=%d calls=%d, want 2/2", mints, calls)
	}
}

func TestMessageFileDownloadURL_EmptyURL(t *testing.T) {
	mints := 0
	srv := downloadTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}, &mints)
	defer srv.Close()

	c := NewClient(nil, srv.URL)
	if _, err := c.messageFileDownloadURL(context.Background(), "k", "s", "robot-1", "dl-code"); err == nil {
		t.Fatal("expected an error on an empty downloadUrl")
	}
}
