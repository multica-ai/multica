package handler

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestCommentDeliveryObligationPersistence(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := t.Context()
	issueID := dbfx.Issue(t, "durable comment obligation persistence")
	commentID := dbfx.Comment(t, issueID, "Please review the current head.")
	agentID := dbfx.Agent(t, "obligation target", "")
	args := commentDeliveryAdmissionForTest(t, db.UpsertCommentDeliveryObligationParams{
		WorkspaceID: parseUUID(testWorkspaceID), CommentID: parseUUID(commentID), AgentID: parseUUID(agentID),
	})
	t.Cleanup(func() {
		_, err := testPool.Exec(context.Background(), `DELETE FROM comment_delivery_obligation WHERE comment_id=$1 AND agent_id=$2`, commentID, agentID)
		if err != nil {
			t.Errorf("clean up obligation: %v", err)
		}
	})
	first, err := testHandler.Queries.UpsertCommentDeliveryObligation(ctx, args)
	if err != nil {
		t.Fatal(err)
	}
	second, err := testHandler.Queries.UpsertCommentDeliveryObligation(ctx, args)
	if err != nil {
		t.Fatal(err)
	}
	if first.CommentRevision != second.CommentRevision || !first.CreatedAt.Time.Equal(second.CreatedAt.Time) || !first.NextAttemptAt.Time.Equal(second.NextAttemptAt.Time) {
		t.Fatalf("same-revision admission changed the existing obligation: first=%+v second=%+v", first, second)
	}

	// Read through another connection, independently of the admitting client.
	conn, err := testPool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	fresh := db.New(conn)
	get := db.GetCommentDeliveryObligationParams{
		WorkspaceID: args.WorkspaceID, CommentID: args.CommentID, AgentID: args.AgentID,
	}
	persisted, err := fresh.GetCommentDeliveryObligation(ctx, get)
	if err != nil || persisted.CommentRevision != first.CommentRevision || persisted.IssueID != parseUUID(issueID) {
		t.Fatalf("read persisted obligation: row=%+v error=%v", persisted, err)
	}
	get.WorkspaceID = pgtype.UUID{}
	if _, err := fresh.GetCommentDeliveryObligation(ctx, get); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("cross-workspace read: %v, want no rows", err)
	}

	claimed, err := testHandler.Queries.TryClaimCommentDeliveryObligation(ctx, db.TryClaimCommentDeliveryObligationParams{
		WorkspaceID: args.WorkspaceID, CommentID: args.CommentID, AgentID: args.AgentID, CommentRevision: first.CommentRevision,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := testHandler.Queries.BumpCommentRevision(ctx, db.BumpCommentRevisionParams{ID: args.CommentID, WorkspaceID: args.WorkspaceID}); err != nil {
		t.Fatal(err)
	}
	neutral, err := testHandler.Queries.UpsertCommentDeliveryObligation(ctx, commentDeliveryAdmissionForTest(t, args))
	if err != nil {
		t.Fatal(err)
	}
	if neutral.CommentRevision != first.CommentRevision || !neutral.InputUpdatedAt.Time.Equal(first.InputUpdatedAt.Time) || !neutral.LeaseUntil.Time.Equal(claimed.LeaseUntil.Time) || !neutral.NextAttemptAt.Time.Equal(claimed.NextAttemptAt.Time) {
		t.Fatalf("routing-neutral re-admission replaced accepted input or its active claim: before=%+v after=%+v", claimed, neutral)
	}
	testHandler.releaseCommentDeliveryObligation(ctx, claimed)

	dbfx.Exec(t, `UPDATE comment SET revision=revision+1, content='Updated instruction' WHERE id=$1`, commentID)
	newer, err := testHandler.Queries.UpsertCommentDeliveryObligation(ctx, commentDeliveryAdmissionForTest(t, args))
	if err != nil || newer.CommentRevision != first.CommentRevision+2 {
		t.Fatalf("admit edited comment: row=%+v error=%v", newer, err)
	}
	staleDelete := db.DeleteCommentDeliveryObligationParams{
		WorkspaceID: args.WorkspaceID, CommentID: args.CommentID, AgentID: args.AgentID, CommentRevision: first.CommentRevision,
	}
	if err := testHandler.Queries.DeleteCommentDeliveryObligation(ctx, staleDelete); err != nil {
		t.Fatal(err)
	}
	get.WorkspaceID = args.WorkspaceID
	if current, err := fresh.GetCommentDeliveryObligation(ctx, get); err != nil || current.CommentRevision != newer.CommentRevision {
		t.Fatalf("stale settlement removed edited intent: row=%+v error=%v", current, err)
	}
	staleDelete.CommentRevision = newer.CommentRevision
	staleDelete.WorkspaceID = pgtype.UUID{}
	if err := testHandler.Queries.DeleteCommentDeliveryObligation(ctx, staleDelete); err != nil {
		t.Fatal(err)
	}
	if _, err := fresh.GetCommentDeliveryObligation(ctx, get); err != nil {
		t.Fatalf("cross-workspace settlement removed intent: %v", err)
	}
	staleDelete.WorkspaceID = args.WorkspaceID
	if err := testHandler.Queries.DeleteCommentDeliveryObligation(ctx, staleDelete); err != nil {
		t.Fatal(err)
	}
	if _, err := fresh.GetCommentDeliveryObligation(ctx, get); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("settled obligation remains: %v", err)
	}
}

func TestCommentDeliveryObligationAdmissionRequiresScopedReferences(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	issueID := dbfx.Issue(t, "obligation scope")
	commentID := dbfx.Comment(t, issueID, "Do not cross workspace boundaries.")
	agentID := dbfx.Agent(t, "scoped obligation target", "")
	args := commentDeliveryAdmissionForTest(t, db.UpsertCommentDeliveryObligationParams{
		WorkspaceID: parseUUID(testWorkspaceID), CommentID: parseUUID(commentID), AgentID: parseUUID(agentID),
	})
	t.Run("missing comment", func(t *testing.T) {
		missing := args
		missing.CommentID = pgtype.UUID{}
		if _, err := testHandler.Queries.UpsertCommentDeliveryObligation(t.Context(), missing); !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("missing comment admitted: %v", err)
		}
	})
	t.Run("missing agent", func(t *testing.T) {
		missing := args
		missing.AgentID = pgtype.UUID{}
		if _, err := testHandler.Queries.UpsertCommentDeliveryObligation(t.Context(), missing); !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("missing agent admitted: %v", err)
		}
	})
	otherWorkspace := dbfx.Workspace(t, "Other obligation workspace", "other-obligation-workspace")
	otherAgent := dbfx.Agent(t, "foreign obligation target", "", testutil.Cols{"workspace_id": otherWorkspace})
	foreignIssue := dbfx.Issue(t, "foreign obligation issue", testutil.Cols{"workspace_id": otherWorkspace})
	t.Run("inconsistent comment issue workspace", func(t *testing.T) {
		comment := dbfx.Comment(t, foreignIssue, "Mismatched issue scope", testutil.Cols{"workspace_id": testWorkspaceID})
		t.Cleanup(func() {
			_, _ = testPool.Exec(context.Background(), `DELETE FROM comment_delivery_obligation WHERE comment_id=$1 AND agent_id=$2`, comment, agentID)
		})
		foreign := args
		foreign.CommentID = parseUUID(comment)
		foreign = commentDeliveryAdmissionForTest(t, foreign)
		if _, err := testHandler.Queries.UpsertCommentDeliveryObligation(t.Context(), foreign); !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("mismatched issue workspace admitted: %v", err)
		}
	})
	t.Run("agent in another workspace", func(t *testing.T) {
		foreign := args
		foreign.AgentID = parseUUID(otherAgent)
		if _, err := testHandler.Queries.UpsertCommentDeliveryObligation(t.Context(), foreign); !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("cross-workspace agent admitted: %v", err)
		}
	})
	t.Run("wrong workspace", func(t *testing.T) {
		foreign := args
		foreign.WorkspaceID = pgtype.UUID{}
		if _, err := testHandler.Queries.UpsertCommentDeliveryObligation(t.Context(), foreign); !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("foreign references admitted: %v", err)
		}
	})
}

func TestDeleteWorkspaceRemovesOnlyOwnedCommentDeliveryObligations(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	lockRollupSingleton(t)
	victim := dbfx.Workspace(t, "Deleted obligation workspace", "deleted-obligation-workspace")
	dbfx.Insert(t, "member", testutil.Cols{"workspace_id": victim, "user_id": testUserID, "role": "owner"})
	comments := make(map[string]string)
	for _, workspace := range []string{victim, testWorkspaceID} {
		issue := dbfx.Issue(t, "Workspace-owned pending instruction", testutil.Cols{"workspace_id": workspace})
		comment := dbfx.Comment(t, issue, "Keep this accepted input until delivery", testutil.Cols{"workspace_id": workspace})
		agent := dbfx.Agent(t, "Workspace-owned recipient", "", testutil.Cols{"workspace_id": workspace})
		comments[workspace] = comment
		t.Cleanup(func() {
			_, err := testPool.Exec(context.Background(), `DELETE FROM comment_delivery_obligation WHERE comment_id=$1`, comment)
			if err != nil {
				t.Errorf("clean up obligation: %v", err)
			}
		})
		_, err := testHandler.Queries.UpsertCommentDeliveryObligation(t.Context(), commentDeliveryAdmissionForTest(t, db.UpsertCommentDeliveryObligationParams{
			WorkspaceID: parseUUID(workspace), CommentID: parseUUID(comment), AgentID: parseUUID(agent),
		}))
		if err != nil {
			t.Fatal(err)
		}
	}
	req := withURLParam(newRequest("DELETE", "/api/workspaces/"+victim, nil), "id", victim)
	testutil.Call(t, testHandler.DeleteWorkspace, req).Want(204)
	for workspace, comment := range comments {
		var remains bool
		if err := testPool.QueryRow(t.Context(), `SELECT EXISTS(SELECT 1 FROM comment_delivery_obligation WHERE comment_id=$1)`, comment).Scan(&remains); err != nil {
			t.Fatal(err)
		}
		if want := workspace != victim; remains != want {
			t.Errorf("workspace %s obligation remains=%v, want %v", workspace, remains, want)
		}
	}
}

func commentDeliverySourceForTest(t *testing.T, id pgtype.UUID) commentDeliverySource {
	t.Helper()
	comment, err := testHandler.Queries.GetComment(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	originator := comment.AuthorID
	if comment.AuthorType != "member" {
		originator = testHandler.TaskService.ResolveOriginatorFromTriggerComment(t.Context(), comment.WorkspaceID, comment.ID)
	}
	return commentDeliverySource{Comment: comment, ActorType: comment.AuthorType, ActorID: uuidToString(comment.AuthorID), OriginatorUserID: uuidToString(originator)}
}

func commentDeliveryAdmissionForTest(t *testing.T, args db.UpsertCommentDeliveryObligationParams) db.UpsertCommentDeliveryObligationParams {
	t.Helper()
	source := commentDeliverySourceForTest(t, args.CommentID)
	args.SourceRevision = source.Comment.Revision
	args.SourceContent = source.Comment.Content
	args.ActorType = source.ActorType
	args.ActorID = source.Comment.AuthorID
	args.OriginatorUserID = source.Comment.AuthorID
	if source.ActorType != "member" {
		args.OriginatorUserID = testHandler.TaskService.ResolveOriginatorFromTriggerComment(t.Context(), source.Comment.WorkspaceID, args.CommentID)
	}
	args.AuthoringTaskID = source.Comment.SourceTaskID
	return args
}

func commentDeliveryRecordForTest(t *testing.T, commentID, agentID pgtype.UUID) db.CommentDeliveryObligation {
	t.Helper()
	args := commentDeliveryAdmissionForTest(t, db.UpsertCommentDeliveryObligationParams{WorkspaceID: parseUUID(testWorkspaceID), CommentID: commentID, AgentID: agentID})
	record, err := testHandler.Queries.UpsertCommentDeliveryObligation(t.Context(), args)
	if err != nil {
		t.Fatal(err)
	}
	record, err = testHandler.Queries.TryClaimCommentDeliveryObligation(t.Context(), db.TryClaimCommentDeliveryObligationParams{WorkspaceID: record.WorkspaceID, CommentID: record.CommentID, AgentID: record.AgentID, CommentRevision: record.CommentRevision})
	if err != nil {
		t.Fatal(err)
	}
	dbfx.Cleanup(t, `DELETE FROM comment_delivery_obligation WHERE comment_id=$1 AND agent_id=$2`, commentID, agentID)
	return record
}
