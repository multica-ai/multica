package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/handler"
)

type capabilitiesCardProvider struct {
	card handler.AgentCapabilities
}

func (p capabilitiesCardProvider) BuildAgentCapabilitiesCard(context.Context, pgtype.UUID) (handler.AgentCapabilities, error) {
	return p.card, nil
}

type rejectingTaskMandates struct {
	rejected string
}

func (rejectingTaskMandates) Issue(context.Context, pgtype.UUID, pgtype.UUID, pgtype.UUID, []string, time.Time) error {
	return nil
}

func (m rejectingTaskMandates) Authorize(_ context.Context, _, _, _ pgtype.UUID, tool string) error {
	if tool == m.rejected {
		return errors.New("tool is outside the issued task mandate")
	}
	return nil
}

func TestGetAgentCapabilitiesAppliesRejectedTaskMandate(t *testing.T) {
	id := pgtype.UUID{Bytes: [16]byte{1}, Valid: true}
	tool := FirtalGetAgentCapabilitiesTool{
		provider: capabilitiesCardProvider{card: handler.AgentCapabilities{Tools: []handler.AgentCapabilityTool{
			{Key: "allowed_tool", Permission: "allow", Allowed: true, Available: true, Enforced: true, Callable: true},
			{Key: "rejected_tool", Permission: "allow", Allowed: true, Available: true, Enforced: true, Callable: true},
		}}},
		tctx: ToolContext{
			AgentID:      id,
			WorkspaceID:  id,
			TaskID:       id,
			TaskMandates: rejectingTaskMandates{rejected: "rejected_tool"},
		},
	}

	raw, err := tool.Call(context.Background(), nil)
	if err != nil {
		t.Fatalf("Call() error = %v", err)
	}
	var card handler.AgentCapabilities
	if err := json.Unmarshal([]byte(raw), &card); err != nil {
		t.Fatalf("unmarshal card: %v", err)
	}
	if got := card.Tools[0].Permission; got != "allow" {
		t.Errorf("allowed tool permission = %q, want allow", got)
	}
	if got := card.Tools[1].Permission; got != "deny" {
		t.Errorf("rejected tool permission = %q, want deny", got)
	}
	if card.Tools[1].Reason == "" {
		t.Error("rejected task mandate must explain the denial")
	}
	if card.Tools[1].Allowed || card.Tools[1].Callable || card.Tools[1].BlockedReason == "" || card.Tools[1].HowToFix == "" {
		t.Fatalf("rejected task mandate left a positive or unexplained truth verdict: %+v", card.Tools[1])
	}
}
