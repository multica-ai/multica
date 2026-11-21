// Package maintenance runs explicitly requested, bounded database maintenance.
// It has no scheduler: each call advances one transaction on the primary database.
package maintenance

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrInvalid   = errors.New("invalid maintenance request")
	ErrConflict  = errors.New("maintenance job conflict")
	ErrBusy      = errors.New("maintenance job busy")
	ErrThrottled = errors.New("maintenance job rate limited")
	ErrNotFound  = errors.New("maintenance job not found")
)

type Options struct {
	BatchSize          int `json:"batch_size"`
	DelayMS            int `json:"delay_ms"`
	LockTimeoutMS      int `json:"lock_timeout_ms"`
	StatementTimeoutMS int `json:"statement_timeout_ms"`
}

func (o *Options) normalize() error {
	if o.BatchSize == 0 {
		o.BatchSize = 500
	}
	if o.DelayMS == 0 {
		o.DelayMS = 100
	}
	if o.LockTimeoutMS == 0 {
		o.LockTimeoutMS = 500
	}
	if o.StatementTimeoutMS == 0 {
		o.StatementTimeoutMS = 3000
	}
	if o.BatchSize < 1 || o.BatchSize > 5000 || o.DelayMS < 1 || o.DelayMS > 60000 ||
		o.LockTimeoutMS < 1 || o.StatementTimeoutMS < o.LockTimeoutMS || o.StatementTimeoutMS > 5000 {
		return fmt.Errorf("%w: batch_size 1..5000, delay_ms 1..60000, 1 <= lock_timeout_ms <= statement_timeout_ms <= 5000", ErrInvalid)
	}
	return nil
}

type Job struct {
	ID             string          `json:"id"`
	Type           string          `json:"job_type"`
	Version        int             `json:"job_version"`
	Scope          string          `json:"scope_key"`
	IdempotencyKey string          `json:"idempotency_key"`
	RequestHash    string          `json:"-"`
	Status         string          `json:"status"`
	Revision       int64           `json:"revision"`
	DryRun         bool            `json:"dry_run"`
	Options        Options         `json:"options"`
	Parameters     json.RawMessage `json:"parameters"`
	Checkpoint     json.RawMessage `json:"checkpoint"`
	Progress       json.RawMessage `json:"progress"`
	Result         json.RawMessage `json:"result"`
	LastError      string          `json:"last_error"`
	NextAllowedAt  time.Time       `json:"next_allowed_at"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
	CompletedAt    *time.Time      `json:"completed_at,omitempty"`
}
type CreateRequest struct {
	Type           string          `json:"job_type"`
	Version        int             `json:"job_version"`
	Scope          string          `json:"scope_key"`
	IdempotencyKey string          `json:"idempotency_key"`
	DryRun         *bool           `json:"dry_run,omitempty"`
	Options        Options         `json:"options"`
	Parameters     json.RawMessage `json:"parameters"`
}

// Processor implementations must only write through the supplied transaction.
// External side effects cannot share this checkpoint's atomicity guarantee.
type Processor interface {
	Type() string
	Version() int
	Validate(scope string, dryRun bool, parameters json.RawMessage) (json.RawMessage, error)
	Preflight(context.Context, pgx.Tx) error
	Step(context.Context, pgx.Tx, Job) (checkpoint, progress, result json.RawMessage, complete bool, err error)
}
type Service struct {
	pool       *pgxpool.Pool
	processors map[string]Processor
}

func NewService(pool *pgxpool.Pool, processors ...Processor) *Service {
	s := &Service{pool: pool, processors: make(map[string]Processor)}
	for _, p := range processors {
		if _, ok := s.processors[p.Type()]; ok {
			panic("duplicate maintenance processor")
		}
		s.processors[p.Type()] = p
	}
	return s
}
func decode(raw []byte, target any) error {
	if len(bytes.TrimSpace(raw)) == 0 || bytes.TrimSpace(raw)[0] != '{' {
		return fmt.Errorf("%w: expected JSON object", ErrInvalid)
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(target); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("%w: expected one JSON object", ErrInvalid)
	}
	return nil
}
func asJSON(v any) json.RawMessage { b, _ := json.Marshal(v); return b }

const columns = `id::text, job_type, job_version, scope_key, idempotency_key, request_hash, status, revision,
dry_run, options, parameters, checkpoint, progress, result, last_error, next_allowed_at,
created_at, updated_at, completed_at`

func scan(row pgx.Row) (Job, error) {
	var j Job
	var opts []byte
	err := row.Scan(&j.ID, &j.Type, &j.Version, &j.Scope, &j.IdempotencyKey, &j.RequestHash, &j.Status, &j.Revision,
		&j.DryRun, &opts, &j.Parameters, &j.Checkpoint, &j.Progress, &j.Result, &j.LastError,
		&j.NextAllowedAt, &j.CreatedAt, &j.UpdatedAt, &j.CompletedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return j, ErrNotFound
	}
	if err != nil {
		return j, err
	}
	err = decode(opts, &j.Options)
	return j, err
}
func (s *Service) processor(j Job) (Processor, error) {
	p, ok := s.processors[j.Type]
	if !ok || p.Version() != j.Version {
		return nil, fmt.Errorf("%w: unsupported job type/version", ErrConflict)
	}
	return p, nil
}
func (s *Service) Get(ctx context.Context, id string) (Job, error) {
	return scan(s.pool.QueryRow(ctx, "SELECT "+columns+" FROM maintenance_job WHERE id=$1::uuid", id))
}
func setTimeouts(ctx context.Context, tx pgx.Tx, o Options) error {
	_, err := tx.Exec(ctx, `SELECT set_config('lock_timeout', $1, true), set_config('statement_timeout', $2, true)`,
		fmt.Sprintf("%dms", o.LockTimeoutMS), fmt.Sprintf("%dms", o.StatementTimeoutMS))
	return err
}
func classify(err error) error {
	var pe *pgconn.PgError
	if errors.As(err, &pe) {
		switch pe.Code {
		case "55P03":
			return ErrBusy
		case "23505":
			return ErrConflict
		}
	}
	return err
}
func (s *Service) Create(ctx context.Context, r CreateRequest) (Job, error) {
	if r.IdempotencyKey == "" || len(r.IdempotencyKey) > 200 || len(r.Scope) > 200 {
		return Job{}, ErrInvalid
	}
	if err := r.Options.normalize(); err != nil {
		return Job{}, err
	}
	dry := r.DryRun == nil || *r.DryRun
	j := Job{Type: r.Type, Version: r.Version, Scope: r.Scope, DryRun: dry, Options: r.Options}
	p, err := s.processor(j)
	if err != nil {
		return Job{}, err
	}
	if len(r.Parameters) == 0 {
		r.Parameters = asJSON(struct{}{})
	}
	params, err := p.Validate(r.Scope, dry, r.Parameters)
	if err != nil {
		return Job{}, err
	}
	// Keep creation identity stable even after operational options are tuned.
	hash := fmt.Sprintf("%x", sha256.Sum256(asJSON([]any{r.Type, r.Version, r.Scope, dry, r.Options, params})))
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Job{}, err
	}
	defer tx.Rollback(context.Background())
	if err = setTimeouts(ctx, tx, r.Options); err != nil {
		return Job{}, err
	}
	// Every processor relies on these shared identity/concurrency constraints.
	var count int
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM pg_index i
 WHERE i.indrelid='maintenance_job'::regclass AND i.indexrelid IN
 (to_regclass('idx_maintenance_job_id'),to_regclass('idx_maintenance_job_idempotency'),to_regclass('idx_maintenance_job_active'))
 AND i.indisvalid AND i.indisunique`).Scan(&count); err != nil {
		return Job{}, err
	}
	if count != 3 {
		return Job{}, fmt.Errorf("maintenance indexes are missing or invalid; finish migrations 480-482")
	}
	// Serialize creation requests for this type/scope; the unique indexes remain the final guard.
	var locked bool
	err = tx.QueryRow(ctx, "SELECT pg_try_advisory_xact_lock(hashtextextended($1, 0))", "maintenance:"+r.Type+":"+r.Scope).Scan(&locked)
	if err != nil {
		return Job{}, err
	}
	if !locked {
		return Job{}, ErrBusy
	}
	existing, err := scan(tx.QueryRow(ctx, "SELECT "+columns+" FROM maintenance_job WHERE idempotency_key=$1", r.IdempotencyKey))
	if err == nil {
		if existing.RequestHash != hash {
			return existing, ErrConflict
		}
		return existing, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return Job{}, err
	}
	if err = p.Preflight(ctx, tx); err != nil {
		return Job{}, err
	}
	j, err = scan(tx.QueryRow(ctx, `INSERT INTO maintenance_job
 (job_type,job_version,scope_key,idempotency_key,status,dry_run,options,parameters,request_hash)
 VALUES ($1,$2,$3,$4,'ready',$5,$6,$7,$8) RETURNING `+columns,
		r.Type, r.Version, r.Scope, r.IdempotencyKey, dry, asJSON(r.Options), params, hash))
	if err != nil {
		return Job{}, classify(err)
	}
	return j, tx.Commit(ctx)
}

// Mutate uses a row lock and a caller-supplied revision. Stale retries return
// the durable state without advancing again, including after an ambiguous commit.
func (s *Service) Mutate(ctx context.Context, id string, revision int64, action string) (Job, error) {
	return s.mutate(ctx, id, revision, action, nil)
}

// Configure changes operational limits only while paused; task parameters and
// processor versions are immutable for a run.
func (s *Service) Configure(ctx context.Context, id string, revision int64, options Options) (Job, error) {
	if err := options.normalize(); err != nil {
		return Job{}, err
	}
	return s.mutate(ctx, id, revision, "configure", &options)
}

func (s *Service) mutate(ctx context.Context, id string, revision int64, action string, options *Options) (Job, error) {
	if revision < 0 {
		return Job{}, ErrInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Job{}, err
	}
	defer tx.Rollback(context.Background())
	if err = setTimeouts(ctx, tx, Options{LockTimeoutMS: 500, StatementTimeoutMS: 5000}); err != nil {
		return Job{}, err
	}
	j, err := scan(tx.QueryRow(ctx, "SELECT "+columns+" FROM maintenance_job WHERE id=$1::uuid FOR UPDATE NOWAIT", id))
	if err != nil {
		return Job{}, classify(err)
	}
	if j.Revision != revision {
		return j, ErrConflict
	}
	if j.Status == "completed" || j.Status == "cancelled" {
		return j, ErrConflict
	}
	var stepErr error
	switch action {
	case "configure":
		if j.Status != "paused" || options == nil {
			return j, ErrConflict
		}
		j.Options = *options
	case "pause":
		j.Status = "paused"
	case "cancel":
		j.Status = "cancelled"
	case "resume":
		j.Status = "ready"
	case "advance":
		if j.Status != "ready" {
			return j, ErrConflict
		}
		p, e := s.processor(j)
		if e != nil {
			return j, e
		}
		var eligible bool
		if err = tx.QueryRow(ctx, "SELECT clock_timestamp() >= $1", j.NextAllowedAt).Scan(&eligible); err != nil {
			return j, err
		}
		if !eligible {
			return j, ErrThrottled
		}
		if _, e = p.Validate(j.Scope, j.DryRun, j.Parameters); e != nil {
			return j, e
		}
		if e = j.Options.normalize(); e != nil {
			return j, e
		}
		// Savepoint lets failures retain diagnostics without retaining partial data.
		batch, e := tx.Begin(ctx)
		if e != nil {
			return j, e
		}
		e = setTimeouts(ctx, batch, j.Options)
		if e == nil {
			e = p.Preflight(ctx, batch)
		}
		var cp, progress, result json.RawMessage
		var complete bool
		if e == nil {
			cp, progress, result, complete, e = p.Step(ctx, batch, j)
		}
		if e != nil {
			if rollbackErr := batch.Rollback(ctx); rollbackErr != nil {
				return j, e
			}
			stepErr = e
			j.LastError = e.Error()
			// Repeated failures require an explicit operator resume.
			j.Status = "paused"
		} else {
			if e = batch.Commit(ctx); e != nil {
				return j, e
			}
			j.Checkpoint, j.Progress, j.Result = cp, progress, result
			j.LastError = ""
			if complete {
				j.Status = "completed"
			}
		}
	default:
		return j, ErrInvalid
	}
	j, err = scan(tx.QueryRow(ctx, `UPDATE maintenance_job SET status=$2, revision=revision+1,
 checkpoint=$3,progress=$4,result=$5,last_error=$6,updated_at=clock_timestamp(),
	next_allowed_at=CASE WHEN $7 THEN GREATEST(next_allowed_at,clock_timestamp()+$8*interval '1 millisecond') ELSE next_allowed_at END,
	options=$9,
 completed_at=CASE WHEN $2 IN ('completed','cancelled') THEN clock_timestamp() ELSE completed_at END
 WHERE id=$1::uuid RETURNING `+columns, id, j.Status, j.Checkpoint, j.Progress, j.Result, j.LastError,
		action == "advance" || action == "configure", j.Options.DelayMS, asJSON(j.Options)))
	if err != nil {
		return j, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Job{}, err
	}
	slog.Info("maintenance job operation committed", "job_id", j.ID, "job_type", j.Type, "action", action, "status", j.Status, "revision", j.Revision, "progress", string(j.Progress))
	return j, stepErr
}
