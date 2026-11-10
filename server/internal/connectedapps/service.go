package connectedapps

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	composiointeg "github.com/multica-ai/multica/server/internal/integrations/composio"
	"github.com/multica-ai/multica/server/internal/runtimeapps"
	"github.com/multica-ai/multica/server/internal/util"
	sdk "github.com/multica-ai/multica/server/pkg/composio"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// GatewayServerName is the MCP server name every run sees its apps under.
const GatewayServerName = "multica_apps"

// GatewayPath is the API route of the MCP gateway.
const GatewayPath = "/api/apps/mcp"

// Provider tags gateway-served apps in a run's connected-app metadata.
const Provider = "multica"

// RequestTTL is how long a confirmation waits before it expires.
const RequestTTL = 24 * time.Hour

// ConnectRequestTTL is how long a connection request waits. Connecting an
// account can need someone else (an IT admin approving the OAuth app), so it
// waits longer than a confirmation.
const ConnectRequestTTL = 7 * 24 * time.Hour

// DefaultCallsPerMinute caps the calls one account runs per minute.
const DefaultCallsPerMinute = 30

func requestTTL(kind string) time.Duration {
	if kind == KindConnect {
		return ConnectRequestTTL
	}
	return RequestTTL
}

// tokenTTL bounds a gateway token. The task status check is the real bound;
// this only caps a token that leaks from a run that never finishes.
const tokenTTL = 26 * time.Hour

// Errors surfaced to the HTTP layer.
var (
	ErrRequestNotFound = errors.New("connectedapps: request not found")
	ErrNotApprover     = errors.New("connectedapps: only the approver can decide")
	ErrNotPending      = errors.New("connectedapps: request is no longer pending")
	ErrAccountMissing  = errors.New("connectedapps: account not connected")
	// ErrNoLongerAllowed: the approver left the workspace or lost access to
	// the agent while the request waited. The request is withdrawn.
	ErrNoLongerAllowed = errors.New("connectedapps: the approver can no longer use this agent")
	// ErrRateLimited: the account used its per-minute budget. The request
	// stays pending, so approving again a minute later runs it.
	ErrRateLimited = errors.New("connectedapps: the account is over its per-minute call budget")
)

// Resumer continues the agent's work after a person decides on a request: a
// new run on the issue, or a message in the chat.
type Resumer interface {
	ResumeAfterDecision(ctx context.Context, req db.AppActionRequest, actorID pgtype.UUID, note string) error
	// CanActFor reports whether userID may still run agentID in the
	// workspace, by the same rule that admits a new run.
	CanActFor(ctx context.Context, workspaceID, agentID, userID pgtype.UUID) bool
}

// Config wires a Service.
type Config struct {
	Queries  *db.Queries
	Composio *composiointeg.Service
	// TokenSecret signs gateway tokens. Derive it with DeriveTokenSecret.
	TokenSecret []byte
	// PublicBaseURL is the API's public origin, e.g. "https://api.multica.ai".
	PublicBaseURL string
	Bus           *events.Bus
	Now           func() time.Time
	// CallsPerMinute caps the calls one account runs per minute;
	// DefaultCallsPerMinute when zero.
	CallsPerMinute int
}

// Service is the connected-apps runtime: overlay, gateway, and confirmations.
type Service struct {
	q          *db.Queries
	composio   *composiointeg.Service
	secret     []byte
	gatewayURL string
	bus        *events.Bus
	now        func() time.Time
	resumer    Resumer
	perMinute  int
}

// New returns a Service, or an error when a required dependency is missing.
func New(cfg Config) (*Service, error) {
	if cfg.Queries == nil || cfg.Composio == nil {
		return nil, errors.New("connectedapps: queries and composio are required")
	}
	if len(cfg.TokenSecret) == 0 {
		return nil, errors.New("connectedapps: token secret is required")
	}
	base := strings.TrimRight(strings.TrimSpace(cfg.PublicBaseURL), "/")
	if base == "" {
		return nil, errors.New("connectedapps: public base url is required")
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	perMinute := cfg.CallsPerMinute
	if perMinute <= 0 {
		perMinute = DefaultCallsPerMinute
	}
	return &Service{
		q:          cfg.Queries,
		composio:   cfg.Composio,
		secret:     cfg.TokenSecret,
		gatewayURL: base + GatewayPath,
		bus:        cfg.Bus,
		now:        now,
		perMinute:  perMinute,
	}, nil
}

// SetResumer installs how agents are resumed after a decision.
func (s *Service) SetResumer(r Resumer) { s.resumer = r }

// Catalog is which apps a workspace lets its members use.
type Catalog struct {
	Mode string
	// Allowed lists the apps of a custom catalog.
	Allowed []string
}

// Allows reports whether the catalog permits slug.
func (c Catalog) Allows(slug string) bool {
	switch c.Mode {
	case CatalogCurated:
		_, ok := Curated(slug)
		return ok
	case CatalogCustom:
		slug = NormalizeSlug(slug)
		for _, allowed := range c.Allowed {
			if NormalizeSlug(allowed) == slug {
				return true
			}
		}
		return false
	}
	return true
}

// Catalog returns the workspace's catalog, every app by default.
func (s *Service) Catalog(ctx context.Context, workspaceID pgtype.UUID) Catalog {
	row, err := s.q.GetWorkspaceAppSetting(ctx, workspaceID)
	if err != nil {
		return Catalog{Mode: CatalogAll}
	}
	switch row.CatalogMode {
	case CatalogCurated:
		return Catalog{Mode: CatalogCurated}
	case CatalogCustom:
		return Catalog{Mode: CatalogCustom, Allowed: row.AllowedToolkits}
	}
	return Catalog{Mode: CatalogAll}
}

// ClassOverrides returns the workspace's class corrections for one app, keyed
// by upper-case tool slug.
func (s *Service) ClassOverrides(ctx context.Context, workspaceID pgtype.UUID, slug string) map[string]string {
	rows, err := s.q.ListAppToolClassOverrides(ctx, db.ListAppToolClassOverridesParams{
		WorkspaceID: workspaceID, ToolkitSlug: NormalizeSlug(slug),
	})
	out := make(map[string]string, len(rows))
	if err != nil {
		return out
	}
	for _, row := range rows {
		out[strings.ToUpper(row.ToolSlug)] = row.ActionClass
	}
	return out
}

// classify is Classify with the workspace's corrections applied first.
func (s *Service) classify(ctx context.Context, workspaceID pgtype.UUID, slug string, tool *sdk.Tool) string {
	class, err := s.q.GetAppToolClassOverride(ctx, db.GetAppToolClassOverrideParams{
		WorkspaceID: workspaceID, ToolkitSlug: NormalizeSlug(slug), ToolSlug: strings.ToUpper(tool.Slug),
	})
	if err == nil {
		return class
	}
	return Classify(slug, tool.Slug, tool.Tags)
}

type gatewayServer struct {
	Type    string            `json:"type"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers,omitempty"`
}

type gatewayOverlay struct {
	MCPServers map[string]gatewayServer `json:"mcpServers"`
}

// BuildTaskOverlay is the enqueue-time overlay for a run of agent. It carries
// no credential: only the gateway address and which apps the agent has. The
// claim handler adds the run's token (AuthorizeOverlay) once the task id is
// final. A zero result means the agent has no apps.
func (s *Service) BuildTaskOverlay(ctx context.Context, originatorUserID pgtype.UUID, agent db.Agent) (runtimeapps.MCPOverlayResult, error) {
	apps, err := s.q.ListAgentApps(ctx, agent.ID)
	if err != nil {
		return runtimeapps.MCPOverlayResult{}, fmt.Errorf("connectedapps: list agent apps: %w", err)
	}
	if len(apps) == 0 {
		return runtimeapps.MCPOverlayResult{}, nil
	}
	catalog := s.Catalog(ctx, agent.WorkspaceID)
	team, err := s.activeTeamAccounts(ctx, agent.WorkspaceID)
	if err != nil {
		return runtimeapps.MCPOverlayResult{}, err
	}
	personal := map[string]db.UserComposioConnection{}
	if originatorUserID.Valid {
		if personal, err = s.activePersonalAccounts(ctx, originatorUserID); err != nil {
			return runtimeapps.MCPOverlayResult{}, err
		}
	}
	meta := make([]runtimeapps.ConnectedApp, 0, len(apps))
	for _, app := range apps {
		if !catalog.Allows(app.ToolkitSlug) {
			continue
		}
		status := "not_connected"
		if app.AccountScope == ScopeTeam {
			if _, ok := team[app.ToolkitSlug]; ok {
				status = "connected"
			}
		} else if _, ok := personal[app.ToolkitSlug]; ok {
			status = "connected"
		}
		meta = append(meta, runtimeapps.ConnectedApp{
			Provider:      Provider,
			ServerName:    GatewayServerName,
			ToolkitSlug:   app.ToolkitSlug,
			ToolkitName:   runtimeapps.DisplayNameForToolkitSlug(app.ToolkitSlug),
			AccountScope:  app.AccountScope,
			Level:         app.Level,
			AccountStatus: status,
		})
	}
	if len(meta) == 0 {
		return runtimeapps.MCPOverlayResult{}, nil
	}
	raw, err := json.Marshal(gatewayOverlay{MCPServers: map[string]gatewayServer{
		GatewayServerName: {Type: "http", URL: s.gatewayURL},
	}})
	if err != nil {
		return runtimeapps.MCPOverlayResult{}, err
	}
	return runtimeapps.MCPOverlayResult{MCPOverlay: raw, ConnectedApps: meta}, nil
}

// AuthorizeOverlay adds a token for taskID to the gateway entry of a stored
// overlay. Overlays without a gateway entry are returned unchanged.
func (s *Service) AuthorizeOverlay(overlay []byte, taskID pgtype.UUID) ([]byte, error) {
	var parsed struct {
		MCPServers map[string]json.RawMessage `json:"mcpServers"`
	}
	if err := json.Unmarshal(overlay, &parsed); err != nil {
		return nil, err
	}
	entry, ok := parsed.MCPServers[GatewayServerName]
	if !ok {
		return overlay, nil
	}
	var server gatewayServer
	if err := json.Unmarshal(entry, &server); err != nil {
		return nil, err
	}
	token, err := SignTaskToken(s.secret, util.UUIDToString(taskID), s.now().Add(tokenTTL))
	if err != nil {
		return nil, err
	}
	server.Type = "http"
	server.URL = s.gatewayURL
	server.Headers = map[string]string{"Authorization": "Bearer " + token}
	raw, err := json.Marshal(server)
	if err != nil {
		return nil, err
	}
	parsed.MCPServers[GatewayServerName] = raw
	return json.Marshal(map[string]any{"mcpServers": parsed.MCPServers})
}

func (s *Service) activeTeamAccounts(ctx context.Context, workspaceID pgtype.UUID) (map[string]db.WorkspaceComposioConnection, error) {
	rows, err := s.q.ListWorkspaceComposioConnections(ctx, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("connectedapps: list team accounts: %w", err)
	}
	out := map[string]db.WorkspaceComposioConnection{}
	for _, row := range rows {
		slug := NormalizeSlug(row.ToolkitSlug)
		if row.Status != "active" {
			continue
		}
		if _, dup := out[slug]; !dup {
			out[slug] = row
		}
	}
	return out, nil
}

func (s *Service) activePersonalAccounts(ctx context.Context, userID pgtype.UUID) (map[string]db.UserComposioConnection, error) {
	rows, err := s.q.ListActiveUserComposioConnections(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("connectedapps: list personal accounts: %w", err)
	}
	out := map[string]db.UserComposioConnection{}
	for _, row := range rows {
		slug := NormalizeSlug(row.ToolkitSlug)
		if _, dup := out[slug]; !dup {
			out[slug] = row
		}
	}
	return out, nil
}

// account is the connected account one call acts through.
type account struct {
	scope              string
	connectedAccountID string
	composioUserID     string
	connectionID       pgtype.UUID
	// owner is the member whose personal account it is; invalid for team.
	owner pgtype.UUID
}

// resolveAccount finds the account an app acts as for a run: the workspace's
// team account, or the personal account of the member who triggered the run.
func (s *Service) resolveAccount(ctx context.Context, workspaceID, triggeredBy pgtype.UUID, slug, scope string) (account, error) {
	if scope == ScopeTeam {
		team, err := s.activeTeamAccounts(ctx, workspaceID)
		if err != nil {
			return account{}, err
		}
		row, ok := team[slug]
		if !ok {
			return account{}, ErrAccountMissing
		}
		return account{scope: ScopeTeam, connectedAccountID: row.ConnectedAccountID, composioUserID: row.ComposioUserID, connectionID: row.ID}, nil
	}
	if !triggeredBy.Valid {
		return account{}, ErrAccountMissing
	}
	personal, err := s.activePersonalAccounts(ctx, triggeredBy)
	if err != nil {
		return account{}, err
	}
	row, ok := personal[slug]
	if !ok {
		return account{}, ErrAccountMissing
	}
	return account{scope: ScopePersonal, connectedAccountID: row.ConnectedAccountID, composioUserID: row.ComposioUserID, connectionID: row.ID, owner: triggeredBy}, nil
}

// accountRule returns the account owner's rule for one class, or "".
func (s *Service) accountRule(ctx context.Context, workspaceID pgtype.UUID, slug string, acct account, class string) (string, error) {
	rules, err := s.q.ListAppActionRules(ctx, db.ListAppActionRulesParams{
		WorkspaceID: workspaceID,
		ToolkitSlug: slug,
		OwnerUserID: acct.owner,
	})
	if err != nil {
		return "", fmt.Errorf("connectedapps: list rules: %w", err)
	}
	for _, r := range rules {
		if r.ActionClass == class {
			return r.Policy, nil
		}
	}
	return "", nil
}

func (s *Service) touch(ctx context.Context, workspaceID pgtype.UUID, acct account) {
	if acct.scope == ScopeTeam {
		_ = s.q.TouchWorkspaceComposioConnection(ctx, db.TouchWorkspaceComposioConnectionParams{ID: acct.connectionID, WorkspaceID: workspaceID})
		return
	}
	_ = s.q.TouchUserComposioConnection(ctx, db.TouchUserComposioConnectionParams{ID: acct.connectionID, UserID: acct.owner})
}

type toolCall struct {
	workspaceID, taskID, agentID, issueID, chatSessionID, triggeredBy pgtype.UUID
	acct                                                              account
	toolkit, tool, class, outcome                                     string
	requestID                                                         pgtype.UUID
	err                                                               string
}

// Upstream errors can contain tool arguments or private app data. Log only
// fixed failure classes and the local identifiers needed to find the call.
func logExecutionFailure(ctx context.Context, c toolCall, errorClass string) {
	slog.WarnContext(ctx, "connectedapps: tool execution failed",
		"workspace_id", util.UUIDToString(c.workspaceID),
		"task_id", util.UUIDToString(c.taskID),
		"request_id", util.UUIDToString(c.requestID),
		"toolkit", c.toolkit, "tool", c.tool,
		"account_scope", c.acct.scope, "error_class", errorClass)
}

func (s *Service) recordCall(ctx context.Context, c toolCall) {
	scope := c.acct.scope
	if scope == "" {
		scope = ScopePersonal
	}
	_ = s.q.InsertAppToolCall(ctx, db.InsertAppToolCallParams{
		WorkspaceID:        c.workspaceID,
		TaskID:             c.taskID,
		AgentID:            c.agentID,
		IssueID:            c.issueID,
		ChatSessionID:      c.chatSessionID,
		TriggeredBy:        c.triggeredBy,
		AccountScope:       scope,
		AccountOwnerUserID: c.acct.owner,
		ToolkitSlug:        c.toolkit,
		ToolSlug:           c.tool,
		ActionClass:        c.class,
		Outcome:            c.outcome,
		RequestID:          c.requestID,
		Error:              pgtype.Text{String: c.err, Valid: c.err != ""},
	})
	if s.bus != nil && c.issueID.Valid {
		s.bus.Publish(events.Event{
			Type:        EventCallCreated,
			WorkspaceID: util.UUIDToString(c.workspaceID),
			ActorType:   "agent",
			ActorID:     util.UUIDToString(c.agentID),
			Payload:     map[string]any{"issue_id": util.UUIDToString(c.issueID)},
		})
	}
}

// overLimit reports whether acct already ran its per-minute share of calls.
func (s *Service) overLimit(ctx context.Context, workspaceID pgtype.UUID, slug string, acct account) bool {
	n, err := s.q.CountRecentAppAccountCalls(ctx, db.CountRecentAppAccountCallsParams{
		WorkspaceID:  workspaceID,
		ToolkitSlug:  slug,
		AccountScope: acct.scope,
		Owner:        acct.owner,
		Since:        pgtype.Timestamptz{Time: s.now().Add(-time.Minute), Valid: true},
	})
	return err == nil && n >= int64(s.perMinute)
}

// firstBlock reports whether this is the run's first blocked call of a tool,
// so the person who triggered it is told once, not on every retry.
func (s *Service) firstBlock(ctx context.Context, c toolCall) bool {
	if !c.taskID.Valid {
		return true
	}
	seen, err := s.q.HasBlockedAppToolCall(ctx, db.HasBlockedAppToolCallParams{
		WorkspaceID: c.workspaceID, ToolkitSlug: c.toolkit, TaskID: c.taskID, ToolSlug: c.tool,
		Since: pgtype.Timestamptz{Time: s.now().Add(-tokenTTL), Valid: true},
	})
	return err == nil && !seen
}

func isNoRows(err error) bool { return errors.Is(err, pgx.ErrNoRows) }
