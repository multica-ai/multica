package mention

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// NameResolver looks up the canonical display name for an entity referenced
// by a mention link. Implemented by *db.Queries in production.
type NameResolver interface {
	GetAgentInWorkspace(ctx context.Context, arg db.GetAgentInWorkspaceParams) (db.Agent, error)
	GetSquadInWorkspace(ctx context.Context, arg db.GetSquadInWorkspaceParams) (db.Squad, error)
	GetUser(ctx context.Context, id pgtype.UUID) (db.User, error)
}

// CanonicalizeMentions rewrites every agent/member/squad mention in `content`
// so the visible label matches the actual entity name stored in the database.
// Defends against the failure mode in which an author (typically an LLM)
// writes `[@A](mention://agent/<B-uuid>)` — the UI renders "@A" but the
// routing layer triggers B. After this pass, the label and the UUID's
// resolved entity always agree, eliminating that silent mismatch.
//
// Behaviour:
//   - agent / member / squad mention whose UUID resolves in this workspace:
//     label replaced with the entity's current `name` (with `[` and `]`
//     escaped to keep the link grammar intact).
//   - agent / member / squad mention whose UUID does NOT resolve (deleted,
//     cross-workspace, fake): mention link is stripped down to plain
//     `@<original-label>` text so it can never falsely appear addressed to
//     a real entity.
//   - `issue` and `all` mentions: left untouched. Issue mention labels are
//     resolved at render time via IssueMentionLink; `all` is a literal.
//   - Mentions inside inline code or fenced code blocks: left untouched
//     (shared with ExpandIssueIdentifiers via findSkipRegions).
func CanonicalizeMentions(ctx context.Context, resolver NameResolver, workspaceID pgtype.UUID, content string) string {
	if content == "" {
		return content
	}
	matches := util.MentionRe.FindAllStringSubmatchIndex(content, -1)
	if len(matches) == 0 {
		return content
	}
	skipRegions := findSkipRegions(content)

	type replacement struct {
		start, end int
		text       string
	}
	var replacements []replacement

	for _, m := range matches {
		fullStart, fullEnd := m[0], m[1]
		if inSkipRegion(fullStart, skipRegions) {
			continue
		}
		// util.MentionRe submatch layout: 1=label (without leading @),
		// 2=type, 3=id. Index pairs land at m[2:4], m[4:6], m[6:8].
		label := content[m[2]:m[3]]
		mentionType := content[m[4]:m[5]]
		idStr := content[m[6]:m[7]]

		if mentionType != "agent" && mentionType != "member" && mentionType != "squad" {
			continue
		}

		canonical, ok := lookupCanonicalName(ctx, resolver, workspaceID, mentionType, idStr)
		if !ok {
			replacements = append(replacements, replacement{
				start: fullStart,
				end:   fullEnd,
				text:  "@" + label,
			})
			continue
		}
		if canonical == label {
			continue
		}
		replacements = append(replacements, replacement{
			start: fullStart,
			end:   fullEnd,
			text:  fmt.Sprintf("[@%s](mention://%s/%s)", escapeMentionLabel(canonical), mentionType, idStr),
		})
	}

	if len(replacements) == 0 {
		return content
	}
	result := content
	for i := len(replacements) - 1; i >= 0; i-- {
		r := replacements[i]
		result = result[:r.start] + r.text + result[r.end:]
	}
	return result
}

func lookupCanonicalName(ctx context.Context, r NameResolver, workspaceID pgtype.UUID, mentionType, idStr string) (string, bool) {
	id, err := util.ParseUUID(idStr)
	if err != nil {
		return "", false
	}
	switch mentionType {
	case "agent":
		ag, err := r.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{ID: id, WorkspaceID: workspaceID})
		if err != nil {
			return "", false
		}
		return ag.Name, true
	case "squad":
		sq, err := r.GetSquadInWorkspace(ctx, db.GetSquadInWorkspaceParams{ID: id, WorkspaceID: workspaceID})
		if err != nil {
			return "", false
		}
		return sq.Name, true
	case "member":
		u, err := r.GetUser(ctx, id)
		if err != nil {
			return "", false
		}
		return u.Name, true
	}
	return "", false
}

// escapeMentionLabel mirrors the editor's mention-extension escaping
// (packages/views/editor/extensions/mention-extension.ts): only `[` and `]`
// are escaped so the bracket structure of the resulting markdown link
// survives names that contain them (e.g. "David[TF]").
func escapeMentionLabel(s string) string {
	s = strings.ReplaceAll(s, "[", "\\[")
	s = strings.ReplaceAll(s, "]", "\\]")
	return s
}
