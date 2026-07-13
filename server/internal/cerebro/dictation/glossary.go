package dictation

import (
	"context"
	"log/slog"
	"strings"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	maxWorkspaceGlossaryTerms = 64
	maxGlossaryTerms          = 328
)

// workspaceGlossary returns bounded correction candidates from the current
// workspace. These terms are used after transcription; they must never be fed
// into Whisper's audio decoder.
func (h *Handler) workspaceGlossary(ctx context.Context, workspaceID string) string {
	if h.queries == nil {
		return ""
	}
	wsUUID, err := util.ParseUUID(workspaceID)
	if err != nil {
		return ""
	}

	terms := make([]string, 0, maxWorkspaceGlossaryTerms)
	seen := map[string]struct{}{}
	add := func(value string) {
		value = strings.TrimSpace(value)
		key := strings.ToLower(value)
		if value == "" || len(terms) >= maxWorkspaceGlossaryTerms {
			return
		}
		if _, exists := seen[key]; exists {
			return
		}
		seen[key] = struct{}{}
		terms = append(terms, value)
	}

	if agents, err := h.queries.ListAgents(ctx, wsUUID); err != nil {
		slog.Debug("dictation glossary: list agents failed", "error", err)
	} else {
		for _, agent := range agents {
			add(agent.Name)
		}
	}
	if squads, err := h.queries.ListSquads(ctx, wsUUID); err != nil {
		slog.Debug("dictation glossary: list squads failed", "error", err)
	} else {
		for _, squad := range squads {
			add(squad.Name)
		}
	}
	if projects, err := h.queries.ListProjects(ctx, db.ListProjectsParams{WorkspaceID: wsUUID}); err != nil {
		slog.Debug("dictation glossary: list projects failed", "error", err)
	} else {
		for _, project := range projects {
			add(project.Title)
		}
	}
	if members, err := h.queries.ListMembersWithUser(ctx, wsUUID); err != nil {
		slog.Debug("dictation glossary: list members failed", "error", err)
	} else {
		for _, member := range members {
			add(member.UserName)
		}
	}
	return strings.Join(terms, ", ")
}

func mergeGlossary(inputs ...string) string {
	terms := make([]string, 0, maxGlossaryTerms)
	seen := map[string]struct{}{}
	for _, input := range inputs {
		for _, raw := range strings.Split(input, ",") {
			term := strings.TrimSpace(raw)
			key := strings.ToLower(term)
			if term == "" || len(terms) >= maxGlossaryTerms {
				continue
			}
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			terms = append(terms, term)
		}
	}
	return strings.Join(terms, ", ")
}
