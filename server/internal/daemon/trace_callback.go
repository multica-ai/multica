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
func BuildTraceCallback(store trace.TraceStore, taskID, runID, providerName string) agent.TraceCallback {
	if store == nil || taskID == "" || runID == "" {
		return nil
	}
	return func(channel, content, rawPayload string) {
		if channel == "" {
			channel = trace.ChannelNormalized
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
