package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

type fakeMinter struct {
	token string
	err   error
	calls int
	lastInstallation int64
}

func (f *fakeMinter) MintToken(_ context.Context, installationID int64) (string, error) {
	f.calls++
	f.lastInstallation = installationID
	return f.token, f.err
}

func TestToken_NoMinter_Returns503(t *testing.T) {
	srv := newTestServer(t, "http://unused", map[string]struct{}{"zoopone/multica": {}})
	defer srv.Close()
	// no SetTokenMinter

	req := httptest.NewRequest(http.MethodGet, "/installation-token?nonce=anything", nil)
	rr := httptest.NewRecorder()
	srv.Router().ServeHTTP(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rr.Code)
	}
}

func TestToken_MissingNonce_Returns400(t *testing.T) {
	srv := newTestServer(t, "http://unused", map[string]struct{}{"zoopone/multica": {}})
	defer srv.Close()
	srv.SetTokenMinter(&fakeMinter{token: "x"})

	req := httptest.NewRequest(http.MethodGet, "/installation-token", nil)
	rr := httptest.NewRecorder()
	srv.Router().ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rr.Code)
	}
}

func TestToken_UnknownNonce_Returns404(t *testing.T) {
	srv := newTestServer(t, "http://unused", map[string]struct{}{"zoopone/multica": {}})
	defer srv.Close()
	srv.SetTokenMinter(&fakeMinter{token: "x"})

	req := httptest.NewRequest(http.MethodGet, "/installation-token?nonce=never-was", nil)
	rr := httptest.NewRecorder()
	srv.Router().ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rr.Code)
	}
}

func TestToken_HappyPath_ReturnsTokenAndConsumesNonce(t *testing.T) {
	srv := newTestServer(t, "http://unused", map[string]struct{}{"zoopone/multica": {}})
	defer srv.Close()
	m := &fakeMinter{token: "ghs_freshtoken"}
	srv.SetTokenMinter(m)

	// Seed a nonce as if a webhook had just landed.
	prCtx := PRContext{InstallationID: 999, Owner: "zoopone", Repo: "multica", PRNumber: 7, HeadSHA: "abc"}
	nonce, err := srv.nonces.Put(prCtx)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/installation-token?nonce="+nonce, nil)
	rr := httptest.NewRecorder()
	srv.Router().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
	if m.calls != 1 || m.lastInstallation != 999 {
		t.Errorf("minter not called with right installation: calls=%d installation=%d", m.calls, m.lastInstallation)
	}

	var body map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["token"] != "ghs_freshtoken" {
		t.Errorf("token = %v", body["token"])
	}
	if body["repo"] != "zoopone/multica" {
		t.Errorf("repo = %v", body["repo"])
	}
	if body["head_sha"] != "abc" {
		t.Errorf("head_sha = %v", body["head_sha"])
	}

	// Second call with same nonce must 404 (single-use).
	rr2 := httptest.NewRecorder()
	srv.Router().ServeHTTP(rr2, httptest.NewRequest(http.MethodGet, "/installation-token?nonce="+nonce, nil))
	if rr2.Code != http.StatusNotFound {
		t.Fatalf("second consume status = %d, want 404", rr2.Code)
	}
}

func TestToken_MinterError_Returns500(t *testing.T) {
	srv := newTestServer(t, "http://unused", map[string]struct{}{"zoopone/multica": {}})
	defer srv.Close()
	srv.SetTokenMinter(&fakeMinter{err: errors.New("github 503")})

	nonce, _ := srv.nonces.Put(PRContext{InstallationID: 1})

	req := httptest.NewRequest(http.MethodGet, "/installation-token?nonce="+nonce, nil)
	rr := httptest.NewRecorder()
	srv.Router().ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rr.Code)
	}
}
