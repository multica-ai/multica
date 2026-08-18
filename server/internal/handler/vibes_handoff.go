package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/vibeshandoff"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const vibesTagAudience = "vibes-tag-local"

var (
	vibesHandoffCodePattern   = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)
	vibesWorkspaceSlugPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
)

type vibesHandoffRequest struct {
	Code          string `json:"code"`
	Audience      string `json:"audience"`
	WorkspaceSlug string `json:"workspaceSlug"`
}

func (h *Handler) VIBESHandoff(w http.ResponseWriter, r *http.Request) {
	client, err := vibeshandoff.NewClient(h.cfg.VIBESHandoffConsumeURL, h.cfg.VIBESHandoffServiceSecret)
	if err != nil {
		writeErrorCode(w, http.StatusNotFound, "not_found", "Not found")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16*1024)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var request vibesHandoffRequest
	if err := decoder.Decode(&request); err != nil || request.Audience != vibesTagAudience || !vibesHandoffCodePattern.MatchString(request.Code) || !vibesWorkspaceSlugPattern.MatchString(request.WorkspaceSlug) {
		writeErrorCode(w, http.StatusBadRequest, "invalid_handoff", "Tag handoff is invalid")
		return
	}
	identity, err := client.Consume(r.Context(), request.Code, request.Audience)
	if err != nil || identity.WorkspaceSlug != request.WorkspaceSlug || !validVIBESIdentity(identity) {
		writeErrorCode(w, http.StatusUnauthorized, "handoff_rejected", "Tag handoff was rejected")
		return
	}
	user, err := h.mirrorVIBESIdentity(r.Context(), identity)
	if err != nil {
		writeErrorCode(w, http.StatusUnauthorized, "handoff_rejected", "Tag handoff was rejected")
		return
	}
	token, err := h.issueJWT(user)
	if err != nil || auth.SetAuthCookies(w, token) != nil {
		writeErrorCode(w, http.StatusInternalServerError, "session_failed", "Tag session could not be created")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNoContent)
}

func validVIBESIdentity(identity vibeshandoff.Identity) bool {
	return len(identity.UserID) <= 255 &&
		len(identity.WorkspaceID) <= 255 &&
		len(identity.Name) <= 255 &&
		len(identity.WorkspaceName) <= 255 &&
		len(identity.Email) <= 320 &&
		vibesWorkspaceSlugPattern.MatchString(identity.WorkspaceSlug)
}

func syntheticVIBESEmail(stableUserID string) string {
	digest := sha256.Sum256([]byte(stableUserID))
	return "vibes+" + hex.EncodeToString(digest[:]) + "@identity.invalid"
}

func (h *Handler) mirrorVIBESIdentity(ctx context.Context, identity vibeshandoff.Identity) (db.User, error) {
	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		return db.User{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, "vibes-user:"+identity.UserID); err != nil {
		return db.User{}, err
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, "vibes-workspace:"+identity.WorkspaceID); err != nil {
		return db.User{}, err
	}
	queries := db.New(tx)

	var userID pgtype.UUID
	err = tx.QueryRow(ctx, `SELECT multica_user_id FROM vibes_user_mirror WHERE vibes_user_id = $1`, identity.UserID).Scan(&userID)
	var user db.User
	if errors.Is(err, pgx.ErrNoRows) {
		user, err = queries.CreateUser(ctx, db.CreateUserParams{
			Name:  identity.Name,
			Email: syntheticVIBESEmail(identity.UserID),
		})
		if err != nil {
			return db.User{}, err
		}
		userID = user.ID
		_, err = tx.Exec(ctx, `
			INSERT INTO vibes_user_mirror (vibes_user_id, multica_user_id, profile_email)
			VALUES ($1, $2, $3)
		`, identity.UserID, userID, identity.Email)
	} else if err == nil {
		user, err = queries.GetUser(ctx, userID)
		if err == nil {
			_, err = tx.Exec(ctx, `
				UPDATE vibes_user_mirror SET profile_email = $2, updated_at = now()
				WHERE vibes_user_id = $1
			`, identity.UserID, identity.Email)
		}
		if err == nil && user.Name != identity.Name {
			_, err = tx.Exec(ctx, `UPDATE "user" SET name = $2, updated_at = now() WHERE id = $1`, userID, identity.Name)
			user.Name = identity.Name
		}
	}
	if err != nil {
		return db.User{}, err
	}

	var workspaceID pgtype.UUID
	err = tx.QueryRow(ctx, `SELECT multica_workspace_id FROM vibes_workspace_mirror WHERE vibes_workspace_id = $1`, identity.WorkspaceID).Scan(&workspaceID)
	if errors.Is(err, pgx.ErrNoRows) {
		workspace, createErr := queries.CreateWorkspace(ctx, db.CreateWorkspaceParams{
			Name:        identity.WorkspaceName,
			Slug:        identity.WorkspaceSlug,
			IssuePrefix: "TAG",
		})
		if createErr != nil {
			return db.User{}, createErr
		}
		workspaceID = workspace.ID
		_, err = tx.Exec(ctx, `
			INSERT INTO vibes_workspace_mirror (vibes_workspace_id, multica_workspace_id)
			VALUES ($1, $2)
		`, identity.WorkspaceID, workspaceID)
	} else if err == nil {
		var mirroredSlug string
		err = tx.QueryRow(ctx, `SELECT slug FROM workspace WHERE id = $1`, workspaceID).Scan(&mirroredSlug)
		if err == nil && mirroredSlug != identity.WorkspaceSlug {
			err = errors.New("VIBES workspace slug does not match its mirror")
		}
	}
	if err != nil {
		return db.User{}, err
	}

	role := "member"
	if identity.Role == "owner" {
		role = "owner"
	}
	member, err := queries.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
		UserID:      userID,
		WorkspaceID: workspaceID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		_, err = queries.CreateMember(ctx, db.CreateMemberParams{
			WorkspaceID: workspaceID,
			UserID:      userID,
			Role:        role,
		})
	} else if err == nil && member.Role != role {
		_, err = queries.UpdateMemberRole(ctx, db.UpdateMemberRoleParams{ID: member.ID, Role: role})
	}
	if err != nil {
		return db.User{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return db.User{}, err
	}
	return user, nil
}
