package service

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestAgentReadiness_BuiltinAgentWithoutRuntimeIsReady(t *testing.T) {
	agent := db.MulticaAgent{
		ID:        pgtype.UUID{Bytes: [16]byte{1}, Valid: true},
		IsBuiltin: true,
	}

	ready, reason, err := AgentReadiness(context.Background(), nil, agent)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ready {
		t.Fatalf("expected built-in agent to be ready, got reason %q", reason)
	}
	if reason != "" {
		t.Fatalf("expected empty reason, got %q", reason)
	}
}

func TestAgentReadiness_NonBuiltinAgentWithoutRuntimeIsNotReady(t *testing.T) {
	agent := db.MulticaAgent{
		ID:        pgtype.UUID{Bytes: [16]byte{2}, Valid: true},
		IsBuiltin: false,
	}

	ready, reason, err := AgentReadiness(context.Background(), nil, agent)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ready {
		t.Fatal("expected non-built-in agent without runtime to be not ready")
	}
	if reason != "agent has no runtime bound" {
		t.Fatalf("expected 'agent has no runtime bound', got %q", reason)
	}
}

func TestAgentReadiness_ArchivedAgentIsNotReady(t *testing.T) {
	agent := db.MulticaAgent{
		ID:         pgtype.UUID{Bytes: [16]byte{3}, Valid: true},
		IsBuiltin:  true,
		ArchivedAt: pgtype.Timestamptz{Valid: true},
	}

	ready, reason, err := AgentReadiness(context.Background(), nil, agent)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ready {
		t.Fatal("expected archived agent to be not ready")
	}
	if reason != "agent is archived" {
		t.Fatalf("expected 'agent is archived', got %q", reason)
	}
}
