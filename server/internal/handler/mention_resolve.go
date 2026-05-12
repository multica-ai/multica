package handler

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type MentionInput struct {
	Label string `json:"label"`
	Type  string `json:"type"`
	ID    string `json:"id,omitempty"`
}

type MentionCandidate struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Type  string `json:"type"`
}

type MentionResolveError struct {
	Message    string
	Candidates []MentionCandidate
}

func (e *MentionResolveError) Error() string { return e.Message }

func (h *Handler) ResolveMentionInputs(ctx context.Context, workspaceID pgtype.UUID, inputs []MentionInput) ([]string, error) {
	links := make([]string, 0, len(inputs))
	for _, input := range inputs {
		candidate, err := h.resolveMentionInput(ctx, workspaceID, input)
		if err != nil {
			return nil, err
		}
		links = append(links, canonicalMention(candidate))
	}
	return links, nil
}

func (h *Handler) resolveMentionInput(ctx context.Context, workspaceID pgtype.UUID, input MentionInput) (MentionCandidate, error) {
	input.Label = strings.TrimSpace(input.Label)
	input.Type = strings.TrimSpace(strings.ToLower(input.Type))
	input.ID = strings.TrimSpace(input.ID)

	if input.Label == "" {
		return MentionCandidate{}, fmt.Errorf("mention label is required")
	}
	if input.Type != "agent" && input.Type != "member" {
		return MentionCandidate{}, fmt.Errorf("mention type must be 'agent' or 'member'")
	}

	if input.ID != "" {
		id, err := util.ParseUUID(input.ID)
		if err != nil {
			return MentionCandidate{}, fmt.Errorf("invalid mention id: %w", err)
		}
		if input.Type == "agent" {
			if _, err := h.Queries.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{ID: id, WorkspaceID: workspaceID}); err != nil {
				return MentionCandidate{}, fmt.Errorf("agent %s is not mentionable in this workspace", input.ID)
			}
		} else {
			members, err := h.Queries.ListMembersWithUser(ctx, workspaceID)
			if err != nil {
				return MentionCandidate{}, fmt.Errorf("failed to list workspace members")
			}
			found := false
			for _, m := range members {
				if uuidToString(m.UserID) == input.ID {
					found = true
					break
				}
			}
			if !found {
				return MentionCandidate{}, fmt.Errorf("member %s is not mentionable in this workspace", input.ID)
			}
		}
		return MentionCandidate{ID: input.ID, Label: input.Label, Type: input.Type}, nil
	}

	switch input.Type {
	case "agent":
		agents, err := h.Queries.ListAgents(ctx, workspaceID)
		if err != nil {
			return MentionCandidate{}, fmt.Errorf("failed to list workspace agents")
		}
		matches := make([]MentionCandidate, 0)
		for _, a := range agents {
			if strings.EqualFold(a.Name, input.Label) {
				matches = append(matches, MentionCandidate{ID: uuidToString(a.ID), Label: a.Name, Type: "agent"})
			}
		}
		return singleMentionMatch(input.Label, "agent", matches)
	case "member":
		members, err := h.Queries.ListMembersWithUser(ctx, workspaceID)
		if err != nil {
			return MentionCandidate{}, fmt.Errorf("failed to list workspace members")
		}
		matches := make([]MentionCandidate, 0)
		for _, m := range members {
			if strings.EqualFold(m.UserName, input.Label) {
				matches = append(matches, MentionCandidate{ID: uuidToString(m.UserID), Label: m.UserName, Type: "member"})
			}
		}
		return singleMentionMatch(input.Label, "member", matches)
	}

	return MentionCandidate{}, fmt.Errorf("mention type must be 'agent' or 'member'")
}

func singleMentionMatch(label, mentionType string, matches []MentionCandidate) (MentionCandidate, error) {
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) == 0 {
		return MentionCandidate{}, &MentionResolveError{
			Message:    fmt.Sprintf("no %s matches %q", mentionType, label),
			Candidates: []MentionCandidate{},
		}
	}
	return MentionCandidate{}, &MentionResolveError{
		Message:    fmt.Sprintf("ambiguous: %d %ss match %q", len(matches), mentionType, label),
		Candidates: matches,
	}
}

func canonicalMention(candidate MentionCandidate) string {
	return fmt.Sprintf("[@%s](mention://%s/%s)", candidate.Label, candidate.Type, candidate.ID)
}

func spliceCanonicalMentions(content string, links []string) string {
	if len(links) == 0 {
		return content
	}
	return content + "\n\n" + strings.Join(links, " ")
}

func writeMentionResolveError(w http.ResponseWriter, err error) bool {
	if mre, ok := err.(*MentionResolveError); ok {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":      mre.Message,
			"candidates": mre.Candidates,
		})
		return true
	}
	return false
}

func (h *Handler) ResolveMention(w http.ResponseWriter, r *http.Request) {
	workspaceID := ctxWorkspaceID(r.Context())
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}
	wsUUID, err := util.ParseUUID(workspaceID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid workspace_id")
		return
	}

	input := MentionInput{
		Label: r.URL.Query().Get("name"),
		Type:  r.URL.Query().Get("type"),
	}
	candidate, err := h.resolveMentionInput(r.Context(), wsUUID, input)
	if err != nil {
		if writeMentionResolveError(w, err) {
			return
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"id":      candidate.ID,
		"label":   candidate.Label,
		"type":    candidate.Type,
		"mention": canonicalMention(candidate),
	})
}
