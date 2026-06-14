package handler

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"unicode"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ── Route types ───────────────────────────────────────────────────────────────

type routeRequest struct {
	Query string `json:"query"`
}

type routeMatch struct {
	SquadID      string   `json:"squad_id"`
	Name         string   `json:"name"`
	Score        int      `json:"score"`
	MatchedWords []string `json:"matched_words"`
}

type routeResponse struct {
	Matches     []routeMatch `json:"matches"`
	Recommend   *routeMatch  `json:"recommend"`
	Undeclared  []string     `json:"undeclared"`
}

// ── Keyword matching engine ───────────────────────────────────────────────────

// tokenize splits a query string into normalized tokens. Handles both Latin and
// CJK text. For CJK, each character is a token; for Latin, tokens are
// whitespace-separated words.
func tokenize(query string) []string {
	lower := strings.ToLower(strings.TrimSpace(query))
	if lower == "" {
		return nil
	}

	var tokens []string
	// Split by whitespace first.
	parts := strings.Fields(lower)
	for _, part := range parts {
		// For each part, extract CJK characters individually and keep
		// Latin/num runs together.
		var runBuf strings.Builder
		for _, r := range part {
			if unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hiragana, r) ||
				unicode.Is(unicode.Katakana, r) || unicode.Is(unicode.Hangul, r) {
				// Flush Latin buffer.
				if runBuf.Len() > 0 {
					tokens = append(tokens, runBuf.String())
					runBuf.Reset()
				}
				tokens = append(tokens, string(r))
			} else {
				runBuf.WriteRune(r)
			}
		}
		if runBuf.Len() > 0 {
			tokens = append(tokens, runBuf.String())
		}
	}
	return tokens
}

// matchScore computes the overlap score between query tokens and a squad's
// capability. Returns score and the list of matched keywords.
func matchScore(tokens []string, cap SquadCapability) (int, []string) {
	score := 0
	matched := make(map[string]bool)

	tokenSet := make(map[string]bool, len(tokens))
	for _, t := range tokens {
		tokenSet[t] = true
	}

	// Exact keyword match: +10 points each.
	for _, kw := range cap.Keywords {
		kwLower := strings.ToLower(strings.TrimSpace(kw))
		if tokenSet[kwLower] {
			score += 10
			matched[kw] = true
			continue
		}
		// Substring match: keyword appears inside any token or vice versa.
		for _, t := range tokens {
			if strings.Contains(t, kwLower) || strings.Contains(kwLower, t) {
				score += 3
				matched[kw] = true
				break
			}
		}
	}

	// Domain bonus: domain appearing in query text gives +5.
	queryLower := strings.Join(tokens, " ")
	for _, d := range cap.Domains {
		dLower := strings.ToLower(strings.TrimSpace(d))
		if strings.Contains(queryLower, dLower) {
			score += 5
		}
	}

	matchedList := make([]string, 0, len(matched))
	for kw := range matched {
		matchedList = append(matchedList, kw)
	}
	sort.Strings(matchedList)
	return score, matchedList
}

// ── Handler ───────────────────────────────────────────────────────────────────

// RouteSquad finds the best-matching squads for a given query using keyword
// overlap scoring. Returns ranked matches plus a list of squads that haven't
// declared capabilities yet.
func (h *Handler) RouteSquad(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "workspaceId")
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}

	var req routeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Query == "" {
		writeError(w, http.StatusBadRequest, "query is required")
		return
	}

	rows, err := h.Queries.ListSquadsWithCapability(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list squad capabilities")
		return
	}

	tokens := tokenize(req.Query)
	if len(tokens) == 0 {
		writeJSON(w, http.StatusOK, routeResponse{
			Matches:    []routeMatch{},
			Undeclared: undeclaredNames(rows),
		})
		return
	}

	var matches []routeMatch
	var undeclared []string

	for _, row := range rows {
		cap := parseCapability(row.Capability)
		if len(cap.Keywords) == 0 && len(cap.Domains) == 0 {
			undeclared = append(undeclared, row.Name)
			continue
		}

		score, matched := matchScore(tokens, cap)
		if score > 0 {
			matches = append(matches, routeMatch{
				SquadID:      uuidToString(row.ID),
				Name:         row.Name,
				Score:        score,
				MatchedWords: matched,
			})
		} else {
			undeclared = append(undeclared, row.Name)
		}
	}

	// Sort by score descending, then name ascending for stability.
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].Score != matches[j].Score {
			return matches[i].Score > matches[j].Score
		}
		return matches[i].Name < matches[j].Name
	})

	resp := routeResponse{
		Matches:    matches,
		Undeclared: undeclared,
	}
	if len(matches) > 0 {
		first := matches[0]
		resp.Recommend = &first
	}

	writeJSON(w, http.StatusOK, resp)
}

func undeclaredNames(rows []db.ListSquadsWithCapabilityRow) []string {
	var names []string
	for _, row := range rows {
		cap := parseCapability(row.Capability)
		if len(cap.Keywords) == 0 && len(cap.Domains) == 0 {
			names = append(names, row.Name)
		}
	}
	return names
}
