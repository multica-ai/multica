package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// GH #8845 / #9014: a formatted agent reply must reach storage byte-for-byte,
// while a reply that arrived as one escaped blob still gets its paragraph
// breaks back (GH #1820).
var taskOutputBackslashCases = []struct {
	name   string
	output string
	want   string
}{
	{
		name:   "latex",
		output: "Formula:\n$a \\times b$, $\\theta$, $\\text{hi}$, $A^\\top$, $n \\neq 1$, $\\rho$",
		want:   "Formula:\n$a \\times b$, $\\theta$, $\\text{hi}$, $A^\\top$, $n \\neq 1$, $\\rho$",
	},
	{
		name:   "windows path",
		output: "| Path |\n| --- |\n| `C:\\tools\\repo\\node_modules` |",
		want:   "| Path |\n| --- |\n| `C:\\tools\\repo\\node_modules` |",
	},
	{
		name:   "quoted escapes",
		output: "Use `\\n` for a newline\nand `\\t` for a tab",
		want:   "Use `\\n` for a newline\nand `\\t` for a tab",
	},
	{
		name:   "single line latex",
		output: `$a \times b \neq 0$ and $n \neq -1$`,
		want:   `$a \times b \neq 0$ and $n \neq -1$`,
	},
	{
		name:   "single line path",
		output: `Error: cannot read C:\tools\node_modules\repo`,
		want:   `Error: cannot read C:\tools\node_modules\repo`,
	},
	{
		name:   "escaped blob",
		output: `First paragraph with $a \times b$.\n\nSecond paragraph.`,
		want:   "First paragraph with $a \\times b$.\n\nSecond paragraph.",
	},
}

func TestWriteChatCompletionOutcomeBackslashes(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	workspaceID, userID, agentID, _ := seedAttributionFixture(t, pool)
	svc := &TaskService{Queries: q, TxStarter: pool}

	for _, tc := range taskOutputBackslashCases {
		t.Run(tc.name, func(t *testing.T) {
			var chatSessionID, taskID string
			if err := pool.QueryRow(ctx, `
				INSERT INTO chat_session (workspace_id, agent_id, creator_id)
				VALUES ($1, $2, $3) RETURNING id`, workspaceID, agentID, userID).Scan(&chatSessionID); err != nil {
				t.Fatalf("seed chat session: %v", err)
			}
			t.Cleanup(func() {
				pool.Exec(context.Background(), `DELETE FROM chat_message WHERE chat_session_id = $1`, chatSessionID)
				pool.Exec(context.Background(), `DELETE FROM chat_session WHERE id = $1`, chatSessionID)
			})
			if err := pool.QueryRow(ctx, `SELECT gen_random_uuid()::text`).Scan(&taskID); err != nil {
				t.Fatalf("new task id: %v", err)
			}

			result, _ := json.Marshal(protocol.TaskCompletedPayload{Output: tc.output})
			row, err := svc.writeChatCompletionOutcome(ctx, q, db.AgentTaskQueue{
				ID:            util.MustParseUUID(taskID),
				ChatSessionID: util.MustParseUUID(chatSessionID),
				AgentID:       util.MustParseUUID(agentID),
			}, result)
			if err != nil {
				t.Fatalf("writeChatCompletionOutcome: %v", err)
			}
			if row == nil {
				t.Fatal("writeChatCompletionOutcome wrote no row")
			}
			var got string
			if err := pool.QueryRow(ctx, `SELECT content FROM chat_message WHERE id = $1`, row.ID).Scan(&got); err != nil {
				t.Fatalf("read chat message: %v", err)
			}
			if got != tc.want {
				t.Errorf("stored chat content = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestCompleteTaskFallbackCommentBackslashes(t *testing.T) {
	f, s, issue, agent := conditionFixture(t)
	ctx := context.Background()
	f.Cleanup(t, "DELETE FROM comment WHERE issue_id=$1", issue)

	for _, tc := range taskOutputBackslashCases {
		t.Run(tc.name, func(t *testing.T) {
			output, _ := json.Marshal(protocol.TaskCompletedPayload{Output: tc.output})
			taskID := parseTestUUID(t, f.Task(t, agent, testutil.Cols{
				"issue_id":   issue,
				"status":     "running",
				"started_at": testutil.Raw("now()"),
				"context":    "{}",
				"runtime_id": testutil.Raw("(SELECT runtime_id FROM agent WHERE id='" + agent + "')"),
			}))
			if _, err := s.Tasks.CompleteTask(ctx, taskID, output, "", "", "", false, "", ""); err != nil {
				t.Fatalf("CompleteTask: %v", err)
			}
			var got string
			f.QueryRow(t, "SELECT content FROM comment WHERE issue_id=$1 AND source_task_id=$2", issue, taskID).Scan(&got)
			if got != tc.want {
				t.Errorf("stored fallback comment = %q, want %q", got, tc.want)
			}
		})
	}
}
