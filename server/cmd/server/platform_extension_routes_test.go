package main

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestPlatformExtensionRoutesAreRegisteredInWorkspaceMemberGroup(t *testing.T) {
	list := authRequest(t, http.MethodGet, "/api/extensions", nil)
	if list.StatusCode != http.StatusOK {
		var body any
		_ = json.NewDecoder(list.Body).Decode(&body)
		list.Body.Close()
		t.Fatalf("GET /api/extensions = %d, want 200: %+v", list.StatusCode, body)
	}
	var releases []any
	readJSON(t, list, &releases)

	invalidImport := authRequest(t, http.MethodPost, "/api/extensions/import", map[string]any{})
	if invalidImport.StatusCode != http.StatusBadRequest {
		var body any
		_ = json.NewDecoder(invalidImport.Body).Decode(&body)
		invalidImport.Body.Close()
		t.Fatalf("POST /api/extensions/import = %d, want 400: %+v", invalidImport.StatusCode, body)
	}
	var importError struct {
		Code string `json:"code"`
	}
	readJSON(t, invalidImport, &importError)
	if importError.Code != "EXTENSION_INVALID" {
		t.Fatalf("import error code = %q", importError.Code)
	}

	detail := authRequest(t, http.MethodGet, "/api/extensions/00000000-0000-0000-0000-000000000001", nil)
	if detail.StatusCode != http.StatusNotFound {
		var body any
		_ = json.NewDecoder(detail.Body).Decode(&body)
		detail.Body.Close()
		t.Fatalf("GET /api/extensions/{id} = %d, want 404: %+v", detail.StatusCode, body)
	}
	var detailError struct {
		Code string `json:"code"`
	}
	readJSON(t, detail, &detailError)
	if detailError.Code != "EXTENSION_NOT_FOUND" {
		t.Fatalf("detail error code = %q", detailError.Code)
	}
}

func TestPlatformExtensionRoutesRejectRequestsWithoutAuthentication(t *testing.T) {
	for _, path := range []string{"/api/extensions", "/api/extensions/00000000-0000-0000-0000-000000000001"} {
		response, err := http.Get(testServer.URL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusUnauthorized {
			t.Fatalf("GET %s = %d, want 401", path, response.StatusCode)
		}
	}
}
