package sessionmode

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrVersionNotFound = errors.New("session mode version not found")

type Version struct {
	ID          string `json:"id"`
	Version     string `json:"version"`
	Config      Config `json:"config"`
	Description string `json:"description"`
	CreatedBy   string `json:"created_by,omitempty"`
	CreatedAt   string `json:"created_at"`
}

type Record struct {
	Mode          Mode      `json:"mode"`
	ActiveVersion string    `json:"active_version"`
	Active        Config    `json:"active"`
	Draft         *Config   `json:"draft,omitempty"`
	Versions      []Version `json:"versions"`
	UpdatedAt     string    `json:"updated_at"`
}

type Store struct{ pool *pgxpool.Pool }

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

func nextVersion(current int) int {
	if current < 1 {
		return 1
	}
	return current + 1
}

func configForVersion(config Config, version int) Config {
	config.Version = strconv.Itoa(version)
	config.AllowedTools = append([]string(nil), config.AllowedTools...)
	config.DataSources = append([]string(nil), config.DataSources...)
	config.EvalIDs = append([]string(nil), config.EvalIDs...)
	return config
}

func (s *Store) EnsureWorkspace(ctx context.Context, workspaceID pgtype.UUID) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	for _, mode := range []Mode{Plan, Build, Research, Review} {
		config := DefaultConfigs()[mode]
		config.Version = ""
		raw, err := json.Marshal(config)
		if err != nil {
			return err
		}
		var registryID pgtype.UUID
		var activeVersion int
		if err := tx.QueryRow(ctx, `
			INSERT INTO cerebro_session_mode_registry (workspace_id, mode)
			VALUES ($1, $2)
			ON CONFLICT (workspace_id, mode) DO UPDATE SET mode = EXCLUDED.mode
			RETURNING id, active_version`, workspaceID, mode).Scan(&registryID, &activeVersion); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO cerebro_session_mode_version (registry_id, version, config, description)
			VALUES ($1, $2, $3, 'Initial profile')
			ON CONFLICT (registry_id, version) DO NOTHING`, registryID, activeVersion, raw); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (s *Store) List(ctx context.Context, workspaceID pgtype.UUID) ([]Record, error) {
	if err := s.EnsureWorkspace(ctx, workspaceID); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `
		SELECT r.id, r.mode, r.active_version, active.config, r.draft, r.updated_at
		FROM cerebro_session_mode_registry r
		JOIN cerebro_session_mode_version active
		  ON active.registry_id = r.id AND active.version = r.active_version
		WHERE r.workspace_id = $1
		ORDER BY CASE r.mode WHEN 'plan' THEN 1 WHEN 'build' THEN 2 WHEN 'research' THEN 3 ELSE 4 END`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	records := make([]Record, 0, 4)
	for rows.Next() {
		var registryID pgtype.UUID
		var mode string
		var activeVersion int
		var activeRaw, draftRaw []byte
		var updatedAt pgtype.Timestamptz
		if err := rows.Scan(&registryID, &mode, &activeVersion, &activeRaw, &draftRaw, &updatedAt); err != nil {
			return nil, err
		}
		record, err := decodeRecord(ctx, s.pool, registryID, Mode(mode), activeVersion, activeRaw, draftRaw, updatedAt)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

type rowQuerier interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func decodeRecord(ctx context.Context, q rowQuerier, registryID pgtype.UUID, mode Mode, activeVersion int, activeRaw, draftRaw []byte, updatedAt pgtype.Timestamptz) (Record, error) {
	var active Config
	if err := json.Unmarshal(activeRaw, &active); err != nil {
		return Record{}, err
	}
	active.Mode = mode
	active = configForVersion(active, activeVersion)
	var draft *Config
	if len(draftRaw) > 0 {
		var decoded Config
		if err := json.Unmarshal(draftRaw, &decoded); err != nil {
			return Record{}, err
		}
		decoded.Mode = mode
		draft = &decoded
	}
	versions, err := listVersions(ctx, q, registryID, mode)
	if err != nil {
		return Record{}, err
	}
	return Record{Mode: mode, ActiveVersion: strconv.Itoa(activeVersion), Active: active, Draft: draft, Versions: versions, UpdatedAt: updatedAt.Time.UTC().Format("2006-01-02T15:04:05Z")}, nil
}

func listVersions(ctx context.Context, q rowQuerier, registryID pgtype.UUID, mode Mode) ([]Version, error) {
	rows, err := q.Query(ctx, `
		SELECT id::text, version, config, description, COALESCE(created_by::text, ''), created_at
		FROM cerebro_session_mode_version WHERE registry_id = $1 ORDER BY version DESC`, registryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	versions := []Version{}
	for rows.Next() {
		var version Version
		var number int
		var raw []byte
		var createdAt pgtype.Timestamptz
		if err := rows.Scan(&version.ID, &number, &raw, &version.Description, &version.CreatedBy, &createdAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, &version.Config); err != nil {
			return nil, err
		}
		version.Config.Mode = mode
		version.Config = configForVersion(version.Config, number)
		version.Version = strconv.Itoa(number)
		version.CreatedAt = createdAt.Time.UTC().Format("2006-01-02T15:04:05Z")
		versions = append(versions, version)
	}
	return versions, rows.Err()
}

func (s *Store) UpdateDraft(ctx context.Context, workspaceID, actorID pgtype.UUID, mode Mode, config Config) (Record, error) {
	config.Mode = mode
	config.Version = ""
	if err := ValidateConfig(config); err != nil {
		return Record{}, err
	}
	if err := s.EnsureWorkspace(ctx, workspaceID); err != nil {
		return Record{}, err
	}
	raw, err := json.Marshal(config)
	if err != nil {
		return Record{}, err
	}
	if _, err := s.pool.Exec(ctx, `
		UPDATE cerebro_session_mode_registry SET draft = $3, updated_by = $4, updated_at = now()
		WHERE workspace_id = $1 AND mode = $2`, workspaceID, mode, raw, actorID); err != nil {
		return Record{}, err
	}
	return s.Get(ctx, workspaceID, mode)
}

func (s *Store) Get(ctx context.Context, workspaceID pgtype.UUID, mode Mode) (Record, error) {
	records, err := s.List(ctx, workspaceID)
	if err != nil {
		return Record{}, err
	}
	for _, record := range records {
		if record.Mode == mode {
			return record, nil
		}
	}
	return Record{}, pgx.ErrNoRows
}

func (s *Store) Publish(ctx context.Context, workspaceID, actorID pgtype.UUID, mode Mode, description string) (Record, error) {
	if err := s.EnsureWorkspace(ctx, workspaceID); err != nil {
		return Record{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Record{}, err
	}
	defer tx.Rollback(ctx)
	var registryID pgtype.UUID
	var current int
	var draft []byte
	if err := tx.QueryRow(ctx, `SELECT id, active_version, draft FROM cerebro_session_mode_registry WHERE workspace_id=$1 AND mode=$2 FOR UPDATE`, workspaceID, mode).Scan(&registryID, &current, &draft); err != nil {
		return Record{}, err
	}
	if len(draft) == 0 {
		return Record{}, fmt.Errorf("mode has no draft")
	}
	var config Config
	if err := json.Unmarshal(draft, &config); err != nil {
		return Record{}, err
	}
	config.Mode = mode
	if err := ValidateConfig(config); err != nil {
		return Record{}, err
	}
	next := nextVersion(current)
	if _, err := tx.Exec(ctx, `INSERT INTO cerebro_session_mode_version (registry_id, version, config, description, created_by) VALUES ($1,$2,$3,$4,$5)`, registryID, next, draft, description, actorID); err != nil {
		return Record{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE cerebro_session_mode_registry SET active_version=$2,draft=NULL,updated_by=$3,updated_at=now() WHERE id=$1`, registryID, next, actorID); err != nil {
		return Record{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Record{}, err
	}
	return s.Get(ctx, workspaceID, mode)
}

func (s *Store) Restore(ctx context.Context, workspaceID, actorID pgtype.UUID, mode Mode, version int) (Record, error) {
	if err := s.EnsureWorkspace(ctx, workspaceID); err != nil {
		return Record{}, err
	}
	var raw []byte
	if err := s.pool.QueryRow(ctx, `
		SELECT v.config FROM cerebro_session_mode_version v
		JOIN cerebro_session_mode_registry r ON r.id=v.registry_id
		WHERE r.workspace_id=$1 AND r.mode=$2 AND v.version=$3`, workspaceID, mode, version).Scan(&raw); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Record{}, ErrVersionNotFound
		}
		return Record{}, err
	}
	if _, err := s.pool.Exec(ctx, `UPDATE cerebro_session_mode_registry SET draft=$3,updated_by=$4,updated_at=now() WHERE workspace_id=$1 AND mode=$2`, workspaceID, mode, raw, actorID); err != nil {
		return Record{}, err
	}
	return s.Publish(ctx, workspaceID, actorID, mode, fmt.Sprintf("Restored from version %d", version))
}

func (s *Store) Active(ctx context.Context, workspaceID pgtype.UUID, mode Mode) (Config, error) {
	if err := s.EnsureWorkspace(ctx, workspaceID); err != nil {
		return Config{}, err
	}
	var version int
	var raw []byte
	if err := s.pool.QueryRow(ctx, `
		SELECT r.active_version, v.config FROM cerebro_session_mode_registry r
		JOIN cerebro_session_mode_version v ON v.registry_id=r.id AND v.version=r.active_version
		WHERE r.workspace_id=$1 AND r.mode=$2`, workspaceID, mode).Scan(&version, &raw); err != nil {
		return Config{}, err
	}
	var config Config
	if err := json.Unmarshal(raw, &config); err != nil {
		return Config{}, err
	}
	config.Mode = mode
	return configForVersion(config, version), nil
}
