package rounds

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

var ErrNotFound = errors.New("round not found")

type Round struct {
	ID          string    `json:"id"`
	WorkspaceID string    `json:"workspace_id"`
	OwnerID     string    `json:"owner_id"`
	Name        string    `json:"name"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type Member struct {
	RoundID     string    `json:"round_id"`
	IssueID     string    `json:"issue_id"`
	AddedByType string    `json:"added_by_type"`
	AddedByID   string    `json:"added_by_id"`
	CreatedAt   time.Time `json:"created_at"`
}

type Cycle struct {
	ID        string      `json:"id"`
	RoundID   string      `json:"round_id"`
	StartedAt time.Time   `json:"started_at"`
	Items     []CycleItem `json:"items"`
}

type CycleItem struct {
	IssueID   string     `json:"issue_id"`
	HandledAt *time.Time `json:"handled_at"`
}

type StatusResult struct {
	Round       Round    `json:"round"`
	Members     []Member `json:"members"`
	ActiveCycle *Cycle   `json:"active_cycle"`
}

type Service struct {
	Pool    *pgxpool.Pool
	Queries *db.Queries
}

func New(pool *pgxpool.Pool, queries *db.Queries) *Service {
	return &Service{Pool: pool, Queries: queries}
}

func (s *Service) Create(ctx context.Context, workspaceID, ownerID pgtype.UUID, name string) (Round, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Round{}, fmt.Errorf("name is required")
	}
	return scanRound(s.Pool.QueryRow(ctx, `INSERT INTO cerebro_round(workspace_id,owner_id,name) VALUES($1,$2,$3) RETURNING id::text,workspace_id::text,owner_id::text,name,created_at,updated_at`, workspaceID, ownerID, name))
}

func (s *Service) List(ctx context.Context, workspaceID, ownerID pgtype.UUID) ([]Round, error) {
	rows, err := s.Pool.Query(ctx, `SELECT id::text,workspace_id::text,owner_id::text,name,created_at,updated_at FROM cerebro_round WHERE workspace_id=$1 AND owner_id=$2 ORDER BY created_at`, workspaceID, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Round{}
	for rows.Next() {
		r, err := scanRound(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Service) Get(ctx context.Context, workspaceID, ownerID, id pgtype.UUID) (Round, error) {
	r, err := scanRound(s.Pool.QueryRow(ctx, `SELECT id::text,workspace_id::text,owner_id::text,name,created_at,updated_at FROM cerebro_round WHERE id=$1 AND workspace_id=$2 AND owner_id=$3`, id, workspaceID, ownerID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Round{}, ErrNotFound
	}
	return r, err
}

func (s *Service) Update(ctx context.Context, workspaceID, ownerID, id pgtype.UUID, name *string) (Round, error) {
	current, err := s.Get(ctx, workspaceID, ownerID, id)
	if err != nil {
		return Round{}, err
	}
	if name != nil {
		current.Name = strings.TrimSpace(*name)
	}
	if current.Name == "" {
		return Round{}, fmt.Errorf("name is required")
	}
	return scanRound(s.Pool.QueryRow(ctx, `UPDATE cerebro_round SET name=$4,updated_at=now() WHERE id=$1 AND workspace_id=$2 AND owner_id=$3 RETURNING id::text,workspace_id::text,owner_id::text,name,created_at,updated_at`, id, workspaceID, ownerID, current.Name))
}

func (s *Service) Delete(ctx context.Context, workspaceID, ownerID, id pgtype.UUID) error {
	ct, err := s.Pool.Exec(ctx, `DELETE FROM cerebro_round WHERE id=$1 AND workspace_id=$2 AND owner_id=$3`, id, workspaceID, ownerID)
	if err == nil && ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

func scanRound(row pgx.Row) (Round, error) {
	var r Round
	err := row.Scan(&r.ID, &r.WorkspaceID, &r.OwnerID, &r.Name, &r.CreatedAt, &r.UpdatedAt)
	return r, err
}

func (s *Service) AddMember(ctx context.Context, workspaceID, ownerID, roundID, issueID pgtype.UUID, actorType string, actorID pgtype.UUID) (Member, error) {
	if actorType != "member" && actorType != "agent" {
		return Member{}, fmt.Errorf("invalid actor type")
	}
	if _, err := s.Get(ctx, workspaceID, ownerID, roundID); err != nil {
		return Member{}, err
	}
	issue, err := s.Queries.GetIssue(ctx, issueID)
	if err != nil || issue.WorkspaceID != workspaceID {
		return Member{}, fmt.Errorf("issue not found")
	}
	if _, err := s.Pool.Exec(ctx, `INSERT INTO cerebro_round_member(round_id,issue_id,added_by_type,added_by_id) VALUES($1,$2,$3,$4) ON CONFLICT(round_id,issue_id) DO NOTHING`, roundID, issueID, actorType, actorID); err != nil {
		return Member{}, err
	}
	return s.member(ctx, roundID, issueID)
}

func scanMember(row pgx.Row) (Member, error) {
	var m Member
	err := row.Scan(&m.RoundID, &m.IssueID, &m.AddedByType, &m.AddedByID, &m.CreatedAt)
	return m, err
}

func (s *Service) member(ctx context.Context, roundID, issueID pgtype.UUID) (Member, error) {
	return scanMember(s.Pool.QueryRow(ctx, `SELECT round_id::text,issue_id::text,added_by_type,added_by_id::text,created_at FROM cerebro_round_member WHERE round_id=$1 AND issue_id=$2`, roundID, issueID))
}

func (s *Service) Members(ctx context.Context, roundID pgtype.UUID) ([]Member, error) {
	rows, err := s.Pool.Query(ctx, `SELECT round_id::text,issue_id::text,added_by_type,added_by_id::text,created_at FROM cerebro_round_member WHERE round_id=$1 ORDER BY created_at`, roundID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Member{}
	for rows.Next() {
		m, err := scanMember(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Service) IssueMembership(ctx context.Context, workspaceID, issueID pgtype.UUID) (*Member, error) {
	var roundID pgtype.UUID
	err := s.Pool.QueryRow(ctx, `SELECT m.round_id FROM cerebro_round_member m JOIN cerebro_round r ON r.id=m.round_id WHERE m.issue_id=$1 AND r.workspace_id=$2`, issueID, workspaceID).Scan(&roundID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	m, err := s.member(ctx, roundID, issueID)
	return &m, err
}

func (s *Service) RemoveMember(ctx context.Context, workspaceID, ownerID, roundID, issueID pgtype.UUID) error {
	if _, err := s.Get(ctx, workspaceID, ownerID, roundID); err != nil {
		return err
	}
	_, err := s.Pool.Exec(ctx, `DELETE FROM cerebro_round_member WHERE round_id=$1 AND issue_id=$2`, roundID, issueID)
	return err
}

func (s *Service) Start(ctx context.Context, workspaceID, ownerID, roundID pgtype.UUID) (Cycle, error) {
	if _, err := s.Get(ctx, workspaceID, ownerID, roundID); err != nil {
		return Cycle{}, err
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return Cycle{}, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `UPDATE cerebro_round_cycle SET ended_at=now() WHERE round_id=$1 AND ended_at IS NULL`, roundID); err != nil {
		return Cycle{}, err
	}
	var cycleID pgtype.UUID
	if err := tx.QueryRow(ctx, `INSERT INTO cerebro_round_cycle(round_id) VALUES($1) RETURNING id`, roundID).Scan(&cycleID); err != nil {
		return Cycle{}, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO cerebro_round_cycle_item(cycle_id,issue_id)
		SELECT $1,m.issue_id
		FROM cerebro_round_member m
		WHERE m.round_id=$2
		AND EXISTS (
			SELECT 1 FROM inbox_item i
			WHERE i.workspace_id=$3 AND i.recipient_type='member' AND i.recipient_id=$4
			AND i.issue_id=m.issue_id AND i.route='inbox' AND i.archived=false AND i.read=false
		)
		AND NOT EXISTS (
			SELECT 1 FROM agent_task_queue q
			WHERE q.issue_id=m.issue_id AND q.status IN ('queued','dispatched','running')
		)
		GROUP BY m.issue_id`, cycleID, roundID, workspaceID, ownerID)
	if err != nil {
		return Cycle{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Cycle{}, err
	}
	cycle, err := s.cycle(ctx, cycleID)
	return *cycle, err
}

func (s *Service) cycle(ctx context.Context, cycleID pgtype.UUID) (*Cycle, error) {
	var c Cycle
	if err := s.Pool.QueryRow(ctx, `SELECT id::text,round_id::text,started_at FROM cerebro_round_cycle WHERE id=$1`, cycleID).Scan(&c.ID, &c.RoundID, &c.StartedAt); err != nil {
		return nil, err
	}
	rows, err := s.Pool.Query(ctx, `SELECT issue_id::text,handled_at FROM cerebro_round_cycle_item WHERE cycle_id=$1 ORDER BY issue_id`, cycleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	c.Items = []CycleItem{}
	for rows.Next() {
		var item CycleItem
		if err := rows.Scan(&item.IssueID, &item.HandledAt); err != nil {
			return nil, err
		}
		c.Items = append(c.Items, item)
	}
	return &c, rows.Err()
}

func (s *Service) ActiveCycle(ctx context.Context, roundID pgtype.UUID) (*Cycle, error) {
	var id pgtype.UUID
	err := s.Pool.QueryRow(ctx, `SELECT id FROM cerebro_round_cycle WHERE round_id=$1 AND ended_at IS NULL`, roundID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return s.cycle(ctx, id)
}

func (s *Service) Status(ctx context.Context, workspaceID, ownerID, roundID pgtype.UUID) (StatusResult, error) {
	round, err := s.Get(ctx, workspaceID, ownerID, roundID)
	if err != nil {
		return StatusResult{}, err
	}
	members, err := s.Members(ctx, roundID)
	if err != nil {
		return StatusResult{}, err
	}
	cycle, err := s.ActiveCycle(ctx, roundID)
	return StatusResult{Round: round, Members: members, ActiveCycle: cycle}, err
}

func (s *Service) MarkHandled(ctx context.Context, workspaceID, issueID, ownerID pgtype.UUID) error {
	_, err := s.Pool.Exec(ctx, `UPDATE cerebro_round_cycle_item i SET handled_at=COALESCE(i.handled_at,now())
		FROM cerebro_round_cycle c JOIN cerebro_round r ON r.id=c.round_id
		WHERE i.cycle_id=c.id AND c.ended_at IS NULL AND i.issue_id=$1
		AND r.workspace_id=$2 AND r.owner_id=$3`, issueID, workspaceID, ownerID)
	return err
}

func (s *Service) ObserveMemberReply(ctx context.Context, workspaceID, issueID, ownerID pgtype.UUID) error {
	return s.MarkHandled(ctx, workspaceID, issueID, ownerID)
}
