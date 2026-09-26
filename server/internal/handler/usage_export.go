package handler

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	obsmetrics "github.com/multica-ai/multica/server/internal/metrics"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	usageExportDefaultPageSize = 200
	usageExportMaxPageSize     = 1000
)

var usageExportDimensionOrder = []string{"day", "agent", "provider", "model"}

type WorkspaceUsageExportItem struct {
	Day                      string `json:"day,omitempty"`
	AgentID                  string `json:"agent_id,omitempty"`
	AgentName                string `json:"agent_name,omitempty"`
	Provider                 string `json:"provider,omitempty"`
	Model                    string `json:"model,omitempty"`
	InputTokens              int64  `json:"input_tokens"`
	OutputTokens             int64  `json:"output_tokens"`
	CacheReadTokens          int64  `json:"cache_read_tokens"`
	CacheWriteTokens         int64  `json:"cache_write_tokens"`
	CostUSDTicks             int64  `json:"cost_usd_ticks"`
	UncostedInputTokens      int64  `json:"uncosted_input_tokens"`
	UncostedOutputTokens     int64  `json:"uncosted_output_tokens"`
	UncostedCacheReadTokens  int64  `json:"uncosted_cache_read_tokens"`
	UncostedCacheWriteTokens int64  `json:"uncosted_cache_write_tokens"`
}

type WorkspaceUsageExportResponse struct {
	From       string                     `json:"from"`
	To         string                     `json:"to"`
	Timezone   string                     `json:"timezone"`
	GroupBy    []string                   `json:"group_by"`
	Items      []WorkspaceUsageExportItem `json:"items"`
	NextCursor *string                    `json:"next_cursor"`
}

type usageExportGroupKey struct {
	Day, AgentID, Provider, Model string
}

func parseUsageExportBound(raw string, loc *time.Location) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, fmt.Errorf("value is required")
	}
	if day, err := time.ParseInLocation("2006-01-02", raw, loc); err == nil {
		return day, nil
	}
	instant, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, fmt.Errorf("must be RFC3339 or YYYY-MM-DD")
	}
	return instant, nil
}

func parseUsageExportGroupBy(raw string) ([]string, map[string]bool, error) {
	if strings.TrimSpace(raw) == "" {
		raw = "agent,provider,model,day"
	}
	wanted := make(map[string]bool)
	for _, part := range strings.Split(raw, ",") {
		dim := strings.ToLower(strings.TrimSpace(part))
		switch dim {
		case "agent", "provider", "model", "day":
			wanted[dim] = true
		case "":
			return nil, nil, fmt.Errorf("group_by contains an empty dimension")
		default:
			return nil, nil, fmt.Errorf("unsupported group_by dimension %q", dim)
		}
	}
	if len(wanted) == 0 {
		return nil, nil, fmt.Errorf("group_by must contain at least one dimension")
	}
	normalized := make([]string, 0, len(wanted))
	for _, dim := range usageExportDimensionOrder {
		if wanted[dim] {
			normalized = append(normalized, dim)
		}
	}
	return normalized, wanted, nil
}

func parseUsageExportOptionalUUID(w http.ResponseWriter, r *http.Request, name string) (pgtype.UUID, bool) {
	raw := strings.TrimSpace(r.URL.Query().Get(name))
	if raw == "" {
		return pgtype.UUID{}, true
	}
	u, err := util.ParseUUID(raw)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid "+name)
		return pgtype.UUID{}, false
	}
	return u, true
}

func usageExportCursorKey(item WorkspaceUsageExportItem) [4]string {
	return [4]string{item.Day, item.AgentID, item.Provider, item.Model}
}

func compareUsageExportCursor(a, b [4]string) int {
	for i := range a {
		if a[i] < b[i] {
			return -1
		}
		if a[i] > b[i] {
			return 1
		}
	}
	return 0
}

func encodeUsageExportCursor(item WorkspaceUsageExportItem) string {
	b, _ := json.Marshal(usageExportCursorKey(item))
	return base64.RawURLEncoding.EncodeToString(b)
}

func decodeUsageExportCursor(raw string) ([4]string, error) {
	var key [4]string
	b, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return key, err
	}
	if err := json.Unmarshal(b, &key); err != nil {
		return key, err
	}
	return key, nil
}

func addUsageExportItem(dst *WorkspaceUsageExportItem, src WorkspaceUsageExportItem) {
	dst.InputTokens += src.InputTokens
	dst.OutputTokens += src.OutputTokens
	dst.CacheReadTokens += src.CacheReadTokens
	dst.CacheWriteTokens += src.CacheWriteTokens
	dst.CostUSDTicks += src.CostUSDTicks
	dst.UncostedInputTokens += src.UncostedInputTokens
	dst.UncostedOutputTokens += src.UncostedOutputTokens
	dst.UncostedCacheReadTokens += src.UncostedCacheReadTokens
	dst.UncostedCacheWriteTokens += src.UncostedCacheWriteTokens
}

// GetWorkspaceUsageExport returns exact workspace usage for [from,to). Date-only
// bounds are local midnights in timezone; RFC3339 bounds remain exact instants.
func (h *Handler) GetWorkspaceUsageExport(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}

	tz := strings.TrimSpace(r.URL.Query().Get("timezone"))
	if tz == "" {
		tz = "UTC"
	}
	loc, err := time.LoadLocation(tz)
	if err != nil || loc == nil {
		writeError(w, http.StatusBadRequest, "invalid timezone")
		return
	}
	from, err := parseUsageExportBound(r.URL.Query().Get("from"), loc)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid from: "+err.Error())
		return
	}
	to, err := parseUsageExportBound(r.URL.Query().Get("to"), loc)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid to: "+err.Error())
		return
	}
	if !from.Before(to) {
		writeError(w, http.StatusBadRequest, "from must be before to")
		return
	}

	groupBy, grouped, err := parseUsageExportGroupBy(r.URL.Query().Get("group_by"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	projectID, ok := parseUsageExportOptionalUUID(w, r, "project_id")
	if !ok {
		return
	}
	var runtimeID pgtype.UUID
	if runtimeRef := strings.TrimSpace(r.URL.Query().Get("runtime_id")); runtimeRef != "" {
		rt, runtimeMember, ok := h.requireRuntimeReadAccess(w, r, obsmetrics.RuntimeLookupSourceTask, runtimeRef)
		if !ok {
			return
		}
		if rt.WorkspaceID != parseUUID(workspaceID) {
			writeError(w, http.StatusNotFound, "runtime not found")
			return
		}
		runtimeID = rt.ID
		member = runtimeMember
	}
	restricted, ok := h.dashboardRestrictedAgents(w, r, workspaceID, member.Role)
	if !ok {
		return
	}

	rows, err := h.Queries.ListWorkspaceUsageExport(r.Context(), db.ListWorkspaceUsageExportParams{
		WorkspaceID: parseUUID(workspaceID),
		Tz:          tz,
		FromTime:    pgtype.Timestamptz{Time: from, Valid: true},
		ToTime:      pgtype.Timestamptz{Time: to, Valid: true},
		ProjectID:   projectID,
		RuntimeID:   runtimeID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to export usage")
		return
	}

	base := make([]WorkspaceUsageExportItem, 0, len(rows))
	for _, row := range rows {
		agentID := uuidToString(row.AgentID)
		agentName := row.AgentName
		if _, hidden := restricted[agentID]; hidden {
			agentID = restrictedAgentsRowID
			agentName = "Restricted agents"
		}
		base = append(base, WorkspaceUsageExportItem{
			Day: row.Day.Time.Format("2006-01-02"), AgentID: agentID, AgentName: agentName,
			Provider: row.Provider, Model: row.Model,
			InputTokens: row.InputTokens, OutputTokens: row.OutputTokens,
			CacheReadTokens: row.CacheReadTokens, CacheWriteTokens: row.CacheWriteTokens,
			CostUSDTicks:             row.CostUsdTicks,
			UncostedInputTokens:      row.UncostedInputTokens,
			UncostedOutputTokens:     row.UncostedOutputTokens,
			UncostedCacheReadTokens:  row.UncostedCacheReadTokens,
			UncostedCacheWriteTokens: row.UncostedCacheWriteTokens,
		})
	}

	agentRefs := r.URL.Query()["agent"]
	if len(agentRefs) > 0 {
		matched := make(map[string]struct{})
		for _, ref := range agentRefs {
			ref = strings.TrimSpace(ref)
			if ref == "" {
				writeError(w, http.StatusBadRequest, "agent must not be empty")
				return
			}
			found := false
			for _, row := range base {
				if row.AgentID != restrictedAgentsRowID && (row.AgentID == ref || strings.EqualFold(row.AgentName, ref)) {
					matched[row.AgentID] = struct{}{}
					found = true
				}
			}
			if !found {
				writeError(w, http.StatusBadRequest, "agent not found or not visible")
				return
			}
		}
		filtered := base[:0]
		for _, row := range base {
			if _, keep := matched[row.AgentID]; keep {
				filtered = append(filtered, row)
			}
		}
		base = filtered
	}

	merged := make(map[usageExportGroupKey]WorkspaceUsageExportItem)
	for _, row := range base {
		key := usageExportGroupKey{}
		item := WorkspaceUsageExportItem{}
		if grouped["day"] {
			key.Day, item.Day = row.Day, row.Day
		}
		if grouped["agent"] {
			key.AgentID, item.AgentID, item.AgentName = row.AgentID, row.AgentID, row.AgentName
		}
		if grouped["provider"] {
			key.Provider, item.Provider = row.Provider, row.Provider
		}
		if grouped["model"] {
			key.Model, item.Model = row.Model, row.Model
		}
		current, exists := merged[key]
		if !exists {
			current = item
		}
		addUsageExportItem(&current, row)
		merged[key] = current
	}
	items := make([]WorkspaceUsageExportItem, 0, len(merged))
	for _, item := range merged {
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		return compareUsageExportCursor(usageExportCursorKey(items[i]), usageExportCursorKey(items[j])) < 0
	})

	pageSize := usageExportDefaultPageSize
	if raw := strings.TrimSpace(r.URL.Query().Get("page_size")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > usageExportMaxPageSize {
			writeError(w, http.StatusBadRequest, "page_size must be between 1 and 1000")
			return
		}
		pageSize = parsed
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("cursor")); raw != "" {
		cursor, err := decodeUsageExportCursor(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid cursor")
			return
		}
		start := sort.Search(len(items), func(i int) bool {
			return compareUsageExportCursor(usageExportCursorKey(items[i]), cursor) > 0
		})
		items = items[start:]
	}

	var nextCursor *string
	if len(items) > pageSize {
		items = items[:pageSize]
		next := encodeUsageExportCursor(items[len(items)-1])
		nextCursor = &next
	}
	if items == nil {
		items = []WorkspaceUsageExportItem{}
	}
	writeJSON(w, http.StatusOK, WorkspaceUsageExportResponse{
		From: from.Format(time.RFC3339Nano), To: to.Format(time.RFC3339Nano),
		Timezone: tz, GroupBy: groupBy, Items: items, NextCursor: nextCursor,
	})
}
