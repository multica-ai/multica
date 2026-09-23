package handler

import (
	"net/http"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/interaction"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/pkg/agent"
)

func TestCompleteCurrentRunScopesIntentAndWaitsForAcknowledgement(t *testing.T) {
	runtime, worker, _, task := interactiveFixture(t)
	sync := interaction.Sync{Owner: "process", Register: true, Deadline: time.Now().Add(time.Hour), State: agent.InteractionSnapshot{State: agent.InteractionWorking, Activity: 1}}
	testutil.Call(t, testHandler.SyncTaskInteraction, interactionRequest(t, task, runtime, "POST", sync)).Want(200)
	request := func() *http.Request {
		r := interactionRequest(t, task, runtime, "POST", nil)
		r.Header.Set("X-Actor-Source", "task_token")
		r.Header.Set("X-Task-ID", task)
		r.Header.Set("X-Agent-ID", worker)
		return r
	}
	for _, header := range []string{"X-Actor-Source", "X-Task-ID", "X-Agent-ID"} {
		r := request()
		r.Header.Set(header, "wrong")
		testutil.Call(t, testHandler.CompleteCurrentRun, r).Want(403)
	}
	for i := 0; i < 2; i++ {
		testutil.Call(t, testHandler.CompleteCurrentRun, request()).Want(202)
	}
	var record interaction.Record
	testutil.Call(t, testHandler.GetTaskInteraction, interactionRequest(t, task, runtime, "GET", nil)).Want(200).JSON(&record)
	if len(record.Commands) != 1 || record.Commands[0].Kind != "complete" || record.Finishing {
		t.Fatalf("invalid completion intent: %+v", record)
	}
	sync.Register = false
	sync.Finish = true
	testutil.Call(t, testHandler.SyncTaskInteraction, interactionRequest(t, task, runtime, "POST", sync)).Want(200).JSON(&record)
	if record.Finishing {
		t.Fatal("unacknowledged completion crossed finish barrier")
	}
	sync.Finish = false
	sync.Acknowledgement = &interaction.Input{InteractionCommand: record.Commands[0].InteractionCommand, Receipt: &agent.InteractionReceipt{Outcome: "applied", Snapshot: sync.State}}
	testutil.Call(t, testHandler.SyncTaskInteraction, interactionRequest(t, task, runtime, "POST", sync)).Want(200)
	input := agent.InteractionCommand{ID: "human-correction", Kind: "input", Activity: 1, Text: "one more change"}
	testutil.Call(t, testHandler.SendTaskInteraction, interactionRequest(t, task, runtime, "POST", map[string]any{"owner": "process", "id": input.ID, "kind": input.Kind, "activity": 1, "text": input.Text})).Want(202)
	sync.Acknowledgement = &interaction.Input{InteractionCommand: input, Receipt: &agent.InteractionReceipt{Outcome: "applied", Snapshot: sync.State}}
	testutil.Call(t, testHandler.SyncTaskInteraction, interactionRequest(t, task, runtime, "POST", sync)).Want(200)
	testutil.Call(t, testHandler.CompleteCurrentRun, request()).Want(202)
	testutil.Call(t, testHandler.GetTaskInteraction, interactionRequest(t, task, runtime, "GET", nil)).Want(200).JSON(&record)
	if len(record.Commands) != 3 || record.Commands[2].ID == record.Commands[0].ID {
		t.Fatal("correction prevented a fresh completion request")
	}
	dbfx.Exec(t, "UPDATE agent_task_queue SET status='completed' WHERE id=$1", task)
	testutil.Call(t, testHandler.CompleteCurrentRun, request()).Want(409)
}
