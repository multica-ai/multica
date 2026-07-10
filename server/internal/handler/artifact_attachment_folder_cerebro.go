package handler

// CEREBRO-PATCH(attachment-folder): FIR-2697 part 4 — an agent-authored
// document/note that gets ATTACHED to a chat message or issue comment must
// ALWAYS have a folder. Attaching is not a first-class record: the CLI flags
// `multica issue comment add --artifact <id>` and `multica chat session send
// --artifact <id>` simply append a `mention://artifact/<id>` token to the body,
// which the frontend renders as the white artifact card. So the authoritative
// enforcement point is the two body-persist handlers: after a comment or an
// agent chat message is stored, we scan its body for artifact tokens and, for
// every agent-authored artifact that still has no folder, file it into the same
// automatic "Agent Runs > owner > agent > run" structure part 3 builds.
//
// This closes the gap part 3 alone leaves open: part 3 only auto-files at
// CREATE time, and only when its flag was on then. A document created before
// rollout (or by any path that skipped the create-time hook) could still be
// attached with a NULL folder. Part 4 guarantees "attached ⇒ has a folder".
//
// Scope, matching the locked plan:
//   - only artifacts authored by an AGENT (a raw file upload has no artifact
//     behind it and never reaches here — it is not a `mention://artifact` token);
//   - only artifacts that do not already have a folder (an agent's own choice,
//     or a folder part 3 already gave it, is never overridden);
//   - the run leaf is resolved for the ARTIFACT'S author agent, reusing part 3's
//     ensureAgentRunFolder so the document lands under the agent that wrote it.
//
// Everything here is best-effort: a comment / chat message must always succeed
// even if the folder cannot be resolved, so every failure is logged and
// swallowed. Gated by the workspace feature flag cerebro_attachment_folder
// (default off), mirrored in packages/cerebro-feature-flags/registry.ts. The
// two upstream/persist handlers gain a single marked call each; all logic lives
// in this file.

import (
	"context"
	"log/slog"
	"net/http"
	"regexp"

	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// attachmentFolderFlagKey is the workspace kill switch for the part-4 behaviour
// (both the attach-time enforcement here and the folder link on the card).
// Mirrored in packages/cerebro-feature-flags/registry.ts.
const attachmentFolderFlagKey = "cerebro_attachment_folder"

// artifactMentionRe extracts the artifact id from every `mention://artifact/<id>`
// token in a body. The id is a UUID; we match loosely (hex + hyphens) and let
// parseUUID reject anything malformed rather than being strict in the regex.
var artifactMentionRe = regexp.MustCompile(`mention://artifact/([0-9a-fA-F-]{8,})`)

// attachmentFolderEnabled reports whether part 4 is switched ON for this
// workspace. The flag default is OFF (no stored row), so a missing override or
// a lookup error resolves to OFF — attaching behaves exactly as before.
func (h *Handler) attachmentFolderEnabled(ctx context.Context, wsID pgtype.UUID) bool {
	if h.CerebroQueries == nil {
		return false
	}
	enabled, err := h.CerebroQueries.GetCerebroFeatureFlag(ctx, cerebrodb.GetCerebroFeatureFlagParams{
		WorkspaceID: wsID,
		UserID:      pgtype.UUID{Valid: true}, // all-zero sentinel = workspace-level row
		FlagKey:     attachmentFolderFlagKey,
	})
	if err != nil {
		return false
	}
	return enabled
}

// parseArtifactMentions returns the de-duplicated list of artifact ids
// referenced by `mention://artifact/<id>` tokens in body, preserving first-seen
// order. An empty body or no tokens yields nil.
func parseArtifactMentions(body string) []string {
	if body == "" {
		return nil
	}
	matches := artifactMentionRe.FindAllStringSubmatch(body, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(matches))
	ids := make([]string, 0, len(matches))
	for _, m := range matches {
		id := m[1]
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	return ids
}

// ensureAttachedArtifactsFiled is the single hook the comment-create and
// agent-chat-send handlers call after they persist a body. For every artifact
// token in that body it guarantees an agent-authored, folder-less artifact ends
// up in a folder. Best-effort and non-fatal: the surrounding comment / chat
// message has already been saved, and nothing here is allowed to fail the
// request. A no-op when the flag is off or the body has no artifact tokens.
func (h *Handler) ensureAttachedArtifactsFiled(r *http.Request, workspaceID, body string) {
	ctx := r.Context()
	wsUUID := parseUUID(workspaceID)
	if !wsUUID.Valid || !h.attachmentFolderEnabled(ctx, wsUUID) {
		return
	}
	ids := parseArtifactMentions(body)
	if len(ids) == 0 {
		return
	}

	for _, idStr := range ids {
		artifactUUID := parseUUID(idStr)
		if !artifactUUID.Valid {
			continue
		}
		art, err := h.Queries.GetArtifact(ctx, db.GetArtifactParams{
			ID:          artifactUUID,
			WorkspaceID: wsUUID,
		})
		if err != nil {
			// Unknown / cross-workspace / deleted reference — nothing to file.
			continue
		}
		// Only an agent's own document/note gets a folder; a folder already set
		// (agent's choice or part 3) is never overridden.
		if art.AuthorType != "agent" || art.FolderID.Valid {
			continue
		}

		surface := "document"
		if art.Kind == "note" {
			surface = "note"
		}

		folderID, err := h.ensureAgentRunFolder(r, wsUUID, uuidToString(art.AuthorID), surface)
		if err != nil {
			slog.Warn("attachment-folder: could not resolve folder for attached artifact, leaving at root",
				"error", err, "workspace_id", workspaceID, "artifact_id", idStr, "surface", surface)
			continue
		}
		if _, err := h.Queries.MoveArtifactToFolder(ctx, db.MoveArtifactToFolderParams{
			ID:          artifactUUID,
			WorkspaceID: wsUUID,
			FolderID:    folderID,
		}); err != nil {
			slog.Warn("attachment-folder: could not file attached artifact into folder",
				"error", err, "workspace_id", workspaceID, "artifact_id", idStr)
			continue
		}
	}
}
