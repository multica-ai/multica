package facadeimpl

import (
	"testing"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func TestChannelTurnOutput(t *testing.T) {
	t.Parallel()

	got, err := taskCompletionOutput(db.AgentTaskQueue{
		Result: []byte(`{"output":"STA-1 已更新。"}`),
	}, "channel turn")
	if err != nil {
		t.Fatal(err)
	}
	if got != "STA-1 已更新。" {
		t.Fatalf("output = %q", got)
	}
}

func TestChannelTurnOutput_EmptyIsError(t *testing.T) {
	t.Parallel()

	if _, err := taskCompletionOutput(db.AgentTaskQueue{Result: []byte(`{"output":""}`)}, "channel turn"); err == nil {
		t.Fatal("expected empty output error")
	}
}

func TestRuntimeSupportsChannelTurn(t *testing.T) {
	t.Parallel()

	if !runtimeSupports(db.AgentRuntime{
		Metadata: []byte(`{"capabilities":["channel_turn"]}`),
	}, protocol.DaemonCapabilityChannelTurn) {
		t.Fatal("expected capability to be accepted")
	}
	if runtimeSupports(db.AgentRuntime{
		Metadata: []byte(`{"capabilities":["other"]}`),
	}, protocol.DaemonCapabilityChannelTurn) {
		t.Fatal("unexpected support without channel_turn capability")
	}
	if runtimeSupports(db.AgentRuntime{
		Metadata: []byte(`{"cli_version":"999.0.0"}`),
	}, protocol.DaemonCapabilityChannelTurn) {
		t.Fatal("version alone must not imply channel_turn support")
	}
}
