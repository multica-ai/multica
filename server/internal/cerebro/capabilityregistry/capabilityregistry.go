// Package capabilityregistry implements the Fase 1 capability register
// (FIR-2129): a normalized record of every capability known in a workspace
// plus the subjects (members, agents, groups, runtimes, workspace) that
// report, own, or use them.
//
// Runtimes report their capability snapshots through the daemon
// refresh/heartbeat path; operators and other services query the register
// through GET /api/capabilities. The source-of-truth discovery payload stays
// in Multica core — this schema has no Persona dependency.
//
// The handler layer talks to this service through an interface + adapter
// (see cmd/server/cerebro_capability_register.go), matching the
// runtime-tools / persona-mask seam pattern so the handler package never
// imports cerebro-side types directly.
package capabilityregistry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/util"
)

// Relations a subject can have to a capability.
const (
	RelationReporter = "reporter"
	RelationOwner    = "owner"
	RelationUser     = "user"
)

// Errors returned to the adapter so it can map them to HTTP status codes.
var (
	ErrInvalidSubject = errors.New("subject must be one of member, agent, group, runtime, workspace with a UUID id")
	ErrInvalidReport  = errors.New("capability key, title, and category are required")
)

var knownSubjectTypes = map[string]bool{
	"member": true, "agent": true, "group": true, "runtime": true, "workspace": true,
}

// Subject is a member/agent/group/runtime/workspace tied to a capability.
type Subject struct {
	Type        string
	ID          string
	DisplayName string
	Metadata    map[string]any
}

// ReportInput is one capability declared in a report call.
type ReportInput struct {
	Key         string
	Title       string
	Category    string
	Description string
	Source      string
	Metadata    map[string]any
	Owners      []Subject
	Users       []Subject
}

// View is a fully hydrated capability row with its subjects.
type View struct {
	ID             string
	WorkspaceID    string
	Key            string
	Title          string
	Category       string
	Description    string
	Source         string
	Metadata       map[string]any
	Owners         []Subject
	Users          []Subject
	Reporters      []Subject
	FirstSeenAt    time.Time
	LastReportedAt time.Time
	UpdatedAt      time.Time
}

// Service is the capability register backed by a pgx pool.
type Service struct {
	pool *pgxpool.Pool
}

// New constructs a Service backed by the given pool.
func New(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool}
}

// Report upserts the reported capabilities for a workspace and records the
// reporter, owners, and users as subjects. It returns the hydrated views for
// the reported capability keys.
func (s *Service) Report(ctx context.Context, workspaceID pgtype.UUID, reporter Subject, caps []ReportInput) ([]View, error) {
	if err := validateSubject(reporter); err != nil {
		return nil, err
	}
	if len(caps) == 0 {
		return nil, ErrInvalidReport
	}
	for _, c := range caps {
		if blank(c.Key) || blank(c.Title) || blank(c.Category) {
			return nil, ErrInvalidReport
		}
		if err := validateSubjects(c.Owners); err != nil {
			return nil, err
		}
		if err := validateSubjects(c.Users); err != nil {
			return nil, err
		}
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	keys := make([]string, 0, len(caps))
	for _, c := range caps {
		source := c.Source
		if source == "" {
			source = "report"
		}
		var capID pgtype.UUID
		err := tx.QueryRow(ctx, `
			INSERT INTO cerebro_capability
				(workspace_id, capability_key, title, category, description, source, metadata, last_reported_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, now(), now())
			ON CONFLICT (workspace_id, capability_key) DO UPDATE SET
				title = EXCLUDED.title,
				category = EXCLUDED.category,
				description = EXCLUDED.description,
				source = EXCLUDED.source,
				metadata = EXCLUDED.metadata,
				last_reported_at = now(),
				updated_at = now()
			RETURNING id
		`, workspaceID, c.Key, c.Title, c.Category, c.Description, source, marshalMetadata(c.Metadata)).Scan(&capID)
		if err != nil {
			return nil, fmt.Errorf("upsert capability %q: %w", c.Key, err)
		}

		if err := upsertSubject(ctx, tx, capID, workspaceID, reporter, RelationReporter); err != nil {
			return nil, err
		}
		for _, o := range c.Owners {
			if err := upsertSubject(ctx, tx, capID, workspaceID, o, RelationOwner); err != nil {
				return nil, err
			}
		}
		for _, u := range c.Users {
			if err := upsertSubject(ctx, tx, capID, workspaceID, u, RelationUser); err != nil {
				return nil, err
			}
		}
		keys = append(keys, c.Key)
	}

	views, err := loadViews(ctx, tx, workspaceID, nil, keys)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return views, nil
}

// List returns capabilities for a workspace, optionally filtered to those a
// given subject is tied to and/or a set of capability keys.
func (s *Service) List(ctx context.Context, workspaceID pgtype.UUID, subject *Subject, keys []string) ([]View, error) {
	if subject != nil {
		if err := validateSubject(*subject); err != nil {
			return nil, err
		}
	}
	return loadViews(ctx, s.pool, workspaceID, subject, keys)
}

// querier is satisfied by both *pgxpool.Pool and pgx.Tx; only Query is needed
// for the read paths (loadViews / attachSubjects).
type querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

func upsertSubject(ctx context.Context, tx pgx.Tx, capID, workspaceID pgtype.UUID, sub Subject, relation string) error {
	subjectUUID, err := util.ParseUUID(sub.ID)
	if err != nil {
		return ErrInvalidSubject
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO cerebro_capability_subject
			(capability_id, workspace_id, subject_type, subject_id, relation, metadata, first_seen_at, last_seen_at)
		VALUES ($1, $2, $3, $4, $5, $6, now(), now())
		ON CONFLICT (capability_id, subject_type, subject_id, relation) DO UPDATE SET
			metadata = EXCLUDED.metadata,
			last_seen_at = now()
	`, capID, workspaceID, sub.Type, subjectUUID, relation, marshalMetadata(sub.Metadata))
	if err != nil {
		return fmt.Errorf("upsert subject %s:%s: %w", sub.Type, sub.ID, err)
	}
	return nil
}

func loadViews(ctx context.Context, q querier, workspaceID pgtype.UUID, subject *Subject, keys []string) ([]View, error) {
	args := []any{workspaceID}
	where := "c.workspace_id = $1"
	if subject != nil {
		subjectUUID, err := util.ParseUUID(subject.ID)
		if err != nil {
			return nil, ErrInvalidSubject
		}
		args = append(args, subject.Type, subjectUUID)
		where += fmt.Sprintf(`
			AND EXISTS (
				SELECT 1 FROM cerebro_capability_subject f
				WHERE f.capability_id = c.id
				  AND f.subject_type = $%d AND f.subject_id = $%d
			)`, len(args)-1, len(args))
	}
	if len(keys) > 0 {
		args = append(args, keys)
		where += fmt.Sprintf(" AND c.capability_key = ANY($%d)", len(args))
	}

	rows, err := q.Query(ctx, `
		SELECT c.id, c.workspace_id, c.capability_key, c.title, c.category,
		       c.description, c.source, c.metadata,
		       c.first_seen_at, c.last_reported_at, c.updated_at
		FROM cerebro_capability c
		WHERE `+where+`
		ORDER BY c.category, lower(c.title)
	`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	views := make([]View, 0)
	index := make(map[string]int)
	for rows.Next() {
		var v View
		var rawMeta []byte
		if err := rows.Scan(&v.ID, &v.WorkspaceID, &v.Key, &v.Title, &v.Category,
			&v.Description, &v.Source, &rawMeta,
			&v.FirstSeenAt, &v.LastReportedAt, &v.UpdatedAt); err != nil {
			return nil, err
		}
		v.Metadata = unmarshalMetadata(rawMeta)
		index[v.ID] = len(views)
		views = append(views, v)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(views) == 0 {
		return views, nil
	}

	if err := attachSubjects(ctx, q, views, index); err != nil {
		return nil, err
	}
	return views, nil
}

func attachSubjects(ctx context.Context, q querier, views []View, index map[string]int) error {
	ids := make([]string, 0, len(views))
	for _, v := range views {
		ids = append(ids, v.ID)
	}
	rows, err := q.Query(ctx, `
		SELECT capability_id, subject_type, subject_id, relation, metadata
		FROM cerebro_capability_subject
		WHERE capability_id = ANY($1)
		ORDER BY relation, subject_type, subject_id
	`, ids)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var capID, subjectID pgtype.UUID
		var subjectType, relation string
		var rawMeta []byte
		if err := rows.Scan(&capID, &subjectType, &subjectID, &relation, &rawMeta); err != nil {
			return err
		}
		i, ok := index[util.UUIDToString(capID)]
		if !ok {
			continue
		}
		sub := Subject{
			Type:     subjectType,
			ID:       util.UUIDToString(subjectID),
			Metadata: unmarshalMetadata(rawMeta),
		}
		switch relation {
		case RelationOwner:
			views[i].Owners = append(views[i].Owners, sub)
		case RelationUser:
			views[i].Users = append(views[i].Users, sub)
		case RelationReporter:
			views[i].Reporters = append(views[i].Reporters, sub)
		}
	}
	return rows.Err()
}

func validateSubjects(subs []Subject) error {
	for _, s := range subs {
		if err := validateSubject(s); err != nil {
			return err
		}
	}
	return nil
}

func validateSubject(s Subject) error {
	if !knownSubjectTypes[s.Type] {
		return ErrInvalidSubject
	}
	if _, err := util.ParseUUID(s.ID); err != nil {
		return ErrInvalidSubject
	}
	return nil
}

func blank(s string) bool {
	for _, r := range s {
		if r != ' ' && r != '\t' && r != '\n' && r != '\r' {
			return false
		}
	}
	return true
}

func marshalMetadata(m map[string]any) []byte {
	if len(m) == 0 {
		return []byte("{}")
	}
	b, err := json.Marshal(m)
	if err != nil {
		return []byte("{}")
	}
	return b
}

func unmarshalMetadata(raw []byte) map[string]any {
	if len(raw) == 0 {
		return map[string]any{}
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return map[string]any{}
	}
	if m == nil {
		return map[string]any{}
	}
	return m
}
