package handler

// CEREBRO-PATCH(user-profile-prompt): JEH-304/JEH-1031 — compile the stored
// user communication profile into the prompt string the daemon injects as
// `## User Communication Profile`. Extracted to a cerebro sibling file after
// upstream sync #4454 deleted the inline helper from daemon.go (FIR-2743);
// only the two marked call sites in ClaimTaskByRuntime remain upstream.

import (
	"context"
	"errors"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/profile"
)

// compileProfileForUser loads the user's saved communication profile (if any)
// and compiles it into a prompt fragment. Returns "" when the user has no
// profile or on any error — the claim must never fail because profile
// compilation did.
func (h *Handler) compileProfileForUser(ctx context.Context, userID pgtype.UUID) string {
	if !userID.Valid {
		return ""
	}
	row, err := h.Queries.GetUserProfile(ctx, userID)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			slog.Warn("load user profile failed", "user_id", uuidToString(userID), "error", err)
		}
		return ""
	}
	displayName := ""
	if user, err := h.Queries.GetUser(ctx, userID); err == nil {
		displayName = user.Name
	}
	// CEREBRO-PATCH(user-profile-v2-compile): JEH-1031 — pass the 4 scope
	// ratings + custom prompt + mode into the compiler.
	prompt, err := profile.CompileFromRow(
		row.Persona,
		row.Language,
		displayName,
		int(row.LengthPref),
		int(row.AutonomyPref),
		int(row.GitPref),
		int(row.CodePref),
		int(row.ComputerPref),
		int(row.ProcessPref),
		row.AntiPatterns,
		row.CustomPrompt,
		row.PromptMode,
	)
	if err != nil {
		slog.Warn("compile user profile failed", "user_id", uuidToString(userID), "error", err)
		return ""
	}
	return prompt
}
