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
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/robfig/cron/v3"
)

const (
	RunRunning   = "running"
	RunReady     = "ready"
	RunCompleted = "completed"
	RunFailed    = "failed"
)

var ErrNotFound = errors.New("round not found")

const stalledTaskTimeout = 30 * time.Minute

func taskProgress(status string, startedAt *time.Time, now time.Time, timeout time.Duration) string {
	if status == "running" && startedAt != nil && now.Sub(*startedAt) >= timeout {
		return "stalled"
	}
	return status
}

type Round struct {
	ID           string     `json:"id"`
	WorkspaceID  string     `json:"workspace_id"`
	OwnerID      string     `json:"owner_id"`
	Name         string     `json:"name"`
	Mode         string     `json:"mode"`
	ScheduleCron string     `json:"schedule_cron"`
	Timezone     string     `json:"timezone"`
	NextRunAt    *time.Time `json:"next_run_at"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

func normalizeMode(mode string) (string, error) {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "" {
		return "batch", nil
	}
	if mode != "live" && mode != "batch" {
		return "", fmt.Errorf("mode must be live or batch")
	}
	return mode, nil
}

func shouldHoldComment(mode string) bool { return mode == "batch" }

type Member struct {
	RoundID          string    `json:"round_id"`
	IssueID          string    `json:"issue_id"`
	AddedByType      string    `json:"added_by_type"`
	AddedByID        string    `json:"added_by_id"`
	HeldTriggerCount int       `json:"held_trigger_count"`
	CreatedAt        time.Time `json:"created_at"`
}

type Run struct {
	ID             string     `json:"id"`
	RoundID        string     `json:"round_id"`
	Status         string     `json:"status"`
	TotalCount     int        `json:"total_count"`
	RespondedCount int        `json:"responded_count"`
	StalledCount   int        `json:"stalled_count"`
	NudgedCount    int        `json:"nudged_count"`
	StartedAt      time.Time  `json:"started_at"`
	ReadyAt        *time.Time `json:"ready_at"`
	CompletedAt    *time.Time `json:"completed_at"`
	CreatedAt      time.Time  `json:"created_at"`
}

type Service struct {
	Pool    *pgxpool.Pool
	Queries *db.Queries
	Tasks   *service.TaskService
}

func New(pool *pgxpool.Pool, queries *db.Queries, tasks *service.TaskService) *Service {
	return &Service{Pool: pool, Queries: queries, Tasks: tasks}
}

func nextRunAt(spec, timezone string, now time.Time) (time.Time, error) {
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid timezone: %w", err)
	}
	schedule, err := cron.ParseStandard(spec)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid schedule_cron: %w", err)
	}
	return schedule.Next(now.In(loc)).UTC(), nil
}

func runStatus(total, responded, failed int) string {
	if responded+failed >= total {
		return RunReady
	}
	return RunRunning
}

func (s *Service) Create(ctx context.Context, workspaceID, ownerID pgtype.UUID, name, mode, schedule, timezone string) (Round, error) {
	name, schedule, timezone = strings.TrimSpace(name), strings.TrimSpace(schedule), strings.TrimSpace(timezone)
	if name == "" {
		return Round{}, fmt.Errorf("name is required")
	}
	if timezone == "" {
		timezone = "UTC"
	}
	mode, err := normalizeMode(mode)
	if err != nil {
		return Round{}, err
	}
	var next *time.Time
	if schedule != "" {
		n, err := nextRunAt(schedule, timezone, time.Now())
		if err != nil {
			return Round{}, err
		}
		next = &n
	}
	row := s.Pool.QueryRow(ctx, `INSERT INTO cerebro_round(workspace_id,owner_id,name,mode,schedule_cron,timezone,next_run_at) VALUES($1,$2,$3,$4,NULLIF($5,''),$6,$7) RETURNING id::text,workspace_id::text,owner_id::text,name,mode,COALESCE(schedule_cron,''),timezone,next_run_at,created_at,updated_at`, workspaceID, ownerID, name, mode, schedule, timezone, next)
	return scanRound(row)
}

func (s *Service) List(ctx context.Context, workspaceID, ownerID pgtype.UUID) ([]Round, error) {
	rows, err := s.Pool.Query(ctx, `SELECT id::text,workspace_id::text,owner_id::text,name,mode,COALESCE(schedule_cron,''),timezone,next_run_at,created_at,updated_at FROM cerebro_round WHERE workspace_id=$1 AND owner_id=$2 ORDER BY created_at`, workspaceID, ownerID)
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
	r, err := scanRound(s.Pool.QueryRow(ctx, `SELECT id::text,workspace_id::text,owner_id::text,name,mode,COALESCE(schedule_cron,''),timezone,next_run_at,created_at,updated_at FROM cerebro_round WHERE id=$1 AND workspace_id=$2 AND owner_id=$3`, id, workspaceID, ownerID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Round{}, ErrNotFound
	}
	return r, err
}

func (s *Service) Update(ctx context.Context, workspaceID, ownerID, id pgtype.UUID, name, mode, schedule, timezone *string) (Round, error) {
	current, err := s.Get(ctx, workspaceID, ownerID, id)
	if err != nil {
		return Round{}, err
	}
	if name != nil {
		current.Name = strings.TrimSpace(*name)
	}
	if mode != nil {
		current.Mode, err = normalizeMode(*mode)
		if err != nil {
			return Round{}, err
		}
	}
	if schedule != nil {
		current.ScheduleCron = strings.TrimSpace(*schedule)
	}
	if timezone != nil {
		current.Timezone = strings.TrimSpace(*timezone)
	}
	if current.Name == "" {
		return Round{}, fmt.Errorf("name is required")
	}
	if current.Timezone == "" {
		current.Timezone = "UTC"
	}
	var next *time.Time
	if current.ScheduleCron != "" {
		n, e := nextRunAt(current.ScheduleCron, current.Timezone, time.Now())
		if e != nil {
			return Round{}, e
		}
		next = &n
	}
	return scanRound(s.Pool.QueryRow(ctx, `UPDATE cerebro_round SET name=$4,mode=$5,schedule_cron=NULLIF($6,''),timezone=$7,next_run_at=$8,updated_at=now() WHERE id=$1 AND workspace_id=$2 AND owner_id=$3 RETURNING id::text,workspace_id::text,owner_id::text,name,mode,COALESCE(schedule_cron,''),timezone,next_run_at,created_at,updated_at`, id, workspaceID, ownerID, current.Name, current.Mode, current.ScheduleCron, current.Timezone, next))
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
	err := row.Scan(&r.ID, &r.WorkspaceID, &r.OwnerID, &r.Name, &r.Mode, &r.ScheduleCron, &r.Timezone, &r.NextRunAt, &r.CreatedAt, &r.UpdatedAt)
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
	_, err = s.Pool.Exec(ctx, `INSERT INTO cerebro_round_member(round_id,issue_id,added_by_type,added_by_id) VALUES($1,$2,$3,$4) ON CONFLICT(round_id,issue_id) DO NOTHING`, roundID, issueID, actorType, actorID)
	if err != nil {
		return Member{}, err
	}
	return s.member(ctx, roundID, issueID)
}

func (s *Service) member(ctx context.Context, roundID, issueID pgtype.UUID) (Member, error) {
	var m Member
	err := s.Pool.QueryRow(ctx, `SELECT m.round_id::text,m.issue_id::text,m.added_by_type,m.added_by_id::text,count(h.id)::int,m.created_at FROM cerebro_round_member m LEFT JOIN cerebro_round_held_trigger h ON h.round_id=m.round_id AND h.issue_id=m.issue_id AND h.state='held' WHERE m.round_id=$1 AND m.issue_id=$2 GROUP BY m.round_id,m.issue_id,m.added_by_type,m.added_by_id,m.created_at`, roundID, issueID).Scan(&m.RoundID, &m.IssueID, &m.AddedByType, &m.AddedByID, &m.HeldTriggerCount, &m.CreatedAt)
	return m, err
}
func (s *Service) Members(ctx context.Context, roundID pgtype.UUID) ([]Member, error) {
	rows, err := s.Pool.Query(ctx, `SELECT m.round_id::text,m.issue_id::text,m.added_by_type,m.added_by_id::text,count(h.id)::int,m.created_at FROM cerebro_round_member m LEFT JOIN cerebro_round_held_trigger h ON h.round_id=m.round_id AND h.issue_id=m.issue_id AND h.state='held' WHERE m.round_id=$1 GROUP BY m.round_id,m.issue_id,m.added_by_type,m.added_by_id,m.created_at ORDER BY m.created_at`, roundID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Member{}
	for rows.Next() {
		var m Member
		if err := rows.Scan(&m.RoundID, &m.IssueID, &m.AddedByType, &m.AddedByID, &m.HeldTriggerCount, &m.CreatedAt); err != nil {
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
func (s *Service) ActiveRun(ctx context.Context, roundID pgtype.UUID) (*Run, error) {
	var id pgtype.UUID
	err := s.Pool.QueryRow(ctx, `SELECT id FROM cerebro_round_run WHERE round_id=$1 AND status IN ('running','ready') ORDER BY created_at DESC LIMIT 1`, roundID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	s.RefreshRun(ctx, id)
	r, err := s.GetRun(ctx, id)
	return &r, err
}

func (s *Service) RemoveMember(ctx context.Context, workspaceID, ownerID, roundID, issueID pgtype.UUID) error {
	if _, err := s.Get(ctx, workspaceID, ownerID, roundID); err != nil {
		return err
	}
	_, err := s.Pool.Exec(ctx, `DELETE FROM cerebro_round_member WHERE round_id=$1 AND issue_id=$2`, roundID, issueID)
	return err
}

type heldTarget struct {
	kind string
	id   pgtype.UUID
}

func heldTargets(content string) []heldTarget {
	var out []heldTarget
	for _, mention := range util.ParseMentions(content) {
		if mention.Type != "agent" && mention.Type != "squad" {
			continue
		}
		id, err := util.ParseUUID(mention.ID)
		if err == nil {
			out = append(out, heldTarget{kind: mention.Type, id: id})
		}
	}
	return out
}

func (s *Service) HoldComment(ctx context.Context, workspaceID, issueID, commentID pgtype.UUID, actorType, actorID, content string) (bool, error) {
	if actorType != "member" {
		return false, nil
	}
	var mode string
	err := s.Pool.QueryRow(ctx, `SELECT r.mode FROM cerebro_round_member m JOIN cerebro_round r ON r.id=m.round_id WHERE m.issue_id=$1 AND r.workspace_id=$2`, issueID, workspaceID).Scan(&mode)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !shouldHoldComment(mode)) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	targets := heldTargets(content)
	if len(targets) == 0 {
		targets = []heldTarget{{kind: "assignee", id: pgtype.UUID{}}}
	}
	held := false
	for _, target := range targets {
		ct, err := s.Pool.Exec(ctx, `INSERT INTO cerebro_round_held_trigger(round_id,issue_id,comment_id,target_type,target_id) SELECT m.round_id,m.issue_id,$3,$4,$5 FROM cerebro_round_member m JOIN cerebro_round r ON r.id=m.round_id WHERE m.issue_id=$1 AND r.workspace_id=$2 ON CONFLICT DO NOTHING`, issueID, workspaceID, commentID, target.kind, target.id)
		if err != nil {
			return held, err
		}
		held = held || ct.RowsAffected() > 0
	}
	return held, nil
}

func (s *Service) Start(ctx context.Context, workspaceID, ownerID, roundID pgtype.UUID) (Run, error) {
	if _, err := s.Get(ctx, workspaceID, ownerID, roundID); err != nil {
		return Run{}, err
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return Run{}, err
	}
	defer tx.Rollback(ctx)
	var run Run
	err = tx.QueryRow(ctx, `INSERT INTO cerebro_round_run(round_id,total_count) SELECT $1,count(*) FROM cerebro_round_held_trigger WHERE round_id=$1 AND state='held' RETURNING id::text,round_id::text,status,total_count,responded_count,stalled_count,nudged_count,started_at,ready_at,completed_at,created_at`, roundID).Scan(&run.ID, &run.RoundID, &run.Status, &run.TotalCount, &run.RespondedCount, &run.StalledCount, &run.NudgedCount, &run.StartedAt, &run.ReadyAt, &run.CompletedAt, &run.CreatedAt)
	if err != nil {
		return Run{}, err
	}
	rows, err := tx.Query(ctx, `SELECT DISTINCT issue_id,comment_id,target_type,target_id FROM cerebro_round_held_trigger WHERE round_id=$1 AND state='held' ORDER BY issue_id`, roundID)
	if err != nil {
		return Run{}, err
	}
	type held struct {
		i, c       pgtype.UUID
		targetType string
		targetID   pgtype.UUID
	}
	var hs []held
	for rows.Next() {
		var h held
		if err := rows.Scan(&h.i, &h.c, &h.targetType, &h.targetID); err != nil {
			rows.Close()
			return Run{}, err
		}
		hs = append(hs, h)
	}
	rows.Close()
	if _, err = tx.Exec(ctx, `UPDATE cerebro_round_held_trigger SET state='released',released_at=now() WHERE round_id=$1 AND state='held'`, roundID); err != nil {
		return Run{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Run{}, err
	}
	runID := util.MustParseUUID(run.ID)
	for _, h := range hs {
		issue, e := s.Queries.GetIssue(ctx, h.i)
		if e != nil {
			s.markItem(ctx, runID, h.i, h.targetType, h.targetID, "failed", pgtype.UUID{})
			continue
		}
		var task db.AgentTaskQueue
		switch h.targetType {
		case "agent":
			task, e = s.Tasks.EnqueueTaskForMention(ctx, issue, h.targetID, h.c)
		case "squad":
			squad, squadErr := s.Queries.GetSquad(ctx, h.targetID)
			if squadErr != nil {
				e = squadErr
			} else {
				task, e = s.Tasks.EnqueueTaskForSquadLeader(ctx, issue, squad.LeaderID, squad.ID, h.c)
			}
		default:
			task, e = s.Tasks.EnqueueTaskForIssue(ctx, issue, h.c)
		}
		if e != nil {
			s.markItem(ctx, runID, h.i, h.targetType, h.targetID, "failed", pgtype.UUID{})
			continue
		}
		// Rounds explicitly permit a tool-less batch attempt. The gateway
		// executor still resolves the agent's current tool surface immediately
		// before execution; any callable tool vetoes batching and keeps the
		// existing synchronous tool loop.
		contextJSON := fmt.Sprintf(`{"type":%q,"run_id":%q,"batch_tool_mode":"none"}`, service.RoundTaskContextType, run.ID)
		_, _ = s.Pool.Exec(ctx, `UPDATE agent_task_queue SET context=$2::jsonb WHERE id=$1`, task.ID, contextJSON)
		s.markItem(ctx, runID, h.i, h.targetType, h.targetID, "queued", task.ID)
	}
	s.RefreshRun(ctx, runID)
	return s.GetRun(ctx, runID)
}

func (s *Service) markItem(ctx context.Context, runID, issueID pgtype.UUID, targetType string, targetID pgtype.UUID, status string, taskID pgtype.UUID) {
	_, _ = s.Pool.Exec(ctx, `INSERT INTO cerebro_round_run_item(run_id,issue_id,target_type,target_id,status,task_id) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(run_id,issue_id,target_type,target_id) DO UPDATE SET status=EXCLUDED.status,task_id=EXCLUDED.task_id,updated_at=now()`, runID, issueID, targetType, targetID, status, taskID)
}
func (s *Service) GetRun(ctx context.Context, id pgtype.UUID) (Run, error) {
	var r Run
	err := s.Pool.QueryRow(ctx, `SELECT id::text,round_id::text,status,total_count,responded_count,stalled_count,nudged_count,started_at,ready_at,completed_at,created_at FROM cerebro_round_run WHERE id=$1`, id).Scan(&r.ID, &r.RoundID, &r.Status, &r.TotalCount, &r.RespondedCount, &r.StalledCount, &r.NudgedCount, &r.StartedAt, &r.ReadyAt, &r.CompletedAt, &r.CreatedAt)
	return r, err
}
func (s *Service) RefreshRun(ctx context.Context, id pgtype.UUID) {
	var queuedTaskID pgtype.UUID
	if err := s.Pool.QueryRow(ctx, `SELECT i.task_id FROM cerebro_round_run_item i JOIN cerebro_round_run r ON r.id=i.run_id JOIN agent_task_queue q ON q.id=i.task_id WHERE i.run_id=$1 AND i.status='queued' AND q.status IN ('queued','dispatched') AND i.updated_at < now()-interval '5 minutes' AND r.nudged_count < 3 ORDER BY i.updated_at LIMIT 1`, id).Scan(&queuedTaskID); err == nil {
		if task, err := s.Queries.GetAgentTask(ctx, queuedTaskID); err == nil {
			s.Tasks.NotifyTaskEnqueued(ctx, task)
			_, _ = s.Pool.Exec(ctx, `UPDATE cerebro_round_run_item SET updated_at=now() WHERE run_id=$1 AND task_id=$2; UPDATE cerebro_round_run SET nudged_count=nudged_count+1 WHERE id=$1`, id, queuedTaskID)
		}
	}
	_, _ = s.Pool.Exec(ctx, `UPDATE cerebro_round_run_item i SET status='stalled',updated_at=now() FROM agent_task_queue q WHERE i.run_id=$1 AND q.id=i.task_id AND q.status='running' AND q.started_at < now()-$2::interval`, id, stalledTaskTimeout.String())
	_, _ = s.Pool.Exec(ctx, `WITH c AS (SELECT count(*) FILTER(WHERE q.status='completed')::int responded,count(*) FILTER(WHERE i.status IN ('failed','stalled') OR q.status='failed')::int failed FROM cerebro_round_run_item i LEFT JOIN agent_task_queue q ON q.id=i.task_id WHERE i.run_id=$1) UPDATE cerebro_round_run r SET responded_count=c.responded,stalled_count=c.failed,status=CASE WHEN c.responded+c.failed>=r.total_count THEN 'ready' ELSE 'running' END,ready_at=CASE WHEN c.responded+c.failed>=r.total_count THEN COALESCE(r.ready_at,now()) ELSE NULL END FROM c WHERE r.id=$1`, id)
	s.publishProgress(ctx, id)
}

func (s *Service) publishProgress(ctx context.Context, runID pgtype.UUID) {
	if s.Tasks == nil || s.Tasks.Bus == nil {
		return
	}
	var workspaceID, ownerID string
	if err := s.Pool.QueryRow(ctx, `SELECT r.workspace_id::text,r.owner_id::text FROM cerebro_round_run rr JOIN cerebro_round r ON r.id=rr.round_id WHERE rr.id=$1`, runID).Scan(&workspaceID, &ownerID); err != nil {
		return
	}
	run, err := s.GetRun(ctx, runID)
	if err != nil {
		return
	}
	s.Tasks.Bus.Publish(events.Event{Type: "round:progress", WorkspaceID: workspaceID, ActorType: "system", ActorID: ownerID, AudienceUserIDs: []string{ownerID}, Payload: map[string]any{"run": run}})
}

func (s *Service) Sweep(ctx context.Context) error {
	activeRows, err := s.Pool.Query(ctx, `SELECT id FROM cerebro_round_run WHERE status='running'`)
	if err != nil {
		return err
	}
	var active []pgtype.UUID
	for activeRows.Next() {
		var id pgtype.UUID
		if err := activeRows.Scan(&id); err != nil {
			activeRows.Close()
			return err
		}
		active = append(active, id)
	}
	activeRows.Close()
	for _, id := range active {
		s.RefreshRun(ctx, id)
	}
	rows, err := s.Pool.Query(ctx, `SELECT id,workspace_id,owner_id FROM cerebro_round WHERE next_run_at<=now() ORDER BY next_run_at FOR UPDATE SKIP LOCKED`)
	if err != nil {
		return err
	}
	defer rows.Close()
	type due struct{ id, w, o pgtype.UUID }
	var ds []due
	for rows.Next() {
		var d due
		if err := rows.Scan(&d.id, &d.w, &d.o); err != nil {
			return err
		}
		ds = append(ds, d)
	}
	for _, d := range ds {
		r, e := s.Get(ctx, d.w, d.o, d.id)
		if e != nil {
			continue
		}
		n, e := nextRunAt(r.ScheduleCron, r.Timezone, time.Now())
		if e != nil {
			continue
		}
		_, _ = s.Pool.Exec(ctx, `UPDATE cerebro_round SET next_run_at=$2,updated_at=now() WHERE id=$1`, d.id, n)
		_, _ = s.Start(ctx, d.w, d.o, d.id)
	}
	return nil
}

func (s *Service) RunSweeper(ctx context.Context, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			_ = s.Sweep(ctx)
		}
	}
}
