package toolapproval

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

type recordingRepository struct {
	decisions []Decision
	creations []Creation
	consumes  []Consumption
	cancels   []Cancellation
}

func (r *recordingRepository) Decide(_ context.Context, decision Decision) (Approval, error) {
	r.decisions = append(r.decisions, decision)
	return Approval{ID: decision.ApprovalID, Status: decision.Decision}, nil
}

func (r *recordingRepository) CreateOrGet(_ context.Context, creation Creation) (Approval, error) {
	r.creations = append(r.creations, creation)
	return Approval{ID: "00000000-0000-4000-8000-000000000012", Status: StatusPending}, nil
}

func (r *recordingRepository) Consume(_ context.Context, consumption Consumption) (Approval, error) {
	r.consumes = append(r.consumes, consumption)
	return Approval{ID: consumption.ApprovalID, Status: StatusConsumed}, nil
}

func (r *recordingRepository) Cancel(_ context.Context, cancellation Cancellation) (Approval, error) {
	r.cancels = append(r.cancels, cancellation)
	return Approval{ID: cancellation.ApprovalID, Status: StatusCancelled}, nil
}

func (r *recordingRepository) Get(_ context.Context, lookup Lookup) (Approval, error) {
	return Approval{ID: lookup.ApprovalID}, nil
}

func (r *recordingRepository) ListPending(context.Context, PendingQuery) ([]Approval, error) {
	return []Approval{}, nil
}

func TestServiceDecideIsHumanOperatorOnly(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		actor   Actor
		wantErr error
	}{
		{name: "owner", actor: Actor{Kind: ActorHuman, UserID: "00000000-0000-4000-8000-000000000001", WorkspaceRole: "owner"}},
		{name: "admin", actor: Actor{Kind: ActorHuman, UserID: "00000000-0000-4000-8000-000000000002", WorkspaceRole: "admin"}},
		{name: "member", actor: Actor{Kind: ActorHuman, UserID: "00000000-0000-4000-8000-000000000003", WorkspaceRole: "member"}, wantErr: ErrForbidden},
		{name: "agent", actor: Actor{Kind: ActorAgent, AgentID: "00000000-0000-4000-8000-000000000004"}, wantErr: ErrForbidden},
		{name: "task", actor: Actor{Kind: ActorTask, AgentID: "00000000-0000-4000-8000-000000000004"}, wantErr: ErrForbidden},
		{name: "daemon", actor: Actor{Kind: ActorDaemon}, wantErr: ErrForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			repository := &recordingRepository{}
			service := NewService(repository)
			_, err := service.Decide(context.Background(), Decision{
				WorkspaceID: "00000000-0000-4000-8000-000000000010",
				ApprovalID:  "00000000-0000-4000-8000-000000000011",
				Actor:       tt.actor,
				Decision:    DecisionApproved,
			})
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Decide() error = %v, want %v", err, tt.wantErr)
			}
			wantCalls := 1
			if tt.wantErr != nil {
				wantCalls = 0
			}
			if len(repository.decisions) != wantCalls {
				t.Fatalf("repository calls = %d, want %d", len(repository.decisions), wantCalls)
			}
		})
	}
}

func TestServiceDecideUsesOnlyStructuredDecisionMetadata(t *testing.T) {
	t.Parallel()

	fixed := time.Date(2026, 8, 29, 12, 30, 0, 0, time.UTC)
	tests := []struct {
		name       string
		decision   string
		reasonCode string
		wantReason string
		wantErr    error
	}{
		{name: "approve", decision: DecisionApproved, wantReason: "operator_approved"},
		{name: "deny default", decision: DecisionDenied, wantReason: "operator_denied"},
		{name: "deny structured reason", decision: DecisionDenied, reasonCode: "risk_too_high", wantReason: "risk_too_high"},
		{name: "free form reason", decision: DecisionDenied, reasonCode: "looks risky because arguments contain a secret", wantErr: ErrInvalidDecision},
		{name: "approve custom reason", decision: DecisionApproved, reasonCode: "not_needed", wantErr: ErrInvalidDecision},
		{name: "unknown decision", decision: "allow_once", wantErr: ErrInvalidDecision},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			repository := &recordingRepository{}
			service := NewService(repository)
			service.now = func() time.Time { return fixed }
			_, err := service.Decide(context.Background(), Decision{
				WorkspaceID: "00000000-0000-4000-8000-000000000010",
				ApprovalID:  "00000000-0000-4000-8000-000000000011",
				Actor: Actor{
					Kind:          ActorHuman,
					UserID:        "00000000-0000-4000-8000-000000000001",
					WorkspaceRole: "owner",
				},
				Decision:   tt.decision,
				ReasonCode: tt.reasonCode,
			})
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Decide() error = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr != nil {
				if len(repository.decisions) != 0 {
					t.Fatalf("repository calls = %d, want 0", len(repository.decisions))
				}
				return
			}
			if len(repository.decisions) != 1 {
				t.Fatalf("repository calls = %d, want 1", len(repository.decisions))
			}
			got := repository.decisions[0]
			if got.ReasonCode != tt.wantReason {
				t.Fatalf("reason_code = %q, want %q", got.ReasonCode, tt.wantReason)
			}
			if !got.DecidedAt.Equal(fixed) {
				t.Fatalf("decided_at = %s, want %s", got.DecidedAt, fixed)
			}
		})
	}
}

func TestServiceCreateOrGetCanonicalizesImmutableMetadataForDaemon(t *testing.T) {
	t.Parallel()

	fixed := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	repository := &recordingRepository{}
	service := NewService(repository)
	service.now = func() time.Time { return fixed }

	approval, err := service.CreateOrGet(context.Background(), Creation{
		WorkspaceID:      "00000000-0000-4000-8000-000000000010",
		AgentID:          "00000000-0000-4000-8000-000000000020",
		TaskID:           "00000000-0000-4000-8000-000000000030",
		InvocationID:     "00000000-0000-4000-8000-000000000040",
		IdempotencyKey:   " invocation-1 ",
		TransportKind:    " MANAGED_MCP ",
		ServerKey:        " linear ",
		ToolName:         " list_issues ",
		SchemaDigest:     "SHA256:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
		PolicyRevision:   7,
		SchemaFieldNames: []string{"team_id", "limit"},
		ArgumentBytes:    128,
		ExpiresAt:        fixed.Add(15 * time.Minute),
		Actor:            Actor{Kind: ActorDaemon},
	})
	if err != nil {
		t.Fatalf("CreateOrGet() error = %v", err)
	}
	if approval.Status != StatusPending {
		t.Fatalf("status = %q, want %q", approval.Status, StatusPending)
	}
	if len(repository.creations) != 1 {
		t.Fatalf("repository calls = %d, want 1", len(repository.creations))
	}
	got := repository.creations[0]
	if got.IdempotencyKey != "invocation-1" || got.TransportKind != "managed_mcp" || got.ServerKey != "linear" || got.ToolName != "list_issues" {
		t.Fatalf("canonical identity = %#v", got)
	}
	if got.SchemaDigest != "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Fatalf("schema digest = %q", got.SchemaDigest)
	}
	if !reflect.DeepEqual(got.SchemaFieldNames, []string{"limit", "team_id"}) {
		t.Fatalf("schema fields = %#v", got.SchemaFieldNames)
	}
	if !got.RequestedAt.Equal(fixed) {
		t.Fatalf("requested_at = %s, want %s", got.RequestedAt, fixed)
	}
}

func TestServiceCreateOrGetRejectsNonDaemonAndRawMetadata(t *testing.T) {
	t.Parallel()

	fixed := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	valid := Creation{
		WorkspaceID:      "00000000-0000-4000-8000-000000000010",
		AgentID:          "00000000-0000-4000-8000-000000000020",
		TaskID:           "00000000-0000-4000-8000-000000000030",
		InvocationID:     "00000000-0000-4000-8000-000000000040",
		IdempotencyKey:   "invocation-1",
		TransportKind:    "managed_mcp",
		ServerKey:        "linear",
		ToolName:         "list_issues",
		SchemaDigest:     "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		PolicyRevision:   7,
		SchemaFieldNames: []string{"limit"},
		ArgumentBytes:    128,
		ExpiresAt:        fixed.Add(15 * time.Minute),
		Actor:            Actor{Kind: ActorDaemon},
	}

	tests := []struct {
		name   string
		mutate func(*Creation)
		err    error
	}{
		{name: "human actor", mutate: func(c *Creation) { c.Actor = Actor{Kind: ActorHuman, WorkspaceRole: "owner"} }, err: ErrForbidden},
		{name: "raw server value", mutate: func(c *Creation) { c.ServerKey = "https://linear.example/token=secret" }, err: ErrRawMetadata},
		{name: "raw field value", mutate: func(c *Creation) { c.SchemaFieldNames = []string{"password=secret"} }, err: ErrRawMetadata},
		{name: "invalid digest", mutate: func(c *Creation) { c.SchemaDigest = "sha256:not-a-digest" }, err: ErrInvalidMetadata},
		{name: "expired", mutate: func(c *Creation) { c.ExpiresAt = fixed }, err: ErrInvalidMetadata},
		{name: "overlong lifetime", mutate: func(c *Creation) { c.ExpiresAt = fixed.Add(24*time.Hour + time.Second) }, err: ErrInvalidMetadata},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			creation := valid
			creation.SchemaFieldNames = append([]string(nil), valid.SchemaFieldNames...)
			tt.mutate(&creation)
			repository := &recordingRepository{}
			service := NewService(repository)
			service.now = func() time.Time { return fixed }
			_, err := service.CreateOrGet(context.Background(), creation)
			if !errors.Is(err, tt.err) {
				t.Fatalf("CreateOrGet() error = %v, want %v", err, tt.err)
			}
			if len(repository.creations) != 0 {
				t.Fatalf("repository calls = %d, want 0", len(repository.creations))
			}
		})
	}
}

func TestServiceConsumeAndCancelAreDaemonOnly(t *testing.T) {
	t.Parallel()

	fixed := time.Date(2026, 8, 29, 13, 0, 0, 0, time.UTC)
	for _, actor := range []Actor{
		{Kind: ActorHuman, UserID: "00000000-0000-4000-8000-000000000001", WorkspaceRole: "owner"},
		{Kind: ActorAgent, AgentID: "00000000-0000-4000-8000-000000000020"},
		{Kind: ActorTask, AgentID: "00000000-0000-4000-8000-000000000020"},
	} {
		repository := &recordingRepository{}
		service := NewService(repository)
		_, err := service.Consume(context.Background(), Consumption{
			WorkspaceID: "00000000-0000-4000-8000-000000000010",
			TaskID:      "00000000-0000-4000-8000-000000000030",
			ApprovalID:  "00000000-0000-4000-8000-000000000011",
			Actor:       actor,
		})
		if !errors.Is(err, ErrForbidden) {
			t.Fatalf("Consume(%s) error = %v, want forbidden", actor.Kind, err)
		}
		_, err = service.Cancel(context.Background(), Cancellation{
			WorkspaceID: "00000000-0000-4000-8000-000000000010",
			TaskID:      "00000000-0000-4000-8000-000000000030",
			ApprovalID:  "00000000-0000-4000-8000-000000000011",
			Actor:       actor,
			ReasonCode:  "task_cancelled",
		})
		if !errors.Is(err, ErrForbidden) {
			t.Fatalf("Cancel(%s) error = %v, want forbidden", actor.Kind, err)
		}
	}

	repository := &recordingRepository{}
	service := NewService(repository)
	service.now = func() time.Time { return fixed }
	actor := Actor{Kind: ActorDaemon}
	consumed, err := service.Consume(context.Background(), Consumption{
		WorkspaceID: "00000000-0000-4000-8000-000000000010",
		TaskID:      "00000000-0000-4000-8000-000000000030",
		ApprovalID:  "00000000-0000-4000-8000-000000000011",
		Actor:       actor,
	})
	if err != nil || consumed.Status != StatusConsumed {
		t.Fatalf("Consume() = %#v, %v", consumed, err)
	}
	if len(repository.consumes) != 1 || !repository.consumes[0].ConsumedAt.Equal(fixed) {
		t.Fatalf("consume repository input = %#v", repository.consumes)
	}
	cancelled, err := service.Cancel(context.Background(), Cancellation{
		WorkspaceID: "00000000-0000-4000-8000-000000000010",
		TaskID:      "00000000-0000-4000-8000-000000000030",
		ApprovalID:  "00000000-0000-4000-8000-000000000012",
		Actor:       actor,
		ReasonCode:  "task_cancelled",
	})
	if err != nil || cancelled.Status != StatusCancelled {
		t.Fatalf("Cancel() = %#v, %v", cancelled, err)
	}
	if len(repository.cancels) != 1 || !repository.cancels[0].CancelledAt.Equal(fixed) {
		t.Fatalf("cancel repository input = %#v", repository.cancels)
	}
}

func TestServiceCancelRejectsFreeFormReason(t *testing.T) {
	t.Parallel()

	repository := &recordingRepository{}
	service := NewService(repository)
	_, err := service.Cancel(context.Background(), Cancellation{
		WorkspaceID: "00000000-0000-4000-8000-000000000010",
		TaskID:      "00000000-0000-4000-8000-000000000030",
		ApprovalID:  "00000000-0000-4000-8000-000000000011",
		Actor:       Actor{Kind: ActorDaemon},
		ReasonCode:  "cancel because the raw command is dangerous",
	})
	if !errors.Is(err, ErrInvalidDecision) {
		t.Fatalf("Cancel() error = %v, want %v", err, ErrInvalidDecision)
	}
	if len(repository.cancels) != 0 {
		t.Fatalf("repository calls = %d, want 0", len(repository.cancels))
	}
}
