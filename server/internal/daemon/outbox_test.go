package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/cli"
)

func newTestOutbox(t *testing.T) *reportOutbox {
	t.Helper()
	t.Setenv(cli.TaskConfigRootEnv, t.TempDir())
	o := newReportOutbox("test-profile", slog.Default())
	if o.path == "" {
		t.Fatal("outbox disabled in test env")
	}
	return o
}

func sampleReport(id string) terminalTaskReport {
	return terminalTaskReport{
		kind:          terminalTaskReportFail,
		taskID:        id,
		errorMessage:  "boom",
		failureReason: "provider_network",
	}
}

func TestOutboxDrainSuccessRemovesJournal(t *testing.T) {
	o := newTestOutbox(t)
	r := sampleReport("t1")
	if err := o.enqueue(r.kind, r); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if !o.pending() {
		t.Fatal("expected pending after enqueue")
	}

	var sent []terminalTaskReport
	o.drain(func(_ context.Context, r terminalTaskReport) error {
		sent = append(sent, r)
		return nil
	})

	if len(sent) != 1 || sent[0].taskID != "t1" || sent[0].failureReason != "provider_network" {
		t.Fatalf("unexpected replay: %+v", sent)
	}
	if o.pending() {
		t.Fatal("journal should be removed after full success")
	}
}

func TestOutboxKeepsFailedDropsCorruptAndExpired(t *testing.T) {
	o := newTestOutbox(t)
	good := sampleReport("keep-me")
	if err := o.enqueue(good.kind, good); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	expired := pendingReport{
		Kind:     terminalTaskReportComplete,
		Report:   terminalReportPayload{TaskID: "old"},
		QueuedAt: time.Now().Add(-25 * time.Hour),
	}
	goodLine, err := json.Marshal(newPendingReport(good.kind, good, time.Now()))
	if err != nil {
		t.Fatalf("marshal good: %v", err)
	}
	expLine, err := json.Marshal(expired)
	if err != nil {
		t.Fatalf("marshal expired: %v", err)
	}
	content := append([]byte("{not json\n"), goodLine...)
	content = append(append(content, '\n'), expLine...)
	content = append(content, '\n')
	if err := os.WriteFile(o.path, content, 0o600); err != nil {
		t.Fatalf("seed journal: %v", err)
	}

	var sent []string
	o.drain(func(_ context.Context, r terminalTaskReport) error {
		if r.taskID == "keep-me" {
			return errors.New("server still down")
		}
		sent = append(sent, r.taskID)
		return nil
	})

	raw, err := os.ReadFile(o.path)
	if err != nil {
		t.Fatalf("journal missing but an entry should remain: %v", err)
	}
	lines := bytesLines(raw)
	if len(lines) != 1 || sent != nil {
		t.Fatalf("want exactly the failed entry kept and nothing replayed, got lines=%d sent=%v", len(lines), sent)
	}
}

func TestOutboxNilSafe(t *testing.T) {
	var nilBox *reportOutbox
	if nilBox.pending() {
		t.Fatal("nil outbox must not report pending")
	}
	if err := nilBox.enqueue(terminalTaskReportComplete, sampleReport("x")); err != nil {
		t.Fatalf("nil-safe enqueue: %v", err)
	}
	nilBox.drain(func(context.Context, terminalTaskReport) error { return nil })
}

func TestBytesLines(t *testing.T) {
	got := bytesLines([]byte("a\n\nb\n"))
	if len(got) != 2 || string(got[0]) != "a" || string(got[1]) != "b" {
		t.Fatalf("unexpected split: %q", got)
	}
}
