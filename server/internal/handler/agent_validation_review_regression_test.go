package handler

import (
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func validationReviewMember(t *testing.T) string {
	t.Helper()
	id := dbfx.Insert(t, "user", testutil.Cols{"name": "Validation review", "email": "validation-review-" + uuid.NewString() + "@multica.ai"})
	dbfx.Member(t, testWorkspaceID, id, "member")
	return id
}

func TestAgentValidationReviewTaskStatusDoesNotRejectEdit(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	for _, provider := range []string{"claude", "kagent"} {
		t.Run(provider, func(t *testing.T) {
			t.Setenv("MULTICA_KAGENT_ENABLED", "1")
			runtimeID := dbfx.Runtime(t, "Validation review "+provider, testutil.Cols{"provider": provider})
			agentID := ""
			if provider == "kagent" {
				agentID = lifecycleCreate(t, testHandler, runtimeID, "Validation review kagent").ID
			} else {
				agentID = dbfx.Agent(t, "Validation review claude", runtimeID)
			}
			dbfx.Exec(t, `UPDATE agent SET status='idle' WHERE id=$1`, agentID)
			dbfx.Task(t, agentID, testutil.Cols{"status": "running", "runtime_id": runtimeID})
			before, err := testHandler.Queries.GetAgent(t.Context(), parseUUID(agentID))
			if err != nil {
				t.Fatal(err)
			}
			h := *testHandler
			h.TxStarter = hookedTxStarter{base: testHandler.TxStarter, hook: func() error {
				refreshed, err := testHandler.Queries.RefreshAgentStatusFromTasks(t.Context(), parseUUID(agentID))
				if err != nil {
					return err
				}
				if refreshed.Status != "working" || refreshed.UpdatedAt == before.UpdatedAt {
					t.Fatal("status refresh did not exercise timestamp transition")
				}
				return nil
			}}
			testutil.Call(t, lifecycleRouter(&h).ServeHTTP, newRequest("PUT", "/api/agents/"+agentID, map[string]any{"instructions": "saved during status transition"})).Want(http.StatusOK)
			after, err := h.Queries.GetAgent(t.Context(), parseUUID(agentID))
			if err != nil {
				t.Fatal(err)
			}
			if after.Status != "working" || after.Instructions != "saved during status transition" {
				t.Fatalf("edit lost status or instructions: %+v", after)
			}
			if provider == "kagent" {
				enrollment, err := h.Queries.GetKagentEnrollment(t.Context(), db.GetKagentEnrollmentParams{AgentID: after.ID, RuntimeID: after.RuntimeID, WorkspaceID: after.WorkspaceID})
				if err != nil {
					t.Fatal(err)
				}
				if enrollment.Generation != 2 {
					t.Fatalf("generation=%d, want 2", enrollment.Generation)
				}
			}
		})
	}
}

// A concurrent configuration write remains a conflict even when updated_at
// happens to be unchanged; status timestamps are not a configuration revision.
func TestAgentValidationReviewRejectsChangedConfiguration(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	for _, field := range []string{"runtime_config", "custom_args", "mcp_config", "model", "thinking_level", "service_tier", "instructions", "runtime_id", "owner_id", "archived_at"} {
		t.Run(field, func(t *testing.T) {
			runtimeID := dbfx.Runtime(t, "Validation stale runtime", testutil.Cols{"provider": "claude"})
			agentID := dbfx.Agent(t, "Validation stale agent", runtimeID)
			value := any("")
			switch field {
			case "runtime_config":
				value = `{"concurrent":true}`
			case "custom_args":
				value = `["--verbose"]`
			case "mcp_config":
				value = `{"mcpServers":{}}`
			case "model":
				value = "concurrent-model"
			case "thinking_level":
				value = "high"
			case "service_tier":
				value = "fast"
			case "instructions":
				value = "concurrent instructions"
			case "runtime_id":
				value = dbfx.Runtime(t, "Concurrent runtime", testutil.Cols{"provider": "claude"})
			case "owner_id":
				value = validationReviewMember(t)
			case "archived_at":
				value = time.Date(2026, time.November, 18, 0, 0, 0, 0, time.UTC)
			}
			h := *testHandler
			h.TxStarter = hookedTxStarter{base: testHandler.TxStarter, hook: func() error {
				_, err := testPool.Exec(t.Context(), `UPDATE agent SET `+field+`=$2 WHERE id=$1`, agentID, value)
				return err
			}}
			testutil.Call(t, lifecycleRouter(&h).ServeHTTP, newRequest("PUT", "/api/agents/"+agentID, map[string]any{"description": "stale edit"})).Want(http.StatusConflict)
			var description string
			dbfx.QueryRow(t, `SELECT description FROM agent WHERE id=$1`, agentID).Scan(&description)
			if description == "stale edit" {
				t.Fatal("stale configuration was saved")
			}
		})
	}
}

func TestAgentValidationReviewRechecksRuntimeAccess(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	owner := validationReviewMember(t)
	current := dbfx.Runtime(t, "Validation current runtime", testutil.Cols{"provider": "claude"})
	target := dbfx.Runtime(t, "Validation revoked runtime", testutil.Cols{"provider": "claude", "owner_id": owner, "visibility": "public"})
	agentID := dbfx.Agent(t, "Validation runtime access", current)
	h := *testHandler
	h.TxStarter = hookedTxStarter{base: testHandler.TxStarter, hook: func() error {
		_, err := testPool.Exec(t.Context(), `UPDATE agent_runtime SET visibility='private',shared_with='{}' WHERE id=$1`, target)
		return err
	}}
	testutil.Call(t, lifecycleRouter(&h).ServeHTTP, newRequest("PUT", "/api/agents/"+agentID, map[string]any{"runtime_id": target})).Want(http.StatusForbidden)
	var runtimeID string
	dbfx.QueryRow(t, `SELECT runtime_id FROM agent WHERE id=$1`, agentID).Scan(&runtimeID)
	if runtimeID != current {
		t.Fatal("stale runtime access saved a binding")
	}
}
