package handler

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const emptyBuilderPageSize = 20
const maxEmptyBuilderPageSize = 50

// EmptyAgentBuilderSession contains no transcript, configuration or creator data.
type EmptyAgentBuilderSession struct {
	SessionID    string `json:"session_id"`
	RuntimeID    string `json:"runtime_id"`
	MachineLabel string `json:"machine_label"`
	CreatedAt    string `json:"created_at"`
	Snapshot     string `json:"snapshot"`
}

type ListEmptyAgentBuilderSessionsResponse struct {
	Sessions   []EmptyAgentBuilderSession `json:"sessions"`
	NextCursor string                     `json:"next_cursor"`
}

type DiscardEmptyAgentBuilderSessionRequest struct {
	ProfileID string `json:"profile_id"`
	Snapshot  string `json:"snapshot"`
}

func (h *Handler) builderRecoveryUser(w http.ResponseWriter, r *http.Request) (string, string, bool) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return "", "", false
	}
	workspaceID := h.resolveWorkspaceID(r)
	if isMachineCredentialActor(r) {
		writeError(w, http.StatusForbidden, "only the session creator may recover builder sessions")
		return "", "", false
	}
	workspaceUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return "", "", false
	}
	_, err := h.Queries.GetMemberByUserAndWorkspace(r.Context(), db.GetMemberByUserAndWorkspaceParams{UserID: parseUUID(userID), WorkspaceID: workspaceUUID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "workspace not found")
		} else {
			writeError(w, http.StatusInternalServerError, "failed to check workspace membership")
		}
		return "", "", false
	}
	return userID, workspaceID, true
}

// ListEmptyAgentBuilderSessions is separate from the nonempty unfinished-draft
// list. Its scoped keyset cursor never treats a truncated page as completion.
func (h *Handler) ListEmptyAgentBuilderSessions(w http.ResponseWriter, r *http.Request) {
	userID, workspaceID, ok := h.builderRecoveryUser(w, r)
	if !ok {
		return
	}
	values, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid recovery query")
		return
	}
	for _, key := range []string{"profile_id", "cursor", "page_size"} {
		if len(values[key]) > 1 {
			writeError(w, http.StatusBadRequest, "duplicate "+key)
			return
		}
	}
	profileID, ok := parseUUIDOrBadRequest(w, values.Get("profile_id"), "profile_id")
	if !ok {
		return
	}
	workspaceUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	pageSize := emptyBuilderPageSize
	if raw, present := values["page_size"]; present {
		n, err := strconv.Atoi(raw[0])
		if err != nil || n < 1 || n > maxEmptyBuilderPageSize {
			writeError(w, http.StatusBadRequest, "page_size must be between 1 and 50")
			return
		}
		pageSize = n
	}
	afterID := parseUUID("00000000-0000-0000-0000-000000000000")
	scope := workspaceID + ":" + userID + ":" + uuidToString(profileID) + ":"
	if cursor, present := values["cursor"]; present {
		if len(cursor[0]) > 256 {
			writeError(w, http.StatusBadRequest, "invalid recovery cursor")
			return
		}
		decoded, err := base64.RawURLEncoding.DecodeString(cursor[0])
		if err != nil || len(decoded) != len(scope)+36 || !strings.HasPrefix(string(decoded), scope) {
			writeError(w, http.StatusBadRequest, "invalid recovery cursor")
			return
		}
		afterID, ok = parseUUIDOrBadRequest(w, string(decoded[len(scope):]), "cursor")
		if !ok {
			return
		}
	}
	rows, err := h.Queries.ListEmptyAgentBuilderSessions(r.Context(), db.ListEmptyAgentBuilderSessionsParams{
		WorkspaceID: workspaceUUID, CreatorID: parseUUID(userID), ProfileID: profileID,
		AfterID: afterID, PageLimit: int32(pageSize + 1),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list empty builder sessions")
		return
	}
	response := ListEmptyAgentBuilderSessionsResponse{Sessions: make([]EmptyAgentBuilderSession, 0, pageSize)}
	if len(rows) > pageSize {
		response.NextCursor = base64.RawURLEncoding.EncodeToString([]byte(scope + uuidToString(rows[pageSize-1].ID)))
		rows = rows[:pageSize]
	}
	for _, row := range rows {
		response.Sessions = append(response.Sessions, EmptyAgentBuilderSession{
			SessionID: uuidToString(row.ID), RuntimeID: uuidToString(row.RuntimeID),
			MachineLabel: row.MachineLabel, CreatedAt: timestampToString(row.CreatedAt), Snapshot: emptyBuilderSnapshot(row),
		})
	}
	writeJSON(w, http.StatusOK, response)
}

// The snapshot detects intervening edits/rebinds, including a switch to another
// runtime of the same profile. Eligibility itself is always rechecked in SQL.
func emptyBuilderSnapshot(row db.ListEmptyAgentBuilderSessionsRow) string {
	value := strings.Join([]string{uuidToString(row.ID), uuidToString(row.RuntimeID), uuidToString(row.ProfileID),
		row.SessionUpdatedAt.Time.UTC().Format(time.RFC3339Nano), row.AgentUpdatedAt.Time.UTC().Format(time.RFC3339Nano)}, ":")
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func (h *Handler) DiscardEmptyAgentBuilderSession(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := h.builderRecoveryUser(w, r); !ok {
		return
	}
	var req DiscardEmptyAgentBuilderSessionRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if _, ok := parseUUIDOrBadRequest(w, req.ProfileID, "profile_id"); !ok {
		return
	}
	if decoded, err := hex.DecodeString(req.Snapshot); err != nil || len(decoded) != sha256.Size {
		writeError(w, http.StatusBadRequest, "recovery snapshot is required")
		return
	}
	h.deleteChatSession(w, r, &req)
}

// Called only while the shared deletion transaction holds session -> agent
// FOR UPDATE locks. No pending work is cancelled on this path.
func (h *Handler) checkEmptyBuilderDiscard(w http.ResponseWriter, r *http.Request, qtx *db.Queries, session db.ChatSession, userID string, req DiscardEmptyAgentBuilderSessionRequest) bool {
	locked, err := qtx.GetChatSessionInWorkspace(r.Context(), db.GetChatSessionInWorkspaceParams{ID: session.ID, WorkspaceID: session.WorkspaceID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "chat session not found")
		} else {
			writeError(w, http.StatusInternalServerError, "failed to load chat session")
		}
		return false
	}
	if locked.AgentID != session.AgentID {
		writeError(w, http.StatusConflict, "builder session changed; refresh before discarding")
		return false
	}
	if uuidToString(locked.CreatorID) != userID {
		writeError(w, http.StatusForbidden, "not your chat session")
		return false
	}
	rows, err := qtx.ListEmptyAgentBuilderSessions(r.Context(), db.ListEmptyAgentBuilderSessionsParams{
		WorkspaceID: locked.WorkspaceID, CreatorID: locked.CreatorID, ProfileID: parseUUID(req.ProfileID), SessionID: locked.ID,
		AfterID: pgtype.UUID{Valid: true}, PageLimit: 1,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to check empty builder session")
		return false
	}
	if len(rows) != 1 || emptyBuilderSnapshot(rows[0]) != req.Snapshot {
		writeError(w, http.StatusConflict, "builder session changed; refresh before discarding")
		return false
	}
	return true
}

// Unlike a missing session, a failed database read must not look like a safe
// disappearance to the recovery client.
func (h *Handler) loadBuilderRecoverySession(w http.ResponseWriter, r *http.Request, userID, workspaceID, sessionID string) (db.ChatSession, bool) {
	id, ok := parseUUIDOrBadRequest(w, sessionID, "chat session id")
	if !ok {
		return db.ChatSession{}, false
	}
	workspace, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return db.ChatSession{}, false
	}
	session, err := h.Queries.GetChatSessionInWorkspace(r.Context(), db.GetChatSessionInWorkspaceParams{ID: id, WorkspaceID: workspace})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "chat session not found")
		} else {
			writeError(w, http.StatusInternalServerError, "failed to load chat session")
		}
		return db.ChatSession{}, false
	}
	if uuidToString(session.CreatorID) != userID {
		writeError(w, http.StatusForbidden, "not your chat session")
		return db.ChatSession{}, false
	}
	return session, true
}
