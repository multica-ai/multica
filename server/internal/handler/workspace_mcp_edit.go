package handler

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	mcpEditMaxBytes      = 64 << 10
	mcpEditMaxDepth      = 32
	mcpEditMaxOperations = 64
)

var errMcpEdit = errors.New("invalid configuration changes")

// This projection deliberately never enumerates arbitrary stored keys. A URL,
// command, argument, or innocent-looking extension can itself be a credential.
type WorkspaceMcpEditableConfig struct {
	ID              string            `json:"id"`
	Name            string            `json:"name"`
	Revision        string            `json:"revision"`
	PolicyVersion   string            `json:"policy_version"`
	VisibleConfig   map[string]string `json:"visible_config"`
	ProtectedFields []string          `json:"protected_fields"`
	HasOpaqueFields bool              `json:"has_opaque_fields"`
}

func mcpEditableProjection(server db.WorkspaceMcpServer) WorkspaceMcpEditableConfig {
	out := WorkspaceMcpEditableConfig{
		ID: uuidToString(server.ID), Name: server.Name,
		Revision: strconv.FormatInt(server.Revision, 10), PolicyVersion: "1",
		VisibleConfig: map[string]string{}, ProtectedFields: []string{},
	}
	var entry map[string]json.RawMessage
	if json.Unmarshal(server.Config, &entry) != nil {
		out.HasOpaqueFields = true
		return out
	}
	if raw, ok := entry["type"]; ok {
		var declared string
		if json.Unmarshal(raw, &declared) == nil {
			switch declared {
			case "stdio", "local", "http", "remote", "streamable-http", "sse":
				out.VisibleConfig["type"] = declared
				delete(entry, "type")
			}
		}
	}
	for _, field := range []string{"url", "command", "args", "headers", "env"} {
		if _, ok := entry[field]; ok {
			out.ProtectedFields = append(out.ProtectedFields, field)
			delete(entry, field)
		}
	}
	out.HasOpaqueFields = len(entry) > 0
	return out
}

func writeMcpEditable(w http.ResponseWriter, server db.WorkspaceMcpServer) {
	out := mcpEditableProjection(server)
	w.Header().Set("ETag", strconv.Quote(out.Revision))
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) mcpEditIDs(w http.ResponseWriter, r *http.Request) (pgtype.UUID, pgtype.UUID, bool) {
	w.Header().Set("Cache-Control", "no-store")
	workspaceID := workspaceIDFromURL(r, "id")
	workspace, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return pgtype.UUID{}, pgtype.UUID{}, false
	}
	// Reuse BOTH the role and actor checks, including for GET. An agent under
	// an owner's PAT is not a human settings administrator.
	if !h.requireWorkspaceMcpWriter(w, r, workspaceID) {
		return pgtype.UUID{}, pgtype.UUID{}, false
	}
	server, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "serverId"), "server id")
	return workspace, server, ok
}

func (h *Handler) GetWorkspaceMcpEditableConfig(w http.ResponseWriter, r *http.Request) {
	workspace, id, ok := h.mcpEditIDs(w, r)
	if !ok {
		return
	}
	server, err := h.Queries.GetWorkspaceMcpServer(r.Context(), db.GetWorkspaceMcpServerParams{ID: id, WorkspaceID: workspace})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "MCP server not found")
		} else {
			writeError(w, http.StatusInternalServerError, "failed to read MCP settings")
		}
		return
	}
	// Do not include request attributes that could carry arbitrary paths or bodies.
	slog.Info("workspace mcp settings read", "user_id", requestUserID(r), "workspace_id", uuidToString(workspace), "server_id", uuidToString(id), "revision", strconv.FormatInt(server.Revision, 10))
	writeMcpEditable(w, server)
}

type mcpConfigOperation struct {
	Op    string          `json:"op"`
	Path  string          `json:"path"`
	Value json.RawMessage `json:"value,omitempty"`
}

type mcpConfigPatch struct {
	Name       *string              `json:"name,omitempty"`
	Operations []mcpConfigOperation `json:"operations"`
}

// Validate depth and duplicate keys with a bounded token walk before decoding
// operations or traversing ANY pointer. Decoder errors are never returned.
func checkMcpJSON(raw []byte) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var walk func(int) error
	walk = func(depth int) error {
		if depth > mcpEditMaxDepth {
			return errMcpEdit
		}
		token, err := d.Token()
		if err != nil {
			return errMcpEdit
		}
		delim, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			keys := map[string]bool{}
			for d.More() {
				key, err := d.Token()
				if err != nil {
					return errMcpEdit
				}
				s, ok := key.(string)
				if !ok || keys[s] {
					return errMcpEdit
				}
				keys[s] = true
				if err := walk(depth + 1); err != nil {
					return err
				}
			}
		case '[':
			for d.More() {
				if err := walk(depth + 1); err != nil {
					return err
				}
			}
		default:
			return errMcpEdit
		}
		_, err = d.Token()
		if err != nil {
			return errMcpEdit
		}
		return nil
	}
	if err := walk(0); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return errMcpEdit
	}
	return nil
}

func decodeMcpPatch(raw []byte) (mcpConfigPatch, error) {
	var patch mcpConfigPatch
	if len(raw) > mcpEditMaxBytes || checkMcpJSON(raw) != nil {
		return patch, errMcpEdit
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&patch) != nil || patch.Operations == nil || len(patch.Operations) > mcpEditMaxOperations {
		return patch, errMcpEdit
	}
	if patch.Name != nil {
		name := strings.TrimSpace(*patch.Name)
		if validateWorkspaceMcpServerName(name) != nil {
			return patch, errMcpEdit
		}
		patch.Name = &name
	}
	return patch, nil
}

func mcpPointer(path string) ([]string, error) {
	if !strings.HasPrefix(path, "/") {
		return nil, errMcpEdit
	}
	parts := strings.Split(path[1:], "/")
	if len(parts) > mcpEditMaxDepth {
		return nil, errMcpEdit
	}
	for i, part := range parts {
		for j := 0; j < len(part); j++ {
			if part[j] == '~' {
				if j+1 == len(part) || (part[j+1] != '0' && part[j+1] != '1') {
					return nil, errMcpEdit
				}
				j++
			}
		}
		parts[i] = strings.ReplaceAll(strings.ReplaceAll(part, "~1", "/"), "~0", "~")
	}
	return parts, nil
}

func mcpPathsOverlap(a, b []string) bool {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func applyMcpPatch(config []byte, patch mcpConfigPatch) ([]byte, error) {
	// The public handler decodes bounded input first. Retain the count check
	// here so future callers cannot accidentally bypass it.
	if len(patch.Operations) > mcpEditMaxOperations {
		return nil, errMcpEdit
	}
	paths := make([][]string, len(patch.Operations))
	for i, op := range patch.Operations {
		if (op.Op != "set" && op.Op != "remove") || (op.Op == "set" && len(op.Value) == 0) || (op.Op == "remove" && len(op.Value) != 0) {
			return nil, errMcpEdit
		}
		path, err := mcpPointer(op.Path)
		if err != nil {
			return nil, errMcpEdit
		}
		for _, previous := range paths[:i] {
			if mcpPathsOverlap(path, previous) {
				return nil, errMcpEdit
			}
		}
		paths[i] = path
	}
	// RawMessage preserves numbers and unknown subtrees without float coercion.
	var entry map[string]json.RawMessage
	if json.Unmarshal(config, &entry) != nil || entry == nil {
		return nil, errMcpEdit
	}
	var mutate func(map[string]json.RawMessage, []string, mcpConfigOperation) error
	mutate = func(obj map[string]json.RawMessage, path []string, op mcpConfigOperation) error {
		key := path[0]
		if len(path) == 1 {
			if op.Op == "remove" {
				if _, exists := obj[key]; !exists {
					return errMcpEdit
				}
				delete(obj, key)
			} else {
				obj[key] = op.Value
			}
			return nil
		}
		var child map[string]json.RawMessage
		if json.Unmarshal(obj[key], &child) != nil || child == nil {
			return errMcpEdit
		}
		if err := mutate(child, path[1:], op); err != nil {
			return err
		}
		encoded, err := json.Marshal(child)
		if err != nil {
			return errMcpEdit
		}
		obj[key] = encoded
		return nil
	}
	for i, op := range patch.Operations {
		if err := mutate(entry, paths[i], op); err != nil {
			return nil, err
		}
	}
	out, err := json.Marshal(entry)
	if err != nil || validateWorkspaceMcpServerEntry(out) != nil {
		return nil, errMcpEdit
	}
	return out, nil
}

func (h *Handler) PatchWorkspaceMcpConfig(w http.ResponseWriter, r *http.Request) {
	workspace, id, ok := h.mcpEditIDs(w, r)
	if !ok {
		return
	}
	match := r.Header.Values("If-Match")
	if len(match) == 0 {
		writeError(w, http.StatusPreconditionRequired, "refresh MCP settings before saving")
		return
	}
	if len(match) != 1 {
		writeError(w, http.StatusPreconditionFailed, "MCP settings changed; refresh before saving")
		return
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, mcpEditMaxBytes))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid configuration changes")
		return
	}
	patch, err := decodeMcpPatch(raw)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid configuration changes")
		return
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update MCP settings")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)
	if _, err := qtx.LockWorkspaceMcpServerForUpdate(r.Context(), db.LockWorkspaceMcpServerForUpdateParams{ID: id, WorkspaceID: workspace}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "MCP server not found")
		} else {
			writeError(w, http.StatusInternalServerError, "failed to update MCP settings")
		}
		return
	}
	server, err := qtx.GetWorkspaceMcpServer(r.Context(), db.GetWorkspaceMcpServerParams{ID: id, WorkspaceID: workspace})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update MCP settings")
		return
	}
	// Compare the exact quoted decimal string. No wildcard, weak tag, tag list,
	// leading-zero conversion, or integer coercion can bypass this comparison.
	if match[0] != strconv.Quote(strconv.FormatInt(server.Revision, 10)) {
		writeError(w, http.StatusPreconditionFailed, "MCP settings changed; refresh before saving")
		return
	}
	config, err := applyMcpPatch(server.Config, patch)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid configuration changes")
		return
	}
	params := db.UpdateWorkspaceMcpServerParams{ID: id, WorkspaceID: workspace, Config: config}
	if patch.Name != nil {
		params.Name = pgtype.Text{String: *patch.Name, Valid: true}
	}
	server, err = qtx.UpdateWorkspaceMcpServer(r.Context(), params)
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "an MCP server with this name already exists")
		} else {
			writeError(w, http.StatusInternalServerError, "failed to update MCP settings")
		}
		return
	}
	if tx.Commit(r.Context()) != nil {
		writeError(w, http.StatusInternalServerError, "failed to update MCP settings")
		return
	}
	slog.Info("workspace mcp settings updated", "user_id", requestUserID(r), "workspace_id", uuidToString(workspace), "server_id", uuidToString(id), "revision", strconv.FormatInt(server.Revision, 10))
	writeMcpEditable(w, server)
}
