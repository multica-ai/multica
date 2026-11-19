package continuation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/inbox"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

// Monitor runs in the existing server scheduler. An empty project is off,
// including enrollment and delivery; no daemon or model is involved.
type Monitor struct {
	Pool      *pgxpool.Pool
	Bus       *events.Bus
	ProjectID string
	Notify    bool
}

func FromEnv(pool *pgxpool.Pool, bus *events.Bus) (*Monitor, error) {
	project := os.Getenv("MULTICA_CONTINUATION_PROJECT_ID")
	if project == "" {
		return nil, nil
	}
	if _, err := uuid.Parse(project); err != nil {
		return nil, errors.New("invalid MULTICA_CONTINUATION_PROJECT_ID")
	}
	notify := false
	if raw := os.Getenv("MULTICA_CONTINUATION_NOTIFY"); raw != "" {
		var err error
		notify, err = strconv.ParseBool(raw)
		if err != nil {
			return nil, errors.New("invalid MULTICA_CONTINUATION_NOTIFY")
		}
	}
	return &Monitor{Pool: pool, Bus: bus, ProjectID: project, Notify: notify}, nil
}

const keyWhere = "workspace_id=$1 AND issue_id=$2 AND chain_id=$3 AND remainder_id=$4"

func keyArgs(k Key) []any { return []any{k.WorkspaceID, k.IssueID, k.ChainID, k.RemainderID} }

func load(ctx context.Context, tx pgx.Tx, k Key) (Record, error) {
	var r Record
	var raw []byte
	err := tx.QueryRow(ctx, "SELECT contract,version,state,last_progress_at FROM issue_continuation WHERE "+keyWhere+" FOR UPDATE", keyArgs(k)...).Scan(&raw, &r.Version, &r.State, &r.LastProgressAt)
	if err == nil {
		err = json.Unmarshal(raw, &r.Contract)
	}
	return r, err
}

// Parent locks precede ledger and wakeup locks. They serialize dispatch,
// explicit adoption and issue deletion without locking a task after its issue.
// FOR UPDATE also conflicts with enqueue's issue KEY SHARE: a retry cannot
// appear between the last successor snapshot and the inbox commit.
func (m *Monitor) lockIssue(ctx context.Context, tx pgx.Tx, k Key) error {
	var id string
	if err := tx.QueryRow(ctx, "SELECT id::text FROM workspace WHERE id=$1 FOR KEY SHARE", k.WorkspaceID).Scan(&id); err != nil {
		return err
	}
	return tx.QueryRow(ctx, "SELECT id::text FROM issue WHERE id=$1 AND workspace_id=$2 FOR UPDATE", k.IssueID, k.WorkspaceID).Scan(&id)
}

func (m *Monitor) checkScope(ctx context.Context, tx pgx.Tx, c Contract) error {
	if m.ProjectID == "" {
		return errors.New("continuation monitor is disabled")
	}
	var allowed bool
	err := tx.QueryRow(ctx, `SELECT EXISTS (
        SELECT 1 FROM issue i JOIN member u ON u.workspace_id=i.workspace_id AND u.user_id=$3
        JOIN project p ON p.id=i.project_id
        WHERE i.id=$2 AND i.workspace_id=$1 AND i.project_id=$4
        AND ((p.visibility <> 'private' AND u.role <> 'guest') OR EXISTS (SELECT 1 FROM project_member pm WHERE pm.project_id=p.id AND pm.user_id=$3)))`, c.WorkspaceID, c.IssueID, c.OwnerID, m.ProjectID).Scan(&allowed)
	if err != nil {
		return err
	}
	if !allowed {
		return errors.New("issue, project or private recipient access does not match the enrolled scope")
	}
	return nil
}

// Put is an administrative DB operation. The pilot deliberately has no public
// mutation endpoint. The operator must supply the last observed version; the
// monitor and every successor adoption serialize on the same ledger row.
func (m *Monitor) Put(ctx context.Context, change Change) (Record, error) {
	if err := change.Contract.validate(); err != nil {
		return Record{}, err
	}
	tx, err := m.Pool.Begin(ctx)
	if err != nil {
		return Record{}, err
	}
	defer tx.Rollback(ctx)
	if err = m.lockIssue(ctx, tx, change.Key); err != nil {
		return Record{}, err
	}
	// Serialize the first insertion as well as generation changes. Existing
	// rows use FOR UPDATE below, shared with reconciliation and delivery.
	lockKey := change.WorkspaceID + ":" + change.IssueID + ":" + change.ChainID + ":" + change.RemainderID
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,1769))", lockKey); err != nil {
		return Record{}, err
	}
	old, err := load(ctx, tx, change.Key)
	exists := !errors.Is(err, pgx.ErrNoRows)
	if err != nil && exists {
		return Record{}, err
	}
	if change.ExpectedVersion < 0 || (exists && old.Version != change.ExpectedVersion) || (!exists && change.ExpectedVersion != 0) {
		return Record{}, ErrConflict
	}
	if exists {
		if change.Generation != old.Generation {
			if !change.NewGeneration || change.Generation != old.Generation+1 || old.State != "resolved" {
				return Record{}, errors.New("a new generation requires a resolved previous remainder and explicit increment")
			}
		} else if change.NewGeneration || (old.State == "resolved" && !change.CoverageConfirmed) {
			return Record{}, errors.New("a resolved generation cannot be reopened or used as a retry budget")
		}
	} else if change.NewGeneration {
		return Record{}, errors.New("initial enrollment does not supersede a generation")
	}
	if err = m.checkScope(ctx, tx, change.Contract); err != nil {
		return Record{}, err
	}
	for _, id := range []string{change.SourceTaskID, change.ReceiverTaskID} {
		if id == "" {
			continue
		}
		var valid bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agent_task_queue t JOIN issue i ON i.id=t.issue_id WHERE t.id=$1 AND i.id=$2 AND i.workspace_id=$3)`, id, change.IssueID, change.WorkspaceID).Scan(&valid); err != nil {
			return Record{}, err
		}
		if !valid {
			return Record{}, errors.New("task belongs to a different issue or workspace")
		}
	}
	f, err := readFacts(ctx, tx, &change.Contract)
	if err != nil {
		return Record{}, err
	}
	if f.handoffChanged || f.receiverMissing {
		return Record{}, errors.New("wakeup revision or receiver is not current")
	}
	if change.CoverageConfirmed {
		var valid bool
		err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM comment c WHERE c.id=$1 AND c.issue_id=$2 AND c.workspace_id=$3 AND c.deleted_at IS NULL AND length(trim(c.content))>0 AND
            ((c.author_type='member' AND c.author_id=$4) OR (c.author_type='agent' AND EXISTS(SELECT 1 FROM agent_task_queue t WHERE t.id=$5 AND t.issue_id=c.issue_id AND t.agent_id=c.author_id AND c.source_task_id=t.id))))`, change.ResultCommentID, change.IssueID, change.WorkspaceID, change.OwnerID, nullable(change.ReceiverTaskID)).Scan(&valid)
		if err != nil {
			return Record{}, err
		}
		if !valid {
			return Record{}, errors.New("result comment must belong to this remainder's receiver or human owner on this issue")
		}
	}
	var now time.Time
	if err = tx.QueryRow(ctx, "SELECT now()").Scan(&now); err != nil {
		return Record{}, err
	}
	state, _ := decide(change.Contract, f, now)
	raw, err := json.Marshal(change.Contract)
	if err != nil {
		return Record{}, err
	}
	version := old.Version + 1
	args := append(keyArgs(change.Key), change.Generation, version, state, raw)
	if exists {
		// Preserve every explicit amendment, including resolved generations.
		_, err = tx.Exec(ctx, `UPDATE issue_continuation SET history=history || jsonb_build_array(jsonb_build_object('contract',contract,'version',version,'state',state,'at',now())),generation=$5,version=$6,state=$7,contract=$8,last_progress_at=now(),next_check_at=now() WHERE `+keyWhere, args...)
	} else {
		_, err = tx.Exec(ctx, `INSERT INTO issue_continuation(workspace_id,issue_id,chain_id,remainder_id,generation,version,state,contract) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, args...)
	}
	if err != nil {
		return Record{}, err
	}
	if state == "resolved" {
		if _, err = tx.Exec(ctx, `UPDATE issue_continuation_incident SET outbox_state='suppressed' WHERE `+keyWhere+` AND outbox_state='pending'`, keyArgs(change.Key)...); err != nil {
			return Record{}, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return Record{}, err
	}
	return Record{Contract: change.Contract, Version: version, State: state, LastProgressAt: now}, nil
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// Locks make the final check and private inbox write one linearized decision.
// No terminal path writes the ledger: the durable task rows are reconciled by
// the bounded sweep, including after server restart and runtime reconnect.
func readFacts(ctx context.Context, tx pgx.Tx, c *Contract) (facts, error) {
	var f facts
	originalReceiver := c.ReceiverTaskID
	rootReceiver := originalReceiver
	if c.WakeupID != "" {
		var revision int32
		var receiver *string
		var enabled bool
		err := tx.QueryRow(ctx, `SELECT revision,last_task_id::text,enabled FROM issue_wakeup WHERE id=$1 AND issue_id=$2 AND workspace_id=$3 FOR SHARE`, c.WakeupID, c.IssueID, c.WorkspaceID).Scan(&revision, &receiver, &enabled)
		if errors.Is(err, pgx.ErrNoRows) {
			f.handoffChanged = true
			return f, nil
		}
		if err != nil {
			return f, err
		}
		if revision != c.WakeupRevision || (!enabled && receiver == nil) {
			f.handoffChanged = true
			return f, nil
		}
		if receiver != nil {
			rootReceiver = *receiver
		}
	}
	if rootReceiver == "" {
		return f, nil
	}
	// Terminal outcomes are monotonic. Do not lock the task here: terminal
	// writers already hold the task before touching issue/wakeup rows. A
	// nonterminal snapshot suppresses delivery until the next sweep.
	// Existing platform retry children carry the same remainder. Failure and
	// retry creation are atomic in TaskService, so one statement sees both.
	// A manual successor must be explicitly adopted by Put, not inferred from
	// an unrelated task on the same issue.
	err := tx.QueryRow(ctx, `WITH RECURSIVE successors AS (
        SELECT t.id,t.status,t.completed_at,t.created_at,0 AS depth,ARRAY[t.id] AS path
        FROM agent_task_queue t JOIN issue i ON i.id=t.issue_id
        WHERE t.id=$1 AND i.id=$2 AND i.workspace_id=$3
        UNION ALL
        SELECT t.id,t.status,t.completed_at,t.created_at,s.depth+1,s.path || t.id
        FROM agent_task_queue t JOIN successors s ON t.retry_of_task_id=s.id
        WHERE t.issue_id=$2 AND s.depth<16 AND NOT t.id=ANY(s.path)
    ) SELECT id::text,status,completed_at FROM successors
      ORDER BY (status IN ('queued','deferred','dispatched','running','waiting_local_directory')) DESC,depth DESC,created_at DESC LIMIT 1`, rootReceiver, c.IssueID, c.WorkspaceID).Scan(&c.ReceiverTaskID, &f.status, &f.terminalAt)
	if errors.Is(err, pgx.ErrNoRows) {
		f.receiverMissing = true
		return f, nil
	}
	f.receiverPresent = err == nil
	if err == nil && c.ReceiverTaskID != originalReceiver {
		c.WaitKind = "none"
		c.WaitUntil = nil
	}
	return f, err
}

// Tick is bounded, fair across enrolled remainders and independent of event
// bus delivery. A lost task-terminal event is recovered from PostgreSQL.
func (m *Monitor) Tick(ctx context.Context) error {
	if m.ProjectID == "" {
		return nil
	}
	rows, err := m.Pool.Query(ctx, `SELECT c.workspace_id::text,c.issue_id::text,c.chain_id::text,c.remainder_id::text FROM issue_continuation c JOIN issue i ON i.id=c.issue_id AND i.workspace_id=c.workspace_id WHERE i.project_id=$1 AND c.state<>'resolved' AND c.next_check_at<=now() ORDER BY c.next_check_at,c.issue_id LIMIT 100`, m.ProjectID)
	if err != nil {
		return err
	}
	var keys []Key
	for rows.Next() {
		var k Key
		if err = rows.Scan(&k.WorkspaceID, &k.IssueID, &k.ChainID, &k.RemainderID); err != nil {
			rows.Close()
			return err
		}
		keys = append(keys, k)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	var errs []error
	for _, k := range keys {
		if err = m.Reconcile(ctx, k); err != nil {
			errs = append(errs, err)
			// Revoked access must not monopolize the front of the due queue.
			_, bumpErr := m.Pool.Exec(ctx, "UPDATE issue_continuation SET next_check_at=now()+interval '30 seconds' WHERE "+keyWhere, keyArgs(k)...)
			errs = append(errs, bumpErr)
			continue
		}
		if m.Notify {
			if err = m.Deliver(ctx, k); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}

func (m *Monitor) Reconcile(ctx context.Context, k Key) error {
	tx, err := m.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = m.lockIssue(ctx, tx, k); err != nil {
		return err
	}
	r, err := load(ctx, tx, k)
	if err != nil {
		return err
	}
	if err = m.checkScope(ctx, tx, r.Contract); err != nil {
		return err
	}
	_, _, err = m.reconcile(ctx, tx, &r)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (m *Monitor) reconcile(ctx context.Context, tx pgx.Tx, r *Record) (facts, string, error) {
	previousReceiver, previousState := r.ReceiverTaskID, r.State
	f, err := readFacts(ctx, tx, &r.Contract)
	if err != nil {
		return f, "", err
	}
	var now time.Time
	if err = tx.QueryRow(ctx, "SELECT now()").Scan(&now); err != nil {
		return f, "", err
	}
	state, reason := decide(r.Contract, f, now)
	progress := (previousReceiver != r.ReceiverTaskID || previousState != state) && state != "needs_reconcile"
	raw, err := json.Marshal(r.Contract)
	if err != nil {
		return f, "", err
	}
	args := append(keyArgs(r.Key), state, raw, progress)
	// Reading unchanged facts is not progress and does not invalidate an
	// operator's CAS. Adopted receivers/state changes do invalidate it.
	_, err = tx.Exec(ctx, `UPDATE issue_continuation SET version=version+CASE WHEN state<>$5 OR contract<>$6::jsonb THEN 1 ELSE 0 END,state=$5,contract=$6,last_progress_at=CASE WHEN $7 THEN now() ELSE last_progress_at END,next_check_at=now()+interval '30 seconds' WHERE `+keyWhere, args...)
	if err != nil {
		return f, "", err
	}
	r.State = state
	if state == "needs_reconcile" {
		args = append(keyArgs(r.Key), r.Generation, dbid.NewV7(), reason, f.terminalAt)
		_, err = tx.Exec(ctx, `INSERT INTO issue_continuation_incident(workspace_id,issue_id,chain_id,remainder_id,generation,id,reason,terminal_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8)
            ON CONFLICT(workspace_id,issue_id,chain_id,remainder_id,generation) DO UPDATE SET reason=EXCLUDED.reason,terminal_at=COALESCE(issue_continuation_incident.terminal_at,EXCLUDED.terminal_at),outbox_state=CASE WHEN issue_continuation_incident.outbox_state='delivered' THEN 'delivered' ELSE 'pending' END`, args...)
	} else {
		args = append(keyArgs(r.Key), r.Generation)
		_, err = tx.Exec(ctx, `UPDATE issue_continuation_incident SET outbox_state='suppressed' WHERE `+keyWhere+` AND generation=$5 AND outbox_state='pending'`, args...)
	}
	return f, reason, err
}

// Deliver rechecks actual state under the same ledger lock used for adoption.
// Inbox insertion and the outbox ACK are atomic. After an ambiguous commit,
// retry this method: it reads the persisted ACK, never generates another ID.
func (m *Monitor) Deliver(ctx context.Context, k Key) error {
	if !m.Notify || m.ProjectID == "" {
		return nil
	}
	tx, err := m.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = m.lockIssue(ctx, tx, k); err != nil {
		return err
	}
	r, err := load(ctx, tx, k)
	if err != nil {
		return err
	}
	if err = m.checkScope(ctx, tx, r.Contract); err != nil {
		return err
	}
	_, reason, err := m.reconcile(ctx, tx, &r)
	if err != nil {
		return err
	}
	if r.State != "needs_reconcile" {
		return tx.Commit(ctx)
	}
	var id pgtype.UUID
	args := append(keyArgs(k), r.Generation)
	err = tx.QueryRow(ctx, `SELECT id FROM issue_continuation_incident WHERE `+keyWhere+` AND generation=$5 AND outbox_state='pending' FOR UPDATE`, args...).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return tx.Commit(ctx)
	}
	if err != nil {
		return err
	}
	var recipient, workspace, issue pgtype.UUID
	if err = recipient.Scan(r.OwnerID); err != nil {
		return err
	}
	if err = workspace.Scan(r.WorkspaceID); err != nil {
		return err
	}
	if err = issue.Scan(r.IssueID); err != nil {
		return err
	}
	details, _ := json.Marshal(map[string]any{"chain_id": r.ChainID, "remainder_id": r.RemainderID, "generation": r.Generation, "receiver_task_id": r.ReceiverTaskID, "reason": reason})
	var deferred inbox.Deferred
	_, inserted, err := inbox.Deliver(ctx, db.New(tx), &deferred, inbox.Notification{Params: db.CreateInboxItemParams{
		ID: id, WorkspaceID: workspace, RecipientType: "member", RecipientID: recipient, IssueID: issue,
		Type: "continuation_stalled", Severity: "attention", Title: "An unfinished task needs attention",
		Body: pgtype.Text{String: "The continuation has no confirmed result. Review its current state before resuming work.", Valid: true}, Details: details,
	}})
	if err != nil {
		return err
	}
	if !inserted {
		_, err = tx.Exec(ctx, `UPDATE issue_continuation_incident SET last_delivery_error='recipient_not_eligible' WHERE id=$1`, id)
	} else {
		_, err = tx.Exec(ctx, `UPDATE issue_continuation_incident SET outbox_state='delivered',notified_at=now(),last_delivery_error=NULL WHERE id=$1`, id)
	}
	if err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	deferred.Flush(m.Bus)
	return nil
}

func (m *Monitor) Get(ctx context.Context, k Key) (Record, error) {
	tx, err := m.Pool.Begin(ctx)
	if err != nil {
		return Record{}, err
	}
	defer tx.Rollback(ctx)
	if err = m.lockIssue(ctx, tx, k); err != nil {
		return Record{}, err
	}
	r, err := load(ctx, tx, k)
	if err != nil {
		return Record{}, err
	}
	if err = m.checkScope(ctx, tx, r.Contract); err != nil {
		return Record{}, err
	}
	return r, tx.Commit(ctx)
}

func (k Key) String() string {
	return fmt.Sprintf("%s/%s/%s/%s", k.WorkspaceID, k.IssueID, k.ChainID, k.RemainderID)
}
