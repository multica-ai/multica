package workflow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type ValidationResult struct {
	Valid       bool               `json:"valid"`
	Errors      []ValidationDetail `json:"errors"`
	Warnings    []ValidationDetail `json:"warnings"`
	ContentHash string             `json:"content_hash,omitempty"`
}

type Release struct {
	ID                 string    `json:"id"`
	WorkflowID         string    `json:"workflow_id"`
	VersionNumber      int       `json:"version_number"`
	DraftRevision      int64     `json:"draft_revision"`
	GraphSchemaVersion int       `json:"graph_schema_version"`
	PlanVersion        int       `json:"plan_version"`
	ContentHash        string    `json:"content_hash"`
	Notes              string    `json:"notes,omitempty"`
	CreatedBy          string    `json:"created_by"`
	CreatedAt          time.Time `json:"created_at"`
	Published          bool      `json:"published"`
}

func graphContentHash(graph Graph, plan CompiledPlan) string {
	encoded, _ := json.Marshal(struct {
		Graph Graph        `json:"graph"`
		Plan  CompiledPlan `json:"plan"`
	}{Graph: graph, Plan: plan})
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}

func publishRequestHash(workflowID string, graph Graph, notes string) string {
	payload, _ := json.Marshal(struct {
		WorkflowID string `json:"workflow_id"`
		Graph      Graph  `json:"graph"`
		Notes      string `json:"notes"`
	}{workflowID, graph, notes})
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:])
}

func validationFromError(err error) ValidationResult {
	result := ValidationResult{Valid: false, Errors: []ValidationDetail{detail("workflow_validation_failed", err.Error())}}
	var typed *Error
	if errors.As(err, &typed) && len(typed.Details) > 0 {
		result.Errors = append([]ValidationDetail(nil), typed.Details...)
	}
	return result
}

func (s *Service) ValidateGraph(ctx context.Context, ws, user, wid string, expectedRevision int64) (ValidationResult, error) {
	w, err := s.Get(ctx, ws, wid)
	if err != nil {
		return ValidationResult{}, err
	}
	if expectedRevision > 0 && w.Revision != expectedRevision {
		return ValidationResult{}, Conflict("workflow changed; refresh before validating")
	}
	if err = requireV2Graph(w.Graph); err != nil {
		return validationFromError(err), nil
	}
	if len(w.UpgradeIssues) > 0 {
		return ValidationResult{Valid: false, Errors: append([]ValidationDetail(nil), w.UpgradeIssues...), Warnings: []ValidationDetail{}}, nil
	}
	plan, compileErr := Compile(w.Graph)
	if compileErr != nil {
		return validationFromError(compileErr), nil
	}
	if err = s.validateAgents(ctx, ws, user, w.Graph, true); err != nil {
		return validationFromError(err), nil
	}
	if err = s.validateMembers(ctx, ws, w.Graph, true); err != nil {
		return validationFromError(err), nil
	}
	return ValidationResult{Valid: true, Errors: []ValidationDetail{}, Warnings: []ValidationDetail{}, ContentHash: graphContentHash(w.Graph, plan)}, nil
}

func scanRelease(row pgx.Row) (Release, error) {
	var release Release
	var id, workflowID, createdBy string
	if err := row.Scan(&id, &workflowID, &release.VersionNumber, &release.DraftRevision, &release.GraphSchemaVersion, &release.PlanVersion, &release.ContentHash, &createdBy, &release.Notes, &release.CreatedAt); err != nil {
		return release, err
	}
	release.ID, release.WorkflowID, release.CreatedBy, release.Published = id, workflowID, createdBy, true
	return release, nil
}

const releaseColumns = `id::text, workflow_id::text, version_number, draft_revision, graph_schema_version, plan_version, content_hash, created_by::text, notes, created_at`

func (s *Service) ListReleases(ctx context.Context, ws, wid string) ([]Release, error) {
	if _, err := s.Get(ctx, ws, wid); err != nil {
		return nil, err
	}
	rows, err := s.DB.Query(ctx, `SELECT `+releaseColumns+` FROM workflow_release WHERE workspace_id=$1 AND workflow_id=$2 ORDER BY version_number DESC`, ws, wid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]Release, 0)
	for rows.Next() {
		release, scanErr := scanRelease(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		result = append(result, release)
	}
	return result, rows.Err()
}

func (s *Service) Publish(ctx context.Context, ws, user, wid string, expectedRevision int64, notes, idempotencyKey string) (Release, error) {
	if idempotencyKey == "" || len(idempotencyKey) > 200 {
		return Release{}, Bad("idempotency_key is required")
	}
	tx, err := s.Tx.Begin(ctx)
	if err != nil {
		return Release{}, err
	}
	defer tx.Rollback(ctx)
	w, _, _, err := load(ctx, tx, ws, wid, true)
	if err != nil {
		return Release{}, err
	}
	if err = requireWorkflowManager(ctx, tx, user, w); err != nil {
		return Release{}, err
	}
	requestHash := publishRequestHash(wid, w.Graph, notes)
	var existing Release
	existing, err = scanRelease(tx.QueryRow(ctx, `SELECT `+releaseColumns+` FROM workflow_release WHERE workflow_id=$1 AND created_by=$2 AND idempotency_key=$3`, wid, user, idempotencyKey))
	if err == nil {
		var storedHash string
		if hashErr := tx.QueryRow(ctx, `SELECT idempotency_hash FROM workflow_release WHERE id=$1`, existing.ID).Scan(&storedHash); hashErr != nil {
			return Release{}, hashErr
		}
		if storedHash != "" && storedHash != requestHash {
			return Release{}, Conflict("publish idempotency key was already used for a different release request")
		}
		return existing, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Release{}, err
	}
	if w.Revision != expectedRevision {
		return Release{}, Conflict("workflow changed; refresh before publishing")
	}
	if err = requireV2Graph(w.Graph); err != nil {
		return Release{}, err
	}
	if len(w.UpgradeIssues) > 0 {
		return Release{}, &Error{Status: 422, Code: "upgrade_required", Message: "repair the workflow upgrade issues before publishing", Details: append([]ValidationDetail(nil), w.UpgradeIssues...)}
	}
	plan, err := Compile(w.Graph)
	if err != nil {
		return Release{}, err
	}
	if err = s.validateAgents(ctx, ws, user, w.Graph, true); err != nil {
		return Release{}, err
	}
	if err = s.validateMembers(ctx, ws, w.Graph, true); err != nil {
		return Release{}, err
	}
	var version int
	if err = tx.QueryRow(ctx, `SELECT COALESCE(MAX(version_number), 0) + 1 FROM workflow_release WHERE workflow_id=$1`, wid).Scan(&version); err != nil {
		return Release{}, err
	}
	release := Release{
		ID:                 id(),
		WorkflowID:         wid,
		VersionNumber:      version,
		DraftRevision:      w.Revision,
		GraphSchemaVersion: graphVersion(w.Graph),
		PlanVersion:        plan.Version,
		ContentHash:        graphContentHash(w.Graph, plan),
		Notes:              notes,
		CreatedBy:          user,
		CreatedAt:          time.Now().UTC(),
		Published:          true,
	}
	_, err = tx.Exec(ctx, `INSERT INTO workflow_release(id,workspace_id,workflow_id,version_number,draft_revision,graph_schema_version,graph,compiled_plan,plan_version,content_hash,created_by,notes,idempotency_key,idempotency_hash)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`, release.ID, ws, wid, release.VersionNumber, release.DraftRevision, release.GraphSchemaVersion, body(w.Graph), body(plan), release.PlanVersion, release.ContentHash, user, release.Notes, idempotencyKey, requestHash)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			_ = tx.Rollback(ctx)
			return s.loadReleaseByIdempotency(ctx, ws, wid, user, idempotencyKey)
		}
		return Release{}, err
	}
	w.PublishedReleaseID = release.ID
	if w.OwnerID == "" {
		w.OwnerID = user
	}
	if _, err = tx.Exec(ctx, `UPDATE workflow SET body=$3,published_release_id=$4,graph_schema_version=$5,owner_id=COALESCE(owner_id,$6),updated_at=now() WHERE workspace_id=$1 AND id=$2`, ws, wid, body(w), release.ID, release.GraphSchemaVersion, w.OwnerID); err != nil {
		return Release{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Release{}, err
	}
	s.publish("workflow:updated", ws, wid, "")
	return release, nil
}

func graphVersion(graph Graph) int {
	if graph.SchemaVersion == 0 {
		return 1
	}
	return graph.SchemaVersion
}

func (s *Service) loadReleaseByIdempotency(ctx context.Context, ws, wid, user, key string) (Release, error) {
	release, err := scanRelease(s.DB.QueryRow(ctx, `SELECT `+releaseColumns+` FROM workflow_release WHERE workspace_id=$1 AND workflow_id=$2 AND created_by=$3 AND idempotency_key=$4`, ws, wid, user, key))
	if errors.Is(err, pgx.ErrNoRows) {
		return Release{}, Conflict("publish request was already used")
	}
	return release, err
}

func loadRelease(ctx context.Context, q db.DBTX, ws, wid, releaseID string) (Release, Graph, CompiledPlan, error) {
	if _, err := util.ParseUUID(releaseID); err != nil {
		return Release{}, Graph{}, CompiledPlan{}, Bad("invalid release id")
	}
	var release Release
	var graphRaw, planRaw []byte
	var id, workflowID, createdBy string
	err := q.QueryRow(ctx, `SELECT `+releaseColumns+`, graph, compiled_plan
FROM workflow_release WHERE workspace_id=$1 AND workflow_id=$2 AND id=$3`, ws, wid, releaseID).
		Scan(&id, &workflowID, &release.VersionNumber, &release.DraftRevision, &release.GraphSchemaVersion, &release.PlanVersion, &release.ContentHash, &createdBy, &release.Notes, &release.CreatedAt, &graphRaw, &planRaw)
	if errors.Is(err, pgx.ErrNoRows) {
		return Release{}, Graph{}, CompiledPlan{}, &Error{Status: 404, Message: "release not found", Code: "release_not_found"}
	}
	if err != nil {
		return Release{}, Graph{}, CompiledPlan{}, err
	}
	release.ID, release.WorkflowID, release.CreatedBy, release.Published = id, workflowID, createdBy, true
	var graph Graph
	var plan CompiledPlan
	if err = json.Unmarshal(graphRaw, &graph); err != nil {
		return Release{}, Graph{}, CompiledPlan{}, err
	}
	if err = json.Unmarshal(planRaw, &plan); err != nil {
		return Release{}, Graph{}, CompiledPlan{}, err
	}
	return release, graph, plan, nil
}

func (s *Service) GetRelease(ctx context.Context, ws, wid, releaseID string) (Release, Graph, CompiledPlan, error) {
	if _, err := s.Get(ctx, ws, wid); err != nil {
		return Release{}, Graph{}, CompiledPlan{}, err
	}
	return loadRelease(ctx, s.DB, ws, wid, releaseID)
}

func parseReleaseID(value string) (string, error) {
	if _, err := util.ParseUUID(value); err != nil {
		return "", Bad("invalid release id")
	}
	return value, nil
}
