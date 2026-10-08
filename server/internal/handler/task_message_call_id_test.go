package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func TestTaskMessageCallIDLiveAndHistory(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	taskID := seedBatchTask(t, "call-id")
	h := *testHandler
	h.Bus = events.New()
	var live []map[string]any
	h.Bus.Subscribe(protocol.EventTaskMessage, func(e events.Event) {
		if e.TaskID != taskID {
			return
		}
		data, err := json.Marshal(e.Payload)
		if err != nil {
			t.Error(err)
			return
		}
		var row map[string]any
		if err := json.Unmarshal(data, &row); err != nil {
			t.Error(err)
			return
		}
		live = append(live, row)
	})
	testutil.Call(t, h.ReportTaskMessages, batchMessagesRequest(t, taskID, []any{
		map[string]any{"seq": 1, "type": "tool_use", "tool": "Bash", "call_id": "attempt:A"},
		map[string]any{"seq": 2, "type": "tool_use", "tool": "Bash", "call_id": "attempt:B"},
		map[string]any{"seq": 3, "type": "tool_result", "tool": "Bash", "call_id": "attempt:B", "output": "B finished"},
		map[string]any{"seq": 4, "type": "tool_result", "tool": "Bash", "call_id": "attempt:A", "output": "A finished"},
		map[string]any{"seq": 5, "type": "tool_use", "tool": "Read"},
	})).Want(http.StatusOK)
	if len(live) != 5 {
		t.Fatalf("got %d live events, want 5", len(live))
	}
	for i, want := range []string{"attempt:A", "attempt:B", "attempt:B", "attempt:A"} {
		if live[i]["call_id"] != want {
			t.Errorf("live row %d call_id = %v, want %s", i, live[i]["call_id"], want)
		}
	}
	if _, ok := live[4]["call_id"]; ok {
		t.Error("legacy message must omit call_id")
	}
	var legacyNull bool
	dbfx.QueryRow(t, `SELECT call_id IS NULL FROM task_message WHERE task_id=$1 AND seq=5`, taskID).Scan(&legacyNull)
	if !legacyNull {
		t.Error("missing call_id must persist as NULL")
	}

	// Both readers and incremental reconnects must agree with the live payload.
	for _, reader := range []struct {
		name    string
		handler http.HandlerFunc
	}{
		{"daemon", h.ListTaskMessages}, {"user", h.ListTaskMessagesByUser},
	} {
		for _, suffix := range []string{"", "?since=2"} {
			t.Run(reader.name+suffix, func(t *testing.T) {
				req := testutil.JSONRequest(http.MethodGet, "/api/tasks/"+taskID+"/messages"+suffix, nil)
				req = testutil.WithURLParams(req, "taskId", taskID)
				ctx := middleware.WithDaemonContext(req.Context(), testWorkspaceID, "call-id-daemon")
				ctx = middleware.SetMemberContext(ctx, testWorkspaceID, db.Member{})
				var history []map[string]any
				testutil.Call(t, reader.handler, req.WithContext(ctx)).Want(http.StatusOK).JSON(&history)
				want := live
				if suffix != "" {
					want = live[2:]
				}
				if !reflect.DeepEqual(history, want) {
					t.Fatalf("history differs from live: got %+v want %+v", history, want)
				}
			})
		}
	}
	// The single-row writer is also used outside daemon batch ingestion.
	row, err := h.Queries.CreateTaskMessage(context.Background(), db.CreateTaskMessageParams{
		ID:     pgtype.UUID{Bytes: uuid.Must(uuid.NewV7()), Valid: true},
		TaskID: util.MustParseUUID(taskID), Seq: 6, Type: "tool_result",
		CallID: pgtype.Text{String: "single-call", Valid: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if row.CallID.String != "single-call" {
		t.Fatalf("single writer lost call_id: %+v", row)
	}
}
