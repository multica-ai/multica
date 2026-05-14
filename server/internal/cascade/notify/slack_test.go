package notify

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// captureSlack returns a test server that records the most recent
// payload and lets the test pick the response status.
func captureSlack(status int) (*httptest.Server, *string) {
	captured := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		captured = string(body)
		w.WriteHeader(status)
	}))
	return srv, &captured
}

func TestSlack_FormatsHeadlineAndBody(t *testing.T) {
	srv, captured := captureSlack(http.StatusOK)
	defer srv.Close()

	ch := NewSlackChannel(srv.URL, nil)
	err := ch.Send(context.Background(), Event{
		Type:         EventLoopGuardTripped,
		IssuePUL:     "PUL-102",
		IssueTitle:   "Cascade autonomy",
		PRNumber:     42,
		PRURL:        "https://github.com/x/y/pull/42",
		HumanContext: "3 fails on different sha in last 6h",
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	var p slackPayload
	if err := json.Unmarshal([]byte(*captured), &p); err != nil {
		t.Fatalf("unmarshal captured payload: %v", err)
	}
	if !strings.Contains(p.Text, "Loop guard tripped") {
		t.Errorf("headline missing verb: %q", p.Text)
	}
	if !strings.Contains(p.Text, "PUL-102") || !strings.Contains(p.Text, "#42") {
		t.Errorf("headline missing identifiers: %q", p.Text)
	}
	if len(p.Blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(p.Blocks))
	}
	if !strings.Contains(p.Blocks[0].Text.Text, "3 fails on different sha") {
		t.Errorf("block missing human context: %+v", p.Blocks[0].Text)
	}
	if !strings.Contains(p.Blocks[0].Text.Text, "View PR") {
		t.Errorf("block missing PR link: %+v", p.Blocks[0].Text)
	}
}

func TestSlack_5xxIsTransient(t *testing.T) {
	srv, _ := captureSlack(http.StatusInternalServerError)
	defer srv.Close()

	ch := NewSlackChannel(srv.URL, nil)
	err := ch.Send(context.Background(), Event{Type: EventStuck24h, IssueID: "i"})
	if err == nil {
		t.Fatal("expected error on 500")
	}
	if !errors.Is(err, ErrChannelTransient) {
		t.Errorf("expected ErrChannelTransient wrap, got %v", err)
	}
}

func TestSlack_4xxIsPermanent(t *testing.T) {
	srv, _ := captureSlack(http.StatusBadRequest)
	defer srv.Close()

	ch := NewSlackChannel(srv.URL, nil)
	err := ch.Send(context.Background(), Event{Type: EventStuck24h, IssueID: "i"})
	if err == nil {
		t.Fatal("expected error on 400")
	}
	if errors.Is(err, ErrChannelTransient) {
		t.Errorf("4xx should NOT be transient: %v", err)
	}
}

func TestSlack_HeadlineWithoutPRNumber(t *testing.T) {
	got := buildSlackHeadline(Event{Type: EventPlanCompleted, IssuePUL: "PUL-7", IssueTitle: "Demo"})
	if !strings.Contains(got, "PUL-7") {
		t.Errorf("missing PUL: %q", got)
	}
	if !strings.Contains(got, "Demo") {
		t.Errorf("missing title: %q", got)
	}
}

func TestSlack_HeadlineFallsBackWhenNoIdentifier(t *testing.T) {
	got := buildSlackHeadline(Event{Type: EventPlanAmended})
	if !strings.Contains(got, "Plan amended") {
		t.Errorf("expected verb-only fallback: %q", got)
	}
}
