package permissions

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
)

type fakeStore struct {
	grants []cerebrodb.CerebroWorkspaceGrant
	audits []cerebrodb.InsertCerebroPermissionAuditEventParams
	err    error
}

func (f *fakeStore) ListCerebroWorkspaceGrants(context.Context, cerebrodb.ListCerebroWorkspaceGrantsParams) ([]cerebrodb.CerebroWorkspaceGrant, error) {
	return f.grants, nil
}

func (f *fakeStore) InsertCerebroPermissionAuditEvent(_ context.Context, arg cerebrodb.InsertCerebroPermissionAuditEventParams) error {
	if f.err != nil {
		return f.err
	}
	f.audits = append(f.audits, arg)
	return nil
}

func uuid(b byte) pgtype.UUID {
	var u pgtype.UUID
	u.Valid = true
	u.Bytes[0] = b
	return u
}

func grant(subjectType string, subjectID pgtype.UUID, capability, resource string, opts ...func(*cerebrodb.CerebroWorkspaceGrant)) cerebrodb.CerebroWorkspaceGrant {
	g := cerebrodb.CerebroWorkspaceGrant{
		ID:              uuid(99),
		SubjectType:     subjectType,
		SubjectID:       subjectID,
		Capability:      capability,
		ResourcePattern: resource,
		Status:          grantStatusActive,
	}
	for _, o := range opts {
		o(&g)
	}
	return g
}

func withID(id pgtype.UUID) func(*cerebrodb.CerebroWorkspaceGrant) {
	return func(g *cerebrodb.CerebroWorkspaceGrant) { g.ID = id }
}

func withApproval() func(*cerebrodb.CerebroWorkspaceGrant) {
	return func(g *cerebrodb.CerebroWorkspaceGrant) { g.ApprovalRequired = true }
}

func withStatus(s string) func(*cerebrodb.CerebroWorkspaceGrant) {
	return func(g *cerebrodb.CerebroWorkspaceGrant) { g.Status = s }
}

func withWindow(start, end time.Time) func(*cerebrodb.CerebroWorkspaceGrant) {
	return func(g *cerebrodb.CerebroWorkspaceGrant) {
		if !start.IsZero() {
			g.TimeWindowStart = pgtype.Timestamptz{Time: start, Valid: true}
		}
		if !end.IsZero() {
			g.TimeWindowEnd = pgtype.Timestamptz{Time: end, Valid: true}
		}
	}
}

func TestDecide_DeniesWithoutGrants(t *testing.T) {
	d := Decide(nil, Request{
		Actor:      Actor{Type: subjectMember, ID: uuid(1)},
		Capability: "issue.read",
	})
	if d.Kind != DecisionDeny {
		t.Fatalf("expected deny, got %v (%s)", d.Kind, d.Reason)
	}
	if d.Allowed() {
		t.Fatal("Allowed() must be false on deny")
	}
}

func TestDecide_RequiresCapability(t *testing.T) {
	d := Decide([]cerebrodb.CerebroWorkspaceGrant{grant(subjectWorkspaceDefault, pgtype.UUID{}, "*", "*")}, Request{
		Actor: Actor{Type: subjectMember, ID: uuid(1)},
	})
	if d.Kind != DecisionDeny {
		t.Fatalf("missing capability must deny, got %v", d.Kind)
	}
}

func TestDecide_WorkspaceDefaultAllowsAnyMember(t *testing.T) {
	d := Decide(
		[]cerebrodb.CerebroWorkspaceGrant{grant(subjectWorkspaceDefault, pgtype.UUID{}, "issue.read", "*")},
		Request{Actor: Actor{Type: subjectMember, ID: uuid(1)}, Capability: "issue.read"},
	)
	if !d.Allowed() {
		t.Fatalf("workspace_default should allow, got %v (%s)", d.Kind, d.Reason)
	}
}

func TestDecide_MemberGrantOnlyForThatMember(t *testing.T) {
	alice := uuid(1)
	bob := uuid(2)
	grants := []cerebrodb.CerebroWorkspaceGrant{grant(subjectMember, alice, "issue.read", "*")}

	if !Decide(grants, Request{Actor: Actor{Type: subjectMember, ID: alice}, Capability: "issue.read"}).Allowed() {
		t.Fatal("alice should be allowed")
	}
	if Decide(grants, Request{Actor: Actor{Type: subjectMember, ID: bob}, Capability: "issue.read"}).Allowed() {
		t.Fatal("bob must NOT be allowed by alice's grant")
	}
}

func TestDecide_GroupGrant(t *testing.T) {
	groupOps := uuid(7)
	grants := []cerebrodb.CerebroWorkspaceGrant{grant(subjectGroup, groupOps, "deploy.run", "*")}

	allowed := Decide(grants, Request{
		Actor:      Actor{Type: subjectMember, ID: uuid(1), GroupIDs: []pgtype.UUID{groupOps}},
		Capability: "deploy.run",
	})
	if !allowed.Allowed() {
		t.Fatal("group member should inherit grant")
	}

	denied := Decide(grants, Request{
		Actor:      Actor{Type: subjectMember, ID: uuid(1)},
		Capability: "deploy.run",
	})
	if denied.Allowed() {
		t.Fatal("non-member must be denied")
	}
}

func TestDecide_AgentSubject_GatesAgentInIntersection(t *testing.T) {
	agent := uuid(3)
	other := uuid(4)
	grants := []cerebrodb.CerebroWorkspaceGrant{grant(subjectAgent, agent, "agent.run", "*")}

	if !Decide(grants, Request{
		Actor:      Actor{Type: subjectMember, ID: uuid(1)},
		Agent:      agent,
		Capability: "agent.run",
	}).Allowed() {
		t.Fatal("agent grant should allow when correct agent is in use")
	}
	if Decide(grants, Request{
		Actor:      Actor{Type: subjectMember, ID: uuid(1)},
		Agent:      other,
		Capability: "agent.run",
	}).Allowed() {
		t.Fatal("agent grant must NOT allow a different agent")
	}
}

func TestDecide_CapabilityWildcardAndNamespace(t *testing.T) {
	defaultID := pgtype.UUID{}
	cases := []struct {
		pattern    string
		capability string
		want       bool
	}{
		{"*", "anything.here", true},
		{"issue.*", "issue.read", true},
		{"issue.*", "issue.delete", true},
		{"issue.*", "comment.read", false},
		{"issue.read", "issue.read", true},
		{"issue.read", "issue.delete", false},
	}
	for _, tc := range cases {
		t.Run(tc.pattern+"/"+tc.capability, func(t *testing.T) {
			d := Decide(
				[]cerebrodb.CerebroWorkspaceGrant{grant(subjectWorkspaceDefault, defaultID, tc.pattern, "*")},
				Request{Actor: Actor{Type: subjectMember, ID: uuid(1)}, Capability: tc.capability},
			)
			if d.Allowed() != tc.want {
				t.Fatalf("pattern=%s capability=%s got %v reason=%s, want allowed=%v", tc.pattern, tc.capability, d.Kind, d.Reason, tc.want)
			}
		})
	}
}

func TestDecide_ResourcePattern(t *testing.T) {
	defaultID := pgtype.UUID{}
	grants := []cerebrodb.CerebroWorkspaceGrant{grant(subjectWorkspaceDefault, defaultID, "issue.read", "issues/*")}

	if !Decide(grants, Request{Actor: Actor{Type: subjectMember, ID: uuid(1)}, Capability: "issue.read", Resource: "issues/123"}).Allowed() {
		t.Fatal("issues/* should match issues/123")
	}
	if Decide(grants, Request{Actor: Actor{Type: subjectMember, ID: uuid(1)}, Capability: "issue.read", Resource: "comments/123"}).Allowed() {
		t.Fatal("issues/* must NOT match comments/123")
	}
}

func TestDecide_ApprovalRequiredCollapsesToNeedsApproval(t *testing.T) {
	grants := []cerebrodb.CerebroWorkspaceGrant{grant(subjectWorkspaceDefault, pgtype.UUID{}, "deploy.run", "*", withApproval())}
	d := Decide(grants, Request{Actor: Actor{Type: subjectMember, ID: uuid(1)}, Capability: "deploy.run"})
	if d.Kind != DecisionNeedsApproval {
		t.Fatalf("expected needs_approval, got %v (%s)", d.Kind, d.Reason)
	}
}

func TestDecide_RevokedGrantIgnored(t *testing.T) {
	grants := []cerebrodb.CerebroWorkspaceGrant{grant(subjectWorkspaceDefault, pgtype.UUID{}, "deploy.run", "*", withStatus("revoked"))}
	if Decide(grants, Request{Actor: Actor{Type: subjectMember, ID: uuid(1)}, Capability: "deploy.run"}).Allowed() {
		t.Fatal("revoked grant must not allow")
	}
}

func TestDecide_TimeWindow(t *testing.T) {
	now := time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)
	beforeWindow := time.Date(2026, 5, 20, 8, 0, 0, 0, time.UTC)
	insideStart := time.Date(2026, 5, 20, 10, 0, 0, 0, time.UTC)
	insideEnd := time.Date(2026, 5, 20, 14, 0, 0, 0, time.UTC)
	afterWindow := time.Date(2026, 5, 20, 16, 0, 0, 0, time.UTC)

	grants := []cerebrodb.CerebroWorkspaceGrant{grant(subjectWorkspaceDefault, pgtype.UUID{}, "deploy.run", "*", withWindow(insideStart, insideEnd))}

	if !Decide(grants, Request{Now: now, Actor: Actor{Type: subjectMember, ID: uuid(1)}, Capability: "deploy.run"}).Allowed() {
		t.Fatal("inside window should allow")
	}
	if Decide(grants, Request{Now: beforeWindow, Actor: Actor{Type: subjectMember, ID: uuid(1)}, Capability: "deploy.run"}).Allowed() {
		t.Fatal("before window must deny")
	}
	if Decide(grants, Request{Now: afterWindow, Actor: Actor{Type: subjectMember, ID: uuid(1)}, Capability: "deploy.run"}).Allowed() {
		t.Fatal("after window must deny")
	}
}

func TestDecide_RecordsMatchedGrantIDs(t *testing.T) {
	gid := uuid(42)
	grants := []cerebrodb.CerebroWorkspaceGrant{grant(subjectWorkspaceDefault, pgtype.UUID{}, "issue.read", "*", withID(gid))}
	d := Decide(grants, Request{Actor: Actor{Type: subjectMember, ID: uuid(1)}, Capability: "issue.read"})
	if !d.Allowed() {
		t.Fatalf("expected allow, got %v", d.Kind)
	}
	if len(d.MatchedGrantIDs) != 1 || d.MatchedGrantIDs[0].Bytes != gid.Bytes {
		t.Fatalf("expected matched grant id %x, got %+v", gid.Bytes, d.MatchedGrantIDs)
	}
}

func TestDecide_RecordsWinningOverrideLayer(t *testing.T) {
	groupID := uuid(7)
	grants := []cerebrodb.CerebroWorkspaceGrant{
		grant(subjectWorkspaceDefault, pgtype.UUID{}, "issue.read", "*"),
		grant(subjectGroup, groupID, "issue.read", "*"),
	}
	d := Decide(grants, Request{
		Actor:      Actor{Type: subjectMember, ID: uuid(1), GroupIDs: []pgtype.UUID{groupID}},
		Capability: "issue.read",
	})
	if d.WinningOverrideLayer != "group" {
		t.Fatalf("expected group layer to win, got %q", d.WinningOverrideLayer)
	}
}

func TestCan_WritesPermissionAuditEvent(t *testing.T) {
	store := &fakeStore{
		grants: []cerebrodb.CerebroWorkspaceGrant{grant(subjectWorkspaceDefault, pgtype.UUID{}, "issue.read", "*", withID(uuid(42)))},
	}
	resolver := &Resolver{Cerebro: store, Audit: store}

	decision, err := resolver.Can(context.Background(), Request{
		WorkspaceID: uuid(9),
		Actor:       Actor{Type: subjectMember, ID: uuid(1)},
		Capability:  "issue.read",
		Resource:    "issues/123",
	})
	if err != nil {
		t.Fatalf("Can returned error: %v", err)
	}
	if !decision.Allowed() {
		t.Fatalf("expected allow, got %v", decision.Kind)
	}
	if len(store.audits) != 1 {
		t.Fatalf("expected one audit event, got %d", len(store.audits))
	}
	if store.audits[0].ActorType.String != subjectMember || !store.audits[0].ActorID.Valid {
		t.Fatalf("audit actor not populated: %+v", store.audits[0])
	}

	var details map[string]any
	if err := json.Unmarshal(store.audits[0].Details, &details); err != nil {
		t.Fatalf("audit details should be JSON: %v", err)
	}
	if details["capability"] != "issue.read" || details["scope"] != "issues/123" {
		t.Fatalf("audit details missing request shape: %#v", details)
	}
	if details["decision"] != "allow" || details["winning_override_layer"] != "workspace" {
		t.Fatalf("audit details missing decision/layer: %#v", details)
	}
}

func TestCan_ReturnsAuditWriteError(t *testing.T) {
	wantErr := errors.New("audit unavailable")
	store := &fakeStore{
		grants: []cerebrodb.CerebroWorkspaceGrant{grant(subjectWorkspaceDefault, pgtype.UUID{}, "issue.read", "*")},
		err:    wantErr,
	}
	resolver := &Resolver{Cerebro: store, Audit: store}

	decision, err := resolver.Can(context.Background(), Request{
		WorkspaceID: uuid(9),
		Actor:       Actor{Type: subjectMember, ID: uuid(1)},
		Capability:  "issue.read",
	})
	if err == nil || !errors.Is(err, wantErr) {
		t.Fatalf("expected audit error, got %v", err)
	}
	if !decision.Allowed() {
		t.Fatalf("decision should still describe the evaluated policy, got %v", decision.Kind)
	}
}
