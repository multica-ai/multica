package service

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/localconversation/provider/codex"
	"github.com/multica-ai/multica/server/internal/localconversation/store"
	"github.com/multica-ai/multica/server/internal/localconversation/store/sqlitedb"
)

// runTurn sends a message and completes its turn with one answer.
func (f *serviceFixture) runTurn(t *testing.T, requestID, uuid string) {
	t.Helper()
	ctx := context.Background()
	if _, err := f.svc.Send(ctx, SendCommand{ScopeID: f.scopeID, ConversationID: "conv-1", RequestID: requestID, Text: "ask " + requestID}); err != nil {
		t.Fatal(err)
	}
	a := f.allAdapters()[0]
	a.emit(t,
		`{"type":"system","subtype":"init","session_id":"native-1"}`,
		`{"type":"assistant","uuid":"`+uuid+`","message":{"id":"m-`+requestID+`","content":[{"type":"text","text":"answer `+requestID+`"}]}}`,
		turnDone,
	)
	eventually(t, "turn "+requestID+" to finish", func() bool {
		turn, err := f.svc.Store().Turn(ctx, "conv-1", "turn-"+requestID)
		return err == nil && turn.Status == "completed" && turn.ProviderTurnID == uuid
	})
}

func TestForkThroughATurnCopiesItsHistory(t *testing.T) {
	t.Parallel()

	f := newServiceFixture(t, filepath.Join(t.TempDir(), "c.sqlite"))
	ctx := context.Background()
	f.create(t, "conv-1", "claude")
	f.runTurn(t, "r1", "uuid-1")
	f.runTurn(t, "r2", "uuid-2")
	eventually(t, "the native session", func() bool {
		conv, _ := f.svc.Store().Conversation(ctx, "conv-1")
		return conv.ProviderSessionID == "native-1"
	})

	var forkFields map[string]any
	f.mu.Lock()
	f.reply = func(a *fakeAdapter, typ string) (json.RawMessage, error) {
		if typ == "fork_session" {
			a.mu.Lock()
			forkFields = a.fields[len(a.fields)-1]
			a.mu.Unlock()
			return json.RawMessage(`{"session_id":"native-fork"}`), nil
		}
		return json.RawMessage(`{}`), nil
	}
	f.mu.Unlock()

	forked, err := f.svc.Fork(ctx, ForkCommand{ScopeID: f.scopeID, SourceID: "conv-1", TurnID: "turn-r1", RequestID: "fork-1"})
	if err != nil {
		t.Fatal(err)
	}
	if forkFields["up_to_message_id"] != "uuid-1" || forkFields["session_id"] != "native-1" {
		t.Fatalf("fork request = %v", forkFields)
	}
	if forked.ProviderSessionID != "native-fork" || forked.ForkedFromID != "conv-1" || forked.TurnCount != 1 || !strings.HasSuffix(forked.Title, "· Fork") {
		t.Fatalf("fork = %+v", forked)
	}
	snap, err := f.svc.Snapshot(ctx, f.scopeID, forked.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Turns) != 1 || len(snap.Turns[0].Messages) != 2 || !strings.Contains(snap.Turns[0].Messages[1].Content, "answer r1") {
		t.Fatalf("forked history = %+v", snap.Turns)
	}
	events, _ := f.svc.Store().EventsAfter(ctx, forked.ID, 0, forked.LastSeq, 100)
	if int64(len(events)) != forked.LastSeq {
		t.Fatalf("forked events = %d, last seq %d", len(events), forked.LastSeq)
	}
	for i, e := range events {
		if e.Seq != int64(i+1) || (e.TurnID.Valid && !strings.HasPrefix(e.TurnID.String, forked.ID+"~")) || strings.Contains(string(e.Payload), "answer r2") {
			t.Fatalf("event %d = %+v", i, e)
		}
	}

	again, err := f.svc.Fork(ctx, ForkCommand{ScopeID: f.scopeID, SourceID: "conv-1", TurnID: "turn-r1", RequestID: "fork-1"})
	if err != nil || again.ID != forked.ID {
		t.Fatalf("retried fork = %+v, %v", again, err)
	}
	if _, err := f.svc.Fork(ctx, ForkCommand{ScopeID: f.scopeID, SourceID: "conv-1", RequestID: "fork-1"}); !errors.Is(err, ErrRequestConflict) {
		t.Fatalf("reused request id = %v", err)
	}
}

func TestForkNeedsANativeSession(t *testing.T) {
	t.Parallel()

	f := newServiceFixture(t, filepath.Join(t.TempDir(), "c.sqlite"))
	f.create(t, "conv-1", "claude")
	if _, err := f.svc.Fork(context.Background(), ForkCommand{ScopeID: f.scopeID, SourceID: "conv-1", RequestID: "f"}); !errors.Is(err, ErrForkUnavailable) {
		t.Fatalf("fork of an empty conversation = %v", err)
	}
}

func TestForkCodexThroughItsNativeTurn(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	server := &fakeCodex{params: map[string]map[string]any{}, done: make(chan struct{})}
	svc, err := Open(ctx, Options{Path: filepath.Join(t.TempDir(), "c.sqlite"), Codex: func(_ context.Context, conv sqlitedb.Conversation) (CodexSession, StartCodexServer, error) {
		return CodexSession{Cwd: conv.Cwd}, func(_ context.Context, h codex.Handler, _ []string) (CodexServer, error) {
			server.handler = h
			return server, nil
		}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Close(ctx) })
	scopeID, _ := svc.Store().EnsureScope(ctx, store.Scope{APIOrigin: "o", UserID: "u", WorkspaceID: "w"})
	if _, err := svc.Store().CreateConversation(ctx, store.NewConversation{ID: "conv-1", ScopeID: scopeID, Provider: "codex", ProviderSessionID: "th-1", Cwd: "/repo", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	w := svc.Store().NewWriter(store.WriterOptions{})
	_, err = w.Submit(ctx, store.Batch{ConversationID: "conv-1", StartTurn: &store.NewTurn{ID: "turn-1", StartedAt: time.Now()},
		CompleteTurn: &store.TurnCompletion{TurnID: "turn-1", Status: "completed", ProviderTurnID: "nt-1", CompletedAt: time.Now()}})
	w.Close()
	if err != nil {
		t.Fatal(err)
	}

	forked, err := svc.Fork(ctx, ForkCommand{ScopeID: scopeID, SourceID: "conv-1", TurnID: "turn-1", RequestID: "f1"})
	if err != nil || forked.ProviderSessionID != "th-fork" {
		t.Fatalf("fork = %+v, %v", forked, err)
	}
	server.mu.Lock()
	params := server.params["thread/fork"]
	server.mu.Unlock()
	if params["threadId"] != "th-1" || params["lastTurnId"] != "nt-1" {
		t.Fatalf("thread/fork params = %v", params)
	}
}
