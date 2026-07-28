package commands

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotFound = errors.New("command not found")

type Store struct{ pool *pgxpool.Pool }

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

const commandColumns = `id, workspace_id, command_key, title, description, argv,
 created_by_id, created_by_type, created_at, updated_at`

func scanCommand(row pgx.Row) (Command, error) {
	var value Command
	err := row.Scan(&value.ID, &value.WorkspaceID, &value.Key, &value.Title, &value.Description,
		&value.Argv, &value.CreatedByID, &value.CreatedByType, &value.CreatedAt, &value.UpdatedAt)
	return value, err
}

func (s *Store) List(ctx context.Context, workspaceID uuid.UUID) ([]Command, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+commandColumns+` FROM cerebro_command WHERE workspace_id=$1 ORDER BY title ASC`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]Command, 0)
	for rows.Next() {
		item, err := scanCommand(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) Get(ctx context.Context, workspaceID, id uuid.UUID) (Command, error) {
	value, err := scanCommand(s.pool.QueryRow(ctx, `SELECT `+commandColumns+` FROM cerebro_command WHERE workspace_id=$1 AND id=$2`, workspaceID, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Command{}, ErrNotFound
	}
	return value, err
}

func (s *Store) Create(ctx context.Context, workspaceID, actorID uuid.UUID, actorType string, input CommandInput) (Command, error) {
	return scanCommand(s.pool.QueryRow(ctx, `INSERT INTO cerebro_command
 (workspace_id, command_key, title, description, argv, created_by_id, created_by_type)
 VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING `+commandColumns,
		workspaceID, input.Key, input.Title, input.Description, input.Argv, actorID, actorType))
}

func (s *Store) Update(ctx context.Context, workspaceID, id uuid.UUID, input CommandInput) (Command, error) {
	value, err := scanCommand(s.pool.QueryRow(ctx, `UPDATE cerebro_command SET command_key=$3, title=$4, description=$5, argv=$6, updated_at=now()
 WHERE workspace_id=$1 AND id=$2 RETURNING `+commandColumns,
		workspaceID, id, input.Key, input.Title, input.Description, input.Argv))
	if errors.Is(err, pgx.ErrNoRows) {
		return Command{}, ErrNotFound
	}
	return value, err
}

func (s *Store) Delete(ctx context.Context, workspaceID, id uuid.UUID) error {
	result, err := s.pool.Exec(ctx, `DELETE FROM cerebro_command WHERE workspace_id=$1 AND id=$2`, workspaceID, id)
	if err == nil && result.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}
