package companybraincensus

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/cerebro/connections"
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/cerebro/toolpolicy"
	"github.com/multica-ai/multica/server/internal/middleware"
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
	report := Build(context.Background(), agents, nil, connectionsList, func(_ context.Context, conn connections.Connection) (json.RawMessage, error) {
		if conn.Name == "company-brain-commercial" {
			return whoami(t, `{"transport":"oauth","write_source":"commercial","allowed_read_sources":["shared","commercial"],"access_token":"`+secret+`"}`), nil
		}
		return whoami(t, `{"transport":"oauth","write_source":null,"allowed_read_sources":null}`), nil
	}, func(context.Context, db.Agent, connections.Connection) (policySnapshot, error) {
		return policySnapshot{Whoami: accessAllowed}, nil
	}, nil, time.Date(2026, 7, 28, 0, 0, 0, 0, time.UTC))

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

	report := Build(context.Background(), agents, nil, conns, func(context.Context, connections.Connection) (json.RawMessage, error) {
		return whoami(t, `{"transport":"oauth","write_source":"commercial","allowed_read_sources":["commercial"]}`), nil
	}, func(_ context.Context, agent db.Agent, _ connections.Connection) (policySnapshot, error) {
		switch agent.ID {
		case allowedID:
			return policySnapshot{Whoami: accessAllowed, Tools: []toolAccess{{Tool: "search", Decision: "allow"}}}, nil
		case askID:
			return policySnapshot{Whoami: accessApprovalRequired, Tools: []toolAccess{{Tool: "search", Decision: "ask"}}}, nil
		default:
			return policySnapshot{Whoami: accessDenied, Tools: []toolAccess{{Tool: "search", Decision: "deny"}}}, nil
		}
	}, nil, time.Date(2026, 7, 29, 0, 0, 0, 0, time.UTC))

	byName := map[string]actor{}
	for _, got := range report.Actors {
		byName[got.Name] = got
	}
	if got := byName["allowed"].Sources[0]; got.Status != "verified" || got.Claim == nil || got.Claim.WriteSource != "commercial" {
		t.Fatalf("allowed source = %#v, want verified claim", got)
	}
	if got := byName["allowed"].Sources[0].ToolAccess; len(got) != 1 || got[0].Tool != "search" || got[0].Decision != "allow" {
		t.Fatalf("allowed tool policy = %#v, want source-specific Allow evidence", got)
	}
	if got := byName["ask"].Sources[0]; got.Status != "unverifiable" || got.ErrorCode != "approval_required" || got.Claim != nil {
		t.Fatalf("ask source = %#v, want claim withheld pending approval", got)
	}
	if got := byName["denied"].Sources[0]; got.Status != "unverifiable" || got.ErrorCode != "access_denied" || got.Claim != nil {
		t.Fatalf("denied source = %#v, want claim withheld", got)
	}
}

func TestBuildIncludesActiveAutomaticRunsWithSystemScopedAccess(t *testing.T) {
	agentID := pgtype.UUID{Bytes: [16]byte{1}, Valid: true}
	automationID := pgtype.UUID{Bytes: [16]byte{2}, Valid: true}
	agents := []db.Agent{{
		ID: agentID, Name: "operator", Status: "online",
	}}
	automations := []db.ListAutopilotsRow{{
		Autopilot: db.Autopilot{
			ID: automationID, Title: "Daily commercial brief", Status: "active",
			AssigneeType: "agent", AssigneeID: agentID,
		},
		TriggerKinds: []string{"schedule"},
	}}
	conns := []connections.Connection{{
		ID: "one", Name: "company-brain-commercial", Type: connections.TypeMCPHTTP, Enabled: true,
	}}

	var gotAutomation db.Autopilot
	report := Build(context.Background(), agents, automations, conns, func(context.Context, connections.Connection) (json.RawMessage, error) {
		return whoami(t, `{"transport":"oauth","write_source":"commercial","allowed_read_sources":["commercial"]}`), nil
	}, func(context.Context, db.Agent, connections.Connection) (policySnapshot, error) {
		return policySnapshot{Whoami: accessAllowed}, nil
	}, func(_ context.Context, _ db.Agent, automation db.Autopilot, _ connections.Connection) (policySnapshot, error) {
		gotAutomation = automation
		return policySnapshot{Whoami: accessDenied, Tools: []toolAccess{{Tool: "add_fact", Decision: "deny"}}}, nil
	}, time.Date(2026, 7, 29, 0, 0, 0, 0, time.UTC))

	if gotAutomation.ID != automationID {
		t.Fatalf("automation access received %#v, want %s", gotAutomation, automationID)
	}
	if len(report.Automations) != 1 || report.Automations[0].Title != "Daily commercial brief" {
		t.Fatalf("automations = %#v, want active scheduled automation", report.Automations)
	}
	if got := report.Automations[0].Sources[0]; got.Status != statusUnverifiable || got.ErrorCode != errorAccessDenied || got.Claim != nil {
		t.Fatalf("automation source = %#v, want system-scoped denial", got)
	}
	if got := report.Automations[0].Sources[0].ToolAccess; len(got) != 1 || got[0].Decision != "deny" {
		t.Fatalf("automation tool policy = %#v, want system-scoped Deny evidence", got)
	}
}

func TestBuildMarksUnsupportedAutomaticRunIdentityUnverifiable(t *testing.T) {
	automations := []db.ListAutopilotsRow{{
		Autopilot: db.Autopilot{
			ID: pgtype.UUID{Bytes: [16]byte{2}, Valid: true}, Title: "Squad workflow",
			Status: "active", AssigneeType: "squad",
		},
	}}
	conns := []connections.Connection{{
		ID: "one", Name: "company-brain", Type: connections.TypeMCPHTTP, Enabled: true,
	}}

	report := Build(context.Background(), nil, automations, conns, func(context.Context, connections.Connection) (json.RawMessage, error) {
		return whoami(t, `{"transport":"oauth","write_source":"shared","allowed_read_sources":["shared"]}`), nil
	}, nil, nil, time.Now())

	if got := report.Automations[0].Sources[0]; got.ErrorCode != errorAutomationIdentityUnavailable || got.Claim != nil {
		t.Fatalf("unsupported automation source = %#v, want identity unavailable", got)
	}
}

func TestBuildReturnsOnlySafeLegacyReferenceTokens(t *testing.T) {
	secret := "gbrain_at_must-not-leak"
	agents := []db.Agent{{
		ID:           pgtype.UUID{Bytes: [16]byte{1}, Valid: true},
		Name:         "referencing agent",
		Status:       "online",
		Instructions: "Use mcp__company-brain-commercial__search_knowledge with token " + secret,
		McpConfig:    []byte(`{"server":"company-brain-internal","authorization":"` + secret + `"}`),
		CustomEnv:    []byte(`{"TOKEN":"mcp__company-brain-commercial__secret_value_not_a_tool"}`),
	}}
	automations := []db.ListAutopilotsRow{{
		Autopilot: db.Autopilot{
			ID:                 pgtype.UUID{Bytes: [16]byte{2}, Valid: true},
			Title:              "Brief",
			Status:             "active",
			Description:        pgtype.Text{String: "Call mcp__company-brain-shared__search", Valid: true},
			IssueTitleTemplate: pgtype.Text{String: secret, Valid: true},
		},
	}}

	conns := []connections.Connection{
		{ID: "one", Name: "company-brain-commercial", Type: connections.TypeMCPHTTP, Enabled: true, Tools: []connections.Tool{{Name: "search_knowledge"}}},
		{ID: "two", Name: "company-brain-internal", Type: connections.TypeMCPHTTP, Enabled: true},
		{ID: "three", Name: "company-brain-shared", Type: connections.TypeMCPHTTP, Enabled: true, Tools: []connections.Tool{{Name: "search"}}},
	}
	report := Build(context.Background(), agents, automations, conns, func(context.Context, connections.Connection) (json.RawMessage, error) {
		return whoami(t, `{"transport":"oauth","write_source":"shared","allowed_read_sources":["shared"]}`), nil
	}, nil, nil, time.Now())

	if len(report.References) != 3 {
		t.Fatalf("references = %#v, want three extracted legacy names", report.References)
	}
	raw, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if contains(string(raw), secret) || contains(string(raw), "secret_value_not_a_tool") {
		t.Fatalf("reference scan leaked surrounding secret: %s", raw)
	}
}

type fakeConnectionPolicy struct {
	verdicts []toolpolicy.ConnectionToolVerdict
	query    toolpolicy.TableQuery
}

type fakeFeatureFlags struct {
	rows []cerebrodb.ListCerebroWorkspaceFeatureFlagsRow
	err  error
}

type fakeConnectionReader struct {
	connections []connections.Connection
}

func (f fakeConnectionReader) List(context.Context, pgtype.UUID) ([]connections.Connection, error) {
	return f.connections, nil
}

func (f fakeFeatureFlags) ListCerebroWorkspaceFeatureFlags(context.Context, pgtype.UUID) ([]cerebrodb.ListCerebroWorkspaceFeatureFlagsRow, error) {
	return f.rows, f.err
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

func TestAutomationPolicyUsesSystemIdentityWithoutDelegatedHuman(t *testing.T) {
	policy := &fakeConnectionPolicy{verdicts: []toolpolicy.ConnectionToolVerdict{{
		Connection: "company-brain", Tool: claimTool, Setting: toolpolicy.SettingAllow,
	}}}
	h := &Handler{policy: policy}
	agent := db.Agent{
		ID:          pgtype.UUID{Bytes: [16]byte{1}, Valid: true},
		WorkspaceID: pgtype.UUID{Bytes: [16]byte{2}, Valid: true},
		RuntimeID:   pgtype.UUID{Bytes: [16]byte{3}, Valid: true},
		OwnerID:     pgtype.UUID{Bytes: [16]byte{4}, Valid: true},
	}
	automation := db.Autopilot{
		ID:          pgtype.UUID{Bytes: [16]byte{5}, Valid: true},
		OwnerUserID: pgtype.UUID{Bytes: [16]byte{6}, Valid: true},
	}

	if _, err := h.automationPolicy(context.Background(), agent, automation, connections.Connection{Name: "company-brain"}); err != nil {
		t.Fatal(err)
	}
	if !policy.query.IsSystem || policy.query.SystemID != automation.ID || policy.query.OnBehalfOfID.Valid {
		t.Fatalf("policy query = %#v, want system identity without a delegated human", policy.query)
	}
	if policy.query.AgentID != agent.ID || policy.query.RuntimeID != agent.RuntimeID || policy.query.UserID != agent.OwnerID {
		t.Fatalf("policy query = %#v, want assigned agent identity", policy.query)
	}
}

func TestCallerAccessUsesMemberIdentityAndFailsClosed(t *testing.T) {
	policy := &fakeConnectionPolicy{verdicts: []toolpolicy.ConnectionToolVerdict{{
		Connection: "company-brain", Tool: claimTool, Setting: toolpolicy.SettingAsk,
	}}}
	h := &Handler{policy: policy}
	member := db.Member{
		WorkspaceID: pgtype.UUID{Bytes: [16]byte{1}, Valid: true},
		UserID:      pgtype.UUID{Bytes: [16]byte{2}, Valid: true},
	}

	got, err := h.callerAccess(context.Background(), member, connections.Connection{Name: "company-brain"})
	if err != nil {
		t.Fatal(err)
	}
	if got != accessApprovalRequired {
		t.Fatalf("caller access = %v, want approval required", got)
	}
	if policy.query.WorkspaceID != member.WorkspaceID || policy.query.UserID != member.UserID {
		t.Fatalf("policy query = %#v, want member identity", policy.query)
	}
	if policy.query.AgentID.Valid || policy.query.RuntimeID.Valid || policy.query.IsSystem {
		t.Fatalf("policy query = %#v, caller must not inherit an agent/system identity", policy.query)
	}
}

func TestMigrationCensusFeatureFlagDefaultsOffAndRequiresExplicitEnable(t *testing.T) {
	workspaceID := pgtype.UUID{Bytes: [16]byte{1}, Valid: true}
	if (&Handler{flags: fakeFeatureFlags{}}).enabled(context.Background(), workspaceID) {
		t.Fatal("migration census enabled without an explicit workspace flag")
	}
	h := &Handler{flags: fakeFeatureFlags{rows: []cerebrodb.ListCerebroWorkspaceFeatureFlagsRow{{
		FlagKey: featureFlag, Enabled: true,
	}}}}
	if !h.enabled(context.Background(), workspaceID) {
		t.Fatal("migration census stayed disabled after explicit workspace enable")
	}
}

func TestGetNeverInvokesStoredCredentialWhenCallerCannotUseWhoami(t *testing.T) {
	workspaceID := pgtype.UUID{Bytes: [16]byte{1}, Valid: true}
	member := db.Member{
		WorkspaceID: workspaceID,
		UserID:      pgtype.UUID{Bytes: [16]byte{2}, Valid: true},
	}
	policy := &fakeConnectionPolicy{verdicts: []toolpolicy.ConnectionToolVerdict{{
		Connection: "company-brain-commercial", Tool: claimTool, Setting: toolpolicy.SettingDeny,
	}}}
	upstreamCalls := 0
	h := &Handler{
		listAgents: func(context.Context, pgtype.UUID) ([]db.Agent, error) {
			return nil, nil
		},
		listAutopilots: func(context.Context, pgtype.UUID, db.Member) ([]db.ListAutopilotsRow, error) {
			return nil, nil
		},
		connections: fakeConnectionReader{connections: []connections.Connection{{
			ID: "c1", Name: "company-brain-commercial", Type: connections.TypeMCPHTTP, Enabled: true,
		}}},
		flags: fakeFeatureFlags{rows: []cerebrodb.ListCerebroWorkspaceFeatureFlagsRow{{
			FlagKey: featureFlag, Enabled: true,
		}}},
		policy: policy,
		now:    time.Now,
		call: func(context.Context, connections.Connection) (json.RawMessage, error) {
			upstreamCalls++
			return whoami(t, `{"transport":"oauth","write_source":"commercial","allowed_read_sources":["commercial"]}`), nil
		},
	}
	route := chi.NewRouteContext()
	route.URLParams.Add("id", workspaceID.String())
	ctx := context.WithValue(context.Background(), chi.RouteCtxKey, route)
	ctx = middleware.SetMemberContext(ctx, workspaceID.String(), member)
	req := httptest.NewRequest(http.MethodGet, "/api/workspaces/"+workspaceID.String()+"/connections/company-brain-migration-census", nil).WithContext(ctx)
	rec := httptest.NewRecorder()

	h.Get(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if upstreamCalls != 0 {
		t.Fatalf("stored credential invoked %d times for denied caller", upstreamCalls)
	}
	var report Report
	if err := json.Unmarshal(rec.Body.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if got := report.Connections[0]; got.ErrorCode != errorCallerAccessDenied || got.Claim != nil {
		t.Fatalf("connection evidence = %#v, want caller_access_denied", got)
	}
}

func TestParseClaimMakesWriteSourceReadableWhenReadListIsEmpty(t *testing.T) {
	got, err := parseClaim(whoami(t, `{"transport":"legacy","write_source":"operations","allowed_read_sources":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.AllowedReadSources) != 1 || got.AllowedReadSources[0] != "operations" {
		t.Fatalf("allowed read sources = %#v, want implicit write source", got.AllowedReadSources)
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
