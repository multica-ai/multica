package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// These routes are default-off. A claim is a once-ever reservation, NOT a run
// receipt or permission to dispatch. A task-token caller may PUT a claim, but
// historical GET is restricted to human workspace owners/admins by the router.
// An unknown PUT outcome needs read-back with a human admin credential and
// manual reconciliation; the machine caller must never retry into another run.
const supportEvidenceLimit = 500

type supportSnapshot struct {
	Issue    IssueResponse `json:"issue"`
	Comments struct {
		Rows      []CommentResponse `json:"rows"`
		Total     int               `json:"total"`
		Complete  bool              `json:"complete"`
		Truncated bool              `json:"truncated"`
	} `json:"comments"`
	Members struct {
		Rows     []MemberWithUserResponse `json:"rows"`
		Complete bool                     `json:"complete"`
	} `json:"members"`
	SnapshotAt time.Time `json:"snapshot_at"`
}

type supportClaimRequest struct {
	ExpectedRevision     int64  `json:"expected_revision"`
	StaffCommentID       string `json:"staff_comment_id"`
	StaffCommentRevision int64  `json:"staff_comment_revision"`
	AttestationDigest    string `json:"attestation_digest"`
}

type supportClaim struct {
	WorkspaceID          string    `json:"workspace_id"`
	IssueID              string    `json:"issue_id"`
	IssueRevision        int64     `json:"issue_revision"`
	StaffCommentID       string    `json:"staff_comment_id"`
	StaffCommentRevision int64     `json:"staff_comment_revision"`
	AttestationDigest    string    `json:"attestation_digest"`
	ClaimedByUserID      string    `json:"claimed_by_user_id"`
	ClaimedAt            time.Time `json:"claimed_at"`
}

func supportRequest(r *http.Request) (supportClaimRequest, bool) {
	var body supportClaimRequest
	decoder := json.NewDecoder(io.LimitReader(r.Body, 2049))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		return body, false
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) || body.ExpectedRevision < 1 ||
		body.StaffCommentRevision < 1 || len(body.AttestationDigest) != 64 ||
		strings.ToLower(body.AttestationDigest) != body.AttestationDigest {
		return body, false
	}
	if _, err := util.ParseUUID(body.StaffCommentID); err != nil {
		return body, false
	}
	_, err := hex.DecodeString(body.AttestationDigest)
	return body, err == nil
}

func (h *Handler) supportTx(w http.ResponseWriter, r *http.Request, adminOnly bool) (pgx.Tx, db.Issue, string, bool) {
	if !h.SupportDispatchClaimEnabled {
		writeError(w, http.StatusNotFound, "not found")
		return nil, db.Issue{}, "", false
	}
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return nil, db.Issue{}, "", false
	}
	userID := requestUserID(r)
	userUUID, err := util.ParseUUID(userID)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "user not authenticated")
		return nil, db.Issue{}, "", false
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err == nil {
		_, err = tx.Exec(r.Context(), "SET TRANSACTION ISOLATION LEVEL REPEATABLE READ")
	}
	if err != nil {
		if tx != nil {
			_ = tx.Rollback(r.Context())
		}
		writeError(w, http.StatusServiceUnavailable, "support evidence unavailable")
		return nil, db.Issue{}, "", false
	}
	// CREATE INDEX CONCURRENTLY may leave an invalid index on interruption.
	// Never use a claimed "once ever" contract unless its real DB fence is valid.
	var fenced bool
	if err := tx.QueryRow(r.Context(), `SELECT EXISTS (
		SELECT 1 FROM pg_index WHERE indexrelid = to_regclass('support_dispatch_claim_issue_unique')
		AND indrelid = to_regclass('support_dispatch_claim')
		AND indisunique AND indisvalid AND indisready)`).Scan(&fenced); err != nil || !fenced {
		_ = tx.Rollback(r.Context())
		writeError(w, http.StatusServiceUnavailable, "support claim fence unavailable")
		return nil, db.Issue{}, "", false
	}
	// Lock the workspace before its member and issue rows. Workspace teardown
	// takes FOR UPDATE on this row and checks for claims before deleting data.
	// This lock order prevents a claim committing after that check.
	if err := tx.QueryRow(r.Context(), `SELECT 1 FROM workspace WHERE id = $1 FOR KEY SHARE`,
		issue.WorkspaceID).Scan(new(int)); err != nil {
		_ = tx.Rollback(r.Context())
		writeError(w, http.StatusConflict, "workspace evidence changed")
		return nil, db.Issue{}, "", false
	}
	// Recheck access inside the same DB snapshot; the router's cached member
	// gate alone cannot prove membership at reservation time.
	var role string
	if err := tx.QueryRow(r.Context(), `SELECT role FROM member WHERE workspace_id = $1 AND user_id = $2 FOR SHARE`,
		issue.WorkspaceID, userUUID).Scan(&role); err != nil {
		_ = tx.Rollback(r.Context())
		writeError(w, http.StatusNotFound, "issue not found")
		return nil, db.Issue{}, "", false
	}
	if adminOnly && role != "owner" && role != "admin" {
		_ = tx.Rollback(r.Context())
		writeError(w, http.StatusForbidden, "claim requires workspace administration")
		return nil, db.Issue{}, "", false
	}
	// Issue edits and comment writes lock their parent issue. A stale snapshot
	// under REPEATABLE READ fails with a serialization error rather than quietly
	// binding a new comment to an old revision.
	if err := tx.QueryRow(r.Context(), `SELECT 1 FROM issue WHERE id = $1 AND workspace_id = $2 FOR UPDATE`,
		issue.ID, issue.WorkspaceID).Scan(new(int)); err != nil {
		_ = tx.Rollback(r.Context())
		writeError(w, http.StatusConflict, "issue evidence changed")
		return nil, db.Issue{}, "", false
	}
	current, err := h.Queries.WithTx(tx).GetIssueInWorkspace(r.Context(), db.GetIssueInWorkspaceParams{
		ID: issue.ID, WorkspaceID: issue.WorkspaceID,
	})
	if err != nil || current.Revision != issue.Revision {
		_ = tx.Rollback(r.Context())
		writeError(w, http.StatusConflict, "issue evidence changed")
		return nil, db.Issue{}, "", false
	}
	return tx, current, userID, true
}

func (h *Handler) readSupportSnapshot(ctx context.Context, tx pgx.Tx, issue db.Issue) (supportSnapshot, error) {
	var snapshot supportSnapshot
	var prefix string
	if err := tx.QueryRow(ctx, `SELECT issue_prefix FROM workspace WHERE id = $1`, issue.WorkspaceID).Scan(&prefix); err != nil {
		return snapshot, err
	}
	snapshot.Issue = issueToResponse(issue, prefix)
	members, err := h.Queries.WithTx(tx).ListMembersWithUser(ctx, issue.WorkspaceID)
	if err != nil || len(members) > supportEvidenceLimit {
		return snapshot, errors.New("incomplete support member evidence")
	}
	// The join deliberately excludes missing users; a bare joined array is not
	// positive proof that every workspace membership was represented.
	var memberCount int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM member WHERE workspace_id = $1`, issue.WorkspaceID).Scan(&memberCount); err != nil || memberCount != len(members) {
		return snapshot, errors.New("incomplete support member evidence")
	}
	snapshot.Members.Rows = make([]MemberWithUserResponse, 0, len(members))
	for _, m := range members {
		snapshot.Members.Rows = append(snapshot.Members.Rows, MemberWithUserResponse{
			ID: uuidToString(m.ID), WorkspaceID: uuidToString(m.WorkspaceID),
			UserID: uuidToString(m.UserID), Role: m.Role,
			CreatedAt: timestampToString(m.CreatedAt), Name: m.UserName,
			Email: m.UserEmail, AvatarURL: h.resolveAvatarURLPtr(textToPtr(m.UserAvatarUrl)),
		})
	}
	snapshot.Members.Complete = true
	rows, err := tx.Query(ctx, `SELECT id, issue_id, author_type, author_id, content, type,
		parent_id, created_at, updated_at, revision, deleted_at FROM comment
		WHERE workspace_id = $1 AND issue_id = $2 ORDER BY created_at, id LIMIT $3`,
		issue.WorkspaceID, issue.ID, supportEvidenceLimit+1)
	if err != nil {
		return snapshot, err
	}
	snapshot.Comments.Rows = make([]CommentResponse, 0)
	for rows.Next() {
		var c db.Comment
		if err := rows.Scan(&c.ID, &c.IssueID, &c.AuthorType, &c.AuthorID, &c.Content,
			&c.Type, &c.ParentID, &c.CreatedAt, &c.UpdatedAt, &c.Revision, &c.DeletedAt); err != nil {
			rows.Close()
			return snapshot, err
		}
		if len(snapshot.Comments.Rows) == supportEvidenceLimit {
			rows.Close()
			return snapshot, errors.New("incomplete support comment evidence")
		}
		snapshot.Comments.Rows = append(snapshot.Comments.Rows, commentToResponse(c, nil, nil))
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return snapshot, err
	}
	snapshot.Comments.Total = len(snapshot.Comments.Rows)
	snapshot.Comments.Complete = true
	snapshot.SnapshotAt = time.Now().UTC()
	return snapshot, nil
}

func supportClaimForIssue(ctx context.Context, tx pgx.Tx, issue db.Issue) (supportClaim, error) {
	return supportClaimByID(ctx, tx, issue.WorkspaceID, issue.ID)
}

func supportClaimByID(ctx context.Context, tx pgx.Tx, workspaceID, issueID pgtype.UUID) (supportClaim, error) {
	var claim supportClaim
	err := tx.QueryRow(ctx, `SELECT workspace_id, issue_id, issue_revision, staff_comment_id,
		staff_comment_revision, attestation_digest, claimed_by_user_id, claimed_at
		FROM support_dispatch_claim WHERE workspace_id = $1 AND issue_id = $2`,
		workspaceID, issueID).Scan(&claim.WorkspaceID, &claim.IssueID, &claim.IssueRevision,
		&claim.StaffCommentID, &claim.StaffCommentRevision, &claim.AttestationDigest,
		&claim.ClaimedByUserID, &claim.ClaimedAt)
	return claim, err
}

func (h *Handler) GetSupportDispatchEvidence(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	tx, issue, _, ok := h.supportTx(w, r, false)
	if !ok {
		return
	}
	defer tx.Rollback(r.Context())
	snapshot, err := h.readSupportSnapshot(r.Context(), tx, issue)
	if err != nil {
		writeError(w, http.StatusConflict, "complete support evidence unavailable")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusConflict, "issue evidence changed")
		return
	}
	writeJSON(w, http.StatusOK, snapshot)
}

func (h *Handler) GetSupportDispatchClaim(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !h.SupportDispatchClaimEnabled {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	workspaceID, err := util.ParseUUID(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid workspace id")
		return
	}
	issueID, err := util.ParseUUID(chi.URLParam(r, "issueID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid issue id")
		return
	}
	userID, err := util.ParseUUID(requestUserID(r))
	if err != nil {
		writeError(w, http.StatusUnauthorized, "user not authenticated")
		return
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "support claim unavailable")
		return
	}
	defer tx.Rollback(r.Context())
	// Authorization is rechecked alongside the historical row read. The issue
	// and its comments may no longer exist after a committed claim.
	var role string
	if err := tx.QueryRow(r.Context(), `SELECT role FROM member WHERE workspace_id = $1 AND user_id = $2`,
		workspaceID, userID).Scan(&role); err != nil {
		writeError(w, http.StatusNotFound, "support claim not found")
		return
	}
	if role != "owner" && role != "admin" {
		writeError(w, http.StatusForbidden, "claim requires workspace administration")
		return
	}
	claim, err := supportClaimByID(r.Context(), tx, workspaceID, issueID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "support claim not found")
		} else {
			writeError(w, http.StatusServiceUnavailable, "support claim unavailable")
		}
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusServiceUnavailable, "support claim unavailable")
		return
	}
	writeJSON(w, http.StatusOK, claim)
}

func (h *Handler) ClaimSupportDispatch(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !h.SupportDispatchClaimEnabled {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	request, ok := supportRequest(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid support claim")
		return
	}
	tx, issue, userID, ok := h.supportTx(w, r, true)
	if !ok {
		return
	}
	defer tx.Rollback(r.Context())
	// A repeated PUT, even with identical evidence, is NOT a fresh dispatch
	// grant. Human admins read the historical workspace-scoped row to reconcile
	// unknown outcomes; task-token callers cannot use that GET or auto-retry.
	if _, err := supportClaimForIssue(r.Context(), tx, issue); err == nil {
		writeError(w, http.StatusConflict, "issue already claimed")
		return
	} else if !errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusServiceUnavailable, "support claim unavailable")
		return
	}
	if issue.Revision != request.ExpectedRevision {
		writeError(w, http.StatusConflict, "issue evidence changed")
		return
	}
	snapshot, err := h.readSupportSnapshot(r.Context(), tx, issue)
	if err != nil {
		writeError(w, http.StatusConflict, "complete support evidence unavailable")
		return
	}
	var matching *CommentResponse
	for i := range snapshot.Comments.Rows {
		if snapshot.Comments.Rows[i].ID == request.StaffCommentID {
			matching = &snapshot.Comments.Rows[i]
			break
		}
	}
	if matching == nil || matching.Revision != request.StaffCommentRevision ||
		matching.DeletedAt != nil || matching.AuthorType != "member" ||
		matching.AuthorID != uuidToString(issue.AssigneeID) || matching.Type != "comment" ||
		matching.ParentID != nil {
		writeError(w, http.StatusConflict, "staff attestation changed")
		return
	}
	// The parent issue lock serializes normal comment mutations. This row lock
	// additionally rejects a comment altered without a parent revision bump
	// between the snapshot and reservation (REPEATABLE READ raises 40001).
	var lockedContent string
	var lockedRevision int64
	var deletedAt pgtype.Timestamptz
	if err := tx.QueryRow(r.Context(), `SELECT content, revision, deleted_at FROM comment
		WHERE id = $1 AND issue_id = $2 AND workspace_id = $3 FOR SHARE`,
		request.StaffCommentID, issue.ID, issue.WorkspaceID).Scan(&lockedContent, &lockedRevision, &deletedAt); err != nil ||
		lockedContent != matching.Content || lockedRevision != matching.Revision || deletedAt.Valid {
		writeError(w, http.StatusConflict, "staff attestation changed")
		return
	}
	digest := sha256.Sum256([]byte(matching.Content))
	if hex.EncodeToString(digest[:]) != request.AttestationDigest {
		writeError(w, http.StatusConflict, "staff attestation changed")
		return
	}
	var claimedAt time.Time
	err = tx.QueryRow(r.Context(), `INSERT INTO support_dispatch_claim (
		workspace_id, issue_id, issue_revision, staff_comment_id, staff_comment_revision,
		attestation_digest, claimed_by_user_id) VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (workspace_id, issue_id) DO NOTHING RETURNING claimed_at`,
		issue.WorkspaceID, issue.ID, issue.Revision, request.StaffCommentID,
		request.StaffCommentRevision, request.AttestationDigest, userID).Scan(&claimedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusConflict, "issue already claimed")
		return
	}
	if err != nil {
		writeError(w, http.StatusConflict, "support claim unavailable")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		// An ambiguous commit outcome is never a dispatch grant. Read back only.
		writeError(w, http.StatusConflict, "support claim outcome unknown; read back before operator reconciliation")
		return
	}
	writeJSON(w, http.StatusCreated, struct {
		Claim    supportClaim    `json:"claim"`
		Evidence supportSnapshot `json:"evidence"`
	}{Claim: supportClaim{
		WorkspaceID: uuidToString(issue.WorkspaceID), IssueID: uuidToString(issue.ID),
		IssueRevision: issue.Revision, StaffCommentID: request.StaffCommentID,
		StaffCommentRevision: request.StaffCommentRevision, AttestationDigest: request.AttestationDigest,
		ClaimedByUserID: userID, ClaimedAt: claimedAt,
	}, Evidence: snapshot})
}
