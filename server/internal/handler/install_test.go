package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
