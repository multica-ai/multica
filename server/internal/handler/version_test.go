package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetVersionReportsConfiguredServerBuildInfo(t *testing.T) {
	h := &Handler{cfg: Config{ServerVersion: "v1.2.3", ServerCommit: "abc123"}}

	req := httptest.NewRequest(http.MethodGet, "/api/version", nil)
	w := httptest.NewRecorder()

	h.GetVersion(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GetVersion: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp ServerVersionResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode version response: %v", err)
	}
	if resp.Version != "v1.2.3" {
		t.Fatalf("version: want v1.2.3, got %q", resp.Version)
	}
	if resp.Commit != "abc123" {
		t.Fatalf("commit: want abc123, got %q", resp.Commit)
	}
}

func TestGetVersionDefaultsMissingBuildInfo(t *testing.T) {
	h := &Handler{}

	req := httptest.NewRequest(http.MethodGet, "/api/version", nil)
	w := httptest.NewRecorder()

	h.GetVersion(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GetVersion: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp ServerVersionResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode version response: %v", err)
	}
	if resp.Version != "dev" {
		t.Fatalf("version: want dev, got %q", resp.Version)
	}
	if resp.Commit != "unknown" {
		t.Fatalf("commit: want unknown, got %q", resp.Commit)
	}
}
