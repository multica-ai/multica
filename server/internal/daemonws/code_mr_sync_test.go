package daemonws

import (
	"encoding/json"
	"testing"

	"github.com/multica-ai/multica/server/pkg/protocol"
)

func TestCodeMRSyncFramePreservesStructuredArguments(t *testing.T) {
	want := protocol.CodeMRSyncPayload{
		RuntimeID:             "runtime-1",
		ExternalPullRequestID: "6b19bbf3-2451-442c-a23c-bc9efc948aca",
		RepositoryPath:        "base-biz/agentworks-python",
		ReviewNumber:          28981841,
	}
	frame, err := codeMRSyncFrame(want)
	if err != nil {
		t.Fatalf("codeMRSyncFrame: %v", err)
	}
	var msg protocol.Message
	if err := json.Unmarshal(frame, &msg); err != nil {
		t.Fatalf("decode frame: %v", err)
	}
	if msg.Type != protocol.EventDaemonCodeMRSync {
		t.Fatalf("message type = %q", msg.Type)
	}
	var got protocol.CodeMRSyncPayload
	if err := json.Unmarshal(msg.Payload, &got); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if got != want {
		t.Fatalf("payload = %+v, want %+v", got, want)
	}
}
