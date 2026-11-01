package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/knowledge"
	"github.com/multica-ai/multica/server/internal/util"
	"github.com/multica-ai/multica/server/internal/util/secretbox"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// KnowledgeMCPServerName is the name of the read-only MCP endpoint in WeKnora
// and of its entry in the workspace MCP library.
const KnowledgeMCPServerName = "company-read"

// Knowledge states reported by Status. The first three mirror
// workspace_knowledge.state; the others describe what the row cannot.
const (
	KnowledgeStateDisabled     = "disabled"
	KnowledgeStateProvisioning = "provisioning"
	KnowledgeStateReady        = "ready"
	KnowledgeStateFailed       = "failed"
	// KnowledgeStateOffline means the workspace enabled knowledge but the
	// sidecar does not answer. Nothing else in the product depends on it.
	KnowledgeStateOffline = "offline"
)

var (
	// ErrKnowledgeNotOffered means this deployment has no knowledge sidecar
	// configured, or cannot protect the workspace key at rest.
	ErrKnowledgeNotOffered = errors.New("knowledge is not offered on this deployment")
	// ErrKnowledgeBusy means another request is provisioning the workspace.
	ErrKnowledgeBusy = errors.New("knowledge provisioning is already running for this workspace")
)

// defaultKnowledgeBases are created when knowledge is enabled. Keys are stable
// identifiers; names are editable afterwards.
var defaultKnowledgeBases = []struct{ Key, Name, Description string }{
	{"company", "Company", "Charter, policies, brand voice, org and roles, glossary, standing rules."},
	{"market", "Market", "Market research, competitor profiles, customer voice, reading lists."},
	{"product", "Product", "PRDs, specs, roadmap, analyses, user research."},
	{"engineering", "Engineering", "Architecture decisions, runbooks, post-mortems."},
	{"decisions", "Decisions", "Every proposal decision with its note, and the archive of briefs."},
}

// knowledgeDB is the part of *pgxpool.Pool the service needs: a transaction
// for the agent assignment and a dedicated connection for the per-workspace
// provisioning lock.
type knowledgeDB interface {
	Begin(ctx context.Context) (pgx.Tx, error)
	Acquire(ctx context.Context) (*pgxpool.Conn, error)
}

// KnowledgeService provisions and reports on a workspace's knowledge base.
// Client and Box are nil when the deployment does not offer knowledge.
type KnowledgeService struct {
	Queries *db.Queries
	DB      knowledgeDB
	Client  knowledge.Knowledge
	Box     *secretbox.Box
	// Embedding is the model created in each tenant before any knowledge base.
	Embedding knowledge.EmbeddingModelSpec
	// UIURL is the WeKnora web UI, for the "Open in WeKnora" link.
	UIURL string
	// PingTimeout bounds the reachability check Status makes. Default 2s.
	PingTimeout time.Duration
}

// KnowledgeBaseView is one knowledge base as the API reports it.
type KnowledgeBaseView struct {
	ID         string `json:"id"`
	Key        string `json:"key"`
	Name       string `json:"name"`
	Visibility string `json:"visibility"`
	Position   int32  `json:"position"`
}

// KnowledgeStatus is a workspace's knowledge state.
type KnowledgeStatus struct {
	// Offered is false when the deployment has no sidecar (or no key to seal
	// workspace keys with); nothing else in the status is meaningful then.
	Offered bool `json:"offered"`
	// Reason says why Offered is false: "not_configured" or "secret_key_missing".
	Reason         string              `json:"reason,omitempty"`
	Enabled        bool                `json:"enabled"`
	State          string              `json:"state"`
	KnowledgeBases []KnowledgeBaseView `json:"knowledge_bases"`
	UIURL          string              `json:"ui_url,omitempty"`
}

// offeredReason reports whether this deployment can enable knowledge at all.
func (s *KnowledgeService) offeredReason() (bool, string) {
	switch {
	case s == nil || s.Client == nil:
		return false, "not_configured"
	case s.Box == nil:
		return false, "secret_key_missing"
	}
	return true, ""
}

// Status reports the workspace's knowledge state. It never fails because the
// sidecar is down: that is the "offline" state.
func (s *KnowledgeService) Status(ctx context.Context, workspaceID pgtype.UUID) (KnowledgeStatus, error) {
	out := KnowledgeStatus{State: KnowledgeStateDisabled, KnowledgeBases: []KnowledgeBaseView{}}
	ok, reason := s.offeredReason()
	if !ok {
		out.Reason = reason
		return out, nil
	}
	out.Offered = true
	out.UIURL = s.UIURL

	row, err := s.Queries.GetWorkspaceKnowledge(ctx, workspaceID)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, nil
	}
	if err != nil {
		return out, fmt.Errorf("load workspace knowledge: %w", err)
	}
	out.Enabled = true
	out.State = row.State

	bases, err := s.Queries.ListKnowledgeBases(ctx, workspaceID)
	if err != nil {
		return out, fmt.Errorf("list knowledge bases: %w", err)
	}
	for _, b := range bases {
		out.KnowledgeBases = append(out.KnowledgeBases, KnowledgeBaseView{
			ID: util.UUIDToString(b.ID), Key: b.Key, Name: b.Name, Visibility: b.Visibility, Position: b.Position,
		})
	}

	timeout := s.PingTimeout
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	pingCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if err := s.Client.Ping(pingCtx); err != nil {
		out.State = KnowledgeStateOffline
	}
	return out, nil
}

// Enable provisions knowledge for the workspace. It is idempotent and
// resumable: every step first looks for what an earlier attempt left behind,
// in the database and in WeKnora, and a failure leaves the workspace in the
// "failed" state so the next call picks up where this one stopped.
//
// actorID is the member who asked; it is recorded as the creator of the
// library entry.
func (s *KnowledgeService) Enable(ctx context.Context, workspaceID, actorID pgtype.UUID) (KnowledgeStatus, error) {
	if ok, _ := s.offeredReason(); !ok {
		return KnowledgeStatus{}, ErrKnowledgeNotOffered
	}
	release, err := s.lock(ctx, workspaceID)
	if err != nil {
		return KnowledgeStatus{}, err
	}
	defer release()

	apiKey, err := s.ensureTenantAndKey(ctx, workspaceID)
	if err != nil {
		return KnowledgeStatus{}, err
	}
	// From here a row exists, so a failure is recorded on it.
	if err := s.provision(ctx, workspaceID, actorID, s.Client.Workspace(apiKey)); err != nil {
		s.markFailed(workspaceID)
		return KnowledgeStatus{}, err
	}
	if _, err := s.Queries.SetWorkspaceKnowledgeState(ctx, db.SetWorkspaceKnowledgeStateParams{WorkspaceID: workspaceID, State: KnowledgeStateReady}); err != nil {
		return KnowledgeStatus{}, fmt.Errorf("mark knowledge ready: %w", err)
	}
	return s.Status(ctx, workspaceID)
}

// lock takes the per-workspace provisioning lock on a dedicated connection.
// Two concurrent Enable calls would otherwise both create a WeKnora tenant.
func (s *KnowledgeService) lock(ctx context.Context, workspaceID pgtype.UUID) (func(), error) {
	conn, err := s.DB.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire connection: %w", err)
	}
	key := util.UUIDToString(workspaceID)
	var got bool
	if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock(hashtextextended($1, 2201))`, key).Scan(&got); err != nil {
		conn.Release()
		return nil, fmt.Errorf("take provisioning lock: %w", err)
	}
	if !got {
		conn.Release()
		return nil, ErrKnowledgeBusy
	}
	return func() {
		// A fresh context: the request's may already be cancelled, and an
		// unreleased session lock would outlive the pooled connection.
		unlockCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		var released bool
		if err := conn.QueryRow(unlockCtx, `SELECT pg_advisory_unlock(hashtextextended($1, 2201))`, key).Scan(&released); err != nil || !released {
			slog.Warn("knowledge: release provisioning lock failed; dropping the connection", "error", err)
			_ = conn.Conn().Close(unlockCtx)
		}
		conn.Release()
	}, nil
}

func (s *KnowledgeService) markFailed(workspaceID pgtype.UUID) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := s.Queries.SetWorkspaceKnowledgeState(ctx, db.SetWorkspaceKnowledgeStateParams{WorkspaceID: workspaceID, State: KnowledgeStateFailed}); err != nil {
		slog.Warn("knowledge: could not record failed state", "workspace_id", util.UUIDToString(workspaceID), "error", err)
	}
}

// tenantName is how a retry finds the tenant a crashed attempt created.
func tenantName(workspaceID pgtype.UUID) string { return "multica-" + util.UUIDToString(workspaceID) }

// ensureTenantAndKey returns the workspace API key, creating the tenant and
// minting the key on first use. The sealed key and tenant id are stored in one
// insert, so the row exists only when both do.
func (s *KnowledgeService) ensureTenantAndKey(ctx context.Context, workspaceID pgtype.UUID) (string, error) {
	row, err := s.Queries.GetWorkspaceKnowledge(ctx, workspaceID)
	switch {
	case err == nil:
		key, err := s.Box.Open(row.ApiKeyCiphertext)
		if err != nil {
			return "", fmt.Errorf("open workspace knowledge key (was MULTICA_KNOWLEDGE_SECRET_KEY changed?): %w", err)
		}
		if row.State != KnowledgeStateReady {
			if _, err := s.Queries.SetWorkspaceKnowledgeState(ctx, db.SetWorkspaceKnowledgeStateParams{WorkspaceID: workspaceID, State: KnowledgeStateProvisioning}); err != nil {
				return "", fmt.Errorf("mark provisioning: %w", err)
			}
		}
		return string(key), nil
	case !errors.Is(err, pgx.ErrNoRows):
		return "", fmt.Errorf("load workspace knowledge: %w", err)
	}

	tenant, err := s.Client.EnsureTenant(ctx, tenantName(workspaceID), "Multica workspace "+util.UUIDToString(workspaceID))
	if err != nil {
		return "", fmt.Errorf("create knowledge tenant: %w", err)
	}
	token, err := s.Client.CreateWorkspaceKey(ctx, tenant.ID, "multica")
	if err != nil {
		return "", fmt.Errorf("create knowledge workspace key: %w", err)
	}
	sealed, err := s.Box.Seal([]byte(token))
	if err != nil {
		return "", fmt.Errorf("seal workspace key: %w", err)
	}
	if _, err := s.Queries.CreateWorkspaceKnowledge(ctx, db.CreateWorkspaceKnowledgeParams{
		WorkspaceID: workspaceID, WeknoraTenantID: tenant.ID, ApiKeyCiphertext: sealed,
	}); err != nil {
		return "", fmt.Errorf("store workspace knowledge: %w", err)
	}
	return token, nil
}

func (s *KnowledgeService) provision(ctx context.Context, workspaceID, actorID pgtype.UUID, ws knowledge.Workspace) error {
	// The embedding model is chosen once, before any knowledge base: WeKnora
	// fixes a base's vector store at creation.
	model, err := ws.EnsureEmbeddingModel(ctx, s.Embedding)
	if err != nil {
		return fmt.Errorf("ensure embedding model: %w", err)
	}
	if err := s.ensureKnowledgeBases(ctx, workspaceID, ws, model.ID); err != nil {
		return err
	}
	serverID, err := s.ensureMCPServer(ctx, workspaceID, actorID, ws)
	if err != nil {
		return err
	}
	return s.assignToLiveAgents(ctx, workspaceID, serverID)
}

func (s *KnowledgeService) ensureKnowledgeBases(ctx context.Context, workspaceID pgtype.UUID, ws knowledge.Workspace, modelID string) error {
	local, err := s.Queries.ListKnowledgeBases(ctx, workspaceID)
	if err != nil {
		return fmt.Errorf("list knowledge bases: %w", err)
	}
	have := map[string]bool{}
	for _, b := range local {
		have[b.Key] = true
	}
	var remote []knowledge.KnowledgeBase
	remoteLoaded := false
	for i, def := range defaultKnowledgeBases {
		if have[def.Key] {
			continue
		}
		if !remoteLoaded {
			if remote, err = ws.ListKnowledgeBases(ctx); err != nil {
				return fmt.Errorf("list remote knowledge bases: %w", err)
			}
			remoteLoaded = true
		}
		// A crash between creating the base and recording it leaves a base
		// WeKnora knows and Multica does not; adopt it rather than duplicate.
		kbID := ""
		for _, r := range remote {
			if r.Name == def.Name {
				kbID = r.ID
				break
			}
		}
		if kbID == "" {
			created, err := ws.CreateKnowledgeBase(ctx, knowledge.KnowledgeBaseSpec{Name: def.Name, Description: def.Description, EmbeddingModelID: modelID})
			if err != nil {
				return fmt.Errorf("create knowledge base %q: %w", def.Key, err)
			}
			kbID = created.ID
		}
		if _, err := s.Queries.CreateKnowledgeBase(ctx, db.CreateKnowledgeBaseParams{
			WorkspaceID: workspaceID, WeknoraKbID: kbID, Key: def.Key, Name: def.Name, Visibility: "workspace", Position: int32(i),
		}); err != nil {
			return fmt.Errorf("record knowledge base %q: %w", def.Key, err)
		}
	}
	return nil
}

// workspaceVisibleKBIDs are the WeKnora ids company-read may read. Restricted
// bases are left out: they get their own endpoint.
func (s *KnowledgeService) workspaceVisibleKBIDs(ctx context.Context, workspaceID pgtype.UUID) ([]string, error) {
	bases, err := s.Queries.ListKnowledgeBases(ctx, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list knowledge bases: %w", err)
	}
	var ids []string
	for _, b := range bases {
		if b.Visibility == "workspace" {
			ids = append(ids, b.WeknoraKbID)
		}
	}
	sort.Strings(ids)
	return ids, nil
}

// libraryConfig is the MCP library entry for the endpoint: the remote shape
// every runtime already understands, with the token as a bearer header.
func (s *KnowledgeService) libraryConfig(endpointID, token string) ([]byte, error) {
	return json.Marshal(map[string]any{
		"type":    "http",
		"url":     s.Client.MCPURL(endpointID),
		"headers": map[string]string{"Authorization": "Bearer " + token},
	})
}

func sameStrings(a, b []string) bool {
	a, b = append([]string(nil), a...), append([]string(nil), b...)
	sort.Strings(a)
	sort.Strings(b)
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// ensureMCPServer makes sure the read-only endpoint exists in WeKnora and in
// the workspace MCP library, and returns the library entry's id.
//
// WeKnora shows an endpoint's token once. If the endpoint exists but the
// library entry does not (a crash between the two), the token is gone, so it
// is rotated, which is safe because nothing else holds it.
func (s *KnowledgeService) ensureMCPServer(ctx context.Context, workspaceID, actorID pgtype.UUID, ws knowledge.Workspace) (pgtype.UUID, error) {
	kbIDs, err := s.workspaceVisibleKBIDs(ctx, workspaceID)
	if err != nil {
		return pgtype.UUID{}, err
	}
	if len(kbIDs) == 0 {
		return pgtype.UUID{}, errors.New("no workspace-visible knowledge bases to publish")
	}
	endpoints, err := ws.ListMCPEndpoints(ctx)
	if err != nil {
		return pgtype.UUID{}, fmt.Errorf("list mcp endpoints: %w", err)
	}
	var endpoint *knowledge.MCPEndpoint
	for i := range endpoints {
		if endpoints[i].Name == KnowledgeMCPServerName {
			endpoint = &endpoints[i]
			break
		}
	}
	entry, entryErr := s.Queries.GetWorkspaceMcpServerByName(ctx, db.GetWorkspaceMcpServerByNameParams{WorkspaceID: workspaceID, Name: KnowledgeMCPServerName})
	if entryErr != nil && !errors.Is(entryErr, pgx.ErrNoRows) {
		return pgtype.UUID{}, fmt.Errorf("load mcp library entry: %w", entryErr)
	}
	haveEntry := entryErr == nil

	token := ""
	switch {
	case endpoint == nil:
		created, err := ws.CreateMCPEndpoint(ctx, knowledge.MCPEndpointSpec{
			Name:             KnowledgeMCPServerName,
			Description:      "Read-only company knowledge for Multica agents",
			KnowledgeBaseIDs: kbIDs,
			Tools:            knowledge.ReadOnlyMCPTools,
		})
		if err != nil {
			return pgtype.UUID{}, fmt.Errorf("create mcp endpoint: %w", err)
		}
		endpoint, token = &created, created.Token
	case !haveEntry:
		rotated, err := ws.RotateMCPEndpointToken(ctx, endpoint.ID)
		if err != nil {
			return pgtype.UUID{}, fmt.Errorf("rotate mcp endpoint token: %w", err)
		}
		token = rotated.Token
	}
	if !sameStrings(endpoint.KnowledgeBaseIDs, kbIDs) {
		if _, err := ws.SetMCPEndpointKnowledgeBases(ctx, endpoint.ID, kbIDs); err != nil {
			return pgtype.UUID{}, fmt.Errorf("update mcp endpoint knowledge bases: %w", err)
		}
	}
	if haveEntry {
		return entry.ID, nil
	}

	cfg, err := s.libraryConfig(endpoint.ID, token)
	if err != nil {
		return pgtype.UUID{}, fmt.Errorf("encode mcp library entry: %w", err)
	}
	created, err := s.Queries.CreateWorkspaceMcpServer(ctx, db.CreateWorkspaceMcpServerParams{
		WorkspaceID: workspaceID, Name: KnowledgeMCPServerName, Config: cfg, CreatedBy: actorID,
	})
	if isUniqueViolation(err) {
		// Someone created the library entry by hand meanwhile; keep theirs.
		existing, getErr := s.Queries.GetWorkspaceMcpServerByName(ctx, db.GetWorkspaceMcpServerByNameParams{WorkspaceID: workspaceID, Name: KnowledgeMCPServerName})
		if getErr != nil {
			return pgtype.UUID{}, fmt.Errorf("load mcp library entry: %w", getErr)
		}
		return existing.ID, nil
	}
	if err != nil {
		return pgtype.UUID{}, fmt.Errorf("create mcp library entry: %w", err)
	}
	return created.ID, nil
}

// assignToLiveAgents binds the library entry to every live agent. It follows
// the assignment protocol of the MCP API: the workspace is locked against
// deletion and the server row against concurrent delete, so no binding can
// outlive either (the junction has no foreign keys).
func (s *KnowledgeService) assignToLiveAgents(ctx context.Context, workspaceID, serverID pgtype.UUID) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin agent assignment: %w", err)
	}
	defer tx.Rollback(ctx)
	q := s.Queries.WithTx(tx)
	if _, err := q.LockWorkspaceForChatSessionCreate(ctx, workspaceID); err != nil {
		return fmt.Errorf("lock workspace: %w", err)
	}
	if _, err := q.LockWorkspaceMcpServerForShare(ctx, db.LockWorkspaceMcpServerForShareParams{ID: serverID, WorkspaceID: workspaceID}); err != nil {
		return fmt.Errorf("lock mcp server: %w", err)
	}
	if _, err := q.AssignMcpServerToLiveAgents(ctx, db.AssignMcpServerToLiveAgentsParams{WorkspaceID: workspaceID, ServerID: serverID}); err != nil {
		return fmt.Errorf("assign mcp server to agents: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit agent assignment: %w", err)
	}
	return nil
}
