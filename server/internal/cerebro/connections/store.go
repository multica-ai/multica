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
	ID                  string              `json:"id"`
	WorkspaceID         string              `json:"workspace_id"`
	Name                string              `json:"name"`
	DisplayName         string              `json:"display_name"`
	Type                string              `json:"type"`
	URL                 string              `json:"url"`
	Internal            bool                `json:"internal"`
	AuthConfig          AuthConfig          `json:"auth_config"`
	EndpointPermissions []EndpointPermission `json:"endpoint_permissions"`
	// Tools is the tool list discovered the last time the connection was tested
	// (mcp_http only). Persisted so the permissions UI can render one row per
	// underlying tool without re-probing the server. Empty for API connections.
	Tools               []Tool              `json:"tools"`
	Enabled             bool                `json:"enabled"`
	CreatedAt           time.Time           `json:"created_at"`
	UpdatedAt           time.Time           `json:"updated_at"`
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
		       auth_config, endpoint_permissions, enabled, created_at, updated_at, tools
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
		       auth_config, endpoint_permissions, enabled, created_at, updated_at, tools
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
		       auth_config, endpoint_permissions, enabled, created_at, updated_at, tools
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
	row := s.pool.QueryRow(ctx, `
		INSERT INTO workspace_connection
		  (workspace_id, name, display_name, type, url, internal, auth_config, endpoint_permissions)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, workspace_id, name, display_name, type, url, internal,
		          auth_config, endpoint_permissions, enabled, created_at, updated_at, tools
	`, p.WorkspaceID, p.Name, p.DisplayName, p.Type, p.URL, p.Internal, authJSON, epJSON)
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
	row := s.pool.QueryRow(ctx, `
		UPDATE workspace_connection
		   SET display_name = $3, url = $4, internal = $5,
		       auth_config = $6, endpoint_permissions = $7, enabled = $8,
		       updated_at = now()
		 WHERE id = $1 AND workspace_id = $2
		RETURNING id, workspace_id, name, display_name, type, url, internal,
		          auth_config, endpoint_permissions, enabled, created_at, updated_at, tools
	`, p.ID, p.WorkspaceID, p.DisplayName, p.URL, p.Internal, authJSON, epJSON, p.Enabled)
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

// BuildMCPConfig returns a {"mcpServers": {...}} JSON document for all enabled
// mcp_http connections in the workspace, ready to merge into RuntimeToolsConfig
// at task claim time. Returns nil if there are no enabled MCP connections.
func (s *Store) BuildMCPConfig(ctx context.Context, workspaceID pgtype.UUID) json.RawMessage {
	conns, err := s.ListEnabled(ctx, workspaceID)
	if err != nil || len(conns) == 0 {
		return nil
	}
	servers := make(map[string]any)
	for _, c := range conns {
		if c.Type != TypeMCPHTTP {
			continue
		}
		entry := map[string]any{"url": c.URL}
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
		servers[c.Name] = entry
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
		id, wsID                     pgtype.UUID
		name, displayName, typ, url  string
		internal, enabled            bool
		authRaw, epRaw, toolsRaw     []byte
		createdAt, updatedAt         pgtype.Timestamptz
	)
	if err := scan(&id, &wsID, &name, &displayName, &typ, &url, &internal,
		&authRaw, &epRaw, &enabled, &createdAt, &updatedAt, &toolsRaw); err != nil {
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
		Enabled:             enabled,
		CreatedAt:           createdAt.Time,
		UpdatedAt:           updatedAt.Time,
	}, nil
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
