package registryjobs

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFollowMarkedAcceptedResponsesToFinalResult(t *testing.T) {
	polls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/registry/v1/jobs/job-1" {
			t.Fatalf("poll path = %q", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer secret" || r.Header.Get("X-BQ-Label-App") != "cerebro" {
			t.Fatalf("identity/labels were not preserved: %v", r.Header)
		}
		polls++
		if polls == 1 {
			writeAccepted(w, "/api/registry/v1/jobs/job-1")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[{"value":1}]}`)
	}))
	defer server.Close()

	original, _ := http.NewRequest(http.MethodPost, server.URL+"/api/registry/v1/execute", strings.NewReader(`{}`))
	original.Header.Set("Authorization", "Bearer secret")
	original.Header.Set("X-BQ-Label-App", "cerebro")
	initial := &http.Response{
		StatusCode: http.StatusAccepted,
		Header: http.Header{
			MarkerHeader:  []string{MarkerValue},
			"Location":    []string{"/api/registry/v1/jobs/job-1"},
			"Retry-After": []string{"0"},
		},
		Body: io.NopCloser(strings.NewReader(`{"status":"running"}`)),
	}

	response, err := Follow(context.Background(), server.Client(), original, initial, server.URL+"/api/registry/v1")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	if response.StatusCode != http.StatusOK || !strings.Contains(string(body), `"value":1`) {
		t.Fatalf("status=%d body=%s", response.StatusCode, body)
	}
	if polls != 2 {
		t.Fatalf("polls=%d want 2", polls)
	}
}

func TestFollowRejectsCrossOriginAndOutOfBasePathLocations(t *testing.T) {
	evilCalls := 0
	evil := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { evilCalls++ }))
	defer evil.Close()
	registry := httptest.NewServer(http.NotFoundHandler())
	defer registry.Close()
	original, _ := http.NewRequest(http.MethodPost, registry.URL+"/api/registry/v1/execute", nil)
	original.Header.Set("Authorization", "Bearer secret")

	for _, location := range []string{evil.URL + "/steal", "/api/admin/secrets"} {
		initial := &http.Response{
			StatusCode: http.StatusAccepted,
			Header:     http.Header{MarkerHeader: []string{MarkerValue}, "Location": []string{location}},
			Body:       io.NopCloser(strings.NewReader(`{}`)),
		}
		if _, err := Follow(context.Background(), registry.Client(), original, initial, registry.URL+"/api/registry/v1"); err == nil {
			t.Fatalf("location %q should be rejected", location)
		}
	}
	if evilCalls != 0 {
		t.Fatalf("cross-origin server received %d credential-bearing requests", evilCalls)
	}
}

func TestFollowLeavesUnmarkedAcceptedResponseUntouched(t *testing.T) {
	original, _ := http.NewRequest(http.MethodPost, "https://registry.example/api/registry/v1/execute", nil)
	initial := &http.Response{
		StatusCode: http.StatusAccepted,
		Header:     http.Header{"Location": []string{"/api/registry/v1/jobs/job-1"}},
		Body:       io.NopCloser(strings.NewReader(`{"raw":true}`)),
	}
	response, err := Follow(context.Background(), http.DefaultClient, original, initial, "https://registry.example/api/registry/v1")
	if err != nil || response != initial {
		t.Fatalf("response=%p initial=%p err=%v", response, initial, err)
	}
}

func writeAccepted(w http.ResponseWriter, location string) {
	w.Header().Set(MarkerHeader, MarkerValue)
	w.Header().Set("Location", location)
	w.Header().Set("Retry-After", "0")
	w.WriteHeader(http.StatusAccepted)
	_, _ = io.WriteString(w, `{"status":"running"}`)
}
