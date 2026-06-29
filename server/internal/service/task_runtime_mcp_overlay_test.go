package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
)

// stubOverlayBuilder records every call so the test can assert which
// branches of TaskService.applyRuntimeMCPOverlay reached the builder
// vs short-circuited before it.
type stubOverlayBuilder struct {
	calls     int
	lastUser  pgtype.UUID
	resp      json.RawMessage
	err       error
	respIsNil bool
}

func (s *stubOverlayBuilder) BuildTaskOverlay(_ context.Context, userID pgtype.UUID) (json.RawMessage, error) {
	s.calls++
	s.lastUser = userID
	if s.err != nil {
		return nil, s.err
	}
	if s.respIsNil {
		return nil, nil
	}
	return s.resp, nil
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
	svc.applyRuntimeMCPOverlay(context.Background(), taskID, userID)
}

// TestApplyRuntimeMCPOverlay_InvalidInitiatorIsNoOp covers the
// no-attributable-human-initiator path (autopilot, system-driven). When
// the initiator UUID is not valid we must NOT call BuildTaskOverlay — we
// must not pay the Composio session cost for a guaranteed-empty overlay.
func TestApplyRuntimeMCPOverlay_InvalidInitiatorIsNoOp(t *testing.T) {
	t.Parallel()
	builder := &stubOverlayBuilder{}
	svc := &TaskService{Composio: builder}
	taskID := pgtype.UUID{Bytes: [16]byte{0x03}, Valid: true}

	svc.applyRuntimeMCPOverlay(context.Background(), taskID, pgtype.UUID{}) // invalid
	if builder.calls != 0 {
		t.Errorf("expected 0 builder calls for invalid initiator, got %d", builder.calls)
	}
}

// TestApplyRuntimeMCPOverlay_NilOverlaySkipsUpdate — BuildTaskOverlay
// returning (nil, nil) means the user has no active connections. The
// helper must short-circuit before touching Queries so a unit-test setup
// with Queries=nil is safe and so we don't issue a pointless UPDATE
// against the queue table in production.
func TestApplyRuntimeMCPOverlay_NilOverlaySkipsUpdate(t *testing.T) {
	t.Parallel()
	builder := &stubOverlayBuilder{respIsNil: true}
	svc := &TaskService{Composio: builder} // Queries nil on purpose
	taskID := pgtype.UUID{Bytes: [16]byte{0x04}, Valid: true}
	userID := pgtype.UUID{Bytes: [16]byte{0x05}, Valid: true}

	svc.applyRuntimeMCPOverlay(context.Background(), taskID, userID)
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
	taskID := pgtype.UUID{Bytes: [16]byte{0x06}, Valid: true}
	userID := pgtype.UUID{Bytes: [16]byte{0x07}, Valid: true}

	svc.applyRuntimeMCPOverlay(context.Background(), taskID, userID)
	if builder.calls != 1 {
		t.Errorf("expected 1 builder call, got %d", builder.calls)
	}
}
