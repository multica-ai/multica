package handler

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func squadRuntimeTestUUID(b byte) pgtype.UUID {
	var id pgtype.UUID
	id.Bytes[15] = b
	id.Valid = true
	return id
}

func TestSquadRuntimeRebindBlock(t *testing.T) {
	owner := squadRuntimeTestUUID(1)
	other := squadRuntimeTestUUID(2)
	ownerMember := db.Member{UserID: owner, Role: "member"}
	adminMember := db.Member{UserID: other, Role: "admin"}
	plainMember := db.Member{UserID: other, Role: "member"}
	publicRuntime := db.AgentRuntime{Visibility: "public", OwnerID: owner}
	privateRuntime := db.AgentRuntime{Visibility: "private", OwnerID: other}
	agentOf := func(model string) db.Agent {
		a := db.Agent{OwnerID: owner}
		if model != "" {
			a.Model = pgtype.Text{String: model, Valid: true}
		}
		return a
	}

	t.Run("the agent owner can move their agent", func(t *testing.T) {
		if reason, _ := squadRuntimeRebindBlock(agentOf(""), ownerMember, publicRuntime, "codex", acpEffortPresent); reason != "" {
			t.Fatalf("owner blocked: %s", reason)
		}
	})

	t.Run("an admin can move any agent", func(t *testing.T) {
		if reason, _ := squadRuntimeRebindBlock(agentOf(""), adminMember, publicRuntime, "codex", acpEffortPresent); reason != "" {
			t.Fatalf("admin blocked: %s", reason)
		}
	})

	t.Run("a member who does not own the agent is skipped", func(t *testing.T) {
		reason, _ := squadRuntimeRebindBlock(agentOf(""), plainMember, publicRuntime, "codex", acpEffortPresent)
		if reason != "only the agent owner can manage this agent" {
			t.Fatalf("unexpected reason %q", reason)
		}
	})

	t.Run("a private runtime the agent owner cannot use is skipped", func(t *testing.T) {
		reason, _ := squadRuntimeRebindBlock(agentOf(""), ownerMember, privateRuntime, "codex", acpEffortPresent)
		if reason != "the agent owner has no access to this runtime" {
			t.Fatalf("unexpected reason %q", reason)
		}
	})

	t.Run("a model from another provider family is flagged for clearing", func(t *testing.T) {
		_, clearModel := squadRuntimeRebindBlock(agentOf("claude-opus-5-5"), ownerMember, publicRuntime, "codex", acpEffortPresent)
		if !clearModel {
			t.Fatal("claude model on a codex runtime must be cleared")
		}
	})

	t.Run("an unknown thinking level is skipped when the new provider cannot take it", func(t *testing.T) {
		a := agentOf("")
		a.ThinkingLevel = pgtype.Text{String: "high", Valid: true}
		// A provider with no thinking enum cannot take any level.
		if reason, _ := squadRuntimeRebindBlock(a, ownerMember, publicRuntime, "no-thinking-provider", acpEffortPresent); reason == "" {
			t.Fatal("expected a thinking_level rejection")
		}
	})
}
