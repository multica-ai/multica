// Package taskmandate implements the per-run, call-time access contract.
package taskmandate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{db: pool, now: time.Now}
}

func NewStoreDB(db DB) *Store { return &Store{db: db, now: time.Now} }

// Issue snapshots the exact tools a task may call. Re-issuing for the same task
// replaces the snapshot atomically, which supports a task being reclaimed.
func (s *Store) Issue(ctx context.Context, taskID, workspaceID, agentID pgtype.UUID, tools []string, expiresAt time.Time) error {
	if s == nil || s.db == nil || !taskID.Valid || !workspaceID.Valid || !agentID.Valid {
		return fmt.Errorf("task mandate: invalid issue input")
	}
	if !expiresAt.After(s.now()) {
		return ErrExpired
	}
	if tools == nil {
		tools = []string{}
	}
	raw, err := json.Marshal(tools)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(ctx, `
		INSERT INTO cerebro_task_mandate (task_id, workspace_id, agent_id, allowed_tools, expires_at)
		VALUES ($1,$2,$3,$4,$5)
		ON CONFLICT (task_id) DO UPDATE SET
		  workspace_id=EXCLUDED.workspace_id,
		  agent_id=EXCLUDED.agent_id,
		  allowed_tools=EXCLUDED.allowed_tools,
		  issued_at=now(),
		  expires_at=EXCLUDED.expires_at`, taskID, workspaceID, agentID, raw, expiresAt)
	return err
}

// Authorize is intentionally a fresh database read on every tool call. Expiry
// therefore bites at the call itself, even for a task that started earlier.
func (s *Store) Authorize(ctx context.Context, taskID, workspaceID, agentID pgtype.UUID, tool string) error {
	if s == nil || s.db == nil || !taskID.Valid {
		return ErrMissing
	}
	var expiresAt time.Time
	var contains bool
	err := s.db.QueryRow(ctx, `
		SELECT expires_at, allowed_tools ? $4
		FROM cerebro_task_mandate
		WHERE task_id=$1 AND workspace_id=$2 AND agent_id=$3`, taskID, workspaceID, agentID, tool).Scan(&expiresAt, &contains)
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
