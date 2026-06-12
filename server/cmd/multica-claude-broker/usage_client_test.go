package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func newUsageClientForTest(endpoint string) *UsageClient {
	return &UsageClient{
		Endpoint:   endpoint,
		BetaHeader: "oauth-2025-04-20",
		UserAgent:  "claude-code/2.1.173",
		HTTP:       &http.Client{Timeout: 5 * time.Second},
	}
}

const usageBody = `{
  "five_hour": {"utilization": 33.0, "resets_at": "2026-04-11T07:00:00.528743+00:00"},
  "seven_day": {"utilization": 13.0, "resets_at": "2026-04-17T00:59:59.951713+00:00"},
  "seven_day_opus": null,
  "seven_day_sonnet": {"utilization": 1.0, "resets_at": "2026-04-16T03:00:00.951719+00:00"},
  "extra_usage": {"is_enabled": false, "monthly_limit": null, "used_credits": null, "utilization": null}
}`

func TestUsageFetch_ParsesAndSetsHeaders(t *testing.T) {
	var gotAuth, gotBeta, gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotBeta = r.Header.Get("anthropic-beta")
		gotUA = r.Header.Get("User-Agent")
		_, _ = w.Write([]byte(usageBody))
	}))
	defer srv.Close()

	c := newUsageClientForTest(srv.URL)
	snap, err := c.Fetch(context.Background(), "ACCESS_TOKEN")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if gotAuth != "Bearer ACCESS_TOKEN" {
		t.Errorf("authorization = %q", gotAuth)
	}
	if gotBeta != "oauth-2025-04-20" {
		t.Errorf("anthropic-beta = %q", gotBeta)
	}
	// The claude-code/ User-Agent is load-bearing — without it the endpoint
	// 429s. Guard it so a refactor can't silently drop it.
	if !strings.HasPrefix(gotUA, "claude-code/") {
		t.Errorf("user-agent = %q, want claude-code/ prefix", gotUA)
	}
	if snap.FiveHour == nil || snap.FiveHour.Utilization != 33.0 {
		t.Errorf("five_hour = %+v", snap.FiveHour)
	}
	if snap.SevenDay == nil || snap.SevenDay.Utilization != 13.0 {
		t.Errorf("seven_day = %+v", snap.SevenDay)
	}
	if snap.SevenDaySonnet == nil || snap.SevenDaySonnet.Utilization != 1.0 {
		t.Errorf("seven_day_sonnet = %+v", snap.SevenDaySonnet)
	}
	if snap.SevenDayOpus != nil {
		t.Errorf("seven_day_opus should be nil, got %+v", snap.SevenDayOpus)
	}
	if snap.FiveHour.ResetsAt.IsZero() {
		t.Error("five_hour.resets_at not parsed")
	}
	if snap.FetchedAt.IsZero() {
		t.Error("fetched_at not stamped")
	}
}

func TestUsageFetch_RateLimited(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte("slow down"))
	}))
	defer srv.Close()

	_, err := newUsageClientForTest(srv.URL).Fetch(context.Background(), "TOK")
	if !errors.Is(err, ErrUsageRateLimited) {
		t.Fatalf("want ErrUsageRateLimited, got %v", err)
	}
}

func TestUsageFetch_EmptyTokenRejected(t *testing.T) {
	_, err := newUsageClientForTest("http://unused").Fetch(context.Background(), "")
	if err == nil {
		t.Fatal("want error for empty token, got nil")
	}
}

func TestUsageFetch_ServerErrorIsGeneric(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	_, err := newUsageClientForTest(srv.URL).Fetch(context.Background(), "TOK")
	if err == nil || errors.Is(err, ErrUsageRateLimited) {
		t.Fatalf("want generic error, got %v", err)
	}
}

func TestUsageHandler_StaleUntilFirstPoll(t *testing.T) {
	b := &Broker{}
	rec := httptest.NewRecorder()
	b.usageHandler(rec, httptest.NewRequest(http.MethodGet, "/usage", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("pre-poll status = %d, want 503", rec.Code)
	}

	b.setUsage(&UsageSnapshot{
		FiveHour:  &UsageWindow{Utilization: 50},
		SevenDay:  &UsageWindow{Utilization: 20},
		FetchedAt: time.Now().UTC(),
	})
	rec = httptest.NewRecorder()
	b.usageHandler(rec, httptest.NewRequest(http.MethodGet, "/usage", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("post-poll status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("content-type = %q", ct)
	}
	if !strings.Contains(rec.Body.String(), `"five_hour"`) {
		t.Errorf("body missing five_hour: %s", rec.Body.String())
	}
}

func TestUsageHandler_RejectsNonGet(t *testing.T) {
	b := &Broker{}
	rec := httptest.NewRecorder()
	b.usageHandler(rec, httptest.NewRequest(http.MethodPost, "/usage", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}
