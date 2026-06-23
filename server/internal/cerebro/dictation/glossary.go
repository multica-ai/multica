package dictation

import (
	"context"
	"log/slog"
	"strings"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// maxGlossaryTerms caps the auto-glossary so the combined Whisper prompt stays
// within its ~224-token budget (the prompt also carries the language hint and
// any manual terms).
const maxGlossaryTerms = 64

// workspaceGlossary assembles a decoding-bias glossary from the workspace's own
// business objects — agent, squad, project and teammate names — so dictation
// spells the proper nouns people actually say correctly, without anyone
// maintaining a list. This mirrors how the meeting app auto-discovers the
// attendee names + meeting title and feeds them to Whisper. Best-effort: a
// failing source is logged and skipped, never fatal, because the glossary must
// never break a transcription.
func (h *Handler) workspaceGlossary(ctx context.Context, workspaceID string) string {
	if h.queries == nil {
		return ""
	}
	wsUUID, err := util.ParseUUID(workspaceID)
	if err != nil {
		return ""
	}

	seen := map[string]struct{}{}
	terms := make([]string, 0, maxGlossaryTerms)
	add := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" || len(terms) >= maxGlossaryTerms {
			return
		}
		key := strings.ToLower(name)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		terms = append(terms, name)
	}

	// Order encodes priority: the highest-value, lowest-cardinality proper nouns
	// first, so the cap drops the long tail (teammates) rather than agents or
	// projects.
	if agents, err := h.queries.ListAgents(ctx, wsUUID); err != nil {
		slog.Debug("dictation glossary: list agents failed", "error", err)
	} else {
		for _, a := range agents {
			add(a.Name)
		}
	}
	if squads, err := h.queries.ListSquads(ctx, wsUUID); err != nil {
		slog.Debug("dictation glossary: list squads failed", "error", err)
	} else {
		for _, s := range squads {
			add(s.Name)
		}
	}
	if projects, err := h.queries.ListProjects(ctx, db.ListProjectsParams{WorkspaceID: wsUUID}); err != nil {
		slog.Debug("dictation glossary: list projects failed", "error", err)
	} else {
		for _, p := range projects {
			add(p.Title)
		}
	}
	if members, err := h.queries.ListMembersWithUser(ctx, wsUUID); err != nil {
		slog.Debug("dictation glossary: list members failed", "error", err)
	} else {
		for _, m := range members {
			add(m.UserName)
		}
	}

	return strings.Join(terms, ", ")
}

// mergeGlossary combines the user's manual terms with the auto-glossary,
// deduping case-insensitively. Manual terms come first — the user explicitly
// cared about them — and the comma-separated result is what the inference
// service receives as the decoding hint.
func mergeGlossary(manual, auto string) string {
	seen := map[string]struct{}{}
	parts := []string{}
	addAll := func(s string) {
		for _, t := range strings.Split(s, ",") {
			t = strings.TrimSpace(t)
			if t == "" {
				continue
			}
			key := strings.ToLower(t)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			parts = append(parts, t)
		}
	}
	addAll(manual)
	addAll(auto)
	return strings.Join(parts, ", ")
}
