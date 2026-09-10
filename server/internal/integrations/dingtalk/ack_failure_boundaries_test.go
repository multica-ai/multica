package dingtalk

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func TestAckBatchLookupFailuresKeepExistingReceipt(t *testing.T) {
	for _, failureRead := range []int{2, 3} {
		name := "candidate lookup"
		if failureRead == 3 {
			name = "current input recheck"
		}
		t.Run(name, func(t *testing.T) {
			n, actions := newTestAckWithMessageIDs(time.Now)
			inst, sid := engine.ResolvedInstallation{ID: sessionUUID(90)}, sessionUUID(91)
			q := &ackInputQueries{rows: make(map[pgtype.UUID]db.ChatMessage)}
			n.inputs = q
			for i, name := range []string{"existing", "new"} {
				id := sessionUUID(byte(92 + i))
				q.rows[id] = db.ChatMessage{ID: id, ChatSessionID: sid, ChannelIngested: true, Role: "user", CreatedAt: pgtype.Timestamptz{Time: time.Unix(int64(i), 0), Valid: true}}
				n.client.rememberReplySource(inst.ID, id, groupReactionMessage(name))
			}
			n.OnIngested(context.Background(), inst, groupReactionMessage("existing"), sid)
			reads := 0
			q.read = func(pgtype.UUID) {
				reads++
				if reads == failureRead {
					q.err = errors.New("database unavailable")
				}
			}
			n.OnIngested(context.Background(), inst, groupReactionMessage("new"), sid)
			if reads != failureRead || !slices.Equal(*actions, []string{"add:existing:" + emotionAcknowledged}) {
				t.Fatalf("failed lookup moved receipt: reads=%d actions=%v", reads, *actions)
			}
			// A failed optional lookup must not poison the next accepted hook.
			q.read, q.err = nil, nil
			n.OnIngested(context.Background(), inst, groupReactionMessage("new"), sid)
			want := []string{"add:existing:" + emotionAcknowledged, "recall:existing:" + emotionAcknowledged, "add:new:" + emotionAcknowledged}
			if !slices.Equal(*actions, want) {
				t.Fatalf("recovered lookup: actions=%v want=%v", *actions, want)
			}
		})
	}
}

type terminalOwnerQueries struct {
	noDeliveryOutboundQueries
	task      db.AgentTaskQueue
	taskErr   error
	taskReads int
}

func (q *terminalOwnerQueries) GetAgentTask(context.Context, pgtype.UUID) (db.AgentTaskQueue, error) {
	q.taskReads++
	return q.task, q.taskErr
}

func TestOutboundTerminalOwnerFailureKeepsReceipts(t *testing.T) {
	sid, taskID := sessionUUID(91), sessionUUID(94)
	for _, tc := range []struct {
		name string
		task db.AgentTaskQueue
		err  error
	}{
		{"task lookup failure", db.AgentTaskQueue{}, errors.New("database unavailable")},
		{"missing input owner", db.AgentTaskQueue{ChatSessionID: sid}, nil},
		{"different session", db.AgentTaskQueue{ChatSessionID: sessionUUID(95), ChatInputTaskID: taskID}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n, actions := newTestAckWithMessageIDs(time.Now)
			inst := engine.ResolvedInstallation{ID: sessionUUID(90)}
			for i, name := range []string{"existing", "new"} {
				n.client.rememberReplySource(inst.ID, sessionUUID(byte(92+i)), groupReactionMessage(name))
				n.OnIngested(context.Background(), inst, groupReactionMessage(name), sid)
			}
			before := append([]string(nil), (*actions)...)
			q := &terminalOwnerQueries{task: tc.task, taskErr: tc.err}
			o := NewOutbound(q, nil, n.client, n, nil)
			err := o.processEvent(context.Background(), events.Event{Type: protocol.EventTaskCancelled, TaskID: util.UUIDToString(taskID), ChatSessionID: util.UUIDToString(sid)})
			if err != nil || q.taskReads != 1 || !slices.Equal(*actions, before) {
				t.Fatalf("unknown owner changed receipts: err=%v reads=%d actions=%v", err, q.taskReads, *actions)
			}
			if len(n.active[util.UUIDToString(sid)]) != 2 {
				t.Fatal("unknown owner discarded local anchors")
			}
		})
	}
}
