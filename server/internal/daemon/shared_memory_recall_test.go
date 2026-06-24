package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/recall"
)

func TestRunRecallWithTimeoutNeverBlocksAgentLaunch(t *testing.T) {
	t.Parallel()

	blocked := make(chan struct{})
	started := time.Now()
	result := runRecallWithTimeout(context.Background(), 10*time.Millisecond, func() recall.Result {
		<-blocked
		return recall.Result{Status: recall.StatusHit}
	})
	close(blocked)

	if elapsed := time.Since(started); elapsed > 200*time.Millisecond {
		t.Fatalf("timed recall blocked for %s", elapsed)
	}
	if result.Status != recall.StatusControlledError || result.Reason != "recall_timeout" {
		t.Fatalf("timeout result = %#v", result)
	}
}

func TestPrependRecallBundleMakesStatusVisibleBeforeTaskPrompt(t *testing.T) {
	t.Parallel()

	result := recall.Result{
		Status: recall.StatusNoHit, HitCount: 0, Query: "issue title", IndexVersion: 1,
		ByteBudget: 4096, Reason: "index_missing", Hits: []recall.Hit{},
	}
	prompt := prependRecallBundle("original task prompt", &result)

	if !strings.HasPrefix(prompt, "<shared-memory-recall>\n") {
		t.Fatalf("prompt does not start with recall bundle: %s", prompt)
	}
	if !strings.Contains(prompt, `"recall_status":"no_hit"`) || !strings.Contains(prompt, `"reason":"index_missing"`) {
		t.Fatalf("prompt does not expose recall status: %s", prompt)
	}
	if !strings.HasSuffix(prompt, "original task prompt") {
		t.Fatalf("original prompt missing: %s", prompt)
	}
}

func TestWriteRecallLogAppendsOneBoundedRecordPerRun(t *testing.T) {
	t.Parallel()

	logPath := filepath.Join(t.TempDir(), "logs", "recall.jsonl")
	now := time.Date(2026, time.June, 24, 9, 0, 0, 0, time.UTC)
	result := recall.Result{
		Status: recall.StatusHit, HitCount: 1, Query: "secret issue text", IndexVersion: 1,
		ByteBudget: 4096, BytesUsed: 512, Hits: []recall.Hit{{Path: "notes/a.md"}},
		SkippedFiles: []string{"notes/unreadable.md"},
	}
	if err := writeRecallLog(logPath, Task{ID: "task-1", IssueID: "issue-1"}, result, now); err != nil {
		t.Fatalf("writeRecallLog() error = %v", err)
	}
	if err := writeRecallLog(logPath, Task{ID: "task-2", IssueID: "issue-1"}, result, now.Add(time.Minute)); err != nil {
		t.Fatalf("second writeRecallLog() error = %v", err)
	}

	file, err := os.Open(logPath)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	var records []map[string]any
	for scanner.Scan() {
		var record map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			t.Fatalf("invalid JSONL record: %v", err)
		}
		records = append(records, record)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 {
		t.Fatalf("record count = %d, want 2", len(records))
	}
	first := records[0]
	if first["status"] != "hit" || first["task_id"] != "task-1" || first["issue_id"] != "issue-1" {
		t.Fatalf("unexpected record: %#v", first)
	}
	if first["query_hash"] == "" || first["query_hash"] == result.Query {
		t.Fatalf("query_hash must be non-empty and irreversible: %#v", first["query_hash"])
	}
	if _, exists := first["query"]; exists {
		t.Fatalf("raw query leaked into log: %#v", first)
	}
}
