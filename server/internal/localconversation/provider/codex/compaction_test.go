package codex

import "testing"

func TestCompactionLifecycle(t *testing.T) {
	t.Parallel()
	n := NewNormalizer("multica")
	start := note{"item/started", `{"item":{"id":"compact-1","type":"contextCompaction"}}`}
	done := note{"item/completed", `{"item":{"id":"compact-1","type":"contextCompaction"}}`}
	assertEvents(t, feed(t, n, start, start), "item.started compact-1 kind=compaction")
	assertEvents(t, feed(t, n, done, done, start), "item.completed compact-1 final kind=compaction status=completed")
	assertEvents(t, n.Close("cancelled"))
	// Automatic compaction does not end the surrounding provider turn.
	assertEvents(t, feed(t, n, note{"item/completed", `{"item":{"id":"answer","type":"agentMessage","text":"done"}}`}),
		"item.completed answer final kind=assistant_text status=completed text=done")
}

func TestCompactionCompletionWithoutStart(t *testing.T) {
	t.Parallel()
	n := NewNormalizer("multica")
	assertEvents(t, feed(t, n, note{"item/completed", `{"item":{"id":"compact-1","type":"contextCompaction"}}`}),
		"item.completed compact-1 final kind=compaction status=completed")
}

func TestCompactionInterrupted(t *testing.T) {
	t.Parallel()
	for _, status := range []string{"cancelled", "failed"} {
		t.Run(status, func(t *testing.T) {
			n := NewNormalizer("multica")
			feed(t, n, note{"item/started", `{"item":{"id":"compact-1","type":"contextCompaction"}}`})
			assertEvents(t, n.Close(status), "item.completed compact-1 final kind=compaction status="+status)
			assertEvents(t, n.Close(status))
			assertEvents(t, feed(t, n, note{"item/completed", `{"item":{"id":"compact-1","type":"contextCompaction"}}`}))
		})
	}
}
