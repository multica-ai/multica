package handler

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/multica-ai/multica/server/internal/testutil"
)

type failAgentSkillTouchTxStarter struct {
	inner txStarter
}

func (s failAgentSkillTouchTxStarter) Begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := s.inner.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return failAgentSkillTouchTx{Tx: tx}, nil
}

type failAgentSkillTouchTx struct {
	pgx.Tx
}

func (tx failAgentSkillTouchTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if strings.Contains(sql, "TouchAgentForSkillChange") {
		return pgconn.CommandTag{}, errors.New("injected agent timestamp touch failure")
	}
	return tx.Tx.Exec(ctx, sql, args...)
}

// Run the relation writes against PostgreSQL, failing only the later touch or
// commit. Both failures must leave the assignment and last-modified time intact.
func TestAgentSkillToggleAndRemoveRollback(t *testing.T) {
	for _, operation := range []string{"toggle", "remove"} {
		for _, failure := range []string{"touch", "commit"} {
			t.Run(operation+"/"+failure, func(t *testing.T) {
				agentID := createHandlerTestAgent(t, "Handler Skill Rollback", nil)
				skillID := insertHandlerTestSkill(t, "rollback", "skill body")
				dbfx.InsertNoID(t, "agent_skill", testutil.Cols{
					"agent_id": agentID, "skill_id": skillID, "enabled": true,
				}, "agent_id = $1 AND skill_id = $2", agentID, skillID)
				past := time.Date(2000, time.January, 1, 0, 0, 0, 0, time.UTC)
				dbfx.Exec(t, `UPDATE agent SET updated_at = $2 WHERE id = $1`, agentID, past)

				h := *testHandler
				wantError := "failed to update agent timestamp"
				if failure == "touch" {
					h.TxStarter = failAgentSkillTouchTxStarter{inner: testHandler.TxStarter}
				} else {
					h.TxStarter = rollbackOnCommitTxStarter{pool: testPool}
					wantError = "failed to commit"
				}
				req := newRequest("PUT", "/api/agents/"+agentID+"/skills/"+skillID+"/enabled", map[string]any{"enabled": false})
				handle := h.SetAgentSkillEnabled
				if operation == "remove" {
					req = newRequest("DELETE", "/api/agents/"+agentID+"/skills/"+skillID, nil)
					handle = h.RemoveAgentSkill
				}
				req = withURLParams(req, "id", agentID, "skillId", skillID)
				response := testutil.Call(t, handle, req).Want(http.StatusInternalServerError)
				if !strings.Contains(response.Body.String(), wantError) {
					t.Fatalf("response = %s, want %q", response.Body.String(), wantError)
				}

				var enabled bool
				dbfx.QueryRow(t, `SELECT enabled FROM agent_skill WHERE agent_id = $1 AND skill_id = $2`, agentID, skillID).Scan(&enabled)
				if !enabled {
					t.Fatal("failed mutation persisted a disabled assignment")
				}
				var updatedAt time.Time
				dbfx.QueryRow(t, `SELECT updated_at FROM agent WHERE id = $1`, agentID).Scan(&updatedAt)
				if !updatedAt.Equal(past) {
					t.Fatalf("failed mutation changed agent.updated_at: got %s, want %s", updatedAt, past)
				}
			})
		}
	}
}
