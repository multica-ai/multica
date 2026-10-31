package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func TestWriteChatCompletionOutcomePreservesBackslashes(t *testing.T) {
	pool := sharedTestPool(t)
	ctx := context.Background()
	q := db.New(pool)
	workspaceID, userID, agentID, _ := seedAttributionFixture(t, pool)
	fx := testutil.New(pool, workspaceID, userID)
	svc := &TaskService{Queries: q}

	for _, tc := range []struct {
		name   string
		output string
	}{
		{"latex", `\times \theta \text{hello} \top`},
		{"literal escapes", `\n \r \t \\ \d \w`},
		{"windows path", `C:\notes\reports\test.txt`},
		{"decoded whitespace", "first paragraph\n\nsecond paragraph\r\n\tindented"},
		{"mixed", "Formula:\n" + `a \times b` + "\nLiteral: " + `\n`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			chatSessionID := fx.ChatSession(t, agentID)
			fx.Cleanup(t, `DELETE FROM chat_message WHERE chat_session_id = $1`, chatSessionID)
			result, err := json.Marshal(protocol.TaskCompletedPayload{Output: tc.output})
			if err != nil {
				t.Fatal(err)
			}
			row, err := svc.writeChatCompletionOutcome(ctx, q, db.AgentTaskQueue{
				ID:            dbid.NewV7(),
				ChatSessionID: util.MustParseUUID(chatSessionID),
				AgentID:       util.MustParseUUID(agentID),
			}, result)
			if err != nil {
				t.Fatalf("writeChatCompletionOutcome: %v", err)
			}
			if row == nil {
				t.Fatal("writeChatCompletionOutcome wrote no row")
			}
			var content string
			fx.QueryRow(t, `SELECT content FROM chat_message WHERE id = $1`, row.ID).Scan(&content)
			if content != tc.output {
				t.Errorf("stored content = %q, want %q", content, tc.output)
			}
		})
	}
}
