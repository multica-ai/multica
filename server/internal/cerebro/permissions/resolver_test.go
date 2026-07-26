package permissions

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
)

// fakeAudit captures permission-audit writes. The resolver no longer reads any
// grant table (FIR-1512), so the only collaborator left to fake is the audit
// writer.
type fakeAudit struct {
	audits []cerebrodb.InsertCerebroPermissionAuditEventParams
	err    error
}

func (f *fakeAudit) InsertCerebroPermissionAuditEvent(_ context.Context, arg cerebrodb.InsertCerebroPermissionAuditEventParams) error {
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

// TestCan_DeniesEveryRequest pins the security floor: with grants retired, the
// resolver denies every request (member and agent actors, with or without a
// concrete resource), never returning Allow or NeedsApproval. This is the
// behaviour every call site layers its own allow authority on top of.
func TestCan_DeniesEveryRequest(t *testing.T) {
	store := &fakeAudit{}
	resolver := &Resolver{Audit: store}

	cases := []struct {
		name string
		req  Request
	}{
		{
			name: "member credential reveal",
			req: Request{
				WorkspaceID: uuid(9),
				Actor:       Actor{Type: "member", ID: uuid(1)},
				Capability:  "credential.reveal",
				Resource:    "cerebro-credential:abc",
			},
		},
		{
			name: "agent repo checkout",
			req: Request{
				WorkspaceID: uuid(9),
				Actor:       Actor{Type: "agent", ID: uuid(2)},
				Agent:       uuid(2),
				Capability:  "repo.checkout",
				Resource:    "github.com/org/repo",
			},
		},
		{
			name: "no concrete resource",
			req: Request{
				WorkspaceID: uuid(9),
				Actor:       Actor{Type: "member", ID: uuid(1)},
				Capability:  "agent.run",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d, err := resolver.Can(context.Background(), tc.req)
			if err != nil {
				t.Fatalf("Can returned error: %v", err)
			}
			if d.Kind != DecisionDeny {
				t.Fatalf("expected deny, got %v (%s)", d.Kind, d.Reason)
			}
			if d.Allowed() {
				t.Fatal("Allowed() must be false")
			}
		})
	}
}

// TestCan_MissingCapabilityDenies mirrors the prior Decide guard: an empty
// capability is denied with the capability-required reason.
func TestCan_MissingCapabilityDenies(t *testing.T) {
	resolver := &Resolver{Audit: &fakeAudit{}}
	d, err := resolver.Can(context.Background(), Request{
		WorkspaceID: uuid(9),
		Actor:       Actor{Type: "member", ID: uuid(1)},
	})
	if err != nil {
		t.Fatalf("Can returned error: %v", err)
	}
	if d.Kind != DecisionDeny || d.Reason != "capability required" {
		t.Fatalf("expected deny/capability required, got %v (%s)", d.Kind, d.Reason)
	}
}

func TestCan_WritesPermissionAuditEvent(t *testing.T) {
	store := &fakeAudit{}
	resolver := &Resolver{Audit: store}

	decision, err := resolver.Can(context.Background(), Request{
		WorkspaceID: uuid(9),
		Actor:       Actor{Type: "member", ID: uuid(1)},
		Capability:  "issue.read",
		Resource:    "issues/123",
	})
	if err != nil {
		t.Fatalf("Can returned error: %v", err)
	}
	if decision.Kind != DecisionDeny {
		t.Fatalf("expected deny, got %v", decision.Kind)
	}
	if len(store.audits) != 1 {
		t.Fatalf("expected one audit event, got %d", len(store.audits))
	}
	if store.audits[0].ActorType.String != "member" || !store.audits[0].ActorID.Valid {
		t.Fatalf("audit actor not populated: %+v", store.audits[0])
	}

	var details map[string]any
	if err := json.Unmarshal(store.audits[0].Details, &details); err != nil {
		t.Fatalf("audit details should be JSON: %v", err)
	}
	if details["capability"] != "issue.read" || details["scope"] != "issues/123" {
		t.Fatalf("audit details missing request shape: %#v", details)
	}
	if details["decision"] != "deny" || details["winning_override_layer"] != "none" {
		t.Fatalf("audit details missing decision/layer: %#v", details)
	}
}

func TestCan_ReturnsAuditWriteError(t *testing.T) {
	wantErr := errors.New("audit unavailable")
	store := &fakeAudit{err: wantErr}
	resolver := &Resolver{Audit: store}

	decision, err := resolver.Can(context.Background(), Request{
		WorkspaceID: uuid(9),
		Actor:       Actor{Type: "member", ID: uuid(1)},
		Capability:  "issue.read",
	})
	if err == nil || !errors.Is(err, wantErr) {
		t.Fatalf("expected audit error, got %v", err)
	}
	// The deny decision is still returned alongside the error so callers fail
	// closed.
	if decision.Kind != DecisionDeny {
		t.Fatalf("expected deny decision even on audit error, got %v", decision.Kind)
	}
}

func TestCan_NilResolverDenies(t *testing.T) {
	var resolver *Resolver
	d, err := resolver.Can(context.Background(), Request{Capability: "issue.read"})
	if err != nil {
		t.Fatalf("Can returned error: %v", err)
	}
	if d.Kind != DecisionDeny {
		t.Fatalf("expected deny from nil resolver, got %v", d.Kind)
	}
}
