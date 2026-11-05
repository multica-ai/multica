package localhost

import (
	"net/http"
	"strconv"

	"github.com/multica-ai/multica/server/internal/localconversation/store"
)

func (v *V2) navigation(w http.ResponseWriter, r *http.Request, scopeID int64, id string) {
	query := r.URL.Query()
	if generation := query.Get("store_generation"); generation != "" && generation != v.svc.Generation() {
		writeV2Error(w, AppError("generation_changed", "Conversation storage changed. Reload and retry."))
		return
	}
	input := store.NavigationQuery{Before: 4, After: 5, Limit: 10}
	bad := func() {
		writeV2Error(w, AppError("invalid_request", "Use one nonnegative navigation selector and a window of at most 20 turns"))
	}
	if query.Has("include_body") {
		value := query.Get("include_body")
		if value != "true" && value != "false" {
			bad()
			return
		}
		input.MetadataOnly = value == "false"
	}
	selectors := 0
	for name, target := range map[string]**int64{"node_index": &input.NodeIndex, "turn_index": &input.TurnIndex, "before_index": &input.BeforeIndex, "after_index": &input.AfterIndex} {
		if query.Has(name) {
			value, err := strconv.ParseInt(query.Get(name), 10, 64)
			if err != nil || value < 0 || len(query[name]) != 1 {
				bad()
				return
			}
			*target = &value
			selectors++
		}
	}
	for name, target := range map[string]*int{"before": &input.Before, "after": &input.After, "limit": &input.Limit} {
		if query.Has(name) {
			value, err := strconv.Atoi(query.Get(name))
			if err != nil || value < 0 || value > 20 || len(query[name]) != 1 {
				bad()
				return
			}
			*target = value
		}
	}
	if selectors > 1 || input.Before+input.After+1 > 20 || input.Limit < 1 {
		bad()
		return
	}
	result, err := v.svc.Navigation(r.Context(), scopeID, id, input)
	if err != nil {
		writeV2Error(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"store_generation": v.svc.Generation(), "snapshot_seq": result.Seq, "node_count": result.Count, "nodes": result.Nodes, "directory_range": result.DirectoryRange, "range": result.Range, "turns": turnDTOs(result.Turns)})
}
