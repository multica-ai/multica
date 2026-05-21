package trace

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// testLine is a helper to build a minimal TraceLine for tests.
func testLine(taskID, runID, channel, content string) TraceLine {
	return TraceLine{
		TaskID:  taskID,
		RunID:   runID,
		Channel: channel,
		Content: content,
	}
}

func TestAppendAndRead(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	defer store.Close()

	seq1, err := store.Append(ctx, testLine("task-1", "run-1", ChannelNormalized, "hello"))
	if err != nil {
		t.Fatalf("Append: %v", err)
	}
	if seq1 != 1 {
		t.Fatalf("expected seq=1, got %d", seq1)
	}

	seq2, err := store.Append(ctx, testLine("task-1", "run-1", ChannelNormalized, "world"))
	if err != nil {
		t.Fatalf("Append: %v", err)
	}
	if seq2 != 2 {
		t.Fatalf("expected seq=2, got %d", seq2)
	}

	lines, err := store.ListSince(ctx, "task-1", "run-1", 0)
	if err != nil {
		t.Fatalf("ListSince: %v", err)
	}
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}
	if lines[0].Content != "hello" || lines[1].Content != "world" {
		t.Fatalf("unexpected content order: %+v", lines)
	}
}

func TestListSince(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	defer store.Close()

	for i := 1; i <= 5; i++ {
		content := strings.Repeat("x", i)
		_, err := store.Append(ctx, testLine("task-1", "run-1", ChannelNormalized, content))
		if err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
	}

	// ListSince with afterSeq=0 returns everything.
	all, err := store.ListSince(ctx, "task-1", "run-1", 0)
	if err != nil {
		t.Fatalf("ListSince(0): %v", err)
	}
	if len(all) != 5 {
		t.Fatalf("expected 5 lines from seq=0, got %d", len(all))
	}

	// ListSince with afterSeq=2 returns seq 3,4,5.
	after2, err := store.ListSince(ctx, "task-1", "run-1", 2)
	if err != nil {
		t.Fatalf("ListSince(2): %v", err)
	}
	if len(after2) != 3 {
		t.Fatalf("expected 3 lines after seq=2, got %d", len(after2))
	}
	if after2[0].Seq != 3 || after2[2].Seq != 5 {
		t.Fatalf("unexpected seqs: %+v", after2)
	}

	// ListSince with afterSeq=5 returns nothing.
	after5, err := store.ListSince(ctx, "task-1", "run-1", 5)
	if err != nil {
		t.Fatalf("ListSince(5): %v", err)
	}
	if len(after5) != 0 {
		t.Fatalf("expected 0 lines after seq=5, got %d", len(after5))
	}
}

func TestTail(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	defer store.Close()

	for i := 1; i <= 10; i++ {
		_, err := store.Append(ctx, testLine("task-1", "run-1", ChannelNormalized, "line"))
		if err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
	}

	// Tail with n=3 returns last 3 lines.
	tail3, err := store.Tail(ctx, "task-1", "run-1", 3)
	if err != nil {
		t.Fatalf("Tail(3): %v", err)
	}
	if len(tail3) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(tail3))
	}
	if tail3[0].Seq != 8 || tail3[2].Seq != 10 {
		t.Fatalf("expected seqs 8-10, got %+v", tail3)
	}

	// Tail with n=0 returns all.
	all, err := store.Tail(ctx, "task-1", "run-1", 0)
	if err != nil {
		t.Fatalf("Tail(0): %v", err)
	}
	if len(all) != 10 {
		t.Fatalf("expected 10 lines, got %d", len(all))
	}

	// Tail with n=100 (larger than count) returns all.
	big, err := store.Tail(ctx, "task-1", "run-1", 100)
	if err != nil {
		t.Fatalf("Tail(100): %v", err)
	}
	if len(big) != 10 {
		t.Fatalf("expected 10 lines, got %d", len(big))
	}
}

func TestAppendDifferentRuns(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	defer store.Close()

	_, err := store.Append(ctx, testLine("task-1", "run-1", ChannelNormalized, "run1"))
	if err != nil {
		t.Fatalf("Append run1: %v", err)
	}
	_, err = store.Append(ctx, testLine("task-1", "run-2", ChannelNormalized, "run2"))
	if err != nil {
		t.Fatalf("Append run2: %v", err)
	}

	// Each run gets its own seq starting at 1.
	lines1, err := store.ListSince(ctx, "task-1", "run-1", 0)
	if err != nil {
		t.Fatalf("ListSince run1: %v", err)
	}
	if len(lines1) != 1 || lines1[0].Content != "run1" || lines1[0].Seq != 1 {
		t.Fatalf("unexpected lines for run1: %+v", lines1)
	}

	lines2, err := store.ListSince(ctx, "task-1", "run-2", 0)
	if err != nil {
		t.Fatalf("ListSince run2: %v", err)
	}
	if len(lines2) != 1 || lines2[0].Content != "run2" || lines2[0].Seq != 1 {
		t.Fatalf("unexpected lines for run2: %+v", lines2)
	}
}

func TestAppendMissingFields(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	defer store.Close()

	_, err := store.Append(ctx, TraceLine{TaskID: "t", RunID: ""})
	if err == nil {
		t.Fatal("expected error for empty run_id")
	}

	_, err = store.Append(ctx, TraceLine{TaskID: "", RunID: "r"})
	if err == nil {
		t.Fatal("expected error for empty task_id")
	}
}

func TestTruncate(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	defer store.Close()

	long := strings.Repeat("a", MaxContentLen+1)
	line := testLine("task-1", "run-1", ChannelNormalized, long)

	_, err := store.Append(ctx, line)
	if err != nil {
		t.Fatalf("Append: %v", err)
	}

	lines, err := store.ListSince(ctx, "task-1", "run-1", 0)
	if err != nil {
		t.Fatalf("ListSince: %v", err)
	}
	if len(lines) != 1 {
		t.Fatalf("expected 1 line, got %d", len(lines))
	}
	if !lines[0].Truncated {
		t.Fatal("expected Truncated=true")
	}
	if len(lines[0].Content) != MaxContentLen {
		t.Fatalf("expected content length %d, got %d", MaxContentLen, len(lines[0].Content))
	}

	// Short content should NOT be truncated.
	_, err = store.Append(ctx, testLine("task-1", "run-1", ChannelNormalized, "short"))
	if err != nil {
		t.Fatalf("Append short: %v", err)
	}
	lines, err = store.ListSince(ctx, "task-1", "run-1", 0)
	if err != nil {
		t.Fatalf("ListSince: %v", err)
	}
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}
	if lines[1].Truncated {
		t.Fatal("short line should not be truncated")
	}
}

func TestTruncateRawPayload(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	defer store.Close()

	// A line with an oversized RawPayload must be stored and read back without
	// error, with RawPayload truncated to MaxContentLen.
	longPayload := strings.Repeat("x", MaxContentLen+1)
	line := TraceLine{
		TaskID:     "task-rp",
		RunID:      "run-rp",
		Channel:    ChannelProviderEvent,
		RawPayload: longPayload,
	}
	if _, err := store.Append(ctx, line); err != nil {
		t.Fatalf("Append: %v", err)
	}

	lines, err := store.ListSince(ctx, "task-rp", "run-rp", 0)
	if err != nil {
		t.Fatalf("ListSince: %v", err)
	}
	if len(lines) != 1 {
		t.Fatalf("expected 1 line, got %d", len(lines))
	}
	if !lines[0].Truncated {
		t.Fatal("expected Truncated=true for oversized RawPayload")
	}
	if len(lines[0].RawPayload) != MaxContentLen {
		t.Fatalf("expected raw_payload length %d, got %d", MaxContentLen, len(lines[0].RawPayload))
	}
}

func TestReadLargeLineDoesNotError(t *testing.T) {
	// Write a JSONL file with a line that exceeds the old bufio.Scanner limit
	// (MaxContentLen*2) to verify that readAll handles it without returning an
	// error. This simulates a file written before the RawPayload truncation fix.
	root := t.TempDir()
	store, err := NewJSONLStore(root)
	if err != nil {
		t.Fatalf("NewJSONLStore: %v", err)
	}
	defer store.Close()

	// Bypass truncation by writing the oversized line directly to the JSONL file.
	taskDir := root + "/task-large"
	if err := os.MkdirAll(taskDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	line := TraceLine{
		Seq:        1,
		TaskID:     "task-large",
		RunID:      "run-large",
		Channel:    ChannelProviderEvent,
		Content:    strings.Repeat("c", MaxContentLen),
		RawPayload: strings.Repeat("r", MaxContentLen),
	}
	raw, _ := json.Marshal(line)
	raw = append(raw, '\n')
	if err := os.WriteFile(taskDir+"/run-large.jsonl", raw, 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	ctx := context.Background()
	lines, err := store.ListSince(ctx, "task-large", "run-large", 0)
	if err != nil {
		t.Fatalf("ListSince returned error for large line: %v", err)
	}
	if len(lines) != 1 {
		t.Fatalf("expected 1 line, got %d", len(lines))
	}
}

func TestNonExistentReturnsNil(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	defer store.Close()

	lines, err := store.ListSince(ctx, "no-such-task", "no-such-run", 0)
	if err != nil {
		t.Fatalf("ListSince on missing file: %v", err)
	}
	if lines != nil {
		t.Fatalf("expected nil, got %+v", lines)
	}

	tail, err := store.Tail(ctx, "no-such-task", "no-such-run", 5)
	if err != nil {
		t.Fatalf("Tail on missing file: %v", err)
	}
	if tail != nil {
		t.Fatalf("expected nil, got %+v", tail)
	}
}

func TestConcurrentAppend(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	defer store.Close()

	const goroutines = 10
	const appendsPerGoroutine = 100

	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < appendsPerGoroutine; j++ {
				_, err := store.Append(ctx, testLine("task-con", "run-con", ChannelNormalized, "data"))
				if err != nil {
					t.Errorf("Append: %v", err)
					return
				}
			}
		}(i)
	}
	wg.Wait()

	lines, err := store.ListSince(ctx, "task-con", "run-con", 0)
	if err != nil {
		t.Fatalf("ListSince: %v", err)
	}
	expected := goroutines * appendsPerGoroutine
	if len(lines) != expected {
		t.Fatalf("expected %d lines, got %d", expected, len(lines))
	}

	// With per-file mutex, writes are serialized so JSONL ordering must
	// match seq ordering.
	for i, line := range lines {
		if line.Seq != int64(i+1) {
			t.Fatalf("expected seq %d at index %d, got %d", i+1, i, line.Seq)
		}
	}
}

func TestSeqMonotonicAcrossReopen(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()

	// Write some lines with the first store instance.
	store1, err := NewJSONLStore(root)
	if err != nil {
		t.Fatalf("NewJSONLStore: %v", err)
	}
	for i := 0; i < 5; i++ {
		_, err := store1.Append(ctx, testLine("task-1", "run-1", ChannelNormalized, "hello"))
		if err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
	store1.Close()

	// Re-open the same store directory.
	store2, err := NewJSONLStore(root)
	if err != nil {
		t.Fatalf("NewJSONLStore: %v", err)
	}
	defer store2.Close()

	// The next seq should continue from 5, not restart at 1.
	for i := 0; i < 3; i++ {
		_, err := store2.Append(ctx, testLine("task-1", "run-1", ChannelNormalized, "world"))
		if err != nil {
			t.Fatalf("Append: %v", err)
		}
	}

	lines, err := store2.ListSince(ctx, "task-1", "run-1", 0)
	if err != nil {
		t.Fatalf("ListSince: %v", err)
	}
	if len(lines) != 8 {
		t.Fatalf("expected 8 lines total, got %d", len(lines))
	}

	// Seq 1-5 from first instance, 6-8 from second.
	if lines[4].Seq != 5 || lines[5].Seq != 6 || lines[7].Seq != 8 {
		t.Fatalf("seq not monotonic across reopen: seqs = %+v", lines)
	}
}

func TestAppendPreservesChannel(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	defer store.Close()

	channels := []string{
		ChannelRawStdout,
		ChannelRawStderr,
		ChannelProviderEvent,
		ChannelCommandStdout,
		ChannelCommandStderr,
		ChannelApprovalRequest,
		ChannelApprovalResponse,
		ChannelNormalized,
	}
	for _, ch := range channels {
		_, err := store.Append(ctx, testLine("task-ch", "run-ch", ch, ch+" content"))
		if err != nil {
			t.Fatalf("Append %s: %v", ch, err)
		}
	}

	lines, err := store.ListSince(ctx, "task-ch", "run-ch", 0)
	if err != nil {
		t.Fatalf("ListSince: %v", err)
	}
	if len(lines) != len(channels) {
		t.Fatalf("expected %d lines, got %d", len(channels), len(lines))
	}
	for i, line := range lines {
		if line.Channel != channels[i] {
			t.Fatalf("line %d: expected channel %q, got %q", i, channels[i], line.Channel)
		}
	}
}

func TestListRunsNewestFirst(t *testing.T) {
	ctx := context.Background()
	store, err := NewJSONLStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewJSONLStore: %v", err)
	}
	defer store.Close()

	if _, err := store.Append(ctx, TraceLine{TaskID: "task-runs", RunID: "run-a", Channel: ChannelNormalized, Content: "a"}); err != nil {
		t.Fatalf("append run-a: %v", err)
	}
	time.Sleep(10 * time.Millisecond)
	if _, err := store.Append(ctx, TraceLine{TaskID: "task-runs", RunID: "run-b", Channel: ChannelNormalized, Content: "b"}); err != nil {
		t.Fatalf("append run-b: %v", err)
	}

	runs, err := store.ListRuns(ctx, "task-runs")
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if len(runs) != 2 {
		t.Fatalf("expected 2 runs, got %d", len(runs))
	}
	if runs[0] != "run-b" || runs[1] != "run-a" {
		t.Fatalf("expected newest run first, got %#v", runs)
	}
}

func TestClosedStoreReturnsError(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)

	// Close the store.
	if err := store.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}

	// Append after Close must return ErrStoreClosed.
	_, err := store.Append(ctx, testLine("t", "r", ChannelNormalized, "x"))
	if err != ErrStoreClosed {
		t.Fatalf("Append after Close: expected ErrStoreClosed, got %v", err)
	}

	// ListSince after Close must return ErrStoreClosed.
	_, err = store.ListSince(ctx, "t", "r", 0)
	if err != ErrStoreClosed {
		t.Fatalf("ListSince after Close: expected ErrStoreClosed, got %v", err)
	}

	// Tail after Close must return ErrStoreClosed.
	_, err = store.Tail(ctx, "t", "r", 5)
	if err != ErrStoreClosed {
		t.Fatalf("Tail after Close: expected ErrStoreClosed, got %v", err)
	}

	// Double Close must return ErrStoreClosed.
	err = store.Close()
	if err != ErrStoreClosed {
		t.Fatalf("second Close: expected ErrStoreClosed, got %v", err)
	}
}

// newTestStore creates a JSONLStore backed by a temp directory.
func newTestStore(t testing.TB) *JSONLStore {
	t.Helper()
	store, err := NewJSONLStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewJSONLStore: %v", err)
	}
	return store
}
