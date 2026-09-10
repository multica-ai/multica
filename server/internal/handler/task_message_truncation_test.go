package handler

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// The truncation flag is tri-state on purpose. `false` is a measurement an
// upgraded daemon made; absent is the absence of one, which is all a
// historical row or an older installed daemon can say. Collapsing absent into
// false would make every row that predates the flag assert it is complete —
// the one claim this data cannot support, since the original length is gone.
func TestCreateTaskMessagesKeepsTruncationTriState(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	agentID := createHandlerTestAgent(t, "TaskMessageTruncation", []byte("[]"))
	taskID := dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id": handlerTestRuntimeID(t),
		"status":     "running",
	})

	yes, no := true, false
	messages := []TaskMessageRequest{
		{Seq: 1, Type: "tool_result", Tool: "bash", Output: "cut", OutputTruncated: &yes},
		{Seq: 2, Type: "tool_result", Tool: "bash", Output: "whole", OutputTruncated: &no},
		{Seq: 3, Type: "tool_result", Tool: "bash", Output: "unmeasured"},
	}

	params := db.CreateTaskMessagesParams{
		TaskID:            parseUUID(taskID),
		Ids:               make([]pgtype.UUID, 0, len(messages)),
		Seqs:              make([]int32, 0, len(messages)),
		Types:             make([]string, 0, len(messages)),
		Tools:             make([]string, 0, len(messages)),
		Contents:          make([]string, 0, len(messages)),
		Inputs:            make([]string, 0, len(messages)),
		Outputs:           make([]string, 0, len(messages)),
		OutputTruncations: make([]string, 0, len(messages)),
	}
	for _, msg := range messages {
		params.Ids = append(params.Ids, pgtype.UUID{Bytes: uuid.Must(uuid.NewV7()), Valid: true})
		params.Seqs = append(params.Seqs, int32(msg.Seq))
		params.Types = append(params.Types, msg.Type)
		params.Tools = append(params.Tools, msg.Tool)
		params.Contents = append(params.Contents, msg.Content)
		params.Inputs = append(params.Inputs, "")
		params.Outputs = append(params.Outputs, msg.Output)
		params.OutputTruncations = append(params.OutputTruncations, boolArrayElement(msg.OutputTruncated))
	}

	created, err := testHandler.Queries.CreateTaskMessages(ctx, params)
	if err != nil {
		t.Fatalf("CreateTaskMessages: %v", err)
	}
	if len(created) != len(messages) {
		t.Fatalf("inserted %d rows, want %d", len(created), len(messages))
	}

	// The column, not just the returned row: the text[] parameter has to reach
	// Postgres as a real NULL, which is what makes the third state storable.
	var nullCount int
	if err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM task_message WHERE task_id = $1 AND output_truncated IS NULL`,
		taskID).Scan(&nullCount); err != nil {
		t.Fatalf("count NULL output_truncated: %v", err)
	}
	if nullCount != 1 {
		t.Fatalf("rows stored with NULL output_truncated = %d, want 1 (only the unmeasured message)", nullCount)
	}

	want := []*bool{&yes, &no, nil}
	for i, row := range created {
		got := taskMessageToPayload(db.TaskMessage(row), taskID, "").OutputTruncated
		switch {
		case want[i] == nil && got != nil:
			t.Fatalf("seq %d: payload says truncated=%v, want omitted — an unmeasured output must not report a value", row.Seq, *got)
		case want[i] != nil && got == nil:
			t.Fatalf("seq %d: payload omits the flag, want %v", row.Seq, *want[i])
		case want[i] != nil && *got != *want[i]:
			t.Fatalf("seq %d: payload says truncated=%v, want %v", row.Seq, *got, *want[i])
		}
	}
}

func TestBoolArrayElement(t *testing.T) {
	yes, no := true, false
	// Empty string is the query's NULL, matching how every other nullable
	// column in this batch is encoded.
	if got := boolArrayElement(nil); got != "" {
		t.Fatalf("nil encodes as %q, want the empty string the query maps to NULL", got)
	}
	if got := boolArrayElement(&yes); got != "true" {
		t.Fatalf("true encodes as %q", got)
	}
	if got := boolArrayElement(&no); got != "false" {
		t.Fatalf("false encodes as %q", got)
	}
}
