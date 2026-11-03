package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/issueposition"
	"github.com/multica-ai/multica/server/internal/issueworkflow"
	proposalkinds "github.com/multica-ai/multica/server/internal/service/proposal_kinds"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

// Initiative statuses (spec 3.1). operating is the operate stage and after.
const (
	InitiativeStatusActive    = "active"
	InitiativeStatusShelved   = "shelved"
	InitiativeStatusOperating = "operating"
	InitiativeStatusClosed    = "closed"
)

// Initiative stage keys the service names itself: operate carries the status
// change and the review gates, and learn is where a closed initiative ends up.
const (
	StageOperate = "operate"
	StageLearn   = "learn"
	StageDefine  = "define"
	StageBuild   = "build"
)

// Timeline event kinds, as the initiative_stage_event CHECK allows them.
const (
	InitiativeEventCreated         = "created"
	InitiativeEventAdvanced        = "advanced"
	InitiativeEventSentBack        = "sent_back"
	InitiativeEventShelved         = "shelved"
	InitiativeEventResumed         = "resumed"
	InitiativeEventClosed          = "closed"
	InitiativeEventForced          = "forced"
	InitiativeEventAttested        = "attested"
	InitiativeEventArtifactWaive   = "artifact_waived"
	InitiativeEventArtifactLinked  = "artifact_linked"
	InitiativeEventGateDaysChanged = "gate_days_changed"
)

var (
	// ErrInitiativeNotFound: no such initiative in the workspace.
	ErrInitiativeNotFound = errors.New("initiative not found")
	// ErrInitiativeClosed: the initiative has closed; nothing moves it.
	ErrInitiativeClosed = errors.New("initiative is closed")
	// ErrInitiativeShelved: the initiative is shelved; resume it first.
	ErrInitiativeShelved = errors.New("initiative is shelved; resume it first")
	// ErrInitiativeNotShelved: resume of an initiative that is not shelved.
	ErrInitiativeNotShelved = errors.New("initiative is not shelved")
	// ErrStageNotReady: a required artifact of the stage is neither present nor waived.
	ErrStageNotReady = errors.New("the stage's required artifacts are not all present or waived")
	// ErrStageNeedsAttest: the stage is left by attestation, which has not happened.
	ErrStageNeedsAttest = errors.New("this stage is left by attestation: the owning squad must attest it first")
	// ErrStageIsGate: a gate stage is left by the boss's decision on its gate proposal.
	ErrStageIsGate = errors.New("this stage is left through its gate: the boss decides the gate proposal")
	// ErrNotAttestStage: attest on a stage whose exit is not attestation.
	ErrNotAttestStage = errors.New("this stage does not take an attestation")
	// ErrNoPreviousStage: send-back at the first stage.
	ErrNoPreviousStage = errors.New("the initiative is at its first stage; there is nowhere to send it back to")
	// ErrUnknownStage: the stage key is not one of the pipeline's stages.
	ErrUnknownStage = errors.New("stage is not in this initiative's pipeline")
	// ErrInitiativeNoteRequired: the transition needs a written reason.
	ErrInitiativeNoteRequired = errors.New("a note is required for this change")
	// ErrArtifactNotFound: the stage has no artifact of that kind.
	ErrArtifactNotFound = errors.New("the stage has no artifact of that kind")
	// ErrArtifactNotRequired: only a required artifact can be waived.
	ErrArtifactNotRequired = errors.New("only a required artifact can be waived")
	// ErrGateStale: the gate proposal names a stage the initiative has left.
	ErrGateStale = errors.New("the gate is for a stage the initiative has left; reject it so the squad can refile")
	// ErrOutcomeRequired: an operate-stage review needs continue, scale or sunset.
	ErrOutcomeRequired = errors.New("an operate review needs an outcome: continue, scale or sunset")
	// ErrOutcomeInvalid: the outcome is not one of the three.
	ErrOutcomeInvalid = errors.New("outcome must be continue, scale or sunset")
	// ErrOutcomeNotAllowed: an outcome was given for a gate that has none.
	ErrOutcomeNotAllowed = errors.New("an outcome applies only to an operate-stage review")
	// ErrGateDaysInvalid: the review days are out of range, repeated or too many.
	ErrGateDaysInvalid = errors.New("gate_days takes one to ten days, each from 1 to 365, with no repeats")
)

// StageNotReadyError lists the artifacts still missing. It matches
// ErrStageNotReady under errors.Is.
type StageNotReadyError struct{ Missing []string }

func (e *StageNotReadyError) Error() string {
	return fmt.Sprintf("%s: missing %s", ErrStageNotReady.Error(), strings.Join(e.Missing, ", "))
}

func (e *StageNotReadyError) Is(target error) bool { return target == ErrStageNotReady }

// InitiativeActor is who acts: a member, an agent, or the system (the sweep).
type InitiativeActor struct {
	Type string      // "member", "agent" or "system"
	ID   pgtype.UUID // the user id for a member, the agent id for an agent
}

// CreateInitiativeParams are the already-authorized inputs to Create.
type CreateInitiativeParams struct {
	WorkspaceID pgtype.UUID
	// PipelineID zero means the default pipeline.
	PipelineID pgtype.UUID
	// StageKey zero means the pipeline's first stage. The scale outcome starts a
	// follow-on at define.
	StageKey         string
	Title            string
	Thesis           string
	SourceProposalID pgtype.UUID
	SponsorUserID    pgtype.UUID
	// Actor creates the backing issue, so it must be a member or an agent.
	Actor InitiativeActor
}

// ActionParams are the already-authorized inputs to a transition on one
// initiative.
type ActionParams struct {
	WorkspaceID  pgtype.UUID
	InitiativeID pgtype.UUID
	Actor        InitiativeActor
	Note         string
	// ToStage is the target of a force.
	ToStage string
	// ArtifactKind names the artifact of a waiver.
	ArtifactKind string
}

// InitiativeService moves initiatives through their pipelines. It implements
// proposalkinds.Initiatives, so approved opportunities and gates reach it inside
// the decision transaction.
type InitiativeService struct {
	Queries   *db.Queries
	TxStarter TxStarter
	Issues    *IssueService
	// Proposals files the gate proposals the sweep raises.
	Proposals *ProposalService
	// Retro is called when an initiative closes. See initiative_operate.go.
	Retro RetroHook
}

// NewInitiativeSet wires an initiative service and the proposal service that
// files its gates and decides them. A nil retro uses NoopRetroHook.
func NewInitiativeSet(issues *IssueService, retro RetroHook) (*InitiativeService, *ProposalService) {
	if retro == nil {
		retro = NoopRetroHook{}
	}
	ini := &InitiativeService{Queries: issues.Queries, TxStarter: issues.TxStarter, Issues: issues, Retro: retro}
	props := NewProposalService(issues, ini)
	ini.Proposals = props
	return ini, props
}

// inTx runs fn in one transaction and commits when it returns nil.
func (s *InitiativeService) inTx(ctx context.Context, fn func(tx pgx.Tx, q *db.Queries) error) error {
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	if err := fn(tx, s.Queries.WithTx(tx)); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// Create starts an initiative at its first stage (or the named one): the
// backing issue in the pipeline's project, the stage's artifact checklist, a
// created event, and a note that wakes the owning squad's leader.
func (s *InitiativeService) Create(ctx context.Context, p CreateInitiativeParams) (db.Initiative, error) {
	var out db.Initiative
	err := s.inTx(ctx, func(tx pgx.Tx, q *db.Queries) error {
		var err error
		out, err = s.openInTx(ctx, tx, q, p)
		return err
	})
	return out, err
}

// OpenFromOpportunity creates the initiative an approved opportunity asks for,
// inside the decision transaction.
func (s *InitiativeService) OpenFromOpportunity(ctx context.Context, q *db.Queries, d proposalkinds.Decision, o proposalkinds.Opportunity) error {
	_, err := s.openInTx(ctx, d.Tx, q, CreateInitiativeParams{
		WorkspaceID:      d.WorkspaceID,
		StageKey:         o.TargetStage,
		Title:            o.Title,
		Thesis:           o.Thesis,
		SourceProposalID: d.Proposal.ID,
		SponsorUserID:    d.DeciderID,
		Actor:            InitiativeActor{Type: "member", ID: d.DeciderID},
	})
	return err
}

func (s *InitiativeService) openInTx(ctx context.Context, tx pgx.Tx, q *db.Queries, p CreateInitiativeParams) (db.Initiative, error) {
	if p.Actor.Type != "member" && p.Actor.Type != "agent" {
		return db.Initiative{}, errors.New("an initiative is created by a member or an agent")
	}
	if err := q.LockInitiativeWorkspace(ctx, p.WorkspaceID); err != nil {
		return db.Initiative{}, fmt.Errorf("lock initiative workspace: %w", err)
	}
	pipe, err := s.pipelineForCreate(ctx, q, p.WorkspaceID, p.PipelineID)
	if err != nil {
		return db.Initiative{}, err
	}
	var stages []PipelineStage
	if err := decodeStages(pipe.Stages, &stages); err != nil {
		return db.Initiative{}, err
	}
	stageKey := p.StageKey
	if stageKey == "" && len(stages) > 0 {
		stageKey = stages[0].Key
	}
	idx := stageIndex(stages, stageKey)
	if idx < 0 {
		return db.Initiative{}, ErrUnknownStage
	}
	def, err := s.workflowOf(ctx, q, pipe)
	if err != nil {
		return db.Initiative{}, err
	}

	seq, err := q.NextInitiativeSeq(ctx, p.WorkspaceID)
	if err != nil {
		return db.Initiative{}, fmt.Errorf("next initiative seq: %w", err)
	}
	statusKey := StatusKeyForStage(stageKey)
	assigneeType, assigneeID, hasOwner := handoffFor(def, statusKey, p.Actor.Type, p.Actor.ID)

	policy := ResolveIssueCountPolicy(ctx, s.Issues.Entitlements, p.WorkspaceID)
	number, err := AllocateIssueNumber(ctx, q, p.WorkspaceID, policy)
	if err != nil {
		return db.Initiative{}, err
	}
	position, err := issueposition.NextTopPosition(ctx, tx, p.WorkspaceID, statusKey)
	if err != nil {
		return db.Initiative{}, fmt.Errorf("next top position: %w", err)
	}
	issueID := dbid.NewV7()
	issue, err := q.CreateIssueWithOrigin(ctx, db.CreateIssueWithOriginParams{
		ID:           issueID,
		WorkspaceID:  p.WorkspaceID,
		Title:        p.Title,
		Description:  pgtype.Text{String: initiativeDescription(p.Thesis, int(seq)), Valid: true},
		Status:       statusKey,
		Priority:     "none",
		AssigneeType: assigneeType,
		AssigneeID:   assigneeID,
		CreatorType:  p.Actor.Type,
		CreatorID:    p.Actor.ID,
		Position:     position,
		Number:       number,
		ProjectID:    pipe.ProjectID,
	})
	if err != nil {
		return db.Initiative{}, fmt.Errorf("create initiative issue: %w", err)
	}

	ini, err := q.CreateInitiative(ctx, db.CreateInitiativeParams{
		ID:               dbid.NewV7(),
		WorkspaceID:      p.WorkspaceID,
		PipelineID:       pipe.ID,
		Seq:              seq,
		Title:            p.Title,
		Thesis:           p.Thesis,
		StageKey:         stageKey,
		OwnerSquadID:     ownerIfHanded(assigneeType, assigneeID, hasOwner),
		IssueID:          issue.ID,
		SourceProposalID: p.SourceProposalID,
		SponsorUserID:    p.SponsorUserID,
	})
	if err != nil {
		return db.Initiative{}, fmt.Errorf("create initiative: %w", err)
	}
	if err := s.placeOnStage(ctx, q, ini, stages[idx]); err != nil {
		return db.Initiative{}, err
	}
	if err := s.recordEvent(ctx, q, ini, InitiativeEventCreated, "", stageKey, p.Actor, "", pgtype.UUID{}); err != nil {
		return db.Initiative{}, err
	}
	if hasOwner {
		s.wakeOwner(ctx, q, issue, ini.OwnerSquadID, p.Actor, stageNote(stages[idx], ini))
	}
	return ini, nil
}

// initiativeDescription is the backing issue's body: the thesis and the link
// to the initiative's identifier.
func initiativeDescription(thesis string, seq int) string {
	var b strings.Builder
	b.WriteString("## Thesis\n\n")
	b.WriteString(strings.TrimSpace(thesis))
	fmt.Fprintf(&b, "\n\nThis issue tracks initiative INI-%d. Its stage is the issue's status.\n", seq)
	return b.String()
}

// stageNote is the text the owning leader reads when a stage is entered.
func stageNote(stage PipelineStage, ini db.Initiative) string {
	var b strings.Builder
	fmt.Fprintf(&b, "The initiative **%s** is now at the **%s** stage.\n\n", ini.Title, stage.Name)
	if ini.Thesis != "" {
		b.WriteString("Thesis: " + ini.Thesis + "\n\n")
	}
	if stage.Objective != "" {
		b.WriteString("Objective: " + stage.Objective + "\n\n")
	}
	if stage.Instructions != "" {
		b.WriteString(stage.Instructions + "\n\n")
	}
	if stage.Exit.Criteria != "" {
		b.WriteString("To leave this stage: " + stage.Exit.Criteria + "\n")
	}
	return b.String()
}

// pipelineForCreate loads the named pipeline, or the default one.
func (s *InitiativeService) pipelineForCreate(ctx context.Context, q *db.Queries, ws, pipelineID pgtype.UUID) (db.Pipeline, error) {
	if pipelineID.Valid {
		pipe, err := q.GetPipelineInWorkspace(ctx, db.GetPipelineInWorkspaceParams{ID: pipelineID, WorkspaceID: ws})
		if errors.Is(err, pgx.ErrNoRows) {
			return db.Pipeline{}, ErrNoDefaultPipeline
		}
		return pipe, err
	}
	pipe, err := q.GetDefaultPipeline(ctx, ws)
	if errors.Is(err, pgx.ErrNoRows) {
		return db.Pipeline{}, ErrNoDefaultPipeline
	}
	return pipe, err
}

// workflowOf loads and decodes the pipeline's workflow.
func (s *InitiativeService) workflowOf(ctx context.Context, q *db.Queries, pipe db.Pipeline) (issueworkflow.Definition, error) {
	row, err := q.GetIssueWorkflow(ctx, db.GetIssueWorkflowParams{ID: pipe.WorkflowID, WorkspaceID: pipe.WorkspaceID})
	if err != nil {
		return issueworkflow.Definition{}, fmt.Errorf("load pipeline workflow: %w", err)
	}
	return issueworkflow.Decode(row)
}

// handoffFor resolves the workflow step's handler for a status. The step hands
// the issue to the owning squad; hasOwner is false when the stage is unowned.
func handoffFor(def issueworkflow.Definition, statusKey, creatorType string, creatorID pgtype.UUID) (pgtype.Text, pgtype.UUID, bool) {
	step, ok := def.Step(statusKey)
	if !ok {
		return pgtype.Text{}, pgtype.UUID{}, false
	}
	t, id, ok := issueworkflow.ResolveHandler(step, db.Project{}, creatorType, creatorID)
	if !ok {
		return pgtype.Text{}, pgtype.UUID{}, false
	}
	return pgtype.Text{String: t, Valid: true}, id, true
}

// ownerIfHanded is the owning squad when the handoff named a squad.
func ownerIfHanded(assigneeType pgtype.Text, assigneeID pgtype.UUID, hasOwner bool) pgtype.UUID {
	if hasOwner && assigneeType.String == "squad" {
		return assigneeID
	}
	return pgtype.UUID{}
}

func stageIndex(stages []PipelineStage, key string) int {
	for i, s := range stages {
		if s.Key == key {
			return i
		}
	}
	return -1
}

// placeOnStage writes the stage's artifact checklist. Rows already present keep
// their links and waivers.
func (s *InitiativeService) placeOnStage(ctx context.Context, q *db.Queries, ini db.Initiative, stage PipelineStage) error {
	for _, a := range stage.Artifacts {
		if err := q.CreateInitiativeArtifact(ctx, db.CreateInitiativeArtifactParams{
			WorkspaceID: ini.WorkspaceID, InitiativeID: ini.ID, StageKey: stage.Key,
			Kind: a.Kind, Required: a.Required,
		}); err != nil {
			return fmt.Errorf("create artifact %q: %w", a.Kind, err)
		}
	}
	return nil
}

// recordEvent appends one row to the initiative timeline.
func (s *InitiativeService) recordEvent(ctx context.Context, q *db.Queries, ini db.Initiative, kind, from, to string, actor InitiativeActor, note string, gate pgtype.UUID) error {
	_, err := q.CreateInitiativeStageEvent(ctx, db.CreateInitiativeStageEventParams{
		WorkspaceID:    ini.WorkspaceID,
		InitiativeID:   ini.ID,
		FromStage:      textOrNull(from),
		ToStage:        textOrNull(to),
		Kind:           kind,
		ActorType:      actor.Type,
		ActorID:        actorUUID(actor),
		GateProposalID: gate,
		Note:           strings.TrimSpace(note),
	})
	if err != nil {
		return fmt.Errorf("record initiative event: %w", err)
	}
	return nil
}

func textOrNull(s string) pgtype.Text {
	return pgtype.Text{String: s, Valid: s != ""}
}

// actorUUID is the actor's id, or NULL for the system.
func actorUUID(a InitiativeActor) pgtype.UUID {
	if a.Type == "system" {
		return pgtype.UUID{}
	}
	return a.ID
}

// wakeOwner posts text on the issue, mentioning the owning squad's leader with
// the markdown the mention path parses, so the leader is woken. A squad that no
// longer resolves wakes nobody and the failure is not fatal.
func (s *InitiativeService) wakeOwner(ctx context.Context, q *db.Queries, issue db.Issue, owner pgtype.UUID, actor InitiativeActor, text string) {
	if !owner.Valid {
		return
	}
	content := text
	if squad, err := q.GetSquadInWorkspace(ctx, db.GetSquadInWorkspaceParams{ID: owner, WorkspaceID: issue.WorkspaceID}); err == nil {
		if leader, err := q.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{ID: squad.LeaderID, WorkspaceID: issue.WorkspaceID}); err == nil {
			content = fmt.Sprintf("[@%s](mention://agent/%s) %s", leader.Name, util.UUIDToString(leader.ID), text)
		}
	}
	authorType, authorID := actor.Type, actor.ID
	if actor.Type == "system" {
		authorType, authorID = "system", pgtype.UUID{Valid: true}
	}
	if _, err := q.CreateComment(ctx, db.CreateCommentParams{
		ID:          dbid.NewV7(),
		IssueID:     issue.ID,
		WorkspaceID: issue.WorkspaceID,
		AuthorType:  authorType,
		AuthorID:    authorID,
		Content:     content,
		Type:        "comment",
	}); err != nil {
		// The transition has happened; the leader hears about it on the next wake.
		slog.Warn("initiative: could not post the note to the owning leader", "issue_id", util.UUIDToString(issue.ID), "error", err)
	}
}

// moveTo changes an initiative's stage: the backing issue's status and handoff,
// the stage's checklist, the event, and the note to the new owner. It is the
// only way a stage changes, so every move leaves the same trail.
func (s *InitiativeService) moveTo(ctx context.Context, q *db.Queries, ini db.Initiative, pipe db.Pipeline, stages []PipelineStage, toKey, kind string, actor InitiativeActor, note string, gate pgtype.UUID, now time.Time) (db.Initiative, error) {
	idx := stageIndex(stages, toKey)
	if idx < 0 {
		return db.Initiative{}, ErrUnknownStage
	}
	stage := stages[idx]
	def, err := s.workflowOf(ctx, q, pipe)
	if err != nil {
		return db.Initiative{}, err
	}
	issue, err := q.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{ID: ini.IssueID, WorkspaceID: ini.WorkspaceID})
	if err != nil {
		return db.Initiative{}, fmt.Errorf("load initiative issue: %w", err)
	}
	statusKey := StatusKeyForStage(toKey)
	assigneeType, assigneeID, hasOwner := handoffFor(def, statusKey, issue.CreatorType, issue.CreatorID)

	if _, err := q.UpdateIssueStatus(ctx, db.UpdateIssueStatusParams{ID: issue.ID, WorkspaceID: ini.WorkspaceID, Status: statusKey}); err != nil {
		return db.Initiative{}, fmt.Errorf("move initiative issue: %w", err)
	}
	if hasOwner {
		if _, err := q.UpdateIssue(ctx, reassignIssueParams(issue, assigneeID)); err != nil {
			return db.Initiative{}, fmt.Errorf("hand initiative issue to owner: %w", err)
		}
	}
	if err := s.placeOnStage(ctx, q, ini, stage); err != nil {
		return db.Initiative{}, err
	}

	from := ini.StageKey
	owner := ownerIfHanded(assigneeType, assigneeID, hasOwner)
	buildProject := ini.BuildProjectID
	if toKey == StageBuild && !buildProject.Valid {
		if buildProject, err = s.startBuild(ctx, q, ini, issue, owner, actor); err != nil {
			return db.Initiative{}, err
		}
	}
	status := InitiativeStatusActive
	operatingSince := ini.OperatingSince
	if toKey == StageOperate {
		status = InitiativeStatusOperating
		if !operatingSince.Valid {
			operatingSince = pgtype.Timestamptz{Time: now, Valid: true}
		}
	}
	updated, err := q.UpdateInitiativeState(ctx, db.UpdateInitiativeStateParams{
		ID: ini.ID, WorkspaceID: ini.WorkspaceID,
		StageKey:       toKey,
		OwnerSquadID:   owner,
		Status:         status,
		StageEnteredAt: pgtype.Timestamptz{Time: now, Valid: true},
		ShelvedAt:      ini.ShelvedAt,
		OperatingSince: operatingSince,
		BuildProjectID: buildProject,
		ClosedAt:       ini.ClosedAt,
		ClosedReason:   ini.ClosedReason,
	})
	if err != nil {
		return db.Initiative{}, fmt.Errorf("update initiative stage: %w", err)
	}
	if toKey == StageOperate && !ini.OperatingSince.Valid {
		// The first entry to operate is the launch: the weekly metrics routine starts.
		if err := s.startMetricsRoutine(ctx, q, updated, owner, actor, now); err != nil {
			return db.Initiative{}, err
		}
	}
	if err := s.recordEvent(ctx, q, updated, kind, from, toKey, actor, note, gate); err != nil {
		return db.Initiative{}, err
	}
	text := stageNote(stage, updated)
	if kind == InitiativeEventSentBack && strings.TrimSpace(note) != "" {
		text = "The boss sent this back to the " + stage.Name + " stage: " + strings.TrimSpace(note) + "\n\n" + text
	}
	s.wakeOwner(ctx, q, issue, updated.OwnerSquadID, actor, text)
	return updated, nil
}

// lockInitiative loads an initiative under a row lock for a transition.
func (s *InitiativeService) lockInitiative(ctx context.Context, q *db.Queries, ws, id pgtype.UUID) (db.Initiative, error) {
	ini, err := q.LockInitiativeInWorkspace(ctx, db.LockInitiativeInWorkspaceParams{ID: id, WorkspaceID: ws})
	if errors.Is(err, pgx.ErrNoRows) {
		return db.Initiative{}, ErrInitiativeNotFound
	}
	return ini, err
}

// pipelineOf loads an initiative's pipeline and its stages.
func (s *InitiativeService) pipelineOf(ctx context.Context, q *db.Queries, ini db.Initiative) (db.Pipeline, []PipelineStage, error) {
	pipe, err := q.GetPipelineInWorkspace(ctx, db.GetPipelineInWorkspaceParams{ID: ini.PipelineID, WorkspaceID: ini.WorkspaceID})
	if err != nil {
		return db.Pipeline{}, nil, fmt.Errorf("load initiative pipeline: %w", err)
	}
	var stages []PipelineStage
	if err := decodeStages(pipe.Stages, &stages); err != nil {
		return db.Pipeline{}, nil, err
	}
	return pipe, stages, nil
}

// live refuses a transition on a closed initiative, and on a shelved one unless
// the transition is the resume.
func live(ini db.Initiative) error {
	switch ini.Status {
	case InitiativeStatusClosed:
		return ErrInitiativeClosed
	case InitiativeStatusShelved:
		return ErrInitiativeShelved
	}
	return nil
}

// missingRequired lists the required artifact kinds of the stage that are
// neither present nor waived.
func (s *InitiativeService) missingRequired(ctx context.Context, q *db.Queries, ini db.Initiative) ([]string, error) {
	rows, err := q.ListInitiativeArtifacts(ctx, db.ListInitiativeArtifactsParams{WorkspaceID: ini.WorkspaceID, InitiativeID: ini.ID})
	if err != nil {
		return nil, fmt.Errorf("list initiative artifacts: %w", err)
	}
	var missing []string
	for _, r := range rows {
		if r.StageKey != ini.StageKey || !r.Required {
			continue
		}
		if !artifactSatisfied(r) {
			missing = append(missing, r.Kind)
		}
	}
	return missing, nil
}

// artifactSatisfied: a published document is linked, or the requirement is waived.
func artifactSatisfied(r db.InitiativeArtifact) bool {
	return r.KnowledgeLinkID.Valid || r.WaivedBy.Valid
}

// Advance moves an initiative past its current stage when the stage's exit
// allows it: auto, or attest after the owning squad attested. A gate stage is
// left only through its gate proposal. Advancing past learn closes the
// initiative as completed.
func (s *InitiativeService) Advance(ctx context.Context, p ActionParams) (db.Initiative, error) {
	var out db.Initiative
	err := s.inTx(ctx, func(tx pgx.Tx, q *db.Queries) error {
		ini, err := s.lockInitiative(ctx, q, p.WorkspaceID, p.InitiativeID)
		if err != nil {
			return err
		}
		if err := live(ini); err != nil {
			return err
		}
		pipe, stages, err := s.pipelineOf(ctx, q, ini)
		if err != nil {
			return err
		}
		out, err = s.advanceLocked(ctx, q, ini, pipe, stages, p.Actor, p.Note, pgtype.UUID{}, time.Now())
		return err
	})
	return out, err
}

// advanceLocked is Advance on a locked initiative. It is shared with the sweep
// and with the attest path, which advances in the same transaction.
func (s *InitiativeService) advanceLocked(ctx context.Context, q *db.Queries, ini db.Initiative, pipe db.Pipeline, stages []PipelineStage, actor InitiativeActor, note string, gate pgtype.UUID, now time.Time) (db.Initiative, error) {
	idx := stageIndex(stages, ini.StageKey)
	if idx < 0 {
		return db.Initiative{}, ErrUnknownStage
	}
	// The artifacts are checked first: a missing document is the reason the
	// leader can act on, whatever the exit mode.
	missing, err := s.missingRequired(ctx, q, ini)
	if err != nil {
		return db.Initiative{}, err
	}
	if len(missing) > 0 {
		return db.Initiative{}, &StageNotReadyError{Missing: missing}
	}
	stage := stages[idx]
	switch stage.Exit.Mode {
	case PipelineExitGate:
		return db.Initiative{}, ErrStageIsGate
	case PipelineExitAttest:
		if !s.attestedSince(ctx, q, ini) {
			return db.Initiative{}, ErrStageNeedsAttest
		}
	}
	if idx+1 >= len(stages) {
		return s.closeLocked(ctx, q, ini, pipe, stages, actor, "completed", note, now)
	}
	return s.moveTo(ctx, q, ini, pipe, stages, stages[idx+1].Key, InitiativeEventAdvanced, actor, note, gate, now)
}

// attestedSince reports whether the owning squad attested the current stage
// after it was entered.
func (s *InitiativeService) attestedSince(ctx context.Context, q *db.Queries, ini db.Initiative) bool {
	ev, err := q.LatestInitiativeEventOfKind(ctx, db.LatestInitiativeEventOfKindParams{
		WorkspaceID: ini.WorkspaceID, InitiativeID: ini.ID, Kind: InitiativeEventAttested,
	})
	if err != nil {
		return false
	}
	return ev.ToStage.String == ini.StageKey && !ev.CreatedAt.Time.Before(ini.StageEnteredAt.Time)
}

// Attest records that the owning squad has finished the current stage. If the
// artifacts are already in place the initiative advances in the same step.
func (s *InitiativeService) Attest(ctx context.Context, p ActionParams) (db.Initiative, error) {
	var out db.Initiative
	err := s.inTx(ctx, func(tx pgx.Tx, q *db.Queries) error {
		ini, err := s.lockInitiative(ctx, q, p.WorkspaceID, p.InitiativeID)
		if err != nil {
			return err
		}
		if err := live(ini); err != nil {
			return err
		}
		pipe, stages, err := s.pipelineOf(ctx, q, ini)
		if err != nil {
			return err
		}
		idx := stageIndex(stages, ini.StageKey)
		if idx < 0 {
			return ErrUnknownStage
		}
		if stages[idx].Exit.Mode != PipelineExitAttest {
			return ErrNotAttestStage
		}
		if err := s.recordEvent(ctx, q, ini, InitiativeEventAttested, "", ini.StageKey, p.Actor, p.Note, pgtype.UUID{}); err != nil {
			return err
		}
		out = ini
		if missing, err := s.missingRequired(ctx, q, ini); err != nil {
			return err
		} else if len(missing) > 0 {
			return nil
		}
		out, err = s.advanceLocked(ctx, q, ini, pipe, stages, p.Actor, p.Note, pgtype.UUID{}, time.Now())
		return err
	})
	return out, err
}

// SendBack returns the initiative to the previous stage with a note for the
// leader. A note is required.
func (s *InitiativeService) SendBack(ctx context.Context, p ActionParams) (db.Initiative, error) {
	if strings.TrimSpace(p.Note) == "" {
		return db.Initiative{}, ErrInitiativeNoteRequired
	}
	var out db.Initiative
	err := s.inTx(ctx, func(tx pgx.Tx, q *db.Queries) error {
		ini, err := s.lockInitiative(ctx, q, p.WorkspaceID, p.InitiativeID)
		if err != nil {
			return err
		}
		if err := live(ini); err != nil {
			return err
		}
		pipe, stages, err := s.pipelineOf(ctx, q, ini)
		if err != nil {
			return err
		}
		idx := stageIndex(stages, ini.StageKey)
		if idx <= 0 {
			return ErrNoPreviousStage
		}
		out, err = s.moveTo(ctx, q, ini, pipe, stages, stages[idx-1].Key, InitiativeEventSentBack, p.Actor, p.Note, pgtype.UUID{}, time.Now())
		return err
	})
	return out, err
}

// Force moves the initiative to any stage of its pipeline. The reason is required.
func (s *InitiativeService) Force(ctx context.Context, p ActionParams) (db.Initiative, error) {
	if strings.TrimSpace(p.Note) == "" {
		return db.Initiative{}, ErrInitiativeNoteRequired
	}
	var out db.Initiative
	err := s.inTx(ctx, func(tx pgx.Tx, q *db.Queries) error {
		ini, err := s.lockInitiative(ctx, q, p.WorkspaceID, p.InitiativeID)
		if err != nil {
			return err
		}
		if err := live(ini); err != nil {
			return err
		}
		pipe, stages, err := s.pipelineOf(ctx, q, ini)
		if err != nil {
			return err
		}
		if stageIndex(stages, p.ToStage) < 0 {
			return ErrUnknownStage
		}
		out, err = s.moveTo(ctx, q, ini, pipe, stages, p.ToStage, InitiativeEventForced, p.Actor, p.Note, pgtype.UUID{}, time.Now())
		return err
	})
	return out, err
}

// Shelve pauses an initiative: its stage clock stops and it leaves the active
// board until it is resumed.
func (s *InitiativeService) Shelve(ctx context.Context, p ActionParams) (db.Initiative, error) {
	var out db.Initiative
	err := s.inTx(ctx, func(tx pgx.Tx, q *db.Queries) error {
		ini, err := s.lockInitiative(ctx, q, p.WorkspaceID, p.InitiativeID)
		if err != nil {
			return err
		}
		if err := live(ini); err != nil {
			return err
		}
		now := time.Now()
		out, err = q.UpdateInitiativeState(ctx, db.UpdateInitiativeStateParams{
			ID: ini.ID, WorkspaceID: ini.WorkspaceID, StageKey: ini.StageKey,
			OwnerSquadID: ini.OwnerSquadID, Status: InitiativeStatusShelved,
			StageEnteredAt: ini.StageEnteredAt,
			ShelvedAt:      pgtype.Timestamptz{Time: now, Valid: true},
			OperatingSince: ini.OperatingSince, BuildProjectID: ini.BuildProjectID,
			ClosedAt: ini.ClosedAt, ClosedReason: ini.ClosedReason,
		})
		if err != nil {
			return fmt.Errorf("shelve initiative: %w", err)
		}
		return s.recordEvent(ctx, q, out, InitiativeEventShelved, ini.StageKey, ini.StageKey, p.Actor, p.Note, pgtype.UUID{})
	})
	return out, err
}

// Resume brings a shelved initiative back. The time it was shelved does not
// count against its stage's SLA.
func (s *InitiativeService) Resume(ctx context.Context, p ActionParams) (db.Initiative, error) {
	var out db.Initiative
	err := s.inTx(ctx, func(tx pgx.Tx, q *db.Queries) error {
		ini, err := s.lockInitiative(ctx, q, p.WorkspaceID, p.InitiativeID)
		if err != nil {
			return err
		}
		if ini.Status != InitiativeStatusShelved {
			return ErrInitiativeNotShelved
		}
		now := time.Now()
		entered := ini.StageEnteredAt.Time
		if ini.ShelvedAt.Valid {
			entered = entered.Add(now.Sub(ini.ShelvedAt.Time))
		}
		status := InitiativeStatusActive
		if ini.StageKey == StageOperate {
			status = InitiativeStatusOperating
		}
		out, err = q.UpdateInitiativeState(ctx, db.UpdateInitiativeStateParams{
			ID: ini.ID, WorkspaceID: ini.WorkspaceID, StageKey: ini.StageKey,
			OwnerSquadID: ini.OwnerSquadID, Status: status,
			StageEnteredAt: pgtype.Timestamptz{Time: entered, Valid: true},
			OperatingSince: ini.OperatingSince, BuildProjectID: ini.BuildProjectID,
			ClosedAt: ini.ClosedAt, ClosedReason: ini.ClosedReason,
		})
		if err != nil {
			return fmt.Errorf("resume initiative: %w", err)
		}
		return s.recordEvent(ctx, q, out, InitiativeEventResumed, ini.StageKey, ini.StageKey, p.Actor, p.Note, pgtype.UUID{})
	})
	return out, err
}

// Close ends an initiative with a reason. The backing issue is done and the
// retrospective hook runs in the same transaction.
func (s *InitiativeService) Close(ctx context.Context, p ActionParams) (db.Initiative, error) {
	if strings.TrimSpace(p.Note) == "" {
		return db.Initiative{}, ErrInitiativeNoteRequired
	}
	var out db.Initiative
	err := s.inTx(ctx, func(tx pgx.Tx, q *db.Queries) error {
		ini, err := s.lockInitiative(ctx, q, p.WorkspaceID, p.InitiativeID)
		if err != nil {
			return err
		}
		if ini.Status == InitiativeStatusClosed {
			return ErrInitiativeClosed
		}
		pipe, stages, err := s.pipelineOf(ctx, q, ini)
		if err != nil {
			return err
		}
		out, err = s.closeLocked(ctx, q, ini, pipe, stages, p.Actor, p.Note, p.Note, time.Now())
		return err
	})
	return out, err
}

// closeLocked closes an initiative. The backing issue goes to done, the close is
// recorded, and the retrospective hook is called inside the transaction.
func (s *InitiativeService) closeLocked(ctx context.Context, q *db.Queries, ini db.Initiative, pipe db.Pipeline, stages []PipelineStage, actor InitiativeActor, reason, note string, now time.Time) (db.Initiative, error) {
	issue, err := q.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{ID: ini.IssueID, WorkspaceID: ini.WorkspaceID})
	if err != nil {
		return db.Initiative{}, fmt.Errorf("load initiative issue: %w", err)
	}
	if _, err := q.UpdateIssueStatus(ctx, db.UpdateIssueStatusParams{ID: issue.ID, WorkspaceID: ini.WorkspaceID, Status: "done"}); err != nil {
		return db.Initiative{}, fmt.Errorf("close initiative issue: %w", err)
	}
	out, err := q.UpdateInitiativeState(ctx, db.UpdateInitiativeStateParams{
		ID: ini.ID, WorkspaceID: ini.WorkspaceID, StageKey: ini.StageKey,
		OwnerSquadID: ini.OwnerSquadID, Status: InitiativeStatusClosed,
		StageEnteredAt: ini.StageEnteredAt, ShelvedAt: ini.ShelvedAt,
		OperatingSince: ini.OperatingSince, BuildProjectID: ini.BuildProjectID,
		ClosedAt:     pgtype.Timestamptz{Time: now, Valid: true},
		ClosedReason: strings.TrimSpace(reason),
	})
	if err != nil {
		return db.Initiative{}, fmt.Errorf("close initiative: %w", err)
	}
	if err := s.recordEvent(ctx, q, out, InitiativeEventClosed, ini.StageKey, ini.StageKey, actor, note, pgtype.UUID{}); err != nil {
		return db.Initiative{}, err
	}
	if err := s.Retro.OnInitiativeClosed(ctx, q, out, reason); err != nil {
		return db.Initiative{}, fmt.Errorf("retrospective hook: %w", err)
	}
	return out, nil
}

// WaiveArtifact accepts a required artifact of the current stage without its
// document. The waiver and its note are recorded.
func (s *InitiativeService) WaiveArtifact(ctx context.Context, p ActionParams) (db.InitiativeArtifact, error) {
	if strings.TrimSpace(p.Note) == "" {
		return db.InitiativeArtifact{}, ErrInitiativeNoteRequired
	}
	var out db.InitiativeArtifact
	err := s.inTx(ctx, func(tx pgx.Tx, q *db.Queries) error {
		ini, err := s.lockInitiative(ctx, q, p.WorkspaceID, p.InitiativeID)
		if err != nil {
			return err
		}
		if err := live(ini); err != nil {
			return err
		}
		row, err := q.GetInitiativeArtifact(ctx, db.GetInitiativeArtifactParams{
			WorkspaceID: ini.WorkspaceID, InitiativeID: ini.ID, StageKey: ini.StageKey, Kind: p.ArtifactKind,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrArtifactNotFound
		}
		if err != nil {
			return fmt.Errorf("load artifact: %w", err)
		}
		if !row.Required {
			return ErrArtifactNotRequired
		}
		out, err = q.WaiveInitiativeArtifact(ctx, db.WaiveInitiativeArtifactParams{
			WorkspaceID: ini.WorkspaceID, InitiativeID: ini.ID, StageKey: ini.StageKey, Kind: p.ArtifactKind,
			WaivedBy: actorUUID(p.Actor), WaivedNote: strings.TrimSpace(p.Note),
		})
		if err != nil {
			return fmt.Errorf("waive artifact: %w", err)
		}
		return s.recordEvent(ctx, q, ini, InitiativeEventArtifactWaive, ini.StageKey, ini.StageKey, p.Actor, p.Note, pgtype.UUID{})
	})
	return out, err
}

// LinkArtifact takes a published document into the initiative's checklist when
// its kind is a required artifact of the current stage. It reports false, and
// writes nothing, for a kind the stage does not require (a wrong kind, or an
// optional artifact) and for a closed initiative. A link appends an
// artifact_linked event. Then an auto stage whose artifacts are now complete
// advances, as the sweep would do; a failure there leaves the link in place.
// The knowledge service calls it when a document with this initiative is
// published.
func (s *InitiativeService) LinkArtifact(ctx context.Context, ws, initiativeID, linkID pgtype.UUID, kind string, actor InitiativeActor) (bool, error) {
	linked := false
	err := s.inTx(ctx, func(tx pgx.Tx, q *db.Queries) error {
		ini, err := s.lockInitiative(ctx, q, ws, initiativeID)
		if err != nil {
			return err
		}
		if ini.Status == InitiativeStatusClosed {
			return nil
		}
		row, err := q.GetInitiativeArtifact(ctx, db.GetInitiativeArtifactParams{
			WorkspaceID: ws, InitiativeID: ini.ID, StageKey: ini.StageKey, Kind: kind,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("load artifact: %w", err)
		}
		if !row.Required {
			return nil
		}
		if _, err := q.LinkInitiativeArtifact(ctx, db.LinkInitiativeArtifactParams{
			WorkspaceID: ws, InitiativeID: ini.ID, StageKey: ini.StageKey, Kind: kind, KnowledgeLinkID: linkID,
		}); err != nil {
			return fmt.Errorf("link artifact: %w", err)
		}
		linked = true
		return s.recordEvent(ctx, q, ini, InitiativeEventArtifactLinked, ini.StageKey, ini.StageKey, actor,
			"A published document satisfies the "+kind+" artifact.", pgtype.UUID{})
	})
	if err != nil || !linked {
		return false, err
	}
	return true, s.advanceWhenComplete(ctx, ws, initiativeID)
}

// advanceWhenComplete advances an auto stage whose required artifacts are all
// present or waived. It runs in its own transaction, after the link has
// committed. Other exit modes wait for their own action, and so does a stage
// that is shelved or operating.
func (s *InitiativeService) advanceWhenComplete(ctx context.Context, ws, initiativeID pgtype.UUID) error {
	return s.inTx(ctx, func(tx pgx.Tx, q *db.Queries) error {
		ini, err := s.lockInitiative(ctx, q, ws, initiativeID)
		if err != nil {
			return err
		}
		if ini.Status != InitiativeStatusActive {
			return nil
		}
		pipe, stages, err := s.pipelineOf(ctx, q, ini)
		if err != nil {
			return err
		}
		idx := stageIndex(stages, ini.StageKey)
		if idx < 0 || stages[idx].Exit.Mode != PipelineExitAuto {
			return nil
		}
		missing, err := s.missingRequired(ctx, q, ini)
		if err != nil || len(missing) > 0 {
			return err
		}
		_, err = s.advanceLocked(ctx, q, ini, pipe, stages, InitiativeActor{Type: "system"},
			"Every required artifact of the stage is published.", pgtype.UUID{}, time.Now())
		return err
	})
}

// SetGateDays sets the days after launch at which the operate stage files its
// review gates. A day that has already passed is filed by the next sweep, so the
// change takes effect at the next sweep.
func (s *InitiativeService) SetGateDays(ctx context.Context, p ActionParams, days []int32) (db.Initiative, error) {
	days, err := normalizeGateDays(days)
	if err != nil {
		return db.Initiative{}, err
	}
	var out db.Initiative
	err = s.inTx(ctx, func(tx pgx.Tx, q *db.Queries) error {
		ini, err := s.lockInitiative(ctx, q, p.WorkspaceID, p.InitiativeID)
		if err != nil {
			return err
		}
		if ini.Status == InitiativeStatusClosed {
			return ErrInitiativeClosed
		}
		out, err = q.UpdateInitiativeGateDays(ctx, db.UpdateInitiativeGateDaysParams{
			GateDays: days, ID: ini.ID, WorkspaceID: ini.WorkspaceID,
		})
		if err != nil {
			return fmt.Errorf("set gate days: %w", err)
		}
		return s.recordEvent(ctx, q, out, InitiativeEventGateDaysChanged, ini.StageKey, ini.StageKey, p.Actor,
			"Review days: "+joinDays(ini.GateDays)+" changed to "+joinDays(days)+".", pgtype.UUID{})
	})
	return out, err
}

// Limits on an initiative's review days.
const (
	GateDaysMax      = 365
	gateDaysMaxCount = 10
)

// normalizeGateDays checks the review days and returns them sorted. Each is 1 to
// GateDaysMax, at most gateDaysMaxCount, with no repeats: an operate stage with
// no review would never be reviewed, so at least one is needed.
func normalizeGateDays(days []int32) ([]int32, error) {
	if len(days) == 0 || len(days) > gateDaysMaxCount {
		return nil, ErrGateDaysInvalid
	}
	out := slices.Clone(days)
	slices.Sort(out)
	for i, d := range out {
		if d < 1 || d > GateDaysMax || (i > 0 && out[i-1] == d) {
			return nil, ErrGateDaysInvalid
		}
	}
	return out, nil
}

func joinDays(days []int32) string {
	parts := make([]string, len(days))
	for i, d := range days {
		parts[i] = strconv.Itoa(int(d))
	}
	return strings.Join(parts, ", ")
}

// AdvanceFromGate moves an initiative on an approved gate. It runs in the
// decision transaction; the gate's stage must still be the initiative's.
func (s *InitiativeService) AdvanceFromGate(ctx context.Context, q *db.Queries, d proposalkinds.Decision, g proposalkinds.Gate) error {
	ini, err := s.lockInitiative(ctx, q, d.WorkspaceID, util.MustParseUUID(g.InitiativeID))
	if err != nil {
		return err
	}
	if err := live(ini); err != nil {
		return err
	}
	if ini.StageKey != g.From {
		return ErrGateStale
	}
	pipe, stages, err := s.pipelineOf(ctx, q, ini)
	if err != nil {
		return err
	}
	actor := InitiativeActor{Type: "member", ID: d.DeciderID}
	now := time.Now()
	if g.GateDay > 0 {
		return s.applyOperateOutcome(ctx, q, ini, pipe, stages, d, actor, now)
	}
	if d.Outcome != "" {
		return ErrOutcomeNotAllowed
	}
	if g.To == "" || stageIndex(stages, g.To) < 0 {
		return ErrUnknownStage
	}
	_, err = s.moveTo(ctx, q, ini, pipe, stages, g.To, InitiativeEventAdvanced, actor, d.Note, d.Proposal.ID, now)
	return err
}

// ReturnFromGate records a rejected gate on the initiative's timeline and leaves
// the initiative at its stage. The rejection's note reaches the leader through
// the proposal decision, which posts it on the gate's issue.
func (s *InitiativeService) ReturnFromGate(ctx context.Context, q *db.Queries, d proposalkinds.Decision, g proposalkinds.Gate) error {
	ini, err := s.lockInitiative(ctx, q, d.WorkspaceID, util.MustParseUUID(g.InitiativeID))
	if err != nil {
		return err
	}
	return s.recordEvent(ctx, q, ini, InitiativeEventSentBack, ini.StageKey, ini.StageKey, InitiativeActor{Type: "member", ID: d.DeciderID}, d.Note, d.Proposal.ID)
}
