package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/issuestatus"
	"github.com/multica-ai/multica/server/internal/issueworkflow"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Pipeline stage exit modes (spec 3.1).
const (
	PipelineExitAuto   = "auto"
	PipelineExitAttest = "attest"
	PipelineExitGate   = "gate"
)

// PipelineStatusPrefix names the issue status of each stage: the stage "validate"
// is the issue status "ini_validate". The prefix keeps stage statuses apart from
// a workspace's own statuses of the same name.
const PipelineStatusPrefix = "ini_"

// StatusKeyForStage returns the issue status key of a pipeline stage.
func StatusKeyForStage(stageKey string) string { return PipelineStatusPrefix + stageKey }

// PipelineArtifact is one artifact a stage expects (spec 3.1). Kind is a
// knowledge kind key.
type PipelineArtifact struct {
	Kind     string `json:"kind"`
	Required bool   `json:"required"`
}

// PipelineExit says how a stage is left: Mode is auto, attest or gate.
type PipelineExit struct {
	Mode     string `json:"mode"`
	Criteria string `json:"criteria"`
}

// PipelineStage is one stage as stored in pipeline.stages and returned by the
// API. OwnerSquadID is nil for an unowned stage.
type PipelineStage struct {
	Key          string             `json:"key"`
	Name         string             `json:"name"`
	OwnerSquadID *string            `json:"owner_squad_id"`
	Objective    string             `json:"objective"`
	Instructions string             `json:"instructions"`
	Artifacts    []PipelineArtifact `json:"artifacts"`
	Exit         PipelineExit       `json:"exit"`
	SLADays      int                `json:"sla_days"`
}

// PipelineTemplateStage is a stage as the template writes it. OwnerKey is a
// starter-company squad key; the install resolves it to a squad id through the
// caller's resolver, so the service never names a squad itself.
type PipelineTemplateStage struct {
	Key          string
	Name         string
	OwnerKey     string
	Objective    string
	Instructions string
	Artifacts    []PipelineArtifact
	Exit         PipelineExit
	SLADays      int
}

// DefaultPipelineTemplate is the seven-stage pipeline of spec 3.1. Opportunity
// is the front door, a proposal rather than a stage, so it is not listed.
func DefaultPipelineTemplate() []PipelineTemplateStage {
	req := func(kinds ...string) []PipelineArtifact {
		out := make([]PipelineArtifact, len(kinds))
		for i, k := range kinds {
			out[i] = PipelineArtifact{Kind: k, Required: true}
		}
		return out
	}
	return []PipelineTemplateStage{
		{Key: "validate", Name: "Validate", OwnerKey: "product_discovery",
			Objective:    "Decide with evidence whether the opportunity is worth a product.",
			Instructions: "Validate the opportunity: produce the validation report, the competitor profile and the customer insight. Then ask the boss for a go or no-go.",
			Artifacts:    append(req("validation_report", "competitor_profile"), PipelineArtifact{Kind: "customer_insight", Required: false}),
			Exit:         PipelineExit{Mode: PipelineExitGate, Criteria: "A go / no-go recommendation with evidence."},
			SLADays:      14},
		{Key: "define", Name: "Define", OwnerKey: "product_analysis",
			Objective:    "Describe the product and show its economics and feasibility.",
			Instructions: "Write the product brief, the economics and feasibility analysis and the risk register.",
			Artifacts:    req("product_brief", "analysis", "risk_register"),
			Exit:         PipelineExit{Mode: PipelineExitAttest, Criteria: "The owning leader attests the stage is complete."},
			SLADays:      14},
		{Key: "plan", Name: "Plan", OwnerKey: "planning",
			Objective:    "Plan the build, the launch and the budget.",
			Instructions: "Write the PRD, the roadmap, the launch plan outline and the budget request. Then ask the boss to approve the plan and budget.",
			Artifacts:    req("prd", "roadmap", "launch_plan", "budget_request"),
			Exit:         PipelineExit{Mode: PipelineExitGate, Criteria: "Approval of the plan and the budget."},
			SLADays:      14},
		{Key: "build", Name: "Build", OwnerKey: "build",
			Objective:    "Build the product and verify it.",
			Instructions: "Build against the PRD. Record the ADRs, the test report and the release candidate, then attest the QA verdict.",
			Artifacts:    req("adr", "test_report", "release_candidate"),
			Exit:         PipelineExit{Mode: PipelineExitAttest, Criteria: "The owning leader attests the QA verdict is recorded."},
			SLADays:      30},
		{Key: "launch", Name: "Launch", OwnerKey: "release_ops",
			Objective:    "Ship safely and reach the market.",
			Instructions: "Prepare the launch checklist, the release notes and the go-to-market plan with Growth and GTM. Then ask the boss to go live.",
			Artifacts:    req("launch_checklist", "release_notes", "gtm_plan"),
			Exit:         PipelineExit{Mode: PipelineExitGate, Criteria: "Go live. Publishing outside the workspace is the boss's call."},
			SLADays:      14},
		{Key: "operate", Name: "Operate", OwnerKey: "support",
			Objective:    "Run the product and measure it.",
			Instructions: "Publish the weekly metrics report. The boss reviews the initiative at 30 and 90 days: continue, scale or sunset.",
			Artifacts:    req("metrics_report"),
			Exit:         PipelineExit{Mode: PipelineExitGate, Criteria: "Review at 30 and 90 days after launch: continue, scale or sunset."},
			SLADays:      90},
		{Key: "learn", Name: "Learn", OwnerKey: "build",
			Objective:    "Record what the initiative taught the company.",
			Instructions: "Write the retrospective. The initiative closes when it is published.",
			Artifacts:    req("retrospective"),
			Exit:         PipelineExit{Mode: PipelineExitAuto, Criteria: "The retrospective is published."},
			SLADays:      14},
	}
}

// PipelineSquadResolver maps a template squad key to a squad id. ok is false
// when no live squad of the workspace has the key; the stage is then unowned.
type PipelineSquadResolver func(ctx context.Context, squadKey string) (id pgtype.UUID, ok bool, err error)

// PipelineService installs and reads pipelines.
type PipelineService struct {
	Queries   *db.Queries
	TxStarter TxStarter
}

func NewPipelineService(q *db.Queries, tx TxStarter) *PipelineService {
	return &PipelineService{Queries: q, TxStarter: tx}
}

// InstallPipelineParams are the inputs to InstallDefault.
type InstallPipelineParams struct {
	WorkspaceID pgtype.UUID
	// Template is the stage list; nil means DefaultPipelineTemplate.
	Template []PipelineTemplateStage
	// ResolveSquad resolves each stage's owner key. Required.
	ResolveSquad PipelineSquadResolver
}

// InstallPipelineResult reports what the install did. UnownedStages names the
// stages whose owner did not resolve; the install still succeeds.
type InstallPipelineResult struct {
	Pipeline      db.Pipeline
	Created       bool
	UnownedStages []string
}

// ErrNoDefaultPipeline: the workspace has no default pipeline yet.
var ErrNoDefaultPipeline = errors.New("this workspace has no default pipeline")

// InstallDefault installs the default pipeline: the stage statuses, the
// initiative workflow whose steps hand to the owning squads, the board project
// and the pipeline row. It runs in one transaction under a workspace lock, so a
// second call changes nothing except filling an owner that did not resolve
// before. A stage whose owner does not resolve stays unowned and is reported.
func (s *PipelineService) InstallDefault(ctx context.Context, p InstallPipelineParams) (InstallPipelineResult, error) {
	if p.ResolveSquad == nil {
		return InstallPipelineResult{}, errors.New("install pipeline: a squad resolver is required")
	}
	tpl := p.Template
	if tpl == nil {
		tpl = DefaultPipelineTemplate()
	}
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return InstallPipelineResult{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	q := s.Queries.WithTx(tx)

	if err := q.LockPipelineInstall(ctx, p.WorkspaceID); err != nil {
		return InstallPipelineResult{}, fmt.Errorf("lock pipeline install: %w", err)
	}

	existing, err := q.GetDefaultPipeline(ctx, p.WorkspaceID)
	var res InstallPipelineResult
	switch {
	case err == nil:
		res, err = s.fillUnownedStages(ctx, q, existing, tpl, p.ResolveSquad)
		if err != nil {
			return InstallPipelineResult{}, err
		}
	case errors.Is(err, pgx.ErrNoRows):
		res, err = s.createDefault(ctx, q, p.WorkspaceID, tpl, p.ResolveSquad)
		if err != nil {
			return InstallPipelineResult{}, err
		}
	default:
		return InstallPipelineResult{}, fmt.Errorf("load default pipeline: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return InstallPipelineResult{}, fmt.Errorf("commit: %w", err)
	}
	return res, nil
}

// resolveStages turns the template into stored stages, resolving each owner.
func resolveStages(ctx context.Context, tpl []PipelineTemplateStage, resolve PipelineSquadResolver) ([]PipelineStage, map[string]pgtype.UUID, []string, error) {
	stages := make([]PipelineStage, 0, len(tpl))
	owners := make(map[string]pgtype.UUID, len(tpl))
	var unowned []string
	for _, t := range tpl {
		st := PipelineStage{
			Key: t.Key, Name: t.Name, Objective: t.Objective, Instructions: t.Instructions,
			Artifacts: t.Artifacts, Exit: t.Exit, SLADays: t.SLADays,
		}
		if st.Artifacts == nil {
			st.Artifacts = []PipelineArtifact{}
		}
		id, ok, err := resolve(ctx, t.OwnerKey)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("resolve owner %q of stage %q: %w", t.OwnerKey, t.Key, err)
		}
		if ok && id.Valid {
			owners[t.Key] = id
			sid := util.UUIDToString(id)
			st.OwnerSquadID = &sid
		} else {
			unowned = append(unowned, t.Key)
		}
		stages = append(stages, st)
	}
	return stages, owners, unowned, nil
}

func (s *PipelineService) createDefault(ctx context.Context, q *db.Queries, ws pgtype.UUID, tpl []PipelineTemplateStage, resolve PipelineSquadResolver) (InstallPipelineResult, error) {
	if len(tpl) == 0 {
		return InstallPipelineResult{}, errors.New("install pipeline: the template has no stages")
	}
	stages, _, unowned, err := resolveStages(ctx, tpl, resolve)
	if err != nil {
		return InstallPipelineResult{}, err
	}
	pipeline, err := writePipeline(ctx, q, ws, "Default", true, stages)
	if err != nil {
		return InstallPipelineResult{}, err
	}
	return InstallPipelineResult{Pipeline: pipeline, Created: true, UnownedStages: unowned}, nil
}

// writePipeline writes a pipeline with its stages inside q's transaction: the
// stage statuses, the initiative workflow whose steps hand to the owning squads,
// the board project and the pipeline row. The company import writes exported
// pipelines through it as well (spec 4.9).
func writePipeline(ctx context.Context, q *db.Queries, ws pgtype.UUID, name string, isDefault bool, stages []PipelineStage) (db.Pipeline, error) {
	if len(stages) == 0 {
		return db.Pipeline{}, errors.New("install pipeline: the pipeline has no stages")
	}
	if err := issuestatus.Ensure(ctx, q, ws); err != nil {
		return db.Pipeline{}, fmt.Errorf("seed issue statuses: %w", err)
	}
	for _, st := range stages {
		key := StatusKeyForStage(st.Key)
		if _, err := q.GetIssueStatusEntryByKey(ctx, db.GetIssueStatusEntryByKeyParams{WorkspaceID: ws, Key: key}); err == nil {
			continue
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return db.Pipeline{}, fmt.Errorf("load stage status: %w", err)
		}
		if _, err := q.CreateIssueStatusEntry(ctx, db.CreateIssueStatusEntryParams{
			WorkspaceID: ws, Key: key, Name: st.Name, Description: st.Objective,
			Category: issuestatus.CategoryStarted, Color: "#3b82f6", Icon: "circle",
		}); err != nil {
			return db.Pipeline{}, fmt.Errorf("create stage status %q: %w", key, err)
		}
	}

	owners := make(map[string]pgtype.UUID, len(stages))
	for _, st := range stages {
		if st.OwnerSquadID == nil {
			continue
		}
		if id, err := util.ParseUUID(*st.OwnerSquadID); err == nil {
			owners[st.Key] = id
		}
	}
	steps := stepsForStages(stages, owners)
	encoded, err := issueworkflow.EncodeSteps(issueworkflow.Arrange(steps))
	if err != nil {
		return db.Pipeline{}, fmt.Errorf("encode pipeline workflow: %w", err)
	}
	workflow, err := q.CreateIssueWorkflow(ctx, db.CreateIssueWorkflowParams{
		WorkspaceID:      ws,
		Name:             "Initiative pipeline",
		Description:      "The stages of the default pipeline. Each hands its issue to the owning squad.",
		InitialStatusKey: StatusKeyForStage(stages[0].Key),
		Steps:            encoded,
		OwnerID:          pgtype.UUID{},
	})
	if err != nil {
		return db.Pipeline{}, fmt.Errorf("create pipeline workflow: %w", err)
	}
	project, err := q.CreateProject(ctx, db.CreateProjectParams{
		WorkspaceID: ws, Title: "Initiatives", Status: "planned", Priority: "none",
		WorkflowID: workflow.ID,
	})
	if err != nil {
		return db.Pipeline{}, fmt.Errorf("create initiative board project: %w", err)
	}
	stagesJSON, err := marshalStages(stages)
	if err != nil {
		return db.Pipeline{}, err
	}
	pipeline, err := q.CreatePipeline(ctx, db.CreatePipelineParams{
		WorkspaceID: ws, Name: name, WorkflowID: workflow.ID, ProjectID: project.ID,
		Stages: stagesJSON, IsDefault: isDefault,
	})
	if err != nil {
		return db.Pipeline{}, fmt.Errorf("create pipeline %q: %w", name, err)
	}
	return pipeline, nil
}

// fillUnownedStages gives an owner to each unowned stage whose key now resolves.
// Owners already set are never changed, so the install stays idempotent.
func (s *PipelineService) fillUnownedStages(ctx context.Context, q *db.Queries, existing db.Pipeline, tpl []PipelineTemplateStage, resolve PipelineSquadResolver) (InstallPipelineResult, error) {
	var stages []PipelineStage
	if err := decodeStages(existing.Stages, &stages); err != nil {
		return InstallPipelineResult{}, err
	}
	ownerKey := make(map[string]string, len(tpl))
	for _, t := range tpl {
		ownerKey[t.Key] = t.OwnerKey
	}
	owners := map[string]pgtype.UUID{}
	var unowned []string
	changed := false
	for i := range stages {
		if stages[i].OwnerSquadID != nil {
			if id, err := util.ParseUUID(*stages[i].OwnerSquadID); err == nil {
				owners[stages[i].Key] = id
			}
			continue
		}
		key, ok := ownerKey[stages[i].Key]
		if !ok {
			unowned = append(unowned, stages[i].Key)
			continue
		}
		id, found, err := resolve(ctx, key)
		if err != nil {
			return InstallPipelineResult{}, fmt.Errorf("resolve owner %q of stage %q: %w", key, stages[i].Key, err)
		}
		if !found || !id.Valid {
			unowned = append(unowned, stages[i].Key)
			continue
		}
		sid := util.UUIDToString(id)
		stages[i].OwnerSquadID = &sid
		owners[stages[i].Key] = id
		changed = true
	}
	if changed {
		stagesJSON, err := marshalStages(stages)
		if err != nil {
			return InstallPipelineResult{}, err
		}
		wf, err := q.GetIssueWorkflow(ctx, db.GetIssueWorkflowParams{ID: existing.WorkflowID, WorkspaceID: existing.WorkspaceID})
		if err != nil {
			return InstallPipelineResult{}, fmt.Errorf("load pipeline workflow: %w", err)
		}
		def, err := issueworkflow.Decode(wf)
		if err != nil {
			return InstallPipelineResult{}, err
		}
		encoded, err := issueworkflow.EncodeSteps(issueworkflow.Arrange(stepsWithOwners(def.Steps, owners)))
		if err != nil {
			return InstallPipelineResult{}, err
		}
		if _, err := q.UpdateIssueWorkflow(ctx, db.UpdateIssueWorkflowParams{
			ID: wf.ID, WorkspaceID: wf.WorkspaceID, Name: wf.Name, Description: wf.Description,
			InitialStatusKey: wf.InitialStatusKey, Steps: encoded,
		}); err != nil {
			return InstallPipelineResult{}, fmt.Errorf("update pipeline workflow owners: %w", err)
		}
		if existing, err = q.UpdatePipelineStages(ctx, db.UpdatePipelineStagesParams{
			ID: existing.ID, WorkspaceID: existing.WorkspaceID, Stages: stagesJSON,
		}); err != nil {
			return InstallPipelineResult{}, fmt.Errorf("update pipeline owners: %w", err)
		}
	}
	return InstallPipelineResult{Pipeline: existing, Created: false, UnownedStages: unowned}, nil
}

// stepsForStages builds the workflow steps: each stage's status hands to its
// owner, and the stage's instructions go with the handoff.
func stepsForStages(stages []PipelineStage, owners map[string]pgtype.UUID) []issueworkflow.Step {
	steps := make([]issueworkflow.Step, 0, len(stages))
	for i, st := range stages {
		step := issueworkflow.Step{
			StatusKey:    StatusKeyForStage(st.Key),
			Handler:      issueworkflow.Handler{Type: issueworkflow.HandlerNone},
			Instructions: st.Instructions,
		}
		if i+1 < len(stages) {
			step.NextStatusKey = StatusKeyForStage(stages[i+1].Key)
		}
		if i > 0 {
			step.BackStatusKey = StatusKeyForStage(stages[i-1].Key)
		}
		if id, ok := owners[st.Key]; ok {
			step.Handler = issueworkflow.Handler{Type: issueworkflow.HandlerSquad, ID: util.UUIDToString(id)}
		}
		steps = append(steps, step)
	}
	return steps
}

// stepsWithOwners sets the squad handoff of each step from owners and leaves
// the other steps as they are.
func stepsWithOwners(steps []issueworkflow.Step, owners map[string]pgtype.UUID) []issueworkflow.Step {
	out := make([]issueworkflow.Step, len(steps))
	copy(out, steps)
	for i := range out {
		if stage, ok := strings.CutPrefix(out[i].StatusKey, PipelineStatusPrefix); ok {
			if id, ok := owners[stage]; ok {
				out[i].Handler = issueworkflow.Handler{Type: issueworkflow.HandlerSquad, ID: util.UUIDToString(id)}
			}
		}
	}
	return out
}

func marshalStages(stages []PipelineStage) ([]byte, error) {
	b, err := json.Marshal(stages)
	if err != nil {
		return nil, fmt.Errorf("encode pipeline stages: %w", err)
	}
	return b, nil
}

func decodeStages(raw []byte, into *[]PipelineStage) error {
	if err := json.Unmarshal(raw, into); err != nil {
		return fmt.Errorf("decode pipeline stages: %w", err)
	}
	return nil
}
