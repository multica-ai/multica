package connections

// store.go provides the database layer for workspace connections (TECH-3108).
// Each connection is a named HTTP MCP server or REST API endpoint that all
// runtimes and agents in the workspace can use, subject to the tool-policy chain.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/util"
)

// TypeMCPHTTP is an HTTP/SSE MCP server (StreamableHTTP or SSE transport).
const TypeMCPHTTP = "mcp_http"

// TypeAPI is a plain REST API whose individual endpoints are permission-gated.
const TypeAPI = "api"

// Connection is the wire + domain model for one workspace connection.
type Connection struct {
	ID                  string               `json:"id"`
	WorkspaceID         string               `json:"workspace_id"`
	Name                string               `json:"name"`
	DisplayName         string               `json:"display_name"`
	Type                string               `json:"type"`
	URL                 string               `json:"url"`
	Internal            bool                 `json:"internal"`
	AuthConfig          AuthConfig           `json:"auth_config"`
	EndpointPermissions []EndpointPermission `json:"endpoint_permissions"`
	// Tools is the tool list discovered the last time the connection was tested
	// (mcp_http only). Persisted so the permissions UI can render one row per
	// underlying tool without re-probing the server. Empty for API connections.
	Tools []Tool `json:"tools"`
	// ScopableArgs declares which tool arguments are scoping axes for the
	// tool-policy WHEN layer (FIR-2083). Empty = no scopable arguments.
	ScopableArgs []ScopableArg `json:"scopable_args"`
	Enabled      bool          `json:"enabled"`
	CreatedAt    time.Time     `json:"created_at"`
	UpdatedAt    time.Time     `json:"updated_at"`
}

// ScopableArg declares one tool argument whose value can be scoped from the
// tool-policy WHEN layer (FIR-2083). It names the tool + argument that form the
// scoping axis (e.g. query_run · data_source_id) and the options-source tool
// whose result fills the search + multi-select picker (e.g. data_sources_list),
// plus how the picker groups and tags the options. A connection with no
// scopable arguments behaves exactly as before.
type ScopableArg struct {
	// Tool is the underlying tool the scoping applies to (e.g. "query_run").
	Tool string `json:"tool"`
	// Arg is the argument on Tool that is the scoping axis (e.g. "data_source_id").
	Arg string `json:"arg"`
	// OptionsSourceTool is the tool on this same connection whose result fills
	// the picker's choices (e.g. "data_sources_list").
	OptionsSourceTool string `json:"options_source_tool"`
	// GroupBy names the option field the picker groups rows by (e.g. "folder").
	// Optional; empty renders a flat list.
	GroupBy string `json:"group_by,omitempty"`
	// TagField names the option field carrying tags (e.g. "tags"). Optional.
	TagField string `json:"tag_field,omitempty"`
	// Label is an optional human label for the axis in the admin UI.
	Label string `json:"label,omitempty"`
}

// Tool is one MCP tool exposed by a connection, persisted on the connection row.
type Tool struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// AuthConfig holds connection credentials. Fields are optional; only the
// relevant ones are populated per connection type.
type AuthConfig struct {
	BearerToken    string `json:"bearer_token,omitempty"`
	APIKey         string `json:"api_key,omitempty"`
	APIKeyHeader   string `json:"api_key_header,omitempty"`
	CFAccessID     string `json:"cf_access_id,omitempty"`
	CFAccessSecret string `json:"cf_access_secret,omitempty"`
}

// EndpointPermission describes one REST path and the HTTP methods allowed on it.
// Used to build per-endpoint CRUD controls in the permissions UI.
type EndpointPermission struct {
	Path    string   `json:"path"`
	Methods []string `json:"methods"`
}

// CreateParams are the fields required to create a connection.
type CreateParams struct {
	WorkspaceID         pgtype.UUID
	Name                string
	DisplayName         string
	Type                string
	URL                 string
	Internal            bool
	AuthConfig          AuthConfig
	EndpointPermissions []EndpointPermission
	ScopableArgs        []ScopableArg
}

// UpdateParams are the mutable fields on an existing connection.
type UpdateParams struct {
	ID                  pgtype.UUID
	WorkspaceID         pgtype.UUID
	DisplayName         string
	URL                 string
	Internal            bool
	AuthConfig          AuthConfig
	EndpointPermissions []EndpointPermission
	ScopableArgs        []ScopableArg
	Enabled             bool
}

var ErrNotFound = errors.New("connection not found")
var ErrDuplicateName = errors.New("a connection with that name already exists")

// Store is the database-backed connection registry.
type Store struct {
	pool *pgxpool.Pool
}

// New constructs a Store backed by the given pool.
func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// List returns all connections for a workspace ordered by created_at.
func (s *Store) List(ctx context.Context, workspaceID pgtype.UUID) ([]Connection, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, workspace_id, name, display_name, type, url, internal,
		       auth_config, endpoint_permissions, enabled, created_at, updated_at, tools, scopable_args
		FROM workspace_connection
		WHERE workspace_id = $1
		ORDER BY created_at ASC
	`, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("connections: list: %w", err)
	}
	defer rows.Close()
	return scanRows(rows)
}

// ListEnabled returns only enabled connections for a workspace.
// Used by the daemon injection path at task spawn time.
func (s *Store) ListEnabled(ctx context.Context, workspaceID pgtype.UUID) ([]Connection, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, workspace_id, name, display_name, type, url, internal,
		       auth_config, endpoint_permissions, enabled, created_at, updated_at, tools, scopable_args
		FROM workspace_connection
		WHERE workspace_id = $1 AND enabled = true
		ORDER BY created_at ASC
	`, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("connections: list enabled: %w", err)
	}
	defer rows.Close()
	return scanRows(rows)
}

// Get returns one connection by ID, scoped to the workspace.
func (s *Store) Get(ctx context.Context, id, workspaceID pgtype.UUID) (Connection, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, workspace_id, name, display_name, type, url, internal,
		       auth_config, endpoint_permissions, enabled, created_at, updated_at, tools, scopable_args
		FROM workspace_connection
		WHERE id = $1 AND workspace_id = $2
	`, id, workspaceID)
	c, err := scanRow(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Connection{}, ErrNotFound
	}
	return c, err
}

// Create inserts a new connection and returns it.
func (s *Store) Create(ctx context.Context, p CreateParams) (Connection, error) {
	if err := validateType(p.Type); err != nil {
		return Connection{}, err
	}
	authJSON, err := json.Marshal(p.AuthConfig)
	if err != nil {
		return Connection{}, fmt.Errorf("connections: marshal auth: %w", err)
	}
	epJSON, err := marshalEndpoints(p.EndpointPermissions)
	if err != nil {
		return Connection{}, err
	}
	saJSON, err := marshalScopableArgs(p.ScopableArgs)
	if err != nil {
		return Connection{}, err
	}
	row := s.pool.QueryRow(ctx, `
		INSERT INTO workspace_connection
		  (workspace_id, name, display_name, type, url, internal, auth_config, endpoint_permissions, scopable_args)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id, workspace_id, name, display_name, type, url, internal,
		          auth_config, endpoint_permissions, enabled, created_at, updated_at, tools, scopable_args
	`, p.WorkspaceID, p.Name, p.DisplayName, p.Type, p.URL, p.Internal, authJSON, epJSON, saJSON)
	c, err := scanRow(row)
	if err != nil && strings.Contains(err.Error(), "workspace_connection_name_unique") {
		return Connection{}, ErrDuplicateName
	}
	return c, err
}

// Update modifies the mutable fields of a connection and returns the updated row.
func (s *Store) Update(ctx context.Context, p UpdateParams) (Connection, error) {
	authJSON, err := json.Marshal(p.AuthConfig)
	if err != nil {
		return Connection{}, fmt.Errorf("connections: marshal auth: %w", err)
	}
	epJSON, err := marshalEndpoints(p.EndpointPermissions)
	if err != nil {
		return Connection{}, err
	}
	saJSON, err := marshalScopableArgs(p.ScopableArgs)
	if err != nil {
		return Connection{}, err
	}
	row := s.pool.QueryRow(ctx, `
		UPDATE workspace_connection
		   SET display_name = $3, url = $4, internal = $5,
		       auth_config = $6, endpoint_permissions = $7, enabled = $8,
		       scopable_args = $9, updated_at = now()
		 WHERE id = $1 AND workspace_id = $2
		RETURNING id, workspace_id, name, display_name, type, url, internal,
		          auth_config, endpoint_permissions, enabled, created_at, updated_at, tools, scopable_args
	`, p.ID, p.WorkspaceID, p.DisplayName, p.URL, p.Internal, authJSON, epJSON, p.Enabled, saJSON)
	c, err := scanRow(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Connection{}, ErrNotFound
	}
	return c, err
}

// Delete removes a connection. Returns ErrNotFound if it didn't exist.
func (s *Store) Delete(ctx context.Context, id, workspaceID pgtype.UUID) error {
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM workspace_connection WHERE id = $1 AND workspace_id = $2
	`, id, workspaceID)
	if err != nil {
		return fmt.Errorf("connections: delete: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// CapabilityKey returns the tool-policy key for a connection name.
// Connections appear in the permissions table as "connection:<name>".
func CapabilityKey(name string) string {
	return "connection:" + name
}

// MCPURLRewriter, when supplied to BuildMCPConfigRelayed, replaces a
// connection's direct URL (often an internal-only host a local runtime cannot
// reach) with a Multica-hosted relay URL the runtime CAN reach, plus a scoped
// bearer the relay authenticates. Returning ok=false leaves the direct entry
// untouched (the runtime can already reach that connection). See
// internal/cerebro/mcprelay (FIR-1563).
type MCPURLRewriter interface {
	RelayEntry(workspaceID, connectionName, connectionURL string, internal bool) (url, bearer string, ok bool)
}

// BuildMCPConfig returns a {"mcpServers": {...}} JSON document for all enabled
// mcp_http connections in the workspace, ready to merge into RuntimeToolsConfig
// at task claim time. Returns nil if there are no enabled MCP connections.
func (s *Store) BuildMCPConfig(ctx context.Context, workspaceID pgtype.UUID) json.RawMessage {
	return s.BuildMCPConfigRelayed(ctx, workspaceID, nil)
}

// BuildMCPConfigRelayed is BuildMCPConfig with optional URL relaying. When
// rewriter is non-nil, each enabled mcp_http connection is offered to it; if it
// returns a relay entry, the connection's real URL + credentials are withheld
// from the runtime and replaced by the relay URL + scoped bearer. This is how a
// LOCAL runtime reaches an internal-only connection — through Multica, never the
// internal path directly (FIR-1563). A nil rewriter reproduces the direct path
// used by cloud runtimes that already sit on the internal network.
func (s *Store) BuildMCPConfigRelayed(ctx context.Context, workspaceID pgtype.UUID, rewriter MCPURLRewriter) json.RawMessage {
	conns, err := s.ListEnabled(ctx, workspaceID)
	if err != nil || len(conns) == 0 {
		return nil
	}
	servers := make(map[string]any)
	for _, c := range conns {
		if c.Type != TypeMCPHTTP {
			continue
		}
		servers[c.Name] = mcpServerEntry(c, rewriter)
	}
	if len(servers) == 0 {
		return nil
	}
	b, err := json.Marshal(map[string]any{"mcpServers": servers})
	if err != nil {
		return nil
	}
	return b
}

// mcpServerEntry builds the Claude Code --mcp-config entry for a single
// mcp_http connection, applying the relay rewriter when one is supplied.
//
// "type": "http" is mandatory: Claude Code's MCP client uses it as the
// transport discriminator and silently ignores an HTTP server entry that omits
// it — which is exactly why a local runtime saw zero tools before this was
// added (FIR-1563). Codex/OpenCode translators key off "url", so the field is
// harmless there.
func mcpServerEntry(c Connection, rewriter MCPURLRewriter) map[string]any {
	if rewriter != nil {
		if relayURL, bearer, ok := rewriter.RelayEntry(c.WorkspaceID, c.Name, c.URL, c.Internal); ok {
			entry := map[string]any{"type": "http", "url": relayURL}
			if bearer != "" {
				entry["headers"] = map[string]string{"Authorization": "Bearer " + bearer}
			}
			return entry
		}
	}
	entry := map[string]any{"type": "http", "url": c.URL}
	headers := make(map[string]string)
	if c.AuthConfig.BearerToken != "" {
		headers["Authorization"] = "Bearer " + c.AuthConfig.BearerToken
	}
	if c.AuthConfig.APIKey != "" {
		key := c.AuthConfig.APIKeyHeader
		if key == "" {
			key = "X-API-Key"
		}
		headers[key] = c.AuthConfig.APIKey
	}
	if c.AuthConfig.CFAccessID != "" {
		headers["CF-Access-Client-Id"] = c.AuthConfig.CFAccessID
	}
	if c.AuthConfig.CFAccessSecret != "" {
		headers["CF-Access-Client-Secret"] = c.AuthConfig.CFAccessSecret
	}
	if len(headers) > 0 {
		entry["headers"] = headers
	}
	return entry
}

// GetEnabledByName returns one enabled connection by its (workspace-unique)
// name. Used by the MCP relay to resolve a connection server-side from a token.
func (s *Store) GetEnabledByName(ctx context.Context, workspaceID pgtype.UUID, name string) (Connection, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, workspace_id, name, display_name, type, url, internal,
		       auth_config, endpoint_permissions, enabled, created_at, updated_at, tools, scopable_args
		FROM workspace_connection
		WHERE workspace_id = $1 AND name = $2 AND enabled = true
	`, workspaceID, name)
	c, err := scanRow(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Connection{}, ErrNotFound
	}
	return c, err
}

// --- helpers ----------------------------------------------------------------

func validateType(t string) error {
	if t == TypeMCPHTTP || t == TypeAPI {
		return nil
	}
	return fmt.Errorf("connections: unknown type %q (must be %q or %q)", t, TypeMCPHTTP, TypeAPI)
}

func marshalEndpoints(eps []EndpointPermission) ([]byte, error) {
	if eps == nil {
		eps = []EndpointPermission{}
	}
	b, err := json.Marshal(eps)
	if err != nil {
		return nil, fmt.Errorf("connections: marshal endpoints: %w", err)
	}
	return b, nil
}

func scanRows(rows pgx.Rows) ([]Connection, error) {
	var out []Connection
	for rows.Next() {
		c, err := scanRowValues(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func scanRow(row pgx.Row) (Connection, error) {
	return scanRowValues(row.Scan)
}

type scanFn func(dest ...any) error

func scanRowValues(scan scanFn) (Connection, error) {
	var (
		id, wsID                    pgtype.UUID
		name, displayName, typ, url string
		internal, enabled           bool
		authRaw, epRaw, toolsRaw    []byte
		scopableArgsRaw             []byte
		createdAt, updatedAt        pgtype.Timestamptz
	)
	if err := scan(&id, &wsID, &name, &displayName, &typ, &url, &internal,
		&authRaw, &epRaw, &enabled, &createdAt, &updatedAt, &toolsRaw, &scopableArgsRaw); err != nil {
		return Connection{}, fmt.Errorf("connections: scan: %w", err)
	}
	var auth AuthConfig
	_ = json.Unmarshal(authRaw, &auth)
	var eps []EndpointPermission
	_ = json.Unmarshal(epRaw, &eps)
	if eps == nil {
		eps = []EndpointPermission{}
	}
	var tools []Tool
	_ = json.Unmarshal(toolsRaw, &tools)
	if tools == nil {
		tools = []Tool{}
	}
	var scopableArgs []ScopableArg
	_ = json.Unmarshal(scopableArgsRaw, &scopableArgs)
	if scopableArgs == nil {
		scopableArgs = []ScopableArg{}
	}
	return Connection{
		ID:                  util.UUIDToString(id),
		WorkspaceID:         util.UUIDToString(wsID),
		Name:                name,
		DisplayName:         displayName,
		Type:                typ,
		URL:                 url,
		Internal:            internal,
		AuthConfig:          auth,
		EndpointPermissions: eps,
		Tools:               tools,
		ScopableArgs:        scopableArgs,
		Enabled:             enabled,
		CreatedAt:           createdAt.Time,
		UpdatedAt:           updatedAt.Time,
	}, nil
}

// marshalScopableArgs serializes the scopable-args declaration for storage,
// normalizing nil to an empty array and dropping inert entries (an entry that
// names no tool/arg/options-source is meaningless and never persisted).
func marshalScopableArgs(args []ScopableArg) ([]byte, error) {
	cleaned := make([]ScopableArg, 0, len(args))
	for _, a := range args {
		a.Tool = strings.TrimSpace(a.Tool)
		a.Arg = strings.TrimSpace(a.Arg)
		a.OptionsSourceTool = strings.TrimSpace(a.OptionsSourceTool)
		a.GroupBy = strings.TrimSpace(a.GroupBy)
		a.TagField = strings.TrimSpace(a.TagField)
		a.Label = strings.TrimSpace(a.Label)
		if a.Tool == "" || a.Arg == "" || a.OptionsSourceTool == "" {
			continue
		}
		cleaned = append(cleaned, a)
	}
	b, err := json.Marshal(cleaned)
	if err != nil {
		return nil, fmt.Errorf("connections: marshal scopable_args: %w", err)
	}
	return b, nil
}

// OptionsSourceFor returns the declared scopable argument whose options-source
// tool matches the given tool name, reporting whether one exists. It is the
// allowlist the options endpoint checks before relaying an MCP call so a caller
// cannot drive an arbitrary tool through the connection's credentials.
func (c Connection) OptionsSourceFor(optionsSourceTool string) (ScopableArg, bool) {
	for _, a := range c.ScopableArgs {
		if a.OptionsSourceTool == optionsSourceTool {
			return a, true
		}
	}
	return ScopableArg{}, false
}

// UpdateTools persists the tool list discovered by a connection test so the
// permissions UI can render one row per tool. Best-effort: callers ignore the
// error when the connection was an ad-hoc (unsaved) test.
func (s *Store) UpdateTools(ctx context.Context, id, workspaceID pgtype.UUID, tools []Tool) error {
	if tools == nil {
		tools = []Tool{}
	}
	toolsJSON, err := json.Marshal(tools)
	if err != nil {
		return fmt.Errorf("connections: marshal tools: %w", err)
	}
	_, err = s.pool.Exec(ctx, `
		UPDATE workspace_connection
		   SET tools = $3, updated_at = now()
		 WHERE id = $1 AND workspace_id = $2
	`, id, workspaceID, toolsJSON)
	if err != nil {
		return fmt.Errorf("connections: update tools: %w", err)
	}
	return nil
}
