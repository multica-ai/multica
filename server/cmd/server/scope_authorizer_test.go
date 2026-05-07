package main

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/realtime"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// fakeScopeQuerier implements scopeAuthQuerier with in-memory maps.
type fakeScopeQuerier struct {
	tasks    map[[16]byte]db.AgentTaskQueue
	issues   map[[16]byte]db.Issue
	sessions map[[16]byte]db.ChatSession
}

func (f *fakeScopeQuerier) GetAgentTask(_ context.Context, id pgtype.UUID) (db.AgentTaskQueue, error) {
	if t, ok := f.tasks[id.Bytes]; ok {
		return t, nil
	}
	return db.AgentTaskQueue{}, errors.New("not found")
}
func (f *fakeScopeQuerier) GetIssue(_ context.Context, id pgtype.UUID) (db.Issue, error) {
	if i, ok := f.issues[id.Bytes]; ok {
		return i, nil
	}
	return db.Issue{}, errors.New("not found")
}
func (f *fakeScopeQuerier) GetChatSession(_ context.Context, id pgtype.UUID) (db.ChatSession, error) {
	if s, ok := f.sessions[id.Bytes]; ok {
		return s, nil
	}
	return db.ChatSession{}, errors.New("not found")
}

func mustUUID(t *testing.T) (string, pgtype.UUID) {
	t.Helper()
	u, err := uuid.NewRandom()
	if err != nil {
		t.Fatal(err)
	}
	return u.String(), pgtype.UUID{Bytes: u, Valid: true}
}

// TestScopeAuthorizer_ChatRequiresCreator pins must-fix #2 from PR #1429:
// ScopeChat MUST verify CreatorID == userID. A workspace peer that knows the
// session_id must NOT be able to subscribe to chat:message / chat:done /
// chat:session_read for that private session.
func TestScopeAuthorizer_ChatRequiresCreator(t *testing.T) {
	wsStr, wsUUID := mustUUID(t)
	creatorStr, creatorUUID := mustUUID(t)
	otherStr, _ := mustUUID(t)
	sessStr, sessUUID := mustUUID(t)
	otherWsStr, _ := mustUUID(t)
	otherWsStrOnly, otherWsUUID := mustUUID(t)
	_ = otherWsStrOnly

	q := &fakeScopeQuerier{
		sessions: map[[16]byte]db.ChatSession{
			sessUUID.Bytes: {
				ID:          sessUUID,
				WorkspaceID: wsUUID,
				CreatorID:   creatorUUID,
			},
		},
	}
	a := newScopeAuthorizer(q)
	ctx := context.Background()

	// Creator in matching workspace → allowed.
	ok, err := a.AuthorizeScope(ctx, creatorStr, wsStr, realtime.ScopeChat, sessStr)
	if err != nil || !ok {
		t.Fatalf("creator should be allowed: ok=%v err=%v", ok, err)
	}

	// Same workspace, different (peer) member → must be denied.
	ok, err = a.AuthorizeScope(ctx, otherStr, wsStr, realtime.ScopeChat, sessStr)
	if err != nil || ok {
		t.Fatalf("peer must be denied: ok=%v err=%v", ok, err)
	}

	// Cross-workspace creator (e.g. session in workspace A, request in
	// workspace B) → must be denied even though creator matches.
	ok, err = a.AuthorizeScope(ctx, creatorStr, otherWsStr, realtime.ScopeChat, sessStr)
	if err != nil || ok {
		t.Fatalf("cross-workspace must be denied: ok=%v err=%v", ok, err)
	}
	_ = otherWsUUID

	// Empty userID → must be denied (defensive).
	ok, err = a.AuthorizeScope(ctx, "", wsStr, realtime.ScopeChat, sessStr)
	if err != nil || ok {
		t.Fatalf("empty userID must be denied: ok=%v err=%v", ok, err)
	}

	// Unknown session → denied.
	_, missingStr := mustUUID(t)
	_ = missingStr
	missingUUID, _ := uuid.NewRandom()
	ok, err = a.AuthorizeScope(ctx, creatorStr, wsStr, realtime.ScopeChat, missingUUID.String())
	if err != nil || ok {
		t.Fatalf("unknown session must be denied: ok=%v err=%v", ok, err)
	}
}

// TestScopeAuthorizer_ChatTaskRequiresCreator pins must-fix #2 for the
// task-scope path of chat tasks (task.ChatSessionID set, no IssueID): only
// the chat session creator may subscribe to that task's stream, since
// task:message for chat tasks contains assistant chat content.
func TestScopeAuthorizer_ChatTaskRequiresCreator(t *testing.T) {
	wsStr, wsUUID := mustUUID(t)
	creatorStr, creatorUUID := mustUUID(t)
	otherStr, _ := mustUUID(t)
	sessStr, sessUUID := mustUUID(t)
	taskStr, taskUUID := mustUUID(t)
	_ = sessStr

	q := &fakeScopeQuerier{
		tasks: map[[16]byte]db.AgentTaskQueue{
			taskUUID.Bytes: {
				ID:            taskUUID,
				ChatSessionID: sessUUID,
			},
		},
		sessions: map[[16]byte]db.ChatSession{
			sessUUID.Bytes: {
				ID:          sessUUID,
				WorkspaceID: wsUUID,
				CreatorID:   creatorUUID,
			},
		},
	}
	a := newScopeAuthorizer(q)
	ctx := context.Background()

	ok, err := a.AuthorizeScope(ctx, creatorStr, wsStr, realtime.ScopeTask, taskStr)
	if err != nil || !ok {
		t.Fatalf("creator should be allowed for chat task: ok=%v err=%v", ok, err)
	}

	ok, err = a.AuthorizeScope(ctx, otherStr, wsStr, realtime.ScopeTask, taskStr)
	if err != nil || ok {
		t.Fatalf("peer must be denied for chat task: ok=%v err=%v", ok, err)
	}
}

// TestScopeAuthorizer_IssueTaskWorkspaceOnly verifies issue tasks remain
// workspace-scoped (any member who can see the issue may subscribe).
func TestScopeAuthorizer_IssueTaskWorkspaceOnly(t *testing.T) {
	wsStr, wsUUID := mustUUID(t)
	memberStr, _ := mustUUID(t)
	otherWsStr, _ := mustUUID(t)
	taskStr, taskUUID := mustUUID(t)
	_, issueUUID := mustUUID(t)

	q := &fakeScopeQuerier{
		tasks: map[[16]byte]db.AgentTaskQueue{
			taskUUID.Bytes: {
				ID:      taskUUID,
				IssueID: issueUUID,
			},
		},
		issues: map[[16]byte]db.Issue{
			issueUUID.Bytes: {
				ID:          issueUUID,
				WorkspaceID: wsUUID,
			},
		},
	}
	a := newScopeAuthorizer(q)
	ctx := context.Background()

	ok, err := a.AuthorizeScope(ctx, memberStr, wsStr, realtime.ScopeTask, taskStr)
	if err != nil || !ok {
		t.Fatalf("member in workspace should be allowed: ok=%v err=%v", ok, err)
	}

	ok, err = a.AuthorizeScope(ctx, memberStr, otherWsStr, realtime.ScopeTask, taskStr)
	if err != nil || ok {
		t.Fatalf("cross-workspace must be denied: ok=%v err=%v", ok, err)
	}
}
