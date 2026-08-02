// Package taskmandate implements the per-run, call-time access contract.
package taskmandate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrMissing  = errors.New("task mandate missing")
	ErrExpired  = errors.New("task mandate expired")
	ErrToolDeny = errors.New("tool is outside task mandate")
)

// Store persists immutable task contracts and checks them at dispatch time.
type Store struct {
	db  DB
	now func() time.Time
}

type DB interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

// Snapshot is the immutable access contract issued when a task is claimed.
// It is also the read model shown to operators, so the human and the running
// agent can inspect the same exact allowlist that call-time enforcement uses.
type Snapshot struct {
	TaskID       pgtype.UUID
	WorkspaceID  pgtype.UUID
	AgentID      pgtype.UUID
	AllowedTools []string
	IssuedAt     time.Time
	ExpiresAt    time.Time
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{db: pool, now: time.Now}
}

func NewStoreDB(db DB) *Store { return &Store{db: db, now: time.Now} }

// Get returns the exact stored task contract without applying expiry. Historical
// task transcripts need to show what was issued even after the run has ended.
func (s *Store) Get(ctx context.Context, taskID, workspaceID, agentID pgtype.UUID) (Snapshot, error) {
	if s == nil || s.db == nil || !taskID.Valid || !workspaceID.Valid || !agentID.Valid {
		return Snapshot{}, ErrMissing
	}
	var (
		snapshot Snapshot
		raw      []byte
	)
	err := s.db.QueryRow(ctx, `
		SELECT task_id, workspace_id, agent_id, allowed_tools, issued_at, expires_at
		FROM cerebro_task_mandate
		WHERE task_id=$1 AND workspace_id=$2 AND agent_id=$3`,
		taskID, workspaceID, agentID,
	).Scan(
		&snapshot.TaskID,
		&snapshot.WorkspaceID,
		&snapshot.AgentID,
		&raw,
		&snapshot.IssuedAt,
		&snapshot.ExpiresAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Snapshot{}, ErrMissing
	}
	if err != nil {
		return Snapshot{}, err
	}
	if err := json.Unmarshal(raw, &snapshot.AllowedTools); err != nil {
		return Snapshot{}, fmt.Errorf("task mandate: decode allowed tools: %w", err)
	}
	if snapshot.AllowedTools == nil {
		snapshot.AllowedTools = []string{}
	}
	return snapshot, nil
}

// Issue snapshots the exact tools a task may call through the immutable claim
// finalizer. It remains as the compatibility entry point for existing callers.
func (s *Store) Issue(ctx context.Context, taskID, workspaceID, agentID pgtype.UUID, tools []string, expiresAt time.Time) error {
	input, err := newContractInput(
		taskID,
		workspaceID,
		agentID,
		tools,
		nil,
		"taskmandate:v1",
	)
	if err != nil {
		return err
	}
	_, err = s.FinalizeClaim(ctx, input, "taskmandate.Store", "taskmandate.Store", expiresAt)
	return err
}

// Authorize is intentionally a fresh database read on every tool call. Expiry
// therefore bites at the call itself, even for a task that started earlier.
func (s *Store) Authorize(ctx context.Context, taskID, workspaceID, agentID pgtype.UUID, tool string) error {
	// The self-only capability lookup diagnoses the current policy/mandate
	// intersection. Keeping it behind the snapshot it explains creates a
	// circular lockout: a malformed or incomplete mandate would prevent the
	// agent from discovering that exact fault. The ordinary policy gate and the
	// tool's self-only actor check still apply after this ceiling.
	if IsSelfCapabilityLookup(tool) {
		return nil
	}
	if s == nil || s.db == nil || !taskID.Valid {
		return ErrMissing
	}
	var expiresAt time.Time
	var contains bool
	serverWildcard := MCPServerWildcard(tool)
	err := s.db.QueryRow(ctx, `
		SELECT expires_at, allowed_tools ? $4 OR ($5 <> '' AND allowed_tools ? $5)
		FROM cerebro_task_mandate
		WHERE task_id=$1 AND workspace_id=$2 AND agent_id=$3`,
		taskID, workspaceID, agentID, tool, serverWildcard,
	).Scan(&expiresAt, &contains)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrMissing
	}
	if err != nil {
		return err
	}
	if !expiresAt.After(s.now()) {
		return ErrExpired
	}
	if !contains {
		return ErrToolDeny
	}
	return nil
}

// IsSelfCapabilityLookup recognizes the callable and canonical spellings used
// by the Gateway, local MCP providers, and the capability catalog.
func IsSelfCapabilityLookup(tool string) bool {
	switch strings.TrimSpace(tool) {
	case "get_agent_capabilities",
		"mcp__multica__get_agent_capabilities",
		"platform:get_agent_capabilities":
		return true
	default:
		return false
	}
}

// MCPServerWildcard is the immutable task capability for one raw MCP server.
// A raw relay exposes the server's whole live tools/list surface, which may be
// newer than the connection's last stored discovery manifest. The wildcard is
// scoped to exactly one server and is issued only when the connection resolver
// has admitted that whole server as Allow-only.
func MCPServerWildcard(tool string) string {
	name := strings.TrimSpace(tool)
	if !strings.HasPrefix(name, "mcp__") {
		return ""
	}
	rest := strings.TrimPrefix(name, "mcp__")
	server, operation, ok := strings.Cut(rest, "__")
	if !ok || server == "" || operation == "" {
		return ""
	}
	return "mcp__" + server + "__*"
}
