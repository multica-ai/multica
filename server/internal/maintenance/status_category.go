package maintenance

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

const StatusCategoryType = "issue_status_category"

type StatusCategory struct{}

func (StatusCategory) Type() string { return StatusCategoryType }
func (StatusCategory) Version() int { return 1 }

type statusParameters struct {
	WritersUpgraded bool `json:"writers_upgraded"`
}

func (StatusCategory) Validate(scope string, dry bool, raw json.RawMessage) (json.RawMessage, error) {
	var p statusParameters
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	if scope != "database" {
		return nil, fmt.Errorf("%w: this processor requires scope_key=database", ErrInvalid)
	}
	if !dry && !p.WritersUpgraded {
		return nil, fmt.Errorf("%w: confirm PR1 is fully deployed using writers_upgraded=true", ErrInvalid)
	}
	return asJSON(p), nil
}
func (StatusCategory) Preflight(ctx context.Context, tx pgx.Tx) error {
	var applied bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version='478_issue_status_category_expand')`).Scan(&applied); err != nil {
		return err
	}
	if !applied {
		return fmt.Errorf("migration 478 is not applied")
	}
	var compatible bool
	if err := tx.QueryRow(ctx, `SELECT bool_and(issue_status_category(old_value)=new_value)
 FROM (VALUES ('backlog','unstarted'),('todo','unstarted'),('in_progress','started'),
 ('in_review','started'),('blocked','started'),('cancelled','closed'),
 ('unstarted','unstarted'),('started','started'),('done','done'),('closed','closed')) v(old_value,new_value)`).Scan(&compatible); err != nil {
		return err
	}
	if !compatible {
		return fmt.Errorf("category mapping does not match processor version 1")
	}
	// Read actual CHECK definitions as well as the ledger: a recorded migration
	// alone cannot prove that the compatibility schema is still installed.
	for _, name := range []string{"issue_status_category_check", "issue_status_system_is_canonical"} {
		var expression string
		if err := tx.QueryRow(ctx, `SELECT pg_get_expr(conbin,conrelid) FROM pg_constraint
  WHERE conrelid='issue_status'::regclass AND conname=$1 AND contype='c'`, name).Scan(&expression); err != nil {
			return err
		}
		for _, value := range []string{"backlog", "todo", "in_progress", "in_review", "blocked", "cancelled", "unstarted", "started", "done", "closed"} {
			if !strings.Contains(expression, "'"+value+"'") {
				return fmt.Errorf("compatibility CHECK %s lacks %s", name, value)
			}
		}
	}
	var effective string
	if err := tx.QueryRow(ctx, "SELECT pg_get_functiondef('issue_effective_status(uuid,text)'::regprocedure)").Scan(&effective); err != nil {
		return err
	}
	if !strings.Contains(effective, "WHEN 'cancelled'") || !strings.Contains(effective, "WHEN 'closed'") {
		return fmt.Errorf("issue_effective_status does not recognize both terminal spellings")
	}
	return nil
}

type statusCheckpoint struct {
	Phase   string `json:"phase"`
	AfterID string `json:"after_id"`
}
type statusProgress struct {
	Scanned     int64            `json:"scanned"`
	Updated     int64            `json:"updated"`
	WouldUpdate int64            `json:"would_update"`
	Verified    int64            `json:"verified"`
	Archived    int64            `json:"archived"`
	System      int64            `json:"system"`
	Categories  map[string]int64 `json:"categories"`
}

var categoryMapping = map[string]string{
	"backlog": "unstarted", "todo": "unstarted", "in_progress": "started", "in_review": "started",
	"blocked": "started", "cancelled": "closed", "unstarted": "unstarted", "started": "started", "done": "done", "closed": "closed",
}
var systemCategories = map[string]string{"backlog": "unstarted", "todo": "unstarted", "in_progress": "started", "in_review": "started", "blocked": "started", "done": "done", "cancelled": "closed"}

func (StatusCategory) Step(ctx context.Context, tx pgx.Tx, j Job) (json.RawMessage, json.RawMessage, json.RawMessage, bool, error) {
	var cp statusCheckpoint
	var progress statusProgress
	if err := decode(j.Checkpoint, &cp); err != nil {
		return nil, nil, nil, false, err
	}
	if err := decode(j.Progress, &progress); err != nil {
		return nil, nil, nil, false, err
	}
	if cp.Phase == "" {
		cp.Phase = "scan"
	}
	if cp.Phase != "scan" && cp.Phase != "verify" {
		return nil, nil, nil, false, fmt.Errorf("unknown checkpoint phase")
	}
	if progress.Categories == nil {
		progress.Categories = map[string]int64{}
	}
	// Do not filter on category before LIMIT: even a mostly migrated table scans
	// only one indexed page per request. Advance over examined, not updated, IDs.
	query := "SELECT id::text,key,category,is_system,archived_at IS NOT NULL FROM issue_status"
	args := []any{j.Options.BatchSize}
	if cp.AfterID != "" {
		query += " WHERE id > $2::uuid"
		args = append(args, cp.AfterID)
	}
	// Qualify the UUID column: an unqualified ORDER BY id would resolve to the
	// SELECT's id::text output and require a text sort instead of the UUID index.
	query += " ORDER BY issue_status.id LIMIT $1"
	rows, err := tx.Query(ctx, query, args...)
	if err != nil {
		return nil, nil, nil, false, err
	}
	var ids []string
	for rows.Next() {
		var id, key, category string
		var system, archived bool
		if err = rows.Scan(&id, &key, &category, &system, &archived); err != nil {
			break
		}
		canonical, ok := categoryMapping[category]
		if !ok {
			err = fmt.Errorf("unknown category %q at status %s", category, id)
			break
		}
		if system && (systemCategories[key] != canonical || (category != key && category != canonical)) {
			err = fmt.Errorf("noncanonical system status %s", id)
			break
		}
		if cp.Phase == "verify" {
			if category != canonical {
				err = fmt.Errorf("legacy category remains at status %s; investigate writers and start a new pass", id)
				break
			}
			progress.Verified++
		} else {
			progress.Scanned++
			progress.Categories[category]++
			if category != canonical {
				progress.WouldUpdate++
			}
			if system {
				progress.System++
			}
			if archived {
				progress.Archived++
			}
		}
		ids = append(ids, id)
	}
	rowErr := rows.Err()
	rows.Close()
	if err != nil {
		return nil, nil, nil, false, err
	}
	if rowErr != nil {
		return nil, nil, nil, false, rowErr
	}
	if len(ids) > 0 {
		if cp.Phase == "scan" && !j.DryRun {
			tag, e := tx.Exec(ctx, `UPDATE issue_status SET category=issue_status_category(category)
   WHERE id=ANY($1::uuid[]) AND category IN ('backlog','todo','in_progress','in_review','blocked','cancelled')`, ids)
			if e != nil {
				return nil, nil, nil, false, e
			}
			progress.Updated += tag.RowsAffected()
		}
		cp.AfterID = ids[len(ids)-1]
		return asJSON(cp), asJSON(progress), j.Result, false, nil
	}
	if cp.Phase == "scan" && !j.DryRun {
		cp = statusCheckpoint{Phase: "verify"}
		return asJSON(cp), asJSON(progress), j.Result, false, nil
	}
	result := map[string]any{"dry_run": j.DryRun, "checked_at": time.Now().UTC(), "scanned": progress.Scanned,
		"updated": progress.Updated, "would_update": progress.WouldUpdate, "verified": progress.Verified,
		"invalid": 0, "writers_upgraded_is_operator_attestation": true}
	if !j.DryRun {
		result["remaining_legacy"] = 0
	}
	return asJSON(cp), asJSON(progress), asJSON(result), true, nil
}
