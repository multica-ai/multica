package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

func TestEvaluateCandidateRunStatuses(t *testing.T) {
	now := time.Date(2026, 8, 31, 11, 6, 0, 0, time.UTC)
	base := autopilotAnomalyCandidate{
		ID:             "ap-1",
		WorkspaceID:    "ws-1",
		Title:          "Nightly sync",
		CreatedByType:  "member",
		CreatedByID:    "member-1",
		LastReason:     "dispatch filtered",
		LastReasonCode: "invocation_not_allowed",
		NextRunAt:      pgtype.Timestamptz{Time: time.Date(2026, 8, 31, 10, 0, 0, 0, time.UTC), Valid: true},
		CronExpression: pgtype.Text{String: "0 * * * *", Valid: true},
		Timezone:       pgtype.Text{String: "UTC", Valid: true},
	}

	tests := []struct {
		name      string
		status    string
		wantKind  string
		wantTitle string
		wantBody  string
		wantNil   bool
	}{
		{
			name:      "skipped",
			status:    "skipped",
			wantKind:  "skipped",
			wantTitle: "Autopilot alert: Nightly sync",
			wantBody:  "The latest run for this active autopilot is skipped",
		},
		{
			name:      "failed",
			status:    "failed",
			wantKind:  "failed",
			wantTitle: "Autopilot alert: Nightly sync",
			wantBody:  "The latest run for this active autopilot is failed",
		},
		{
			name:    "running",
			status:  "running",
			wantNil: true,
		},
		{
			name:      "unknown",
			status:    "mystery_status",
			wantKind:  "unknown_status",
			wantTitle: "Autopilot alert: Nightly sync",
			wantBody:  "The latest run for this active autopilot is unknown_status",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			candidate := base
			candidate.LastRunStatus = tt.status

			alert, err := evaluateCandidate(candidate, now)
			if err != nil {
				t.Fatalf("evaluateCandidate: %v", err)
			}
			if tt.wantNil {
				if alert != nil {
					t.Fatalf("expected nil alert, got %#v", alert)
				}
				return
			}
			if alert == nil {
				t.Fatal("expected alert, got nil")
			}
			if alert.Kind != tt.wantKind {
				t.Fatalf("kind = %q, want %q", alert.Kind, tt.wantKind)
			}
			if alert.Title != tt.wantTitle {
				t.Fatalf("title = %q, want %q", alert.Title, tt.wantTitle)
			}
			if got := alert.Body; got == "" || !strings.Contains(got, tt.wantBody) {
				t.Fatalf("body = %q, want substring %q", got, tt.wantBody)
			}
			var details map[string]any
			if err := json.Unmarshal(alert.Details, &details); err != nil {
				t.Fatalf("unmarshal details: %v", err)
			}
			if got := details["autopilot_id"]; got != base.ID {
				t.Fatalf("autopilot_id = %v, want %q", got, base.ID)
			}
			if got := details["anomaly_kind"]; got != tt.wantKind {
				t.Fatalf("anomaly_kind = %v, want %q", got, tt.wantKind)
			}
		})
	}
}

func TestEvaluateCandidateStale(t *testing.T) {
	candidate := autopilotAnomalyCandidate{
		ID:             "ap-1",
		WorkspaceID:    "ws-1",
		Title:          "Nightly sync",
		NextRunAt:      pgtype.Timestamptz{Time: time.Date(2026, 8, 31, 10, 0, 0, 0, time.UTC), Valid: true},
		CronExpression: pgtype.Text{String: "0 * * * *", Valid: true},
		Timezone:       pgtype.Text{String: "UTC", Valid: true},
	}

	alert, err := evaluateCandidate(candidate, time.Date(2026, 8, 31, 11, 6, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("evaluateCandidate: %v", err)
	}
	if alert == nil {
		t.Fatal("expected stale alert")
	}
	if alert.Kind != "stale" {
		t.Fatalf("kind = %q, want stale", alert.Kind)
	}

	early, err := evaluateCandidate(candidate, time.Date(2026, 8, 31, 11, 4, 59, 0, time.UTC))
	if err != nil {
		t.Fatalf("evaluateCandidate early: %v", err)
	}
	if early != nil {
		t.Fatalf("expected nil alert before stale threshold, got %#v", early)
	}
}

func TestDecodeAlertAndKey(t *testing.T) {
	row := inboxAlertRow{
		ID:            "item-1",
		WorkspaceID:   "ws-1",
		RecipientType: "member",
		RecipientID:   "member-1",
		Details:       []byte(`{"autopilot_id":"ap-1","anomaly_kind":"skipped"}`),
	}

	alert, ok := decodeAlert(row)
	if !ok {
		t.Fatal("expected alert to decode")
	}
	if alert.AutopilotID != "ap-1" || alert.AnomalyKind != "skipped" {
		t.Fatalf("unexpected alert: %#v", alert)
	}
	if got := alertKey("ws-1", "member", "member-1", "ap-1", "skipped"); got != "ws-1:member:member-1:ap-1:skipped" {
		t.Fatalf("unexpected alert key: %q", got)
	}
}
