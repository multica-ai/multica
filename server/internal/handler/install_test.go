package handler

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestServeInstallSH(t *testing.T) {
	h := &Handler{}
	req := httptest.NewRequest(http.MethodGet, "/install.sh", nil)
	w := httptest.NewRecorder()

	h.ServeInstallSH(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	ct := w.Header().Get("Content-Type")
	if !strings.Contains(ct, "shellscript") {
		t.Fatalf("expected shellscript content type, got %q", ct)
	}
	body := w.Body.String()
	if !strings.Contains(body, "#!/bin/sh") {
		t.Fatal("response does not contain shebang")
	}
	if !strings.Contains(body, "multica.obs.cn-east-3.myhuaweicloud.com") {
		t.Fatal("response does not contain OBS URL")
	}
	// Must NOT contain a hardcoded version
	if strings.Contains(body, "MULTICA_VERSION=") && !strings.Contains(body, "${MULTICA_VERSION:-}") {
		t.Fatal("script appears to hardcode a version")
	}
}

func TestServeInstallPS1(t *testing.T) {
	h := &Handler{}
	req := httptest.NewRequest(http.MethodGet, "/install.ps1", nil)
	w := httptest.NewRecorder()

	h.ServeInstallPS1(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "Multica CLI installer") {
		t.Fatal("response does not contain expected header comment")
	}
	if !strings.Contains(body, "multica.obs.cn-east-3.myhuaweicloud.com") {
		t.Fatal("response does not contain OBS URL")
	}
}

func TestServeLatestCLIVersionRespectsChannel(t *testing.T) {
	resetCLIVersionCache()

	manifestServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/cli/manifest.json":
			fmt.Fprint(w, `{"version":"v1.2.3"}`)
		case "/cli-test/manifest.json":
			fmt.Fprint(w, `{"version":"v9.8.7"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer manifestServer.Close()

	origProdManifestURL := prodManifestURL
	origTestManifestURL := testManifestURL
	prodManifestURL = manifestServer.URL + "/cli/manifest.json"
	testManifestURL = manifestServer.URL + "/cli-test/manifest.json"
	defer func() {
		prodManifestURL = origProdManifestURL
		testManifestURL = origTestManifestURL
		resetCLIVersionCache()
	}()

	h := &Handler{}

	prodReq := httptest.NewRequest(http.MethodGet, "/install/latest-cli-version", nil)
	prodResp := httptest.NewRecorder()
	h.ServeLatestCLIVersion(prodResp, prodReq)
	if prodResp.Code != http.StatusOK {
		t.Fatalf("expected prod 200, got %d", prodResp.Code)
	}
	if got := prodResp.Body.String(); got != "1.2.3\n" {
		t.Fatalf("expected prod version 1.2.3, got %q", got)
	}

	testReq := httptest.NewRequest(http.MethodGet, "/install/latest-cli-version?channel=test", nil)
	testResp := httptest.NewRecorder()
	h.ServeLatestCLIVersion(testResp, testReq)
	if testResp.Code != http.StatusOK {
		t.Fatalf("expected test 200, got %d", testResp.Code)
	}
	if got := testResp.Body.String(); got != "9.8.7\n" {
		t.Fatalf("expected test version 9.8.7, got %q", got)
	}
}

func TestServeLatestCLIVersionRejectsUnknownChannel(t *testing.T) {
	h := &Handler{}
	req := httptest.NewRequest(http.MethodGet, "/install/latest-cli-version?channel=staging", nil)
	resp := httptest.NewRecorder()

	h.ServeLatestCLIVersion(resp, req)

	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.Code)
	}
}

func resetCLIVersionCache() {
	versionCacheMu.Lock()
	defer versionCacheMu.Unlock()
	cachedVersionByChannel = map[string]string{}
	cacheExpiryByChannel = map[string]time.Time{}
}
