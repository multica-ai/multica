package localhost

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/multica-ai/multica/server/internal/localconversation/catalog"
	"github.com/multica-ai/multica/server/internal/localconversation/service"
	"github.com/multica-ai/multica/server/internal/localconversation/store"
	"github.com/multica-ai/multica/server/internal/localconversation/store/sqlitedb"
	"github.com/multica-ai/multica/server/internal/localconversation/timing"
	"github.com/multica-ai/multica/server/pkg/agent"
)

// V2Version is the wire protocol served under /local/v2.
const V2Version = 2

// ConversationFocusCapability tells clients they may send conversation.focus.
const ConversationFocusCapability = "conversation-focus-v1"

// PromptStatusCapability says clients may ask what became of queued messages,
// and that queue.updated events carry the queue's order and the change's reason.
const PromptStatusCapability = "prompt-status-v1"

// AttachmentReserveCapability says clients may choose attachment ids, link
// stored content by SHA-256 and discard uploads a draft dropped.
const AttachmentReserveCapability = "attachment-reserve-v1"

const (
	v2MaxSubscriptions = 32
	v2WriteTimeout     = 10 * time.Second
	v2MaxBodyBytes     = 8 << 20
	v2PageLimit        = 200
)

// Scope headers name the caller's account and workspace on every v2 request.
const (
	headerAPIOrigin   = "X-Multica-API-Origin"
	headerUserID      = "X-Multica-User-ID"
	headerWorkspaceID = "X-Workspace-ID"
)

// V2 serves the Go-owned local conversation protocol (plan §5).
type V2 struct {
	svc        *service.Service
	token      string
	profileDir string
	upgrader   websocket.Upgrader
	hosts      hosts
	computer   computerHost
	tickets    tickets
	// serverOrigin is the API origin this daemon signs in to; requests
	// for another one are refused instead of opening a scope that cannot
	// reach its server.
	serverOrigin string
	// remoteOpen reports whether other devices may use this one.
	remoteOpen     func() bool
	servicePreview interface {
		Control(http.ResponseWriter, *http.Request, int64, string)
		service.PreviewServices
	}

	// marks records this device's side of remote traffic; nil records nothing.
	marks *timing.Marks
	// reports holds what other devices reported; nil turns reporting off.
	reports      *timing.Log
	reportLimits reportLimits
	// relayedClosed is when each client's WebSocket on the relay last closed, so
	// its return on a direct stream reads as a migration finishing.
	relayedClosed map[string]time.Time

	mu sync.Mutex
	// origins is the client that last acted on each conversation.
	origins map[string]string
	// presence is which remote devices use each conversation, by scope.
	presence map[int64]map[string]map[string]RemotePeer
}

// NewV2 serves svc. Requests must carry token, or a renderer credential
// derived from it, and the profile's account.
func NewV2(svc *service.Service, token, profileDir string) *V2 {
	v := &V2{svc: svc, token: token, profileDir: profileDir, upgrader: websocket.Upgrader{
		ReadBufferSize: 64 << 10, WriteBufferSize: 64 << 10,
		// authenticate already checked the Origin for the caller's credential.
		CheckOrigin: func(*http.Request) bool { return true },
	}, hosts: hosts{byScope: map[int64][]*wsConn{}, pending: map[string]*hostPending{}, orphans: map[string][]*hostPending{}, grace: hostGrace},
		tickets: tickets{items: map[string]ticket{}, now: time.Now}, origins: map[string]string{}, presence: map[int64]map[string]map[string]RemotePeer{}, relayedClosed: map[string]time.Time{}, reportLimits: newReportLimits(time.Now)}
	v.hosts.origin = v.origin
	svc.SetHost(v.hosts.request)
	svc.SetLocalHost(v.hosts.requestLocal)
	svc.SetComputerHost(v.computer.request)
	svc.OnTurnEnd(v.turnEnded)
	return v
}

// SetServerOrigin refuses requests for any API origin but origin.
func (v *V2) SetServerOrigin(origin string) { v.serverOrigin = normalizeOrigin(origin) }

// SetTiming sets where this device's link marks and other devices' reports go.
func (v *V2) SetTiming(marks *timing.Marks, reports *timing.Log) { v.marks, v.reports = marks, reports }

// SetRemoteOpen sets whether other devices may use this one now.
func (v *V2) SetRemoteOpen(open func() bool) { v.remoteOpen = open }

func (v *V2) SetServicePreview(handler interface {
	Control(http.ResponseWriter, *http.Request, int64, string)
	service.PreviewServices
}) {
	v.servicePreview = handler
	v.svc.SetPreviewServices(handler)
}
func (v *V2) PreviewScope(r *http.Request) (int64, error) { return v.scope(r) }
func (v *V2) CheckPreviewConversation(ctx context.Context, scopeID int64, id string) error {
	c, err := v.svc.Store().Conversation(ctx, id)
	if err != nil {
		return err
	}
	if c.ScopeID != scopeID {
		return store.ErrNotFound
	}
	return nil
}

func normalizeOrigin(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return ""
	}
	return strings.ToLower(u.Scheme + "://" + u.Host)
}

func (v *V2) origin(conversationID string) string {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.origins[conversationID]
}

func (v *V2) actedOn(conversationID string, c caller) {
	if c.clientKey == "" {
		return
	}
	v.mu.Lock()
	v.origins[conversationID] = c.clientKey
	v.mu.Unlock()
}

// Register mounts the v2 routes.
func (v *V2) Register(mux *http.ServeMux) { mux.Handle("/local/v2/", v) }

// ServeHTTP is the loopback entry: Desktop's main process and renderer.
func (v *V2) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	kind, ok := v.authenticate(r)
	if !ok {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	v.serve(w, r, caller{kind: kind, clientKey: clientKey("local", r.Header)})
}

// ServeRemote is the device-link entry. The p2p layer admits only this
// account's devices; the connection names the device.
func (v *V2) ServeRemote(w http.ResponseWriter, r *http.Request) {
	peer, _ := r.Context().Value(remotePeerKey{}).(RemotePeer)
	if peer.ID == "" {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if v.remoteOpen == nil || !v.remoteOpen() {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": map[string]any{"kind": "device_link_off", "message": "This device does not share its conversations"}})
		return
	}
	c := caller{kind: callerRemote, clientKey: clientKey(peer.ID, r.Header), peer: peer.ID, deviceName: peer.DeviceName, clientKind: r.Header.Get(headerClientKind), connection: peer.Connection}
	writer, done := v.observeRemote(w, r, c)
	defer done()
	v.serve(writer, r, c)
}

func (v *V2) serve(w http.ResponseWriter, r *http.Request, c caller) {
	path := strings.TrimPrefix(r.URL.Path, "/local/v2")
	if !allowed(c.kind, r.Method, path, "") {
		writeV2Error(w, AppError("forbidden", "This caller cannot use this local conversation route"))
		return
	}
	kind := c.kind
	r = r.WithContext(withCaller(r.Context(), c))
	if path == "/computer-host" && r.Method == http.MethodGet {
		v.serveComputerHost(w, r)
		return
	}
	if path == "/hello" && r.Method == http.MethodGet {
		capabilities := []string{"claude", "codex", "device-link"}
		if v.servicePreview != nil {
			capabilities = append(capabilities, "remote-service-preview-v1")
		}
		capabilities = append(capabilities, ConversationFocusCapability, AttachmentReserveCapability, PromptStatusCapability)
		if v.reports != nil && c.kind == callerRemote {
			capabilities = append(capabilities, TimingReportCapability)
		}
		body := map[string]any{"version": V2Version, "store_generation": v.svc.Generation(), "capabilities": capabilities}
		if r.URL.Query().Get("scoped") == "true" {
			scopeID, err := v.scope(r)
			if err != nil {
				writeV2Error(w, err)
				return
			}
			_, seq, err := v.svc.Store().SummariesAfter(r.Context(), scopeID, 0, 0)
			if err != nil {
				writeV2Error(w, err)
				return
			}
			body["scope_seq"] = seq
		}
		writeJSON(w, http.StatusOK, body)
		return
	}
	// Desktop stops provider processes before an update replaces the app.
	if path == "/stop-providers" && r.Method == http.MethodPost {
		if err := v.svc.StopProviders(r.Context()); err != nil {
			writeV2Error(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "stopped"})
		return
	}
	// Desktop drains before restarting the daemon for a version change.
	if (path == "/drain" || path == "/undrain") && r.Method == http.MethodPost {
		if path == "/undrain" {
			if err := v.svc.Undrain(r.Context()); err != nil {
				writeV2Error(w, err)
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"busy": 0})
			return
		}
		busy, err := v.svc.Drain(r.Context())
		if err != nil {
			writeV2Error(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"busy": busy})
		return
	}
	scopeID, err := v.scope(r)
	if err != nil {
		writeV2Error(w, err)
		return
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	switch {
	case path == "/preview/services" || strings.HasPrefix(path, "/preview/services/"):
		if v.servicePreview == nil {
			http.NotFound(w, r)
			return
		}
		v.servicePreview.Control(w, r, scopeID, c.peer)
	case path == timing.ReportsPath && r.Method == http.MethodPost:
		v.timingReport(w, r, scopeID)
	case path == "/ws-ticket" && r.Method == http.MethodPost:
		writeJSON(w, http.StatusOK, ticketResponse(v.tickets.issue(kind, scopeFromHeaders(r.Header), r.Header.Get(headerClientID))))
	case path == "/ws" && r.Method == http.MethodGet:
		v.serveWS(w, r, scopeID)
	case path == "/sessions" && r.Method == http.MethodGet:
		v.listSessions(w, r, scopeID)
	case path == "/sessions" && r.Method == http.MethodPost:
		v.createSession(w, r, scopeID)
	case path == "/session-groups" && r.Method == http.MethodGet:
		v.sessionGroups(w, r, scopeID)
	case path == "/sessions/browse" && r.Method == http.MethodGet:
		v.browseSessions(w, r, scopeID)
	case path == "/folder-projects" && r.Method == http.MethodGet:
		v.listFolderProjects(w, r, scopeID)
	case path == "/folder-projects" && r.Method == http.MethodPost:
		v.addFolderProject(w, r, scopeID)
	case path == "/imports" && r.Method == http.MethodGet:
		page, err := v.svc.ImportList(r.Context(), scopeID, r.URL.Query().Get("provider"), r.URL.Query().Get("cursor"))
		if err != nil {
			writeV2Error(w, err)
			return
		}
		writeJSON(w, http.StatusOK, page)
	case path == "/imports" && r.Method == http.MethodPost:
		var body struct {
			Provider  string   `json:"provider"`
			IDs       []string `json:"ids"`
			ProjectID *string  `json:"project_id"`
		}
		if err := decodeBody(r, &body); err != nil {
			writeV2Error(w, err)
			return
		}
		result, err := v.svc.Import(r.Context(), scopeID, body.Provider, body.IDs, body.ProjectID)
		if err != nil {
			writeV2Error(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	case path == "/imports/cancel" && r.Method == http.MethodPost:
		v.svc.CancelImport(scopeID)
		writeJSON(w, http.StatusOK, map[string]any{"status": "cancelled"})
	case path == "/providers" && r.Method == http.MethodGet:
		writeJSON(w, http.StatusOK, map[string]any{"providers": v.svc.Providers(r.Context())})
	case path == "/models" && r.Method == http.MethodGet:
		models, err := v.svc.Models(r.Context(), r.URL.Query().Get("provider"), r.URL.Query().Get("force") == "true")
		if err != nil {
			writeV2Error(w, err)
			return
		}
		writeJSON(w, http.StatusOK, models)
	case path == "/usage" && r.Method == http.MethodGet:
		q := r.URL.Query()
		usage, err := v.svc.Usage(r.Context(), scopeID, q.Get("provider"), q.Get("conversation_id"), q.Get("force") == "true")
		if err != nil {
			writeV2Error(w, err)
			return
		}
		writeJSON(w, http.StatusOK, usage)
	case len(parts) == 3 && parts[0] == "attachments" && parts[2] == "metadata" && r.Method == http.MethodGet:
		a, _, err := v.svc.AttachmentFile(r.Context(), scopeID, parts[1])
		if err != nil {
			writeV2Error(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"name": a.FileName, "mime_type": a.MediaType, "size": a.Size})
	case path == "/attachments" && r.Method == http.MethodPost:
		v.upload(w, r, scopeID)
	case path == "/attachments/link" && r.Method == http.MethodPost:
		v.linkUpload(w, r, scopeID)
	case len(parts) == 2 && parts[0] == "attachments" && r.Method == http.MethodDelete:
		if err := v.svc.DiscardUpload(r.Context(), scopeID, parts[1]); err != nil {
			writeV2Error(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	case len(parts) == 2 && parts[0] == "attachments" && r.Method == http.MethodGet:
		v.attachment(w, r, scopeID, parts[1])
	case path == "/preview/roots" && r.Method == http.MethodPost:
		v.approvePreviewRoot(w, r, scopeID)
	case strings.HasPrefix(path, "/preview/") && r.Method == http.MethodGet:
		v.preview(w, r, scopeID, strings.TrimPrefix(path, "/preview/"))
	case len(parts) == 3 && parts[0] == "sessions" && parts[2] == "queue" && r.Method == http.MethodGet:
		queue, err := v.svc.Queue(r.Context(), scopeID, parts[1])
		if err != nil {
			writeV2Error(w, err)
			return
		}
		writeJSON(w, http.StatusOK, queue)
	case len(parts) == 4 && parts[0] == "sessions" && parts[2] == "queue" && parts[3] == "status" && r.Method == http.MethodPost:
		var body struct {
			RequestIDs []string `json:"request_ids"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeV2Error(w, AppError("invalid_request", "invalid request body"))
			return
		}
		statuses, err := v.svc.PromptStatuses(r.Context(), scopeID, parts[1], body.RequestIDs)
		if err != nil {
			writeV2Error(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"statuses": statuses})
	case len(parts) == 3 && parts[0] == "sessions" && parts[2] == "side-workspace" && r.Method == http.MethodGet:
		if _, err := v.svc.Snapshot(r.Context(), scopeID, parts[1], 1); err != nil {
			writeV2Error(w, err)
			return
		}
		state, ok := v.svc.SideWorkspaceState(scopeID, parts[1])
		if !ok {
			writeJSON(w, http.StatusOK, map[string]any{"workspace": nil})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"workspace": state})
	case len(parts) == 3 && parts[0] == "sessions" && parts[2] == "image" && r.Method == http.MethodGet:
		v.conversationImage(w, r, scopeID, parts[1])
	case path == "/directories" && r.Method == http.MethodGet:
		listDirectories(w, r.URL.Query().Get("path"))
	case path == "/directory-source" && r.Method == http.MethodGet:
		v.directorySource(w, r, scopeID)
	case len(parts) == 3 && parts[0] == "sessions" && parts[2] == "history" && r.Method == http.MethodGet:
		before, err := strconv.ParseInt(r.URL.Query().Get("before_index"), 10, 64)
		if err != nil {
			writeV2Error(w, AppError("invalid_request", "A history cursor is required"))
			return
		}
		turnLimit, ok := turnLimitParam(w, r)
		if !ok {
			return
		}
		turns, err := v.svc.History(r.Context(), scopeID, parts[1], before, turnLimit)
		if err != nil {
			writeV2Error(w, err)
			return
		}
		older := len(turns) > 0 && turns[0].Turn.TurnIndex > 0
		writeJSON(w, http.StatusOK, map[string]any{"store_generation": v.svc.Generation(), "turns": turnDTOs(turns), "has_older_turns": older})
	case len(parts) == 3 && parts[0] == "sessions" && parts[2] == "snapshot" && r.Method == http.MethodGet:
		v.snapshot(w, r, scopeID, parts[1])
	case len(parts) == 3 && parts[0] == "sessions" && parts[2] == "commands" && r.Method == http.MethodPost:
		v.command(w, r, scopeID, parts[1])
	default:
		http.NotFound(w, r)
	}
}

// scope resolves the caller's scope. The user must be the account Desktop
// last synced into this daemon profile, as for v1 session.set.
func (v *V2) scope(r *http.Request) (int64, error) {
	origin, user, workspace := r.Header.Get(headerAPIOrigin), r.Header.Get(headerUserID), r.Header.Get(headerWorkspaceID)
	if origin == "" || user == "" || workspace == "" {
		return 0, AppError("invalid_scope", "API origin, user and workspace headers are required")
	}
	if v.serverOrigin != "" && normalizeOrigin(origin) != v.serverOrigin {
		return 0, AppError("api_url_mismatch", "This device signs in to another Multica server")
	}
	data, err := os.ReadFile(filepath.Join(v.profileDir, desktopUserIDFile))
	if err != nil {
		return 0, AppError("not_signed_in", "Sign in to Multica Desktop to use local conversations")
	}
	if strings.TrimSpace(string(data)) != user {
		return 0, AppError("account_mismatch", "This daemon is in use by another account")
	}
	return v.svc.Store().EnsureScope(r.Context(), store.Scope{APIOrigin: origin, UserID: user, WorkspaceID: workspace})
}

func (v *V2) listSessions(w http.ResponseWriter, r *http.Request, scopeID int64) {
	after, _ := strconv.ParseInt(r.URL.Query().Get("after_scope_seq"), 10, 64)
	rows, scopeSeq, err := v.svc.Store().SummariesAfter(r.Context(), scopeID, after, v2PageLimit)
	if err != nil {
		writeV2Error(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"scope_seq": scopeSeq, "summaries": v.summaries(r.Context(), rows), "has_more": len(rows) == v2PageLimit})
}

type createSessionBody struct {
	ID                string  `json:"id"`
	Provider          string  `json:"provider"`
	Title             string  `json:"title"`
	ProjectID         *string `json:"project_id"`
	FolderProjectID   string  `json:"folder_project_id"`
	DirectorySourceID string  `json:"directory_source_id"`
}

func (v *V2) createSession(w http.ResponseWriter, r *http.Request, scopeID int64) {
	var body createSessionBody
	if err := decodeBody(r, &body); err != nil {
		writeV2Error(w, err)
		return
	}
	conv, err := v.svc.Create(r.Context(), service.CreateCommand{
		ScopeID: scopeID, ID: body.ID, Provider: body.Provider, Title: body.Title, ProjectID: body.ProjectID,
		FolderProjectID: body.FolderProjectID, DirectorySourceID: body.DirectorySourceID,
	})
	if err != nil {
		writeV2Error(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, summaryDTO(conv))
}

func (v *V2) snapshot(w http.ResponseWriter, r *http.Request, scopeID int64, id string) {
	began := v.marks.Now()
	turnLimit, ok := turnLimitParam(w, r)
	if !ok {
		return
	}
	snap, err := v.svc.Snapshot(r.Context(), scopeID, id, turnLimit)
	if err != nil {
		writeV2Error(w, err)
		return
	}
	turns := turnDTOs(snap.Turns)
	pending := make([]map[string]any, len(snap.PendingInteractions))
	for i, in := range snap.PendingInteractions {
		pending[i] = map[string]any{"interaction_id": in.ID, "turn_id": in.TurnID, "kind": in.Kind, "request": json.RawMessage(in.Payload), "created_at": in.CreatedAt}
	}
	queue, err := v.svc.Queue(r.Context(), scopeID, id)
	if err != nil {
		writeV2Error(w, err)
		return
	}
	c := snap.Conversation
	settings := map[string]any{"model": nil, "reasoning_effort": nil, "permission_mode": c.PermissionMode}
	// A conversation that picked Claude's old "default" row follows the default.
	if c.Model.Valid && c.Model.String != agent.ClaudeDefaultModelValue {
		settings["model"] = c.Model.String
	}
	if c.ReasoningEffort.Valid {
		settings["reasoning_effort"] = c.ReasoningEffort.String
	}
	v.writeSnapshot(w, r, id, began, map[string]any{
		"version": V2Version, "store_generation": v.svc.Generation(), "snapshot_seq": snap.SnapshotSeq,
		"conversation": summaryDTO(c), "turns": turns, "has_older_turns": snap.HasOlderTurns,
		"pending_interactions": pending, "settings": settings, "queue": queue,
		"created_at": c.CreatedAt, "forked_from_id": c.ForkedFromID, "forked_from_turn_id": c.ForkedFromTurnID,
		"imported_from_session_id": c.ImportedFromSessionID, "open_background_items": eventDTOs(snap.OpenBackgroundItems),
	})
}

// turnLimitParam reads the optional number of turns to return; zero when absent.
func turnLimitParam(w http.ResponseWriter, r *http.Request) (int, bool) {
	raw := r.URL.Query().Get("turn_limit")
	if raw == "" {
		return 0, true
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 1 || limit > 20 {
		writeV2Error(w, AppError("invalid_request", "turn_limit must be between 1 and 20"))
		return 0, false
	}
	return limit, true
}

func eventDTOs(rows []sqlitedb.Event) []map[string]any {
	events := make([]map[string]any, len(rows))
	for i, e := range rows {
		events[i] = eventDTO(store.CommittedFromRow(e))
	}
	return events
}

func turnDTOs(views []store.TurnView) []map[string]any {
	turns := make([]map[string]any, len(views))
	for i, t := range views {
		messages := make([]map[string]any, len(t.Messages))
		for j, m := range t.Messages {
			var attachments []string
			_ = json.Unmarshal([]byte(m.AttachmentIds), &attachments)
			messages[j] = map[string]any{"id": m.ID, "role": m.Role, "content": m.Content, "attachment_ids": attachments, "steered": m.Steered == 1, "failed": m.Failed == 1, "created_at": m.CreatedAt}
		}
		events := make([]map[string]any, len(t.Events))
		for j, e := range t.Events {
			events[j] = eventDTO(store.CommittedFromRow(e))
			if start, ok := t.ItemStarts[e.ItemID.String]; ok && e.ItemID.Valid {
				events[j]["first_seq"], events[j]["first_at"] = start.Seq, start.CreatedAt
			}
		}
		turn := map[string]any{"id": t.Turn.ID, "turn_index": t.Turn.TurnIndex, "request_id": t.Turn.RequestID, "status": t.Turn.Status, "started_at": t.Turn.StartedAt, "messages": messages, "events": events}
		if t.Turn.CompletedAt.Valid {
			turn["completed_at"] = t.Turn.CompletedAt.String
		}
		if t.Turn.ErrorKind != "" {
			turn["error"] = map[string]any{"kind": t.Turn.ErrorKind, "message": t.Turn.Error}
		}
		turns[i] = turn
	}
	return turns
}

type commandBody struct {
	Type          string              `json:"type"`
	RequestID     string              `json:"request_id"`
	Text          string              `json:"text"`
	AttachmentIDs []string            `json:"attachment_ids"`
	References    []service.Reference `json:"references"`
	// Agents, when set, sends the message to these defined agents' cloud
	// chats instead of the local provider.
	Agents []service.RemoteAgent `json:"agents"`
	// answer: the question and the user's answers by question id.
	InteractionID string          `json:"interaction_id"`
	Answer        json.RawMessage `json:"answer"`
	// update_settings: an absent field keeps its value, null resets it to
	// the provider default.
	Model           json.RawMessage `json:"model"`
	ReasoningEffort json.RawMessage `json:"reasoning_effort"`
	PermissionMode  *string         `json:"permission_mode"`
	// update: an absent field keeps its value; project_id null removes it.
	Title     *string         `json:"title"`
	Pinned    *bool           `json:"pinned"`
	Archived  *bool           `json:"archived"`
	ProjectID json.RawMessage `json:"project_id"`
	// set_directory: the folder the conversation runs in.
	Cwd string `json:"cwd"`
	// queue_enqueue: the client's queued message; paused queues it
	// without dispatching. queue_update: the action and, for remove and
	// send_now, the item's request_id.
	Item   json.RawMessage `json:"item"`
	Paused bool            `json:"paused"`
	Action string          `json:"action"`
	// send: provider text replacing Text, such as Text with context.
	Prompt string `json:"prompt"`
	// fork and create_side: the turn the copy ends after; empty takes
	// everything. create_side also quotes a selection.
	TurnID string `json:"turn_id"`
	Quote  string `json:"quote"`
	// side_workspace_sync: the renderer's Side workspace of the conversation.
	Workspace json.RawMessage `json:"workspace"`
}

// optionalString decodes an update_settings field.
func optionalString(raw json.RawMessage) (value *string, clear bool, err error) {
	switch {
	case len(raw) == 0:
		return nil, false, nil
	case string(raw) == "null":
		return nil, true, nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, false, AppError("invalid_request", "settings values must be strings or null")
	}
	return &s, false, nil
}

func (v *V2) command(w http.ResponseWriter, r *http.Request, scopeID int64, id string) {
	received := time.Now()
	var body commandBody
	if err := decodeBody(r, &body); err != nil {
		writeV2Error(w, err)
		return
	}
	if !allowed(callerOf(r.Context()).kind, r.Method, "/sessions/"+id+"/commands", body.Type) {
		writeV2Error(w, AppError("forbidden", "Choose this folder in Multica first"))
		return
	}
	switch body.Type {
	case "send", "answer", "queue_enqueue", "compact":
		v.actedOn(id, callerOf(r.Context()))
	}
	switch body.Type {
	case "send", "compact":
		v.svc.NoteRequest(id, body.RequestID, received, callerOf(r.Context()).timing())
	case "queue_enqueue":
		var item struct {
			RequestID string `json:"requestId"`
		}
		if json.Unmarshal(body.Item, &item) == nil {
			v.svc.NoteRequest(id, item.RequestID, received, callerOf(r.Context()).timing())
		}
	}
	switch body.Type {
	case "compact":
		result, err := v.svc.Compact(r.Context(), scopeID, id, body.RequestID)
		if err != nil {
			writeV2Error(w, err)
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]any{"turn_id": result.TurnID, "replayed": result.Replayed})
	case "send":
		if body.RequestID == "" {
			writeV2Error(w, AppError("invalid_request", "request_id is required"))
			return
		}
		if len(body.Agents) > 0 {
			result, err := v.svc.SendToAgents(r.Context(), service.SendCommand{ScopeID: scopeID, ConversationID: id, RequestID: body.RequestID, Origin: callerOf(r.Context()).origin(), Text: body.Text, Prompt: body.Prompt, AttachmentIDs: body.AttachmentIDs}, body.Agents)
			if err != nil {
				writeV2Error(w, err)
				return
			}
			writeJSON(w, http.StatusAccepted, result)
			return
		}
		// Marked before the turn starts, so an instant turn end still clears it.
		v.startedRun(scopeID, id, callerOf(r.Context()), true)
		result, err := v.svc.Send(r.Context(), service.SendCommand{ScopeID: scopeID, ConversationID: id, RequestID: body.RequestID, Origin: callerOf(r.Context()).origin(), Text: body.Text, Prompt: body.Prompt, AttachmentIDs: body.AttachmentIDs, References: body.References})
		if err != nil || result.Replayed {
			v.startedRun(scopeID, id, callerOf(r.Context()), false)
		}
		if err != nil {
			writeV2Error(w, err)
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]any{"turn_id": result.TurnID, "replayed": result.Replayed})
	case "answer":
		if body.InteractionID == "" {
			writeV2Error(w, AppError("invalid_request", "interaction_id is required"))
			return
		}
		if err := v.svc.Answer(r.Context(), scopeID, id, body.InteractionID, body.Answer, r.Header.Get(headerUserID)); err != nil {
			writeV2Error(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "answered"})
	case "update_settings":
		model, clearModel, err := optionalString(body.Model)
		if err != nil {
			writeV2Error(w, err)
			return
		}
		effort, clearEffort, err := optionalString(body.ReasoningEffort)
		if err != nil {
			writeV2Error(w, err)
			return
		}
		err = v.svc.UpdateSettings(r.Context(), scopeID, id, service.SettingsUpdate{
			Model: model, ClearModel: clearModel, ReasoningEffort: effort, ClearReasoningEffort: clearEffort, PermissionMode: body.PermissionMode,
		})
		if err != nil {
			writeV2Error(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "applied"})
	case "update":
		project, clearProject, err := optionalString(body.ProjectID)
		if err != nil {
			writeV2Error(w, err)
			return
		}
		conv, err := v.svc.UpdateMetadata(r.Context(), scopeID, id, service.MetadataUpdate{
			Title: body.Title, Pinned: body.Pinned, Archived: body.Archived, SetProject: project != nil || clearProject, ProjectID: project,
		})
		if err != nil {
			writeV2Error(w, err)
			return
		}
		writeJSON(w, http.StatusOK, summaryDTO(conv))
	case "set_directory":
		conv, err := v.svc.SetDirectory(r.Context(), scopeID, id, body.Cwd)
		if err != nil {
			writeV2Error(w, err)
			return
		}
		writeJSON(w, http.StatusOK, summaryDTO(conv))
	case "side_workspace_sync":
		var ws service.SideWorkspace
		if err := json.Unmarshal(body.Workspace, &ws); err != nil {
			writeV2Error(w, AppError("invalid_request", "workspace must be a Side workspace snapshot"))
			return
		}
		ws.ConversationID = id
		revision, err := v.svc.SyncSideWorkspace(r.Context(), scopeID, ws)
		if err != nil {
			writeV2Error(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"revision": revision})
	case "create_side":
		conv, err := v.svc.CreateSide(r.Context(), service.SideCommand{ScopeID: scopeID, SourceID: id, TurnID: body.TurnID, Quote: body.Quote, RequestID: body.RequestID, QueuedItem: body.Item, QueuedOrigin: callerOf(r.Context()).origin()})
		if err != nil {
			writeV2Error(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, summaryDTO(conv))
	case "queue_side":
		conv, err := v.svc.QueueToSide(r.Context(), scopeID, id, body.RequestID)
		if err != nil {
			writeV2Error(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, summaryDTO(conv))
	case "fork":
		conv, err := v.svc.Fork(r.Context(), service.ForkCommand{ScopeID: scopeID, SourceID: id, TurnID: body.TurnID, RequestID: body.RequestID})
		if err != nil {
			writeV2Error(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, summaryDTO(conv))
	case "queue_enqueue":
		queue, err := v.svc.Enqueue(r.Context(), scopeID, id, body.Item, body.Paused, callerOf(r.Context()).origin())
		if err != nil {
			writeV2Error(w, err)
			return
		}
		writeJSON(w, http.StatusOK, queue)
	case "queue_update":
		queue, err := v.svc.UpdateQueue(r.Context(), scopeID, id, body.Action, body.RequestID)
		if err != nil {
			writeV2Error(w, err)
			return
		}
		writeJSON(w, http.StatusOK, queue)
	case "stop_background_tasks":
		if err := v.svc.StopBackgroundTasks(r.Context(), scopeID, id); err != nil {
			writeV2Error(w, err)
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]any{"status": "requested"})
	case "interrupt":
		if err := v.svc.Interrupt(r.Context(), scopeID, id); err != nil {
			writeV2Error(w, err)
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]any{"status": "requested"})
	default:
		writeV2Error(w, AppError("invalid_request", "unknown command type"))
	}
}

// --- WebSocket ---

type wsConn struct {
	conn    *websocket.Conn
	writeMu sync.Mutex
	mu      sync.Mutex
	subs    map[string]*service.Subscription
	scope   *service.ScopeSubscription
	caller  caller
	scopeID int64
}

// key names the client; one without an id is one client per connection.
func (c *wsConn) key() string {
	if c.caller.clientKey != "" {
		return c.caller.clientKey
	}
	return fmt.Sprintf("%p", c)
}

func (c *wsConn) send(v any) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_ = c.conn.SetWriteDeadline(time.Now().Add(v2WriteTimeout))
	return c.conn.WriteJSON(v)
}

// sendRaw sends a message already encoded, for a caller that needs its size.
func (c *wsConn) sendRaw(data []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_ = c.conn.SetWriteDeadline(time.Now().Add(v2WriteTimeout))
	return c.conn.WriteMessage(websocket.TextMessage, data)
}

type wsClientMessage struct {
	Version         int    `json:"version"`
	Type            string `json:"type"`
	ConversationID  string `json:"conversation_id"`
	AfterSeq        int64  `json:"after_seq"`
	AfterScopeSeq   int64  `json:"after_scope_seq"`
	StoreGeneration string `json:"store_generation"`
	// conversation.focus
	Focused *bool `json:"focused"`
	// host.reply
	RequestID string          `json:"request_id"`
	Result    json.RawMessage `json:"result"`
	Error     *struct {
		Kind    string `json:"kind"`
		Message string `json:"message"`
	} `json:"error"`
}

func (v *V2) serveWS(w http.ResponseWriter, r *http.Request, scopeID int64) {
	ws, err := v.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	c := &wsConn{conn: ws, subs: map[string]*service.Subscription{}, caller: callerOf(r.Context()), scopeID: scopeID}
	detach := v.hosts.attach(scopeID, c)
	ctx, cancel := context.WithCancel(context.Background())
	disconnected := v.svc.ClientConnected(c.key())
	defer func() {
		v.noteSocketClosed(c)
		disconnected()
		detach()
		cancel()
		c.mu.Lock()
		opened := make([]string, 0, len(c.subs))
		for id, sub := range c.subs {
			sub.Close()
			opened = append(opened, id)
		}
		if c.scope != nil {
			c.scope.Close()
		}
		c.mu.Unlock()
		for _, id := range opened {
			v.setPresence(c, id, false)
		}
		_ = ws.Close()
	}()
	ws.SetReadLimit(1 << 20)
	for {
		var msg wsClientMessage
		if err := ws.ReadJSON(&msg); err != nil {
			return
		}
		if msg.Version != V2Version {
			_ = c.send(statusMessage(msg.ConversationID, "rejected", 0, AppError("unsupported_version", "this daemon speaks local protocol v2")))
			return
		}
		switch msg.Type {
		case "host.reply":
			v.hosts.reply(c, msg)
		case "subscribe":
			v.hosts.touch(scopeID, c)
			v.wsSubscribe(ctx, c, scopeID, msg)
		case "unsubscribe":
			c.mu.Lock()
			sub := c.subs[msg.ConversationID]
			if sub != nil {
				sub.Close()
				delete(c.subs, msg.ConversationID)
			}
			c.mu.Unlock()
			if sub != nil {
				v.setPresence(c, msg.ConversationID, false)
			}
		case "subscribe_scope":
			v.wsSubscribeScope(ctx, c, scopeID, msg)
		case "conversation.focus":
			// A view only asks for a preheat, so a refused one has nothing to
			// report. Every caller kind may do it: remote viewers preheat here.
			if msg.Focused != nil {
				_ = v.svc.SetFocus(ctx, scopeID, msg.ConversationID, c.key(), *msg.Focused, c.caller.timing())
			}
		}
	}
}

func (v *V2) wsSubscribe(ctx context.Context, c *wsConn, scopeID int64, msg wsClientMessage) {
	if msg.StoreGeneration != v.svc.Generation() {
		_ = c.send(statusMessage(msg.ConversationID, "snapshot_required", 0, nil))
		return
	}
	c.mu.Lock()
	if old := c.subs[msg.ConversationID]; old != nil {
		old.Close()
		delete(c.subs, msg.ConversationID)
	}
	full := len(c.subs) >= v2MaxSubscriptions
	c.mu.Unlock()
	if full {
		_ = c.send(statusMessage(msg.ConversationID, "rejected", 0, AppError("subscription_limit", "too many open conversations on this connection")))
		return
	}
	sub, high, err := v.svc.Subscribe(ctx, scopeID, msg.ConversationID, msg.AfterSeq)
	switch {
	case errors.Is(err, service.ErrSnapshotRequired):
		_ = c.send(statusMessage(msg.ConversationID, "snapshot_required", 0, nil))
		return
	case err != nil:
		_ = c.send(statusMessage(msg.ConversationID, "rejected", 0, err))
		return
	}
	c.mu.Lock()
	c.subs[msg.ConversationID] = sub
	c.mu.Unlock()
	_ = c.send(statusMessage(msg.ConversationID, "subscribed", high, nil))
	v.setPresence(c, msg.ConversationID, true)
	v.noteResubscribed(c)
	subscriberKey := c.key()
	go func() {
		live := false
		traffic := v.newLinkTraffic(c, msg.ConversationID)
		defer traffic.flush()
		for {
			events, err := sub.Next(ctx)
			if err != nil {
				if errors.Is(err, service.ErrResubscribe) {
					_ = c.send(statusMessage(msg.ConversationID, "resubscribe", 0, nil))
				}
				return
			}
			dtos := make([]map[string]any, len(events))
			for i, e := range events {
				dtos[i] = eventDTO(e)
			}
			data, err := json.Marshal(map[string]any{"version": V2Version, "type": "conversation.events", "conversation_id": msg.ConversationID,
				"from_seq": events[0].Seq, "to_seq": events[len(events)-1].Seq, "events": dtos})
			if err == nil {
				err = c.sendRaw(data)
			}
			if err != nil {
				sub.Close()
				return
			}
			traffic.sent(events, int64(len(data)))
			v.svc.NoteDelivered(events, subscriberKey, c.caller.timing(), sub.Live())
			if !live && sub.Live() {
				live = true
				_ = c.send(statusMessage(msg.ConversationID, "live", events[len(events)-1].Seq, nil))
			}
		}
	}()
}

func (v *V2) wsSubscribeScope(ctx context.Context, c *wsConn, scopeID int64, msg wsClientMessage) {
	sub, high, err := v.svc.Hub().SubscribeScope(ctx, scopeID, msg.AfterScopeSeq)
	if err != nil {
		status := "rejected"
		if errors.Is(err, service.ErrSnapshotRequired) {
			status, err = "snapshot_required", nil
		}
		_ = c.send(statusMessage("", status, 0, err))
		return
	}
	c.mu.Lock()
	if c.scope != nil {
		c.scope.Close()
	}
	c.scope = sub
	c.mu.Unlock()
	_ = c.send(statusMessage("", "subscribed", high, nil))
	v.sendPresence(c)
	go func() {
		for {
			rows, err := sub.Next(ctx)
			if err != nil {
				if errors.Is(err, service.ErrResubscribe) {
					_ = c.send(statusMessage("", "resubscribe", 0, nil))
				}
				return
			}
			if err := c.send(map[string]any{"version": V2Version, "type": "scope.summaries", "scope_seq": rows[len(rows)-1].SummarySeq, "summaries": v.summaries(ctx, rows)}); err != nil {
				sub.Close()
				return
			}
		}
	}()
}

// --- wire conversion ---

func eventDTO(e store.CommittedEvent) map[string]any {
	dto := map[string]any{"seq": e.Seq, "type": e.Type, "final": e.Final, "created_at": e.CreatedAt.UTC().Format(time.RFC3339Nano),
		"payload": e.Payload, "turn_id": nil, "turn_seq": nil, "item_id": nil}
	if e.TurnID != "" {
		dto["turn_id"], dto["turn_seq"] = e.TurnID, e.TurnSeq
	}
	if e.ItemID != "" {
		dto["item_id"] = e.ItemID
	}
	return dto
}

func summaryDTO(c sqlitedb.Conversation) map[string]any {
	dto := map[string]any{
		"conversation_id": c.ID, "summary_seq": c.SummarySeq, "provider": c.Provider, "title": c.Title, "status": c.Status,
		"cwd": c.Cwd, "project_id": nil, "pinned": c.Pinned == 1, "archived": c.Archived == 1,
		"last_message_preview": c.LastMessagePreview, "pending_interactions": c.PendingInteractions, "queue_count": c.QueueCount, "background_tasks": c.BackgroundTasks,
		"turn_count": c.TurnCount, "created_at": c.CreatedAt,
		"updated_at": c.UpdatedAt,
	}
	if c.ProjectID.Valid {
		dto["project_id"] = c.ProjectID.String
	}
	if c.SideSourceID != "" {
		dto["side_source_id"] = c.SideSourceID
		var side struct {
			TurnID string `json:"turn_id"`
			Quote  string `json:"quote"`
		}
		if json.Unmarshal([]byte(c.Side), &side) == nil {
			dto["side_source_turn_id"], dto["side_quote"] = side.TurnID, side.Quote
		}
	}
	return dto
}

func statusMessage(conversationID, status string, highWater int64, err error) map[string]any {
	msg := map[string]any{"version": V2Version, "type": "subscription.status", "status": status}
	if conversationID != "" {
		msg["conversation_id"] = conversationID
	}
	if status == "subscribed" || status == "live" {
		msg["high_water"] = highWater
	}
	if err != nil {
		kind := appErrorCode(err)
		if kind == "" {
			kind = "internal"
			if errors.Is(err, store.ErrNotFound) {
				kind = "not_found"
			}
		}
		msg["error"] = map[string]any{"kind": kind, "message": err.Error()}
	}
	return msg
}

func decodeBody(r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(nil, r.Body, v2MaxBodyBytes)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		return AppError("invalid_request", "request body is not valid JSON")
	}
	return nil
}

func writeV2Error(w http.ResponseWriter, err error) {
	status, code := http.StatusInternalServerError, "internal"
	switch {
	case appErrorCode(err) != "":
		code = appErrorCode(err)
		status = http.StatusBadRequest
		if code == "not_signed_in" || code == "account_mismatch" || code == "forbidden" {
			status = http.StatusForbidden
		}
	case errors.Is(err, store.ErrNotFound):
		status, code = http.StatusNotFound, "not_found"
	case errors.Is(err, service.ErrRequestConflict):
		status, code = http.StatusConflict, "request_conflict"
	case errors.Is(err, service.ErrConcurrencyLimit):
		status, code = http.StatusTooManyRequests, "concurrency_limit"
	case errors.Is(err, service.ErrTurnRunning):
		status, code = http.StatusConflict, "turn_running"
	case errors.Is(err, service.ErrCompactUnavailable):
		status, code = http.StatusConflict, "compact_unavailable"
	case errors.Is(err, service.ErrInteractionAnswered):
		status, code = http.StatusConflict, "already_answered"
	case errors.Is(err, service.ErrInteractionClosed):
		status, code = http.StatusConflict, "interaction_closed"
	case errors.Is(err, service.ErrOutsideFolder):
		status, code = http.StatusForbidden, "outside_folder"
	case errors.Is(err, store.ErrUploadTooLarge):
		status, code = http.StatusRequestEntityTooLarge, "file_too_large"
	case errors.Is(err, store.ErrAttachmentConflict):
		status, code = http.StatusConflict, "attachment_conflict"
	case errors.Is(err, service.ErrDraining) || errors.Is(err, service.ErrClosing):
		status, code = http.StatusServiceUnavailable, "draining"
	case errors.Is(err, service.ErrCloudUnavailable):
		status, code = http.StatusServiceUnavailable, "cloud_unavailable"
	case errors.Is(err, service.ErrImportRunning):
		status, code = http.StatusConflict, "import_running"
	case errors.Is(err, service.ErrForkUnavailable):
		status, code = http.StatusConflict, "fork_unavailable"
	case errors.Is(err, service.ErrForking):
		status, code = http.StatusConflict, "forking"
	case errors.Is(err, service.ErrQueueItemStarted):
		status, code = http.StatusConflict, "queue_item_started"
	case errors.Is(err, service.ErrQueueFull):
		status, code = http.StatusConflict, "queue_full"
	case errors.Is(err, service.ErrArchived):
		status, code = http.StatusConflict, "archived"
	case errors.Is(err, catalog.ErrUnknownProvider):
		status, code = http.StatusBadRequest, "unsupported_provider"
	case errors.Is(err, service.ErrInvalidRequest):
		status, code = http.StatusBadRequest, "invalid_request"
	case errors.Is(err, service.ErrConversationStarted):
		status, code = http.StatusConflict, "conversation_started"
	case errors.Is(err, service.ErrInvalidSettings):
		status, code = http.StatusBadRequest, "invalid_settings"
	case errors.Is(err, service.ErrUnsupportedProvider):
		status, code = http.StatusBadRequest, "unsupported_provider"
	case errors.Is(err, service.ErrOutcomeUnknown):
		status, code = http.StatusAccepted, "outcome_unknown"
	}
	writeJSON(w, status, map[string]any{"error": map[string]any{"kind": code, "message": err.Error()}})
}

// appErrorCode returns the stable kind of an AppError, or "".
func appErrorCode(err error) string {
	var app *appError
	if !errors.As(err, &app) {
		return ""
	}
	return app.kind
}

func folderProjectDTO(p sqlitedb.FolderProject) map[string]any {
	return map[string]any{"id": p.ID, "title": p.Title, "path": p.Path, "created_at": p.CreatedAt}
}

func (v *V2) listFolderProjects(w http.ResponseWriter, r *http.Request, scopeID int64) {
	rows, err := v.svc.Store().FolderProjects(r.Context(), scopeID)
	if err != nil {
		writeV2Error(w, err)
		return
	}
	projects := make([]map[string]any, len(rows))
	for i, row := range rows {
		projects[i] = folderProjectDTO(row)
	}
	writeJSON(w, http.StatusOK, map[string]any{"projects": projects})
}

func (v *V2) addFolderProject(w http.ResponseWriter, r *http.Request, scopeID int64) {
	var body struct {
		Path string `json:"path"`
	}
	if err := decodeBody(r, &body); err != nil {
		writeV2Error(w, err)
		return
	}
	project, err := v.svc.AddFolderProject(r.Context(), scopeID, body.Path)
	if err != nil {
		writeV2Error(w, err)
		return
	}
	writeJSON(w, http.StatusOK, folderProjectDTO(project))
}

// directorySource names the conversation a new one can share cwd with.
func (v *V2) directorySource(w http.ResponseWriter, r *http.Request, scopeID int64) {
	conv, err := v.svc.Store().ConversationByCwd(r.Context(), scopeID, r.URL.Query().Get("cwd"))
	if errors.Is(err, store.ErrNotFound) {
		writeJSON(w, http.StatusOK, map[string]any{"id": nil})
		return
	}
	if err != nil {
		writeV2Error(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": conv.ID})
}

// upload stores the request body as an attachment. The name travels
// URL-encoded in X-File-Name, the type in Content-Type and an id the client
// chose, if any, in X-Attachment-ID.
func (v *V2) upload(w http.ResponseWriter, r *http.Request, scopeID int64) {
	name, err := url.QueryUnescape(r.Header.Get("X-File-Name"))
	if err != nil {
		writeV2Error(w, AppError("invalid_request", "X-File-Name must be URL-encoded"))
		return
	}
	body := http.MaxBytesReader(w, r.Body, store.MaxUploadBytes+1)
	a, err := v.svc.Upload(r.Context(), scopeID, r.Header.Get("X-Attachment-ID"), name, r.Header.Get("Content-Type"), body)
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		err = store.ErrUploadTooLarge
	}
	if err != nil {
		writeV2Error(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": a.ID, "name": a.FileName, "mime_type": a.MediaType, "size": a.Size})
}

// linkUpload attaches content the scope already has; not_found means upload it.
func (v *V2) linkUpload(w http.ResponseWriter, r *http.Request, scopeID int64) {
	var body struct {
		ID       string `json:"id"`
		SHA256   string `json:"sha256"`
		Name     string `json:"name"`
		MimeType string `json:"mime_type"`
	}
	if err := decodeBody(r, &body); err != nil {
		writeV2Error(w, err)
		return
	}
	a, err := v.svc.LinkUpload(r.Context(), scopeID, body.ID, body.SHA256, body.Name, body.MimeType)
	if err != nil {
		writeV2Error(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": a.ID, "name": a.FileName, "mime_type": a.MediaType, "size": a.Size})
}

func (v *V2) attachment(w http.ResponseWriter, r *http.Request, scopeID int64, id string) {
	a, path, err := v.svc.AttachmentFile(r.Context(), scopeID, id)
	if err != nil {
		writeV2Error(w, err)
		return
	}
	// An attachment id always names the same bytes.
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	serveLocalFile(w, r, path, a.MediaType, a.FileName)
}

func (v *V2) approvePreviewRoot(w http.ResponseWriter, r *http.Request, scopeID int64) {
	var body struct {
		Path string `json:"path"`
	}
	if err := decodeBody(r, &body); err != nil {
		writeV2Error(w, err)
		return
	}
	loc, err := v.svc.ApprovePreviewRoot(r.Context(), scopeID, body.Path)
	if err != nil {
		writeV2Error(w, err)
		return
	}
	writeJSON(w, http.StatusOK, loc)
}

// preview serves locate, list, file (metadata) and content. A
// conversation_id adds that conversation's working directory to the
// readable folders.
func (v *V2) preview(w http.ResponseWriter, r *http.Request, scopeID int64, action string) {
	q := r.URL.Query()
	conv, root, path := q.Get("conversation_id"), q.Get("root"), q.Get("path")
	switch action {
	case "locate":
		loc, err := v.svc.LocatePreview(r.Context(), scopeID, conv, path)
		if err != nil {
			writeV2Error(w, err)
			return
		}
		writeJSON(w, http.StatusOK, loc)
	case "list":
		entries, err := v.svc.ListPreview(r.Context(), scopeID, conv, root, path)
		if err != nil {
			writeV2Error(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"entries": nonNilSlice(entries)})
	case "file", "content":
		file, err := v.svc.PreviewFile(r.Context(), scopeID, conv, root, path)
		if err != nil {
			writeV2Error(w, err)
			return
		}
		if action == "file" {
			writeJSON(w, http.StatusOK, file)
			return
		}
		serveLocalFile(w, r, file.Path, file.MimeType, file.Name)
	default:
		http.NotFound(w, r)
	}
}

func (v *V2) conversationImage(w http.ResponseWriter, r *http.Request, scopeID int64, conversationID string) {
	file, err := v.svc.ConversationImage(r.Context(), scopeID, conversationID, r.URL.Query().Get("path"))
	if err != nil {
		writeV2Error(w, err)
		return
	}
	serveLocalFile(w, r, file.Path, file.MimeType, file.Name)
}

// serveLocalFile streams a file with range support under its declared
// type; browsers must not sniff another one.
func serveLocalFile(w http.ResponseWriter, r *http.Request, path, mediaType, name string) {
	f, err := os.Open(path)
	if err != nil {
		writeV2Error(w, err)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		writeV2Error(w, err)
		return
	}
	w.Header().Set("Content-Type", mediaType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("inline", map[string]string{"filename": name}))
	http.ServeContent(w, r, "", info.ModTime(), f)
}

func nonNilSlice[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

// summaries are list rows with each running conversation's active turn, so
// lists can show running work without loading the conversation.
func (v *V2) summaries(ctx context.Context, rows []sqlitedb.Conversation) []map[string]any {
	running := map[string]string{}
	for _, row := range rows {
		if row.Status == "running" {
			turns, _ := v.svc.Store().RunningTurns(ctx)
			for _, t := range turns {
				running[t.ConversationID] = t.ID
			}
			break
		}
	}
	out := make([]map[string]any, len(rows))
	for i, row := range rows {
		out[i] = summaryDTO(row)
		if id := running[row.ID]; id != "" {
			out[i]["active_turn_id"] = id
		}
	}
	return out
}
