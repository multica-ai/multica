package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Custom issue properties (MUL-4463): workspace-level typed property
// definitions plus a per-issue value bag.
//
// Contract highlights (decided on MUL-4463):
//   - Definitions are managed by human owner/admin members only. Agent actors
//     are rejected even when the runtime owner has the role — otherwise field
//     sprawl becomes something agents can mass-produce.
//   - Values are writable by every member and agent; validation is typed per
//     definition, and errors enumerate legal values so agents can self-correct.
//   - Value writes are single-key atomic (mirror of issue metadata): two
//     agents writing different properties never clobber each other.
//   - Definitions archive instead of delete; archived definitions reject new
//     values but keep existing ones resolvable.
const (
	maxActivePropertiesPerWorkspace = 20
	maxPropertySelectOptions        = 50
	maxPropertyNameLen              = 32
	maxPropertyIconLen              = 32
	maxPropertyDescriptionLen       = 500
	maxPropertyTextValueLen         = 2000
	maxPropertyURLValueLen          = 2048
)

var validPropertyTypes = []string{"text", "number", "select", "multi_select", "date", "checkbox", "url"}

// Property icons use stable catalog keys that the Web client maps to Lucide
// glyphs. Keeping this allowlist at the API boundary prevents arbitrary text
// (including emoji) from leaking into every issue surface that renders icons.
var validPropertyIcons = map[string]struct{}{
	"circle-dot": {}, "signal-high": {}, "user-round": {}, "folder-kanban": {},
	"calendar-days": {}, "tag": {}, "milestone": {}, "flag": {}, "bookmark": {},
	"star": {}, "target": {}, "shield": {}, "bug": {}, "zap": {}, "rocket": {},
	"sparkles": {}, "lightbulb": {}, "globe-2": {}, "link": {}, "hash": {},
	"list-checks": {}, "circle-check": {}, "clock-3": {}, "briefcase-business": {},
	"layers-3": {}, "gauge": {}, "database": {}, "code-2": {}, "palette": {},
	"megaphone": {}, "map-pin": {}, "package": {}, "wrench": {}, "heart": {},
	"circle-alert": {}, "lock-keyhole": {},
}

// errClientRejected marks lock-closure failures already translated to an
// HTTP status by the caller-side fail() capture.
var errClientRejected = errors.New("client rejected")

// reservedPropertyNames blocks definitions that would collide with built-in
// issue fields ("system properties"). Comparison happens on the normalized
// form: lowercased, spaces collapsed to underscores — so "Due Date", "due
// date", and "due_date" are all rejected.
var reservedPropertyNames = map[string]struct{}{
	"status": {}, "priority": {}, "assignee": {}, "project": {}, "parent": {},
	"stage": {}, "label": {}, "labels": {}, "start_date": {}, "due_date": {},
	"title": {}, "description": {}, "creator": {}, "created_at": {}, "updated_at": {},
	"metadata": {}, "properties": {},
}

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

type PropertyOption struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
}

type PropertyConfig struct {
	Options []PropertyOption `json:"options,omitempty"`
}

type PropertyResponse struct {
	ID          string         `json:"id"`
	WorkspaceID string         `json:"workspace_id"`
	Name        string         `json:"name"`
	Type        string         `json:"type"`
	Description string         `json:"description"`
	Icon        string         `json:"icon"`
	Config      PropertyConfig `json:"config"`
	Position    float64        `json:"position"`
	Archived    bool           `json:"archived"`
	ArchivedAt  *string        `json:"archived_at"`
	UsageCount  int64          `json:"usage_count"`
	CreatedAt   string         `json:"created_at"`
	UpdatedAt   string         `json:"updated_at"`
}

func parsePropertyConfig(raw []byte) PropertyConfig {
	var cfg PropertyConfig
	if len(raw) == 0 {
		return cfg
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return PropertyConfig{}
	}
	return cfg
}

func propertyToResponse(p db.IssueProperty, usageCount int64) PropertyResponse {
	resp := PropertyResponse{
		ID:          uuidToString(p.ID),
		WorkspaceID: uuidToString(p.WorkspaceID),
		Name:        p.Name,
		Type:        p.Type,
		Description: p.Description,
		Icon:        p.Icon,
		Config:      parsePropertyConfig(p.Config),
		Position:    p.Position,
		Archived:    p.ArchivedAt.Valid,
		UsageCount:  usageCount,
		CreatedAt:   timestampToString(p.CreatedAt),
		UpdatedAt:   timestampToString(p.UpdatedAt),
	}
	if p.ArchivedAt.Valid {
		s := timestampToString(p.ArchivedAt)
		resp.ArchivedAt = &s
	}
	return resp
}

func propertyListRowToResponse(row db.ListIssuePropertiesRow) PropertyResponse {
	return propertyToResponse(db.IssueProperty{
		ID:          row.ID,
		WorkspaceID: row.WorkspaceID,
		Name:        row.Name,
		Type:        row.Type,
		Description: row.Description,
		Icon:        row.Icon,
		Config:      row.Config,
		Position:    row.Position,
		ArchivedAt:  row.ArchivedAt,
		CreatedAt:   row.CreatedAt,
		UpdatedAt:   row.UpdatedAt,
	}, row.UsageCount)
}

type CreatePropertyRequest struct {
	Name        string          `json:"name"`
	Type        string          `json:"type"`
	Description string          `json:"description"`
	Icon        string          `json:"icon"`
	Config      *PropertyConfig `json:"config"`
}

type UpdatePropertyRequest struct {
	Name        *string         `json:"name"`
	Description *string         `json:"description"`
	Icon        *string         `json:"icon"`
	Config      *PropertyConfig `json:"config"`
	Archived    *bool           `json:"archived"`
}

// ---------------------------------------------------------------------------
// Validation
// ---------------------------------------------------------------------------

func normalizePropertyName(name string) string {
	return strings.ReplaceAll(strings.ToLower(strings.TrimSpace(name)), " ", "_")
}

func validatePropertyName(raw string) (string, error) {
	for _, r := range raw {
		if unicode.IsControl(r) {
			return "", errors.New("name cannot contain tabs, newlines, or control characters")
		}
	}
	name := strings.TrimSpace(raw)
	if name == "" {
		return "", errors.New("name is required")
	}
	if utf8.RuneCountInString(name) > maxPropertyNameLen {
		return "", fmt.Errorf("name must be %d characters or fewer", maxPropertyNameLen)
	}
	if _, reserved := reservedPropertyNames[normalizePropertyName(name)]; reserved {
		return "", fmt.Errorf("%q is reserved for a built-in issue field", name)
	}
	return name, nil
}

func validatePropertyIcon(raw string) (string, error) {
	for _, r := range raw {
		if unicode.IsControl(r) {
			return "", errors.New("icon cannot contain tabs, newlines, or control characters")
		}
	}
	icon := strings.TrimSpace(raw)
	if utf8.RuneCountInString(icon) > maxPropertyIconLen {
		return "", fmt.Errorf("icon must be %d characters or fewer", maxPropertyIconLen)
	}
	if icon == "" {
		return "", nil
	}
	if _, ok := validPropertyIcons[icon]; !ok {
		return "", errors.New("icon must be a supported icon key")
	}
	return icon, nil
}

func validatePropertyType(t string) error {
	for _, v := range validPropertyTypes {
		if t == v {
			return nil
		}
	}
	return fmt.Errorf("invalid type %q; valid types: %s", t, strings.Join(validPropertyTypes, ", "))
}

func propertyTypeHasOptions(t string) bool {
	return t == "select" || t == "multi_select"
}

// validatePropertyConfig canonicalizes the config for storage. Select-type
// properties require 1..50 options; each option gets a stable server-assigned
// UUID if the caller didn't provide one (values reference option IDs, so
// option renames never touch issue rows). Non-select types must not carry
// options and are stored as {}.
func validatePropertyConfig(propType string, cfg *PropertyConfig) ([]byte, error) {
	if !propertyTypeHasOptions(propType) {
		if cfg != nil && len(cfg.Options) > 0 {
			return nil, fmt.Errorf("type %q does not accept options", propType)
		}
		return []byte(`{}`), nil
	}
	if cfg == nil || len(cfg.Options) == 0 {
		return nil, errors.New("select properties require at least one option")
	}
	if len(cfg.Options) > maxPropertySelectOptions {
		return nil, fmt.Errorf("a property cannot have more than %d options", maxPropertySelectOptions)
	}
	seenIDs := make(map[string]struct{}, len(cfg.Options))
	seenNames := make(map[string]struct{}, len(cfg.Options))
	out := PropertyConfig{Options: make([]PropertyOption, 0, len(cfg.Options))}
	for _, opt := range cfg.Options {
		name, err := validateLabelName(opt.Name)
		if err != nil {
			return nil, fmt.Errorf("option %w", err)
		}
		lower := strings.ToLower(name)
		if _, dup := seenNames[lower]; dup {
			return nil, fmt.Errorf("duplicate option name %q", name)
		}
		seenNames[lower] = struct{}{}
		color, err := normalizeColor(opt.Color)
		if err != nil {
			return nil, fmt.Errorf("option %q: %w", name, err)
		}
		id := strings.TrimSpace(opt.ID)
		if id == "" {
			id = uuid.NewString()
		} else if _, err := uuid.Parse(id); err != nil {
			return nil, fmt.Errorf("option %q: id must be a UUID", name)
		}
		if _, dup := seenIDs[id]; dup {
			return nil, fmt.Errorf("duplicate option id %q", id)
		}
		seenIDs[id] = struct{}{}
		out.Options = append(out.Options, PropertyOption{ID: id, Name: name, Color: color})
	}
	return json.Marshal(out)
}

func propertyOptionIDs(cfg PropertyConfig) map[string]int {
	ids := make(map[string]int, len(cfg.Options))
	for i, opt := range cfg.Options {
		ids[opt.ID] = i
	}
	return ids
}

func selectOptionsHint(cfg PropertyConfig) string {
	parts := make([]string, len(cfg.Options))
	for i, opt := range cfg.Options {
		parts[i] = fmt.Sprintf("%s (%s)", opt.ID, opt.Name)
	}
	return strings.Join(parts, ", ")
}

// validatePropertyValue checks a raw JSON value against the definition's type
// and returns the canonical JSON to store. Error strings enumerate the legal
// values where possible — agents consume these directly to self-correct.
func validatePropertyValue(def db.IssueProperty, raw json.RawMessage) ([]byte, error) {
	if len(raw) == 0 {
		return nil, errors.New("value is required")
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, fmt.Errorf("value must be valid JSON: %w", err)
	}
	if v == nil {
		return nil, errors.New("value cannot be null (use DELETE to unset a property)")
	}

	cfg := parsePropertyConfig(def.Config)
	switch def.Type {
	case "text":
		s, ok := v.(string)
		if !ok {
			return nil, errors.New("value must be a string")
		}
		if strings.TrimSpace(s) == "" {
			return nil, errors.New("value cannot be empty (use DELETE to unset a property)")
		}
		if utf8.RuneCountInString(s) > maxPropertyTextValueLen {
			return nil, fmt.Errorf("value must be %d characters or fewer", maxPropertyTextValueLen)
		}
		return json.Marshal(sanitizeNullBytes(s))
	case "url":
		s, ok := v.(string)
		if !ok {
			return nil, errors.New("value must be a URL string")
		}
		s = strings.TrimSpace(s)
		if len(s) > maxPropertyURLValueLen {
			return nil, fmt.Errorf("value must be %d characters or fewer", maxPropertyURLValueLen)
		}
		u, err := url.Parse(s)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return nil, errors.New("value must be an http(s) URL")
		}
		return json.Marshal(s)
	case "number":
		if _, ok := v.(float64); !ok {
			return nil, errors.New("value must be a number")
		}
		return json.Marshal(v)
	case "checkbox":
		if _, ok := v.(bool); !ok {
			return nil, errors.New("value must be true or false")
		}
		return json.Marshal(v)
	case "date":
		s, ok := v.(string)
		if !ok {
			return nil, errors.New("value must be a date string in YYYY-MM-DD format")
		}
		if _, err := time.Parse("2006-01-02", s); err != nil {
			return nil, errors.New("value must be a date string in YYYY-MM-DD format")
		}
		return json.Marshal(s)
	case "select":
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("value must be one of the option ids: %s", selectOptionsHint(cfg))
		}
		if _, exists := propertyOptionIDs(cfg)[s]; !exists {
			return nil, fmt.Errorf("value must be one of the option ids: %s", selectOptionsHint(cfg))
		}
		return json.Marshal(s)
	case "multi_select":
		items, ok := v.([]any)
		if !ok || len(items) == 0 {
			return nil, fmt.Errorf("value must be a non-empty array of option ids: %s", selectOptionsHint(cfg))
		}
		order := propertyOptionIDs(cfg)
		seen := make(map[string]struct{}, len(items))
		ids := make([]string, 0, len(items))
		for _, item := range items {
			s, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("value must be a non-empty array of option ids: %s", selectOptionsHint(cfg))
			}
			if _, exists := order[s]; !exists {
				return nil, fmt.Errorf("unknown option id %q; valid option ids: %s", s, selectOptionsHint(cfg))
			}
			if _, dup := seen[s]; dup {
				continue
			}
			seen[s] = struct{}{}
			ids = append(ids, s)
		}
		// Canonicalize to config order so equal selections serialize equally
		// (stable @> containment filtering and change detection).
		sort.SliceStable(ids, func(a, b int) bool { return order[ids[a]] < order[ids[b]] })
		return json.Marshal(ids)
	default:
		return nil, fmt.Errorf("unsupported property type %q", def.Type)
	}
}

// removedOptionIDs returns option ids present in the stored config but
// absent from the incoming replacement.
func removedOptionIDs(existingConfig, nextConfig []byte) []string {
	next := propertyOptionIDs(parsePropertyConfig(nextConfig))
	var removed []string
	for _, opt := range parsePropertyConfig(existingConfig).Options {
		if _, kept := next[opt.ID]; !kept {
			removed = append(removed, opt.ID)
		}
	}
	return removed
}

// describeOptionsInUse renders the 409 body for in-use option removal, e.g.
// `cannot remove options still in use: "Critical" (3 issues); clear or
// change those values first`.
func describeOptionsInUse(existingConfig []byte, rows []db.CountIssuesUsingPropertyOptionsRow) string {
	names := make(map[string]string)
	for _, opt := range parsePropertyConfig(existingConfig).Options {
		names[opt.ID] = opt.Name
	}
	parts := make([]string, len(rows))
	for i, row := range rows {
		name := names[row.OptionID]
		if name == "" {
			name = row.OptionID
		}
		parts[i] = fmt.Sprintf("%q (%d issues)", name, row.UsageCount)
	}
	sort.Strings(parts)
	return "cannot remove options still in use: " + strings.Join(parts, ", ") + "; clear or change those values first"
}

// parseIssueProperties mirrors parseIssueMetadata for the properties bag.
func parseIssueProperties(raw []byte) map[string]any {
	if len(raw) == 0 {
		return map[string]any{}
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil || out == nil {
		return map[string]any{}
	}
	return out
}

// ---------------------------------------------------------------------------
// Definition handlers
// ---------------------------------------------------------------------------

// requirePropertyAdmin gates definition writes: human owner/admin members
// only. Agent actors are rejected before the role check — an agent inherits
// its runtime owner's credentials, and without this check an admin's agent
// could mass-create definitions (MUL-4463 decision: agents propose via
// comments, humans confirm).
func (h *Handler) requirePropertyAdmin(w http.ResponseWriter, r *http.Request) (workspaceID, userID string, ok bool) {
	workspaceID = h.resolveWorkspaceID(r)
	userID, ok = requireUserID(w, r)
	if !ok {
		return "", "", false
	}
	if actorType, _ := h.resolveActor(r, userID, workspaceID); actorType == "agent" {
		writeError(w, http.StatusForbidden, "agents cannot manage property definitions")
		return "", "", false
	}
	if _, roleOK := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin"); !roleOK {
		return "", "", false
	}
	return workspaceID, userID, true
}

func (h *Handler) ListProperties(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	includeArchived := r.URL.Query().Get("include_archived") == "true"
	rows, err := h.Queries.ListIssueProperties(r.Context(), db.ListIssuePropertiesParams{
		WorkspaceID:     wsUUID,
		IncludeArchived: includeArchived,
	})
	if err != nil {
		slog.Warn("ListIssueProperties failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to list properties")
		return
	}
	resp := make([]PropertyResponse, len(rows))
	for i, row := range rows {
		resp[i] = propertyListRowToResponse(row)
	}
	writeJSON(w, http.StatusOK, map[string]any{"properties": resp, "total": len(resp)})
}

func (h *Handler) GetProperty(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	idUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "property id")
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	property, err := h.Queries.GetIssueProperty(r.Context(), db.GetIssuePropertyParams{ID: idUUID, WorkspaceID: wsUUID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "property not found")
			return
		}
		slog.Warn("GetIssueProperty failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to get property")
		return
	}
	writeJSON(w, http.StatusOK, propertyToResponse(property, 0))
}

func (h *Handler) CreateProperty(w http.ResponseWriter, r *http.Request) {
	workspaceID, userID, ok := h.requirePropertyAdmin(w, r)
	if !ok {
		return
	}
	var req CreatePropertyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	name, err := validatePropertyName(req.Name)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := validatePropertyType(req.Type); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if utf8.RuneCountInString(req.Description) > maxPropertyDescriptionLen {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("description must be %d characters or fewer", maxPropertyDescriptionLen))
		return
	}
	icon, err := validatePropertyIcon(req.Icon)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	configJSON, err := validatePropertyConfig(req.Type, req.Config)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	var property db.IssueProperty
	var capErr error
	err = h.withPropertyLock(r, []string{"props:" + workspaceID}, func(q *db.Queries) error {
		active, err := q.CountActiveIssueProperties(r.Context(), wsUUID)
		if err != nil {
			return err
		}
		if active >= maxActivePropertiesPerWorkspace {
			capErr = fmt.Errorf("a workspace cannot have more than %d active properties; archive unused ones first", maxActivePropertiesPerWorkspace)
			return capErr
		}
		property, err = q.CreateIssueProperty(r.Context(), db.CreateIssuePropertyParams{
			WorkspaceID: wsUUID,
			Name:        name,
			Type:        req.Type,
			Description: sanitizeNullBytes(strings.TrimSpace(req.Description)),
			Icon:        icon,
			Config:      configJSON,
		})
		return err
	})
	if capErr != nil {
		writeError(w, http.StatusBadRequest, capErr.Error())
		return
	}
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "a property with that name already exists")
			return
		}
		slog.Warn("CreateIssueProperty failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to create property")
		return
	}
	resp := propertyToResponse(property, 0)
	h.publish(protocol.EventPropertyCreated, workspaceID, "member", userID, map[string]any{"property": resp})
	writeJSON(w, http.StatusCreated, resp)
}

func (h *Handler) UpdateProperty(w http.ResponseWriter, r *http.Request) {
	workspaceID, userID, ok := h.requirePropertyAdmin(w, r)
	if !ok {
		return
	}
	idUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "property id")
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	var req UpdatePropertyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// The whole read → validate → census → write flow runs under advisory
	// locks (workspace, then property): a concurrent value write on this
	// property serializes behind the property lock, so the in-use census
	// cannot race a value that would reference a removed option (TOCTOU,
	// clean-room review F1); the workspace lock makes the unarchive cap
	// check atomic against creates (F5).
	var property db.IssueProperty
	var httpStatus int
	var httpMsg string
	fail := func(status int, msg string) error {
		httpStatus, httpMsg = status, msg
		return errClientRejected
	}
	err := h.withPropertyLock(r, []string{"props:" + workspaceID, "prop:" + uuidToString(idUUID)}, func(q *db.Queries) error {
		existing, err := q.GetIssueProperty(r.Context(), db.GetIssuePropertyParams{ID: idUUID, WorkspaceID: wsUUID})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return fail(http.StatusNotFound, "property not found")
			}
			return err
		}

		params := db.UpdateIssuePropertyParams{ID: idUUID, WorkspaceID: wsUUID}
		if req.Name != nil {
			name, err := validatePropertyName(*req.Name)
			if err != nil {
				return fail(http.StatusBadRequest, err.Error())
			}
			params.Name = pgtype.Text{String: name, Valid: true}
		}
		if req.Description != nil {
			if utf8.RuneCountInString(*req.Description) > maxPropertyDescriptionLen {
				return fail(http.StatusBadRequest, fmt.Sprintf("description must be %d characters or fewer", maxPropertyDescriptionLen))
			}
			params.Description = pgtype.Text{String: sanitizeNullBytes(strings.TrimSpace(*req.Description)), Valid: true}
		}
		if req.Icon != nil {
			icon, err := validatePropertyIcon(*req.Icon)
			if err != nil {
				return fail(http.StatusBadRequest, err.Error())
			}
			params.Icon = pgtype.Text{String: icon, Valid: true}
		}
		if req.Config != nil {
			configJSON, err := validatePropertyConfig(existing.Type, req.Config)
			if err != nil {
				return fail(http.StatusBadRequest, err.Error())
			}
			if removed := removedOptionIDs(existing.Config, configJSON); len(removed) > 0 {
				rows, err := q.CountIssuesUsingPropertyOptions(r.Context(), db.CountIssuesUsingPropertyOptionsParams{
					OptionIds:   removed,
					WorkspaceID: wsUUID,
					PropertyKey: uuidToString(existing.ID),
				})
				if err != nil {
					return err
				}
				if len(rows) > 0 {
					return fail(http.StatusConflict, describeOptionsInUse(existing.Config, rows))
				}
			}
			params.Config = configJSON
		}
		if req.Archived != nil {
			params.ArchivedSet = true
			if *req.Archived {
				params.ArchivedAt = pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true}
			} else if existing.ArchivedAt.Valid {
				active, err := q.CountActiveIssueProperties(r.Context(), wsUUID)
				if err != nil {
					return err
				}
				if active >= maxActivePropertiesPerWorkspace {
					return fail(http.StatusBadRequest, fmt.Sprintf("a workspace cannot have more than %d active properties; archive unused ones first", maxActivePropertiesPerWorkspace))
				}
			}
		}

		property, err = q.UpdateIssueProperty(r.Context(), params)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return fail(http.StatusNotFound, "property not found")
			}
			if isUniqueViolation(err) {
				return fail(http.StatusConflict, "a property with that name already exists")
			}
			return err
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, errClientRejected) {
			writeError(w, httpStatus, httpMsg)
			return
		}
		slog.Warn("UpdateProperty failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to update property")
		return
	}
	resp := propertyToResponse(property, 0)
	h.publish(protocol.EventPropertyUpdated, workspaceID, "member", userID, map[string]any{"property": resp})
	writeJSON(w, http.StatusOK, resp)
}

// ---------------------------------------------------------------------------
// Issue value handlers
// ---------------------------------------------------------------------------

type SetIssuePropertyRequest struct {
	Value json.RawMessage `json:"value"`
}

func (h *Handler) SetIssueProperty(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "id")
	propertyID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "propertyId"), "property id")
	if !ok {
		return
	}
	var req SetIssuePropertyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	issue, ok := h.loadIssueForUser(w, r, issueID)
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	// Validation and write share the property advisory lock with definition
	// updates: the value written is guaranteed to reference the definition
	// state that a concurrent config edit's usage census will see (TOCTOU,
	// clean-room review F1).
	var updated db.Issue
	var httpStatus int
	var httpMsg string
	fail := func(status int, msg string) error {
		httpStatus, httpMsg = status, msg
		return errClientRejected
	}
	err := h.withPropertyLock(r, []string{"prop:" + uuidToString(propertyID)}, func(q *db.Queries) error {
		def, err := q.GetIssueProperty(r.Context(), db.GetIssuePropertyParams{ID: propertyID, WorkspaceID: issue.WorkspaceID})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return fail(http.StatusNotFound, "property not found")
			}
			return err
		}
		if def.ArchivedAt.Valid {
			return fail(http.StatusBadRequest, fmt.Sprintf("property %q is archived and cannot receive new values", def.Name))
		}
		value, err := validatePropertyValue(def, req.Value)
		if err != nil {
			return fail(http.StatusBadRequest, err.Error())
		}
		updated, err = q.SetIssuePropertyValue(r.Context(), db.SetIssuePropertyValueParams{
			ID:          issue.ID,
			WorkspaceID: issue.WorkspaceID,
			Key:         uuidToString(def.ID),
			Value:       value,
		})
		if err != nil {
			if isCheckViolation(err) {
				return fail(http.StatusBadRequest, "issue properties exceed the 16KB size limit")
			}
			return err
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, errClientRejected) {
			writeError(w, httpStatus, httpMsg)
			return
		}
		slog.Warn("SetIssueProperty failed", append(logger.RequestAttrs(r), "error", err, "issue_id", issueID)...)
		writeError(w, http.StatusInternalServerError, "failed to set property")
		return
	}

	workspaceID := uuidToString(updated.WorkspaceID)
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	properties := parseIssueProperties(updated.Properties)
	h.publish(protocol.EventIssuePropertiesChanged, workspaceID, actorType, actorID, map[string]any{
		"issue_id":   uuidToString(updated.ID),
		"properties": properties,
	})
	writeJSON(w, http.StatusOK, map[string]any{"properties": properties})
}

func (h *Handler) DeleteIssueProperty(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "id")
	propertyID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "propertyId"), "property id")
	if !ok {
		return
	}

	issue, ok := h.loadIssueForUser(w, r, issueID)
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	// Deleting a value is allowed even for archived definitions — cleanup
	// must never be blocked. Unknown property ids only need to belong to the
	// workspace; `properties - key` is a no-op when the key is absent.
	if _, err := h.Queries.GetIssueProperty(r.Context(), db.GetIssuePropertyParams{ID: propertyID, WorkspaceID: issue.WorkspaceID}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "property not found")
			return
		}
		slog.Warn("GetIssueProperty in DeleteIssueProperty failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to unset property")
		return
	}

	updated, err := h.Queries.DeleteIssuePropertyValue(r.Context(), db.DeleteIssuePropertyValueParams{
		ID:          issue.ID,
		WorkspaceID: issue.WorkspaceID,
		Key:         uuidToString(propertyID),
	})
	if err != nil {
		slog.Warn("DeleteIssuePropertyValue failed", append(logger.RequestAttrs(r), "error", err, "issue_id", issueID)...)
		writeError(w, http.StatusInternalServerError, "failed to unset property")
		return
	}

	workspaceID := uuidToString(updated.WorkspaceID)
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	properties := parseIssueProperties(updated.Properties)
	h.publish(protocol.EventIssuePropertiesChanged, workspaceID, actorType, actorID, map[string]any{
		"issue_id":   uuidToString(updated.ID),
		"properties": properties,
	})
	writeJSON(w, http.StatusOK, map[string]any{"properties": properties})
}

// withPropertyLock runs fn inside a transaction holding the advisory lock
// for the given key, serializing definition mutations against value writes
// (TOCTOU: a config update's usage census and a concurrent value write could
// otherwise interleave into a permanently orphaned option reference) and
// definition creates/unarchives against each other (the 20-active cap and
// MAX(position)+1 are read-then-write). Locks are transaction-scoped.
func (h *Handler) withPropertyLock(r *http.Request, lockKeys []string, fn func(q *db.Queries) error) error {
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		return err
	}
	defer tx.Rollback(r.Context())
	// Callers pass keys in a fixed global order (workspace before property)
	// so overlapping lock sets cannot deadlock.
	for _, key := range lockKeys {
		if _, err := tx.Exec(r.Context(), "SELECT pg_advisory_xact_lock(hashtextextended($1, 0))", key); err != nil {
			return err
		}
	}
	if err := fn(h.Queries.WithTx(tx)); err != nil {
		return err
	}
	return tx.Commit(r.Context())
}

// ---------------------------------------------------------------------------
// List-endpoint support: properties filter + property sort expressions
// ---------------------------------------------------------------------------

const (
	maxPropertiesFilterDefinitions = 20
	maxPropertiesFilterValues      = 50
)

// parsePropertiesFilterParam reads the `properties` query parameter — a JSON
// object of {<definitionId>: [<value>, ...]} — and compiles it into OR-groups
// of containment objects: OR within a definition, AND across definitions.
//
// Values are option ids for select/multi_select and "true"/"false" for
// checkbox. The stored value shape differs per type (string, array element,
// boolean), so each value expands to every containment form it could match;
// forms that can't match are simply never satisfied.
//
// Returns (nil, true) when the parameter is empty.
func parsePropertiesFilterParam(w http.ResponseWriter, raw string) ([][]json.RawMessage, bool) {
	if raw == "" {
		return nil, true
	}
	var parsed map[string][]string
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		writeError(w, http.StatusBadRequest, "properties filter must be a JSON object of {definitionId: [values]}")
		return nil, false
	}
	if len(parsed) > maxPropertiesFilterDefinitions {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("properties filter cannot cover more than %d definitions", maxPropertiesFilterDefinitions))
		return nil, false
	}
	groups := make([][]json.RawMessage, 0, len(parsed))
	totalAlternatives := 0
	for definitionID, values := range parsed {
		if _, err := uuid.Parse(definitionID); err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("properties filter key %q is not a definition id", definitionID))
			return nil, false
		}
		if len(values) == 0 {
			continue
		}
		if len(values) > maxPropertiesFilterValues {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("properties filter for %s cannot list more than %d values", definitionID, maxPropertiesFilterValues))
			return nil, false
		}
		alternatives := make([]json.RawMessage, 0, len(values)*3)
		appendAlt := func(v any) bool {
			buf, err := json.Marshal(map[string]any{definitionID: v})
			if err != nil {
				writeError(w, http.StatusBadRequest, "properties filter is invalid")
				return false
			}
			alternatives = append(alternatives, buf)
			return true
		}
		for _, value := range values {
			if value == "" {
				writeError(w, http.StatusBadRequest, "properties filter values cannot be empty")
				return nil, false
			}
			if !appendAlt(value) || !appendAlt([]string{value}) { // select string / multi_select element
				return nil, false
			}
			if value == "true" || value == "false" {
				if !appendAlt(value == "true") { // checkbox boolean
					return nil, false
				}
			}
		}
		totalAlternatives += len(alternatives)
		groups = append(groups, alternatives)
	}
	if len(groups) == 0 {
		return nil, true
	}
	// Bound the OR fan-out: each alternative becomes one bind parameter in
	// the SQL below, and a runaway filter would bloat the statement.
	if totalAlternatives > 256 {
		writeError(w, http.StatusBadRequest, "properties filter is too large")
		return nil, false
	}
	return groups, true
}

// propertiesFilterPredicate renders the AND-of-ORs containment check for a
// compiled filter as plain `i.properties @> $n` disjunctions with one bind
// parameter per alternative. Constant containment operands are what lets the
// planner drive the jsonb_path_ops GIN index (a correlated
// jsonb_array_elements form defeats it — verified via EXPLAIN in review).
func propertiesFilterPredicate(groups [][]json.RawMessage, addArg func(any) string) string {
	groupSQL := make([]string, 0, len(groups))
	for _, alternatives := range groups {
		ors := make([]string, 0, len(alternatives))
		for _, alt := range alternatives {
			ors = append(ors, fmt.Sprintf("i.properties @> %s::jsonb", addArg(string(alt))))
		}
		groupSQL = append(groupSQL, "("+strings.Join(ors, " OR ")+")")
	}
	return "(" + strings.Join(groupSQL, " AND ") + ")"
}

// propertySortExpr resolves a `property:<definitionId>` sort value into a SQL
// ORDER BY expression. Returns handled=false when sortValue is not
// property-shaped (caller falls through to its static whitelist). A malformed
// id writes a 400 (ok=false). An unknown/archived definition or a type that
// has no meaningful order degrades to empty expr — callers keep position
// order, mirroring the frontend's stale-persisted-sort fallback rather than
// breaking installed clients with a 400.
func (h *Handler) propertySortExpr(r *http.Request, workspaceID string, sortValue string) (expr string, handled bool, err error) {
	const prefix = "property:"
	if !strings.HasPrefix(sortValue, prefix) {
		return "", false, nil
	}
	rawID := strings.TrimPrefix(sortValue, prefix)
	parsedID, parseErr := uuid.Parse(rawID)
	if parseErr != nil {
		return "", true, errors.New("invalid sort value")
	}
	wsUUID, wsErr := util.ParseUUID(workspaceID)
	if wsErr != nil {
		return "", true, errors.New("invalid workspace id")
	}
	var defUUID pgtype.UUID
	copy(defUUID.Bytes[:], parsedID[:])
	defUUID.Valid = true
	def, dbErr := h.Queries.GetIssueProperty(r.Context(), db.GetIssuePropertyParams{ID: defUUID, WorkspaceID: wsUUID})
	if dbErr != nil {
		if errors.Is(dbErr, pgx.ErrNoRows) {
			return "", true, nil // stale sort → position order
		}
		return "", true, fmt.Errorf("resolve sort property: %w", dbErr)
	}
	// Archived definitions degrade to position order like unknown ones —
	// their values are hidden from the UI, so sorting by them would order
	// the list by invisible data.
	if def.ArchivedAt.Valid {
		return "", true, nil
	}
	// uuidToString re-serializes the parsed UUID: hex and dashes only, safe
	// to embed in the ORDER BY string.
	id := uuidToString(def.ID)
	switch def.Type {
	case "number":
		return fmt.Sprintf("CASE WHEN jsonb_typeof(i.properties->'%s') = 'number' THEN (i.properties->>'%s')::numeric END", id, id), true, nil
	case "date", "text", "url", "select":
		return fmt.Sprintf("NULLIF(i.properties->>'%s', '')", id), true, nil
	default: // multi_select, checkbox, future types: no meaningful order
		return "", true, nil
	}
}
