package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// stubOverlayBuilder records every call so the test can assert which
// branches of TaskService.applyRuntimeMCPOverlay reached the builder
// vs short-circuited before it.
type stubOverlayBuilder struct {
	calls     int
	lastUser  pgtype.UUID
	lastAgent db.Agent
	resp      json.RawMessage
	err       error
	respIsNil bool
}

func (s *stubOverlayBuilder) BuildTaskOverlay(_ context.Context, originatorUserID pgtype.UUID, agent db.Agent) (json.RawMessage, error) {
	s.calls++
	s.lastUser = originatorUserID
	s.lastAgent = agent
	if s.err != nil {
		return nil, s.err
	}
	if s.respIsNil {
		return nil, nil
	}
	return s.resp, nil
}

// mintTestAgent returns an Agent fixture with the given owner id stamped
// in; other fields are zero because applyRuntimeMCPOverlay treats the
// agent as opaque data forwarded to the builder.
func mintTestAgent(owner pgtype.UUID) db.Agent {
	return db.Agent{OwnerID: owner}
}

// TestApplyRuntimeMCPOverlay_NoComposioIsNoOp pins the safety-net that
// makes Stage 3 a pure addition for every Multica deployment that hasn't
// enabled Composio: when s.Composio is nil, the helper must not panic
// (Queries can legitimately be nil in unit-test setup) and must not call
// any builder. This is the property that lets every existing enqueue test
// keep passing without instantiating a Composio service.
func TestApplyRuntimeMCPOverlay_NoComposioIsNoOp(t *testing.T) {
	t.Parallel()
	svc := &TaskService{} // Queries nil on purpose
	taskID := pgtype.UUID{Bytes: [16]byte{0x01}, Valid: true}
	userID := pgtype.UUID{Bytes: [16]byte{0x02}, Valid: true}

	// Should not panic and should not touch Queries.
	svc.applyRuntimeMCPOverlay(context.Background(), taskID, userID, mintTestAgent(userID))
}

// TestApplyRuntimeMCPOverlay_InvalidTaskIDIsNoOp guards a defensive branch:
// every call site threads a row that was just inserted, but a bug that
// loses the task id between Create and the overlay call must short-circuit
// before touching Queries — otherwise SetAgentTaskRuntimeMCPOverlay would
// be hit with an invalid UUID.
func TestApplyRuntimeMCPOverlay_InvalidTaskIDIsNoOp(t *testing.T) {
	t.Parallel()
	builder := &stubOverlayBuilder{}
	svc := &TaskService{Composio: builder}
	userID := pgtype.UUID{Bytes: [16]byte{0x03}, Valid: true}

	svc.applyRuntimeMCPOverlay(context.Background(), pgtype.UUID{}, userID, mintTestAgent(userID))
	if builder.calls != 0 {
		t.Errorf("expected 0 builder calls for invalid task id, got %d", builder.calls)
	}
}

// TestApplyRuntimeMCPOverlay_InvalidOriginatorReachesBuilder — the
// invalid-originator gate now lives INSIDE BuildTaskOverlay (gate 1 in
// the dispatch.go contract). applyRuntimeMCPOverlay just forwards the
// invalid UUID; the builder is responsible for returning (nil, nil).
// We assert that the helper does not panic and does call the builder so
// the in-process contract stays explicit.
func TestApplyRuntimeMCPOverlay_InvalidOriginatorReachesBuilder(t *testing.T) {
	t.Parallel()
	builder := &stubOverlayBuilder{respIsNil: true}
	svc := &TaskService{Composio: builder} // Queries nil on purpose
	taskID := pgtype.UUID{Bytes: [16]byte{0x04}, Valid: true}

	svc.applyRuntimeMCPOverlay(context.Background(), taskID, pgtype.UUID{}, db.Agent{})
	if builder.calls != 1 {
		t.Errorf("expected 1 builder call (gate 1 lives in the builder), got %d", builder.calls)
	}
	if builder.lastUser.Valid {
		t.Errorf("builder received valid originator; want invalid: %+v", builder.lastUser)
	}
}

// TestApplyRuntimeMCPOverlay_NilOverlaySkipsUpdate — BuildTaskOverlay
// returning (nil, nil) means one of the five gates short-circuited. The
// helper must short-circuit before touching Queries so a unit-test setup
// with Queries=nil is safe and so we don't issue a pointless UPDATE
// against the queue table in production.
func TestApplyRuntimeMCPOverlay_NilOverlaySkipsUpdate(t *testing.T) {
	t.Parallel()
	builder := &stubOverlayBuilder{respIsNil: true}
	svc := &TaskService{Composio: builder} // Queries nil on purpose
	taskID := pgtype.UUID{Bytes: [16]byte{0x05}, Valid: true}
	userID := pgtype.UUID{Bytes: [16]byte{0x06}, Valid: true}

	svc.applyRuntimeMCPOverlay(context.Background(), taskID, userID, mintTestAgent(userID))
	if builder.calls != 1 {
		t.Errorf("expected exactly 1 builder call, got %d", builder.calls)
	}
}

// TestApplyRuntimeMCPOverlay_BuilderErrorSwallowed — best-effort enqueue:
// a builder error (Composio outage etc.) must be logged + swallowed so
// the task still queues. We assert the helper does not panic and does
// not attempt the UPDATE, which would crash with a nil Queries.
func TestApplyRuntimeMCPOverlay_BuilderErrorSwallowed(t *testing.T) {
	t.Parallel()
	builder := &stubOverlayBuilder{err: errors.New("upstream 503")}
	svc := &TaskService{Composio: builder}
	taskID := pgtype.UUID{Bytes: [16]byte{0x07}, Valid: true}
	userID := pgtype.UUID{Bytes: [16]byte{0x08}, Valid: true}

	svc.applyRuntimeMCPOverlay(context.Background(), taskID, userID, mintTestAgent(userID))
	if builder.calls != 1 {
		t.Errorf("expected 1 builder call, got %d", builder.calls)
	}
}

// TestApplyRuntimeMCPOverlay_AgentPassedThrough asserts that the helper
// forwards the agent value verbatim to the builder so the originator-vs-
// owner gate (MUL-3869) actually sees the right OwnerID. Without this
// pass-through every enqueue path would silently fall back to the
// "originator ≠ owner → no overlay" branch of the builder.
func TestApplyRuntimeMCPOverlay_AgentPassedThrough(t *testing.T) {
	t.Parallel()
	builder := &stubOverlayBuilder{respIsNil: true}
	svc := &TaskService{Composio: builder}
	taskID := pgtype.UUID{Bytes: [16]byte{0x09}, Valid: true}
	owner := pgtype.UUID{Bytes: [16]byte{0x0A}, Valid: true}
	agent := db.Agent{
		ID:                       pgtype.UUID{Bytes: [16]byte{0x0B}, Valid: true},
		OwnerID:                  owner,
		ComposioToolkitAllowlist: []string{"notion", "github"},
	}

	svc.applyRuntimeMCPOverlay(context.Background(), taskID, owner, agent)
	if builder.calls != 1 {
		t.Fatalf("expected 1 builder call, got %d", builder.calls)
	}
	if builder.lastAgent.OwnerID != owner {
		t.Errorf("builder received OwnerID=%+v, want %+v", builder.lastAgent.OwnerID, owner)
	}
	if len(builder.lastAgent.ComposioToolkitAllowlist) != 2 {
		t.Errorf("builder received allowlist=%v, want length 2", builder.lastAgent.ComposioToolkitAllowlist)
	}
}
