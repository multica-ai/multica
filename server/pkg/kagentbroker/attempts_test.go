package kagentbroker

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestJournalRestartReplayAndFreshUpstreamIdentity(t *testing.T) {
	f := setup(t)
	var upstreamID string
	f.upstream = func(r *http.Request) (*http.Response, error) {
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		m := object(object(req["params"])["message"])
		upstreamID = str(m["messageId"])
		if upstreamID == testOwner || !uuidPattern.MatchString(upstreamID) || m["contextId"] != nil {
			t.Error("initial session uses caller-controlled selector")
		}
		return reply(map[string]any{"jsonrpc": "2.0", "id": req["id"], "result": map[string]any{"task": loadAlpha7(t)}}), nil
	}
	if w := serve(f.b, "POST", f.b.policy.a2a(), sendBody(), nil); w.Code != 200 {
		t.Fatal(w.Code)
	}
	path := f.b.journal.file.Name()
	if err := f.b.journal.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenAttemptJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	// A fresh broker can only deny replay and old bindings; the attempt journal
	// deliberately grants no task/session authority after restart.
	fresh := setup(t)
	fresh.b.journal = reopened
	if w := serve(fresh.b, "POST", fresh.b.policy.a2a(), sendBody(), nil); w.Code != 409 || fresh.kube+fresh.a2a != 0 {
		t.Fatal("restart redispatched old message", w.Code)
	}
	for _, body := range []string{continuationBody(testID), taskBody("GetTask"), taskBody("CancelTask")} {
		if w := serve(fresh.b, "POST", fresh.b.policy.a2a(), body, nil); w.Code != 403 {
			t.Fatal("durable tombstone adopted a task", w.Code)
		}
	}
	if upstreamID == "" {
		t.Fatal("did not inspect upstream identity")
	}
}

func TestJournalRejectsCorruptionPermissionsAndConcurrentOpen(t *testing.T) {
	for _, raw := range []string{"broken", journalHeader + testID, journalHeader + testID + "\n" + testID + "\n", journalHeader + "bad\n"} {
		path := filepath.Join(t.TempDir(), "attempts")
		if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
			t.Fatal(err)
		}
		if j, err := OpenAttemptJournal(path); err == nil {
			j.Close()
			t.Fatal("corrupt journal accepted")
		}
	}
	path := filepath.Join(t.TempDir(), "attempts")
	j, err := OpenAttemptJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	if other, err := OpenAttemptJournal(path); err == nil {
		other.Close()
		t.Fatal("concurrent owner accepted")
	}
	if err := j.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if other, err := OpenAttemptJournal(path); err == nil {
		other.Close()
		t.Fatal("public journal accepted")
	}
}

func TestJournalCapacitySurvivesRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "attempts")
	j, err := OpenAttemptJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	// Populate a valid full ledger offline to avoid 1024 unnecessary fsyncs.
	if err := j.Close(); err != nil {
		t.Fatal(err)
	}
	raw := journalHeader
	for range bindingLimit {
		raw += uuid.NewString() + "\n"
	}
	if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	j, err = OpenAttemptJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	if status := j.reserve(context.Background(), testID); status != 403 || len(j.ids) != bindingLimit {
		t.Fatal("capacity lost after restart", status)
	}
}

func TestJournalWaitHonorsContextAndDoesNotBlockRecovery(t *testing.T) {
	f := setup(t)
	if w := serve(f.b, "POST", f.b.policy.a2a(), sendBody(), nil); w.Code != 200 {
		t.Fatal(w.Code)
	}
	f.b.journal.gate <- struct{}{}
	defer func() { <-f.b.journal.gate }()
	done := make(chan int, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
		defer cancel()
		done <- serve(f.b, "POST", f.b.policy.a2a(), continuationBody(testID), func(r *http.Request) { *r = *r.WithContext(ctx) }).Code
	}()
	if w := serve(f.b, "POST", f.b.policy.a2a(), taskBody("CancelTask"), nil); w.Code != 200 {
		t.Fatal("disk gate blocked cancellation", w.Code)
	}
	select {
	case status := <-done:
		if status != 504 {
			t.Fatal(status)
		}
	case <-time.After(time.Second):
		t.Fatal("journal wait ignored cancellation")
	}
	if f.b.busy[testContext] || f.b.attempts[testID] {
		t.Fatal("predispatch cancellation retained local busy reservation")
	}
}

func TestJournalFailureNeverDispatches(t *testing.T) {
	f := setup(t)
	if err := f.b.journal.file.Close(); err != nil {
		t.Fatal(err)
	}
	if w := serve(f.b, "POST", f.b.policy.a2a(), sendBody(), nil); w.Code != 503 || f.a2a != 0 {
		t.Fatal("unrecorded send dispatched", w.Code)
	}
	if !f.b.journal.failed {
		t.Fatal("failed durable fence remained usable")
	}
	if w := serve(f.b, "POST", f.b.policy.a2a(), sendBody(), nil); w.Code != 503 || f.a2a != 0 {
		t.Fatal("failed fence retried", w.Code)
	}
}

func TestAmbiguousSendTombstoneSurvivesRestart(t *testing.T) {
	f := setup(t)
	f.upstream = func(*http.Request) (*http.Response, error) { return nil, errDenied }
	if w := serve(f.b, "POST", f.b.policy.a2a(), sendBody(), nil); w.Code != 502 {
		t.Fatal(w.Code)
	}
	path := f.b.journal.file.Name()
	if err := f.b.journal.Close(); err != nil {
		t.Fatal(err)
	}
	j, err := OpenAttemptJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	fresh := setup(t)
	fresh.b.journal = j
	if w := serve(fresh.b, "POST", fresh.b.policy.a2a(), sendBody(), nil); w.Code != 409 || fresh.kube+fresh.a2a != 0 {
		t.Fatal("restart retried uncertain execution", w.Code)
	}
}
