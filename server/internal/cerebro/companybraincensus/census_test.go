package companybraincensus

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/cerebro/connections"
	"github.com/multica-ai/multica/server/internal/cerebro/toolpolicy"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func whoami(t *testing.T, body string) json.RawMessage {
	t.Helper()
	return json.RawMessage(`{"content":[{"type":"text","text":` + string(mustJSON(t, body)) + `}]}`)
}

func mustJSON(t *testing.T, value string) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestBuildRedactsRawIdentityAndMapsOnlyVerifiedClaims(t *testing.T) {
	secret := "gbrain_at_not-for-report"
	agents := []db.Agent{
		{ID: pgtype.UUID{Valid: true}, Name: "active", Status: "online"},
		{ID: pgtype.UUID{Valid: true}, Name: "offline", Status: "offline"},
	}
	connectionsList := []connections.Connection{
		{ID: "one", Name: "company-brain-commercial", Type: connections.TypeMCPHTTP, Enabled: true},
		{ID: "two", Name: "company-brain-sandbox", Type: connections.TypeMCPHTTP, Enabled: true},
		{ID: "three", Name: "another-mcp", Type: connections.TypeMCPHTTP, Enabled: true},
	}
	report := Build(context.Background(), agents, connectionsList, func(_ context.Context, conn connections.Connection) (json.RawMessage, error) {
		if conn.Name == "company-brain-commercial" {
			return whoami(t, `{"transport":"oauth","write_source":"commercial","allowed_read_sources":["shared","commercial"],"access_token":"`+secret+`"}`), nil
		}
		return whoami(t, `{"transport":"oauth","write_source":null,"allowed_read_sources":null}`), nil
	}, func(context.Context, db.Agent, connections.Connection) (actorAccess, error) {
		return accessAllowed, nil
	}, time.Date(2026, 7, 28, 0, 0, 0, 0, time.UTC))

	if len(report.Actors) != 1 || report.Actors[0].Name != "active" {
		t.Fatalf("actors = %#v, want active non-offline actor only", report.Actors)
	}
	if got := report.Actors[0].Sources[0]; got.Status != "verified" || got.Claim == nil || got.Claim.WriteSource != "commercial" {
		t.Fatalf("first source = %#v, want verified commercial claim", got)
	}
	if got := report.Actors[0].Sources[1]; got.Status != "unverifiable" || got.ErrorCode != "invalid_identity_claim" || got.Claim != nil {
		t.Fatalf("second source = %#v, want fail-closed invalid identity claim", got)
	}
	raw, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) == "" || contains(string(raw), secret) || contains(string(raw), "access_token") {
		t.Fatalf("report leaked raw identity data: %s", raw)
	}
}

func TestBuildOnlyMapsClaimsToActorsAllowedToCallWhoami(t *testing.T) {
	allowedID := pgtype.UUID{Bytes: [16]byte{1}, Valid: true}
	askID := pgtype.UUID{Bytes: [16]byte{2}, Valid: true}
	deniedID := pgtype.UUID{Bytes: [16]byte{3}, Valid: true}
	agents := []db.Agent{
		{ID: allowedID, Name: "allowed", Status: "online"},
		{ID: askID, Name: "ask", Status: "online"},
		{ID: deniedID, Name: "denied", Status: "online"},
	}
	conns := []connections.Connection{{ID: "one", Name: "company-brain", Type: connections.TypeMCPHTTP, Enabled: true}}

	report := Build(context.Background(), agents, conns, func(context.Context, connections.Connection) (json.RawMessage, error) {
		return whoami(t, `{"transport":"oauth","write_source":"commercial","allowed_read_sources":["commercial"]}`), nil
	}, func(_ context.Context, agent db.Agent, _ connections.Connection) (actorAccess, error) {
		switch agent.ID {
		case allowedID:
			return accessAllowed, nil
		case askID:
			return accessApprovalRequired, nil
		default:
			return accessDenied, nil
		}
	}, time.Date(2026, 7, 29, 0, 0, 0, 0, time.UTC))

	byName := map[string]actor{}
	for _, got := range report.Actors {
		byName[got.Name] = got
	}
	if got := byName["allowed"].Sources[0]; got.Status != "verified" || got.Claim == nil || got.Claim.WriteSource != "commercial" {
		t.Fatalf("allowed source = %#v, want verified claim", got)
	}
	if got := byName["ask"].Sources[0]; got.Status != "unverifiable" || got.ErrorCode != "approval_required" || got.Claim != nil {
		t.Fatalf("ask source = %#v, want claim withheld pending approval", got)
	}
	if got := byName["denied"].Sources[0]; got.Status != "unverifiable" || got.ErrorCode != "access_denied" || got.Claim != nil {
		t.Fatalf("denied source = %#v, want claim withheld", got)
	}
}

type fakeConnectionPolicy struct {
	verdicts []toolpolicy.ConnectionToolVerdict
	query    toolpolicy.TableQuery
}

func (p *fakeConnectionPolicy) ConnectionToolVerdicts(_ context.Context, query toolpolicy.TableQuery) ([]toolpolicy.ConnectionToolVerdict, error) {
	p.query = query
	return p.verdicts, nil
}

func TestActorAccessUsesTheMatchingConnectionVerdict(t *testing.T) {
	policy := &fakeConnectionPolicy{verdicts: []toolpolicy.ConnectionToolVerdict{
		{Connection: "another-mcp", Tool: claimTool, Setting: toolpolicy.SettingAllow},
		{Connection: "company-brain", Tool: claimTool, Setting: toolpolicy.SettingDeny},
	}}
	h := &Handler{policy: policy}
	agent := db.Agent{
		ID:          pgtype.UUID{Bytes: [16]byte{1}, Valid: true},
		WorkspaceID: pgtype.UUID{Bytes: [16]byte{2}, Valid: true},
		RuntimeID:   pgtype.UUID{Bytes: [16]byte{3}, Valid: true},
		OwnerID:     pgtype.UUID{Bytes: [16]byte{4}, Valid: true},
	}

	got, err := h.actorAccess(context.Background(), agent, connections.Connection{Name: "company-brain"})
	if err != nil {
		t.Fatal(err)
	}
	if got != accessDenied {
		t.Fatalf("actor access = %v, want denied", got)
	}
	if policy.query.WorkspaceID != agent.WorkspaceID || policy.query.RuntimeID != agent.RuntimeID || policy.query.AgentID != agent.ID || policy.query.UserID != agent.OwnerID {
		t.Fatalf("policy query = %#v, want actor identity", policy.query)
	}
}

func TestActorAccessFailsClosedWhenNoVerdictExists(t *testing.T) {
	policy := &fakeConnectionPolicy{verdicts: []toolpolicy.ConnectionToolVerdict{
		{Connection: "another-mcp", Tool: claimTool, Setting: toolpolicy.SettingAllow},
	}}
	h := &Handler{policy: policy}

	got, err := h.actorAccess(context.Background(), db.Agent{}, connections.Connection{Name: "company-brain-internal"})
	if err == nil {
		t.Fatal("actorAccess accepted a connection with no resolved verdict")
	}
	if got != accessDenied {
		t.Fatalf("actor access = %v, want denied", got)
	}
}

func TestParseClaimRejectsMissingOrMalformedClaims(t *testing.T) {
	for _, raw := range []json.RawMessage{
		whoami(t, `{"transport":"oauth","write_source":null,"allowed_read_sources":null}`),
		whoami(t, `{"transport":"oauth","write_source":"Commercial","allowed_read_sources":["commercial"]}`),
		whoami(t, `{"transport":"local","write_source":"commercial","allowed_read_sources":["commercial"]}`),
	} {
		if _, err := parseClaim(raw); err == nil {
			t.Fatalf("parseClaim(%s) accepted an unsafe identity", raw)
		}
	}
}

func contains(value, needle string) bool {
	for i := 0; i+len(needle) <= len(value); i++ {
		if value[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
