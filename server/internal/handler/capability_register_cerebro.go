package handler

// CEREBRO-PATCH(capability-register-handler): FIR-2129 capability register API.

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
)

type CapabilityRegisterService interface {
	Report(ctx context.Context, workspaceID pgtype.UUID, reporter CapabilitySubject, caps []CapabilityReportInput) ([]CapabilityView, error)
	List(ctx context.Context, workspaceID pgtype.UUID, subject *CapabilitySubject, keys []string) ([]CapabilityView, error)
}

type CapabilitySubject struct {
	Type        string         `json:"type"`
	ID          string         `json:"id"`
	DisplayName string         `json:"display_name,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

type CapabilityReportInput struct {
	Key         string              `json:"key"`
	Title       string              `json:"title"`
	Category    string              `json:"category"`
	Description string              `json:"description"`
	Source      string              `json:"source"`
	Metadata    map[string]any      `json:"metadata"`
	Owners      []CapabilitySubject `json:"owners"`
	Users       []CapabilitySubject `json:"users"`
}

type CapabilityView struct {
	ID             string              `json:"id"`
	WorkspaceID    string              `json:"workspace_id"`
	Key            string              `json:"key"`
	Title          string              `json:"title"`
	Category       string              `json:"category"`
	Description    string              `json:"description"`
	Source         string              `json:"source"`
	Metadata       map[string]any      `json:"metadata"`
	Owners         []CapabilitySubject `json:"owners"`
	Users          []CapabilitySubject `json:"users"`
	Reporters      []CapabilitySubject `json:"reporters"`
	FirstSeenAt    string              `json:"first_seen_at"`
	LastReportedAt string              `json:"last_reported_at"`
	UpdatedAt      string              `json:"updated_at"`
}

func (h *Handler) SetCapabilityRegister(svc CapabilityRegisterService) {
	h.capabilityRegister = svc
}

func (h *Handler) requireCapabilityRegister(w http.ResponseWriter) bool {
	if h.capabilityRegister == nil {
		writeError(w, http.StatusServiceUnavailable, "capability register is not configured")
		return false
	}
	return true
}

type capabilityReportRequest struct {
	WorkspaceID  string                  `json:"workspace_id"`
	Subject      CapabilitySubject       `json:"subject"`
	Capabilities []CapabilityReportInput `json:"capabilities"`
}

type capabilityReportItem struct {
	Name     string
	Kind     string
	Metadata map[string]any
}

func (h *Handler) ListCapabilities(w http.ResponseWriter, r *http.Request) {
	if !h.requireCapabilityRegister(w) {
		return
	}
	wsID, wsUUID, ok := h.capabilityWorkspace(w, r)
	if !ok {
		return
	}
	if _, ok := h.requireWorkspaceMember(w, r, wsID, "workspace not found"); !ok {
		return
	}
	subject, err := parseCapabilitySubjectQuery(r.URL.Query().Get("subject"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	caps, err := h.capabilityRegister.List(r.Context(), wsUUID, subject, splitCSV(r.URL.Query().Get("key")))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list capabilities")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"capabilities": caps})
}

func (h *Handler) ReportCapabilities(w http.ResponseWriter, r *http.Request) {
	if !h.requireCapabilityRegister(w) {
		return
	}
	var req capabilityReportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.WorkspaceID == "" {
		req.WorkspaceID = r.URL.Query().Get("workspace_id")
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, req.WorkspaceID, "workspace_id")
	if !ok {
		return
	}
	if _, ok := h.requireWorkspaceMember(w, r, req.WorkspaceID, "workspace not found"); !ok {
		return
	}
	if err := validateCapabilitySubject(req.Subject); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(req.Capabilities) == 0 {
		writeError(w, http.StatusBadRequest, "capabilities is required")
		return
	}
	caps, err := h.capabilityRegister.Report(r.Context(), wsUUID, req.Subject, req.Capabilities)
	if err != nil {
		if errors.Is(err, errCapabilityInvalidSubject) || errors.Is(err, errCapabilityInvalidReport) {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to report capabilities")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"capabilities": caps})
}

func (h *Handler) persistRuntimeCapabilitySnapshot(r *http.Request, rtID, workspaceID pgtype.UUID, snapshot json.RawMessage) {
	if h.capabilityRegister == nil {
		return
	}
	var raw map[string]json.RawMessage
	if len(snapshot) == 0 || json.Unmarshal(snapshot, &raw) != nil {
		return
	}
	items := capabilityItemsFromSnapshot(raw)
	if len(items) == 0 {
		return
	}
	reporter := CapabilitySubject{Type: "runtime", ID: uuidToString(rtID)}
	reports := make([]CapabilityReportInput, 0, len(items))
	for _, item := range items {
		reports = append(reports, CapabilityReportInput{
			Key:      item.Kind + ":" + item.Name,
			Title:    item.Name,
			Category: item.Kind,
			Source:   "runtime_report",
			Metadata: item.Metadata,
			Owners:   []CapabilitySubject{reporter},
			Users:    []CapabilitySubject{reporter},
		})
	}
	_, _ = h.capabilityRegister.Report(r.Context(), workspaceID, reporter, reports)
}

func capabilityItemsFromSnapshot(snapshot map[string]json.RawMessage) []capabilityReportItem {
	kinds := make([]string, 0, len(snapshot))
	for kind := range snapshot {
		kinds = append(kinds, kind)
	}
	sort.Strings(kinds)
	items := []capabilityReportItem{}
	for _, kind := range kinds {
		if kind == "discovery_method" || kind == "providers" {
			continue
		}
		var names []string
		if err := json.Unmarshal(snapshot[kind], &names); err != nil {
			continue
		}
		for _, name := range names {
			name = strings.TrimSpace(name)
			if name != "" {
				items = append(items, capabilityReportItem{Name: name, Kind: kind, Metadata: map[string]any{"snapshot_key": kind}})
			}
		}
	}
	return items
}

func (h *Handler) capabilityWorkspace(w http.ResponseWriter, r *http.Request) (string, pgtype.UUID, bool) {
	wsID := r.URL.Query().Get("workspace_id")
	wsUUID, ok := parseUUIDOrBadRequest(w, wsID, "workspace_id")
	return wsID, wsUUID, ok
}

var (
	errCapabilityInvalidSubject = errors.New("subject must be one of member, agent, group, runtime, workspace with a UUID id")
	errCapabilityInvalidReport  = errors.New("capability key, title, and category are required")
)

func ErrCapabilityInvalidSubjectForAdapter() error { return errCapabilityInvalidSubject }
func ErrCapabilityInvalidReportForAdapter() error  { return errCapabilityInvalidReport }

func parseCapabilitySubjectQuery(raw string) (*CapabilitySubject, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	parts := strings.SplitN(raw, ":", 2)
	if len(parts) != 2 {
		return nil, errCapabilityInvalidSubject
	}
	sub := CapabilitySubject{Type: parts[0], ID: parts[1]}
	if err := validateCapabilitySubject(sub); err != nil {
		return nil, err
	}
	return &sub, nil
}

func validateCapabilitySubject(sub CapabilitySubject) error {
	switch sub.Type {
	case "member", "agent", "group", "runtime", "workspace":
	default:
		return errCapabilityInvalidSubject
	}
	if _, err := util.ParseUUID(sub.ID); err != nil {
		return errCapabilityInvalidSubject
	}
	return nil
}

// capabilityReportItem is a single normalized entry pulled out of a runtime's
// capability snapshot (e.g. one MCP server or one tool).
type capabilityReportItem struct {
	Name string
	Kind string
}

// capabilitySnapshotKinds is the ordered set of snapshot keys we ingest into
// the register. Order is deterministic so the register and tests are stable.
var capabilitySnapshotKinds = []string{"mcp_servers", "tools"}

// capabilityItemsFromSnapshot flattens a daemon capability snapshot into the
// recognized (kind, name) items, skipping blanks and unknown keys.
func capabilityItemsFromSnapshot(raw map[string]json.RawMessage) []capabilityReportItem {
	var items []capabilityReportItem
	for _, kind := range capabilitySnapshotKinds {
		msg, ok := raw[kind]
		if !ok {
			continue
		}
		var names []string
		if err := json.Unmarshal(msg, &names); err != nil {
			continue
		}
		for _, name := range names {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			items = append(items, capabilityReportItem{Name: name, Kind: kind})
		}
	}
	return items
}

// persistRuntimeCapabilitySnapshot mirrors a runtime's reported capability
// snapshot into the normalized register. Best-effort: failures are logged but
// never block the daemon refresh/heartbeat path that calls it.
func (h *Handler) persistRuntimeCapabilitySnapshot(r *http.Request, runtimeID, workspaceID pgtype.UUID, capabilities json.RawMessage) {
	if h.capabilityRegister == nil || len(capabilities) == 0 {
		return
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(capabilities, &raw); err != nil {
		return
	}
	items := capabilityItemsFromSnapshot(raw)
	if len(items) == 0 {
		return
	}
	reporter := CapabilitySubject{Type: "runtime", ID: util.UUIDToString(runtimeID)}
	reports := make([]CapabilityReportInput, 0, len(items))
	for _, item := range items {
		reports = append(reports, CapabilityReportInput{
			Key:      item.Kind + ":" + item.Name,
			Title:    item.Name,
			Category: item.Kind,
			Source:   "runtime",
			Owners:   []CapabilitySubject{reporter},
		})
	}
	if _, err := h.capabilityRegister.Report(r.Context(), workspaceID, reporter, reports); err != nil {
		slog.Warn("persist runtime capability snapshot failed",
			"runtime_id", util.UUIDToString(runtimeID), "error", err)
	}
}

func splitCSV(raw string) []string {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if s := strings.TrimSpace(part); s != "" {
			out = append(out, s)
		}
	}
	return out
}
