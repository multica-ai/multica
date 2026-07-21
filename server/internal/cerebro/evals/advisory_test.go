package evals

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type fakeOwnerAdminLister struct {
	recipients []pgtype.UUID
	err        error
}

func (f *fakeOwnerAdminLister) ListCerebroWorkspaceOwnerAdmins(_ context.Context, _ pgtype.UUID) ([]pgtype.UUID, error) {
	return f.recipients, f.err
}

type fakeInboxWriter struct {
	calls []db.CreateInboxItemParams
	err   error
}

func (f *fakeInboxWriter) CreateInboxItem(_ context.Context, arg db.CreateInboxItemParams) (db.InboxItem, error) {
	f.calls = append(f.calls, arg)
	if f.err != nil {
		return db.InboxItem{}, f.err
	}
	return db.InboxItem{ID: arg.RecipientID, RecipientID: arg.RecipientID, WorkspaceID: arg.WorkspaceID, Type: arg.Type}, nil
}

func recipient() pgtype.UUID {
	return pgtype.UUID{Bytes: [16]byte{1, 2, 3}, Valid: true}
}

func TestAdvisoryWarner(t *testing.T) {
	if evalTestPool == nil {
		t.Skip("no test DB")
	}
	ctx := context.Background()
	store := NewStore(evalTestPool)

	seedFailingAdvisory := func(t *testing.T) evalFixture {
		t.Helper()
		f := seedEvalFixture(t)
		evalID := seedActiveEval(t, f, "adv-warn", 1)
		if _, err := store.CreateBinding(ctx, f.workspaceID, f.actorID, BindingInput{
			WorkflowID: f.workflowID, EvalID: evalID, Phase: "delivery", Blocking: false,
		}); err != nil {
			t.Fatalf("create advisory binding: %v", err)
		}
		return f
	}

	t.Run("one card per owner/admin", func(t *testing.T) {
		f := seedFailingAdvisory(t)
		inbox := &fakeInboxWriter{}
		warner := NewAdvisoryWarner(store, &fakeOwnerAdminLister{recipients: []pgtype.UUID{
			{Bytes: [16]byte{1}, Valid: true}, {Bytes: [16]byte{2}, Valid: true},
		}}, inbox, nil)
		if err := warner.Warn(ctx, f.workflowID, f.issueID, "delivery"); err != nil {
			t.Fatalf("warn: %v", err)
		}
		if len(inbox.calls) != 2 {
			t.Fatalf("expected 2 cards (one per recipient), got %d", len(inbox.calls))
		}
		for _, c := range inbox.calls {
			if c.Type != inboxTypeEvalAdvisoryFailed || c.Severity != "attention" || c.RecipientType != "member" {
				t.Fatalf("unexpected card shape: %+v", c)
			}
		}
	})

	t.Run("no failing advisory bindings = no cards", func(t *testing.T) {
		f := seedEvalFixture(t) // no advisory binding
		inbox := &fakeInboxWriter{}
		warner := NewAdvisoryWarner(store, &fakeOwnerAdminLister{recipients: []pgtype.UUID{recipient()}}, inbox, nil)
		if err := warner.Warn(ctx, f.workflowID, f.issueID, "delivery"); err != nil {
			t.Fatalf("warn: %v", err)
		}
		if len(inbox.calls) != 0 {
			t.Fatalf("expected no cards, got %d", len(inbox.calls))
		}
	})

	t.Run("no recipients = no cards, no error", func(t *testing.T) {
		f := seedFailingAdvisory(t)
		inbox := &fakeInboxWriter{}
		warner := NewAdvisoryWarner(store, &fakeOwnerAdminLister{recipients: nil}, inbox, nil)
		if err := warner.Warn(ctx, f.workflowID, f.issueID, "delivery"); err != nil {
			t.Fatalf("warn: %v", err)
		}
		if len(inbox.calls) != 0 {
			t.Fatalf("expected no cards without recipients, got %d", len(inbox.calls))
		}
	})

	t.Run("nil inbox = no-op", func(t *testing.T) {
		f := seedFailingAdvisory(t)
		warner := NewAdvisoryWarner(store, &fakeOwnerAdminLister{recipients: []pgtype.UUID{recipient()}}, nil, nil)
		if err := warner.Warn(ctx, f.workflowID, f.issueID, "delivery"); err != nil {
			t.Fatalf("warn with nil inbox should be a no-op, got %v", err)
		}
	})

	t.Run("per-card write error is skipped, not fatal", func(t *testing.T) {
		f := seedFailingAdvisory(t)
		inbox := &fakeInboxWriter{err: errors.New("inbox down")}
		warner := NewAdvisoryWarner(store, &fakeOwnerAdminLister{recipients: []pgtype.UUID{
			{Bytes: [16]byte{1}, Valid: true}, {Bytes: [16]byte{2}, Valid: true},
		}}, inbox, nil)
		if err := warner.Warn(ctx, f.workflowID, f.issueID, "delivery"); err != nil {
			t.Fatalf("a per-card write error must not be fatal, got %v", err)
		}
		if len(inbox.calls) != 2 {
			t.Fatalf("expected both recipients attempted despite errors, got %d", len(inbox.calls))
		}
	})

	t.Run("same failed run is notified once", func(t *testing.T) {
		f := seedEvalFixture(t)
		evalID := seedActiveEval(t, f, "adv-dedupe", 1)
		binding, err := store.CreateBinding(ctx, f.workspaceID, f.actorID, BindingInput{
			WorkflowID: f.workflowID, EvalID: evalID, Phase: "monitor", Blocking: false,
		})
		if err != nil {
			t.Fatal(err)
		}
		runID := uuid.New()
		warner := NewAdvisoryWarner(store, &fakeOwnerAdminLister{recipients: []pgtype.UUID{pgUUID(f.actorID)}}, db.New(evalTestPool), nil)
		for i := 0; i < 2; i++ {
			if err := warner.WarnBinding(ctx, binding, f.issueID, &runID, RunStatusFailed); err != nil {
				t.Fatalf("warn %d: %v", i, err)
			}
		}
		var count int
		if err := evalTestPool.QueryRow(ctx, `SELECT count(*) FROM inbox_item WHERE workspace_id=$1 AND type=$2 AND details->>'binding_id'=$3`, f.workspaceID, inboxTypeEvalAdvisoryFailed, binding.ID.String()).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatalf("duplicate advisory cards=%d, want 1", count)
		}
	})
}
