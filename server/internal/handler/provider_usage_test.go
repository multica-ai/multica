package handler

import (
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestRejectCredentialFields(t *testing.T) {
	ok := []byte(`{"provider":"claude","collected_at":"2026-09-22T00:00:00Z","windows":[{"id":"session","percent_used":12}]}`)
	if err := rejectCredentialFields(ok); err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{
		`{"provider":"cursor","access_token":"secret"}`,
		`{"cookie":"WorkosCursorSessionToken=abc::def"}`,
		`{"provider":"codex","windows":[{"id":"primary","percent_used":1,"note":"Bearer abc"}]}`,
		`{"plan_name":"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxIn0.signature-padding"}`,
	} {
		if err := rejectCredentialFields([]byte(body)); err == nil {
			t.Fatalf("accepted credential body %s", body)
		}
	}
}

func TestNormalizeProviderUsageReport(t *testing.T) {
	collected := time.Date(2026, 9, 22, 1, 2, 3, 0, time.UTC)
	reset := collected.Add(time.Hour)
	got, err := normalizeProviderUsageReport(providerUsageReport{
		Provider:    "Claude",
		PlanName:    "Max",
		CollectedAt: collected,
		Windows: []providerUsageWindowReport{
			{ID: "session", PercentUsed: 38, ResetsAt: &reset},
			{ID: "weekly_all", PercentUsed: 4},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Provider != "claude" || got.PlanName != "Max" || len(got.Windows) != 2 || got.ReasonCode != "" {
		t.Fatalf("normalized = %+v", got)
	}

	empty, err := normalizeProviderUsageReport(providerUsageReport{
		Provider:    "codex",
		CollectedAt: collected,
		ReasonCode:  "api_key_only",
	})
	if err != nil || empty.ReasonCode != "api_key_only" || len(empty.Windows) != 0 {
		t.Fatalf("empty = %+v err=%v", empty, err)
	}

	if _, err := normalizeProviderUsageReport(providerUsageReport{Provider: "claude"}); err == nil {
		t.Fatal("missing collected_at accepted")
	}
	if _, err := normalizeProviderUsageReport(providerUsageReport{
		Provider:    "nope",
		CollectedAt: collected,
	}); err == nil {
		t.Fatal("unknown provider accepted")
	}
	if _, err := normalizeProviderUsageReport(providerUsageReport{
		Provider:    "qwen",
		CollectedAt: collected,
		Windows:     []providerUsageWindowReport{{ID: "week", PercentUsed: 1}},
	}); err == nil {
		t.Fatal("qwen accepted without a local plan collector")
	}
	accepted, err := normalizeProviderUsageReport(providerUsageReport{
		Provider:    "Kimi",
		CollectedAt: collected,
		Windows:     []providerUsageWindowReport{{ID: "rolling", PercentUsed: 8}},
	})
	if err != nil || accepted.Provider != "kimi" || accepted.Windows[0].ID != "rolling" {
		t.Fatalf("kimi = %+v err=%v", accepted, err)
	}
	if _, err := normalizeProviderUsageReport(providerUsageReport{
		Provider:    "cursor",
		CollectedAt: collected,
		ReasonCode:  "drop table",
	}); err == nil {
		t.Fatal("unknown reason accepted")
	}
	if _, err := normalizeProviderUsageReport(providerUsageReport{
		Provider:    "cursor",
		CollectedAt: collected,
		Windows:     []providerUsageWindowReport{{ID: "Auto", PercentUsed: 1}},
	}); err == nil {
		t.Fatal("non-lowercase window id accepted")
	}
}

func TestProviderUsageBatchGroupsByRuntime(t *testing.T) {
	claudeID := parseUUID("11111111-1111-1111-1111-111111111111")
	codexID := parseUUID("22222222-2222-2222-2222-222222222222")
	reset := time.Date(2026, 9, 22, 15, 0, 0, 0, time.UTC)
	rows := []db.ListRuntimeProviderUsageByRuntimeIDsRow{
		{
			RuntimeID:   claudeID,
			Provider:    "claude",
			WindowID:    "session",
			PercentUsed: pgtype.Float8{Float64: 38.2, Valid: true},
			ResetsAt:    pgtype.Timestamptz{Time: reset, Valid: true},
			PlanName:    pgtype.Text{String: "Max", Valid: true},
			CollectedAt: pgtype.Timestamptz{Time: reset.Add(-time.Hour), Valid: true},
		},
		{
			RuntimeID:   claudeID,
			Provider:    "cursor",
			WindowID:    "auto",
			PercentUsed: pgtype.Float8{Float64: 10, Valid: true},
			CollectedAt: pgtype.Timestamptz{Time: reset, Valid: true},
		},
		{
			RuntimeID:   codexID,
			Provider:    "codex",
			WindowID:    "",
			ReasonCode:  pgtype.Text{String: "not_logged_in", Valid: true},
			CollectedAt: pgtype.Timestamptz{Time: reset, Valid: true},
		},
	}
	got := providerUsageBatchFromRows([]pgtype.UUID{codexID, claudeID}, rows)
	if len(got.Runtimes) != 2 {
		t.Fatalf("runtimes = %+v", got.Runtimes)
	}
	if got.Runtimes[0].RuntimeID != uuidToString(codexID) || got.Runtimes[0].Providers[0].ReasonCode != "not_logged_in" {
		t.Fatalf("codex group = %+v", got.Runtimes[0])
	}
	if len(got.Runtimes[0].Providers[0].Windows) != 0 {
		t.Fatal("empty login snapshot included a window")
	}
	claude := got.Runtimes[1]
	if claude.RuntimeID != uuidToString(claudeID) || len(claude.Providers) != 2 {
		t.Fatalf("claude group = %+v", claude)
	}
	if claude.Providers[0].PlanName != "Max" || claude.Providers[0].Windows[0].PercentUsed != 38.2 {
		t.Fatalf("session window = %+v", claude.Providers[0])
	}
	if claude.Providers[0].Windows[0].ResetsAt == nil || *claude.Providers[0].Windows[0].ResetsAt != reset.Format(time.RFC3339) {
		t.Fatalf("reset = %+v", claude.Providers[0].Windows[0].ResetsAt)
	}
}

func TestProviderUsageReadableIDsDropsPrivateAndForeign(t *testing.T) {
	ws := parseUUID("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	otherWS := parseUUID("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")
	owner := parseUUID("cccccccc-cccc-cccc-cccc-cccccccccccc")
	viewer := parseUUID("dddddddd-dddd-dddd-dddd-dddddddddddd")
	publicID := parseUUID("11111111-1111-1111-1111-111111111111")
	privateID := parseUUID("22222222-2222-2222-2222-222222222222")
	foreignID := parseUUID("33333333-3333-3333-3333-333333333333")
	member := db.Member{UserID: viewer, WorkspaceID: ws}
	found := map[string]db.AgentRuntime{
		uuidToString(publicID): {
			ID:          publicID,
			WorkspaceID: ws,
			OwnerID:     owner,
			Visibility:  "public",
		},
		uuidToString(privateID): {
			ID:          privateID,
			WorkspaceID: ws,
			OwnerID:     owner,
			Visibility:  "private",
		},
		uuidToString(foreignID): {
			ID:          foreignID,
			WorkspaceID: otherWS,
			OwnerID:     viewer,
			Visibility:  "public",
		},
	}
	got := providerUsageReadableIDs(uuidToString(ws), member, []pgtype.UUID{publicID, privateID, foreignID, publicID}, found)
	if len(got) != 1 || got[0] != publicID {
		t.Fatalf("readable = %+v", got)
	}
}

func TestProviderUsageErrorDoesNotEchoSecrets(t *testing.T) {
	err := rejectCredentialFields([]byte(`{"access_token":"super-secret-token"}`))
	if err == nil {
		t.Fatal("expected rejection")
	}
	if strings.Contains(err.Error(), "super-secret-token") {
		t.Fatal("error echoed the token")
	}
}
