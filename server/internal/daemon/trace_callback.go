package daemon

import (
	"context"
	"time"

	"github.com/multica-ai/multica/server/internal/daemon/trace"
	"github.com/multica-ai/multica/server/pkg/agent"
)

func newTraceRunID(t time.Time) string {
	return t.UTC().Format("20060102T150405.000000000Z")
}

// BuildTraceCallback adapts provider-level trace events into the daemon-local
// JSONL trace store. A nil store disables tracing without changing agent
// execution behaviour.
//
// When streamDisplay is true, the callback suppresses raw_stdout and
// provider_event channels so the user-visible trace only contains
// display_event and approval channels — the standardised output layer.
func BuildTraceCallback(store trace.TraceStore, taskID, runID, providerName string, streamDisplay bool) agent.TraceCallback {
	if store == nil || taskID == "" || runID == "" {
		return nil
	}
	return func(channel, content, rawPayload string) {
		if channel == "" {
			channel = trace.ChannelNormalized
		}
		if streamDisplay && (channel == trace.ChannelRawStdout || channel == trace.ChannelProviderEvent) {
			return
		}
		_, _ = store.Append(context.Background(), trace.TraceLine{
			TaskID:     taskID,
			RunID:      runID,
			Provider:   providerName,
			Channel:    channel,
			Content:    content,
			RawPayload: rawPayload,
		})
	}
}
