package daemon

// CEREBRO-PATCH(run-prompt-snapshot): FIR-3212 — daemon-side upload of the
// per-run production prompt snapshot. Plain postJSON, no retry schedule: the
// server insert is idempotent (first write wins), but the snapshot is
// best-effort evidence and must never delay or fail a run — the caller fires
// it in a goroutine and only logs failures.

import (
	"context"
	"fmt"
)

func (c *Client) ReportTaskPromptSnapshot(ctx context.Context, snapshot PromptSnapshot) error {
	return c.postJSON(ctx, fmt.Sprintf("/api/daemon/tasks/%s/prompt-snapshot", snapshot.TaskID), snapshot, nil)
}
