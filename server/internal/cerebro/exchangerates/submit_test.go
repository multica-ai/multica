package exchangerates

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSubmit_Success(t *testing.T) {
	var gotAuth, gotContentType string
	var gotBody IngestRequest

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotContentType = r.Header.Get("Content-Type")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ingestResponse{Written: len(gotBody.Rates)})
	}))
	defer srv.Close()

	snap := Snapshot{Date: "2026-06-04", Rates: map[string]float64{"DKK": 6.85, "EUR": 0.92}}
	written, err := Submit(context.Background(), srv.Client(), srv.URL, "secret-key", "USD", snap)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if written != 2 {
		t.Fatalf("written: want 2, got %d", written)
	}
	if gotAuth != "Bearer secret-key" {
		t.Fatalf("authorization header: want %q, got %q", "Bearer secret-key", gotAuth)
	}
	if gotContentType != "application/json" {
		t.Fatalf("content-type: want application/json, got %q", gotContentType)
	}
	if gotBody.Base != "USD" || gotBody.Date != "2026-06-04" || gotBody.Rates["DKK"] != 6.85 {
		t.Fatalf("body round-trip mismatch: %+v", gotBody)
	}
}

func TestSubmit_NonOKStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer srv.Close()

	snap := Snapshot{Date: "2026-06-04", Rates: map[string]float64{"DKK": 6.85}}
	if _, err := Submit(context.Background(), srv.Client(), srv.URL, "wrong", "USD", snap); err == nil {
		t.Fatal("want error on 401, got nil")
	}
}
