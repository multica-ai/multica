package service

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// The Secretary setup: one call that makes a workspace's Secretary, its
// Executive floor and squad, the setting that names the Secretary, and the two
// brief autopilots. It runs in one transaction under the office layout lock, and
// it finds what an earlier call made, so running it again changes nothing.

const (
	// OfficeMaxFloors caps the floors a workspace can have. The office handlers
	// enforce the same limit.
	OfficeMaxFloors = 16
	// OfficeSlotsPerFloor is the number of squad rooms on a floor.
	OfficeSlotsPerFloor = 6

	SecretaryAgentName        = "Secretary"
	SecretaryAgentDescription = "Reads the company and writes the boss's brief."
	ExecutiveFloorName        = "Executive"
	ExecutiveSquadName        = "Executive Office"
	ExecutiveSquadDescription = "The Secretary and the boss, who run the company as a whole."
	DailyBriefTitle           = "Daily brief"
	WeeklyReviewTitle         = "Weekly review"
	DailyBriefCron            = "0 8 * * *"
	WeeklyReviewCron          = "0 17 * * 5"

	// OfficeSetupInstructionsMax bounds the Secretary's instructions, which the
	// client sends from the secretary template.
	OfficeSetupInstructionsMax = 20000
)

// Errors the setup returns for a state it will not change.
var (
	// ErrSecretaryNameTaken: an agent already has the Secretary's name and the
	// workspace does not name it as its Secretary, so setup cannot reuse it.
	ErrSecretaryNameTaken = errors.New("an agent named Secretary already exists in this workspace")
	// ErrExecutiveFloorFull: every seat on the Executive floor is taken.
	ErrExecutiveFloorFull = errors.New("every seat on the Executive floor is taken")
	// ErrOfficeFloorLimit: the workspace has no room for another floor.
	ErrOfficeFloorLimit = errors.New("a workspace can have at most 16 floors")
)

// DailyBriefDescription is the run prompt of the daily brief. The autopilot
// passes it to the Secretary as the task prompt, so it states the scope,
// language and tone the boss can edit.
const DailyBriefDescription = `Daily brief for the boss. The autopilot starts this run every morning; the boss can also start it by hand.

Scope: the whole workspace, read through the digest. Start with ` + "`multica brief digest --since 24h --json`" + `, and open only the issues the digest flags. Write the brief to a file in the order of your instructions, then publish it with ` + "`multica brief publish --kind daily --title \"Daily brief\" --file <path>`" + `. If nothing needs the boss, publish a short brief that says so.

Language: write in the language of the boss's last message to you. With no message from the boss, write in English.

Tone: plain and direct. Lead with the decision the boss needs. Keep the brief readable in two minutes, and link squads and issues.

Do not change any issue, proposal, or agent. Ask a leader only through a comment on a flagged issue, at most three per brief.`

// WeeklyReviewDescription is the run prompt of the weekly review. The autopilot
// is created paused; the boss turns it on when they want it.
const WeeklyReviewDescription = `Weekly review for the boss, Friday afternoon. Look back over the week, not the last day: ` + "`multica brief digest --since 7d --json`" + `.

Write the review to a file in the order of your instructions and publish it with ` + "`multica brief publish --kind weekly --title \"Weekly review\" --file <path>`" + `. Cover what shipped, what is stuck, the proposals still waiting for the boss, and what the Research floor found. The same rules as the daily brief apply: decision first, the boss's language, and no changes to issues, proposals, or agents.

Language: write in the language of the boss's last message to you. With no message from the boss, write in English.

This schedule starts paused. The boss turns it on from the autopilot page.`

// OfficeSetupService makes and reports on the Secretary setup.
type OfficeSetupService struct {
	q  *db.Queries
	tx TxStarter
}

func NewOfficeSetupService(q *db.Queries, tx TxStarter) *OfficeSetupService {
	return &OfficeSetupService{q: q, tx: tx}
}

// OfficeSetupInput is what the handler has checked: the actor is an owner or
// admin, and Runtime is an online Hermes runtime the actor may use.
type OfficeSetupInput struct {
	WorkspaceID           pgtype.UUID
	Actor                 db.Member
	Runtime               db.AgentRuntime
	SecretaryInstructions string
	// Timezone is a valid IANA name, or empty for the actor's own timezone and
	// then UTC.
	Timezone string
}

// OfficeSetupCreated holds what one setup call made. A field is nil when the
// setup reused that thing, so the caller publishes events only for new rows.
type OfficeSetupCreated struct {
	Secretary       *db.Agent
	Floor           *db.OfficeFloor
	Squad           *db.Squad
	Workspace       *db.Workspace
	DailyAutopilot  *db.Autopilot
	WeeklyAutopilot *db.Autopilot
}

// OfficeSetupReadiness is the checklist the first-run card shows.
type OfficeSetupReadiness struct {
	RuntimeOnline         bool
	SecretaryID           string // empty when the workspace names no live Secretary
	ExecutiveFloor        bool
	AutopilotDaily        bool
	AutopilotWeeklyPaused bool
	Ready                 bool
}

// Setup creates whatever the workspace is missing and reuses the rest.
func (s *OfficeSetupService) Setup(ctx context.Context, in OfficeSetupInput) (OfficeSetupCreated, error) {
	var out OfficeSetupCreated
	tx, err := s.tx.Begin(ctx)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	q := s.q.WithTx(tx)

	// The layout lock serializes setup with floor, seat and squad writes, so a
	// second call waits here and then sees everything the first one committed.
	if err := q.LockOfficeLayout(ctx, in.WorkspaceID); err != nil {
		return out, err
	}
	ws, err := q.GetWorkspace(ctx, in.WorkspaceID)
	if err != nil {
		return out, err
	}

	secretary, createdSecretary, err := ensureSecretary(ctx, q, in, ws)
	if err != nil {
		return out, err
	}
	out.Secretary = createdSecretary

	floor, createdFloor, err := ensureExecutiveFloor(ctx, q, in.WorkspaceID)
	if err != nil {
		return out, err
	}
	out.Floor = createdFloor

	_, createdSquad, err := ensureExecutiveSquad(ctx, q, in, secretary, floor)
	if err != nil {
		return out, err
	}
	out.Squad = createdSquad

	if !ws.SecretaryAgentID.Valid || ws.SecretaryAgentID != secretary.ID {
		updated, err := q.UpdateWorkspace(ctx, db.UpdateWorkspaceParams{
			ID:                in.WorkspaceID,
			SetSecretaryAgent: true,
			SecretaryAgentID:  secretary.ID,
		})
		if err != nil {
			return out, err
		}
		out.Workspace = &updated
	}

	timezone, err := resolveBriefTimezone(ctx, q, in)
	if err != nil {
		return out, err
	}
	out.DailyAutopilot, err = ensureBriefAutopilot(ctx, q, in, secretary.ID, DailyBriefTitle, DailyBriefDescription, "active", DailyBriefCron, timezone)
	if err != nil {
		return out, err
	}
	out.WeeklyAutopilot, err = ensureBriefAutopilot(ctx, q, in, secretary.ID, WeeklyReviewTitle, WeeklyReviewDescription, "paused", WeeklyReviewCron, timezone)
	if err != nil {
		return out, err
	}

	if err := tx.Commit(ctx); err != nil {
		return OfficeSetupCreated{}, err
	}
	return out, nil
}

// ensureSecretary reuses the agent the workspace names as its Secretary, or
// creates one on the runtime the handler chose. Setup never adopts an agent
// that only has the name: the boss may have made it for something else.
func ensureSecretary(ctx context.Context, q *db.Queries, in OfficeSetupInput, ws db.Workspace) (db.Agent, *db.Agent, error) {
	if ws.SecretaryAgentID.Valid {
		agent, err := q.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{ID: ws.SecretaryAgentID, WorkspaceID: in.WorkspaceID})
		if err == nil && !agent.ArchivedAt.Valid {
			return agent, nil, nil
		}
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return db.Agent{}, nil, err
		}
	}

	// The name is unique per workspace, archived agents included.
	all, err := q.ListAllAgents(ctx, in.WorkspaceID)
	if err != nil {
		return db.Agent{}, nil, err
	}
	for _, a := range all {
		if a.Name == SecretaryAgentName {
			return db.Agent{}, nil, ErrSecretaryNameTaken
		}
	}

	agent, err := q.CreateAgent(ctx, db.CreateAgentParams{
		WorkspaceID:          in.WorkspaceID,
		Name:                 SecretaryAgentName,
		Description:          SecretaryAgentDescription,
		Instructions:         in.SecretaryInstructions,
		RuntimeMode:          in.Runtime.RuntimeMode,
		RuntimeConfig:        []byte("{}"),
		RuntimeID:            in.Runtime.ID,
		Visibility:           "private",
		PermissionMode:       "private",
		MaxConcurrentTasks:   1,
		OwnerID:              in.Actor.UserID,
		CustomEnv:            []byte("{}"),
		CustomArgs:           []byte("[]"),
		ConversationStarters: []byte("[]"),
	})
	if err != nil {
		return db.Agent{}, nil, err
	}
	if err := q.AssignKnowledgeMcpToAgent(ctx, db.AssignKnowledgeMcpToAgentParams{AgentID: agent.ID, WorkspaceID: in.WorkspaceID}); err != nil {
		return db.Agent{}, nil, err
	}
	if _, err := WriteAgentInstructionRevision(ctx, q, in.WorkspaceID, agent.ID, in.SecretaryInstructions, InstructionChange{
		Source: InstructionSourceHuman,
		Actor:  GovernanceActor{Type: GovernanceActorMember, ID: in.Actor.UserID},
	}); err != nil {
		return db.Agent{}, nil, err
	}
	return agent, &agent, nil
}

// ensureExecutiveFloor reuses a floor named Executive or makes one.
func ensureExecutiveFloor(ctx context.Context, q *db.Queries, workspaceID pgtype.UUID) (db.OfficeFloor, *db.OfficeFloor, error) {
	floors, err := q.ListOfficeFloors(ctx, workspaceID)
	if err != nil {
		return db.OfficeFloor{}, nil, err
	}
	for _, f := range floors {
		if f.Name == ExecutiveFloorName {
			return f, nil, nil
		}
	}
	if len(floors) >= OfficeMaxFloors {
		return db.OfficeFloor{}, nil, ErrOfficeFloorLimit
	}
	floor, err := q.CreateOfficeFloor(ctx, db.CreateOfficeFloorParams{WorkspaceID: workspaceID, Name: ExecutiveFloorName})
	if err != nil {
		return db.OfficeFloor{}, nil, err
	}
	return floor, &floor, nil
}

// ensureExecutiveSquad reuses the live Executive Office squad as it is, or
// makes it: the Secretary leads, the boss is a member, the Secretary's home is
// the squad, and the squad takes the first free seat on the Executive floor.
func ensureExecutiveSquad(ctx context.Context, q *db.Queries, in OfficeSetupInput, secretary db.Agent, floor db.OfficeFloor) (db.Squad, *db.Squad, error) {
	squads, err := q.ListSquads(ctx, in.WorkspaceID)
	if err != nil {
		return db.Squad{}, nil, err
	}
	for _, s := range squads {
		if s.Name == ExecutiveSquadName {
			return s, nil, nil
		}
	}

	// Find the seat before writing anything, so a full floor leaves no squad behind.
	slot, err := freeOfficeSeat(ctx, q, in.WorkspaceID, floor.ID)
	if err != nil {
		return db.Squad{}, nil, err
	}
	squad, err := q.CreateSquad(ctx, db.CreateSquadParams{
		WorkspaceID: in.WorkspaceID,
		Name:        ExecutiveSquadName,
		Description: ExecutiveSquadDescription,
		LeaderID:    secretary.ID,
		CreatorID:   in.Actor.UserID,
		AvatarUrl:   pgtype.Text{},
	})
	if err != nil {
		return db.Squad{}, nil, err
	}
	if _, err := q.AddSquadMember(ctx, db.AddSquadMemberParams{
		SquadID: squad.ID, MemberType: "agent", MemberID: secretary.ID, Role: "leader",
	}); err != nil {
		return db.Squad{}, nil, err
	}
	if _, err := q.AddSquadMember(ctx, db.AddSquadMemberParams{
		SquadID: squad.ID, MemberType: "member", MemberID: in.Actor.UserID, Role: "member",
	}); err != nil {
		return db.Squad{}, nil, err
	}
	// The Secretary's first squad becomes its home, so it has a desk. Any other
	// membership it already has keeps it where it is.
	if _, err := q.SetAgentHomeSquadOnFirstMembership(ctx, db.SetAgentHomeSquadOnFirstMembershipParams{
		ID: secretary.ID, WorkspaceID: in.WorkspaceID, SquadID: squad.ID,
	}); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return db.Squad{}, nil, err
	}
	seated, err := q.SetSquadSeat(ctx, db.SetSquadSeatParams{
		ID:          squad.ID,
		WorkspaceID: in.WorkspaceID,
		FloorID:     floor.ID,
		OfficeSlot:  pgtype.Int2{Int16: slot, Valid: true},
	})
	if err != nil {
		return db.Squad{}, nil, err
	}
	return seated, &seated, nil
}

// freeOfficeSeat returns the lowest slot on the floor with no live squad.
func freeOfficeSeat(ctx context.Context, q *db.Queries, workspaceID, floorID pgtype.UUID) (int16, error) {
	for slot := int16(0); slot < OfficeSlotsPerFloor; slot++ {
		_, err := q.GetLiveSquadAtSeat(ctx, db.GetLiveSquadAtSeatParams{
			WorkspaceID: workspaceID,
			FloorID:     floorID,
			OfficeSlot:  pgtype.Int2{Int16: slot, Valid: true},
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return slot, nil
		}
		if err != nil {
			return 0, err
		}
	}
	return 0, ErrExecutiveFloorFull
}

// resolveBriefTimezone picks the schedule's timezone: the request's, else the
// actor's saved timezone, else UTC. A saved timezone that no longer parses falls
// back to UTC rather than blocking setup.
func resolveBriefTimezone(ctx context.Context, q *db.Queries, in OfficeSetupInput) (string, error) {
	if in.Timezone != "" {
		return in.Timezone, nil
	}
	user, err := q.GetUser(ctx, in.Actor.UserID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}
	if err == nil && user.Timezone.Valid && ValidateTimezone(user.Timezone.String) == nil {
		return user.Timezone.String, nil
	}
	return "UTC", nil
}

// ensureBriefAutopilot reuses the Secretary's autopilot with this title, or
// creates it with a schedule. It is assigned to the agent itself, not the
// squad: the Secretary reads and writes, so the squad's dispatch-and-stop
// protocol does not fit.
// It returns the autopilot only when this call created it, nil when it reused one.
func ensureBriefAutopilot(ctx context.Context, q *db.Queries, in OfficeSetupInput, secretaryID pgtype.UUID, title, description, status, cron, timezone string) (*db.Autopilot, error) {
	rows, err := q.ListAutopilots(ctx, db.ListAutopilotsParams{WorkspaceID: in.WorkspaceID})
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		ap := row.Autopilot
		if ap.AssigneeType == "agent" && ap.AssigneeID == secretaryID && ap.Title == title {
			return nil, nil
		}
	}

	actor := in.Actor.UserID
	ap, err := q.CreateAutopilot(ctx, db.CreateAutopilotParams{
		WorkspaceID:   in.WorkspaceID,
		Title:         title,
		AssigneeType:  "agent",
		AssigneeID:    secretaryID,
		Status:        status,
		ExecutionMode: "run_only",
		CreatedByType: "member",
		CreatedByID:   actor,
		Description:   pgtype.Text{String: description, Valid: true},
	})
	if err != nil {
		return nil, err
	}
	if err := RecordAutopilotRuleVersion(ctx, q, ap, "member", actor); err != nil {
		return nil, err
	}

	next, err := ComputeNextRun(cron, timezone)
	if err != nil {
		return nil, err
	}
	if _, err := q.CreateAutopilotTrigger(ctx, db.CreateAutopilotTriggerParams{
		AutopilotID:     ap.ID,
		Kind:            "schedule",
		Enabled:         true,
		CronExpression:  pgtype.Text{String: cron, Valid: true},
		Timezone:        pgtype.Text{String: timezone, Valid: true},
		NextRunAt:       pgtype.Timestamptz{Time: next, Valid: true},
		PublishedByType: pgtype.Text{String: "member", Valid: true},
		PublishedByID:   actor,
		CreatedByType:   pgtype.Text{String: "member", Valid: true},
		CreatedByID:     actor,
	}); err != nil {
		return nil, err
	}
	return &ap, nil
}

// Readiness reports the checklist without changing anything. runtimeOnline is
// whether the actor can see an online Hermes runtime, which the handler checks.
func (s *OfficeSetupService) Readiness(ctx context.Context, workspaceID pgtype.UUID, runtimeOnline bool) (OfficeSetupReadiness, error) {
	out := OfficeSetupReadiness{RuntimeOnline: runtimeOnline}
	ws, err := s.q.GetWorkspace(ctx, workspaceID)
	if err != nil {
		return out, err
	}

	var secretaryID pgtype.UUID
	if ws.SecretaryAgentID.Valid {
		agent, err := s.q.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{ID: ws.SecretaryAgentID, WorkspaceID: workspaceID})
		switch {
		case err == nil && !agent.ArchivedAt.Valid:
			secretaryID = agent.ID
			out.SecretaryID = util.UUIDToString(agent.ID)
		case err != nil && !errors.Is(err, pgx.ErrNoRows):
			return out, err
		}
	}

	floors, err := s.q.ListOfficeFloors(ctx, workspaceID)
	if err != nil {
		return out, err
	}
	for _, f := range floors {
		if f.Name == ExecutiveFloorName {
			out.ExecutiveFloor = true
		}
	}

	if secretaryID.Valid {
		rows, err := s.q.ListAutopilots(ctx, db.ListAutopilotsParams{WorkspaceID: workspaceID})
		if err != nil {
			return out, err
		}
		for _, row := range rows {
			ap := row.Autopilot
			if ap.AssigneeType != "agent" || ap.AssigneeID != secretaryID || ap.ExecutionMode != "run_only" {
				continue
			}
			if ap.Title == DailyBriefTitle && ap.Status == "active" {
				out.AutopilotDaily = true
			}
			if ap.Title == WeeklyReviewTitle && ap.Status == "paused" {
				out.AutopilotWeeklyPaused = true
			}
		}
	}

	out.Ready = out.RuntimeOnline && out.SecretaryID != "" && out.ExecutiveFloor &&
		out.AutopilotDaily && out.AutopilotWeeklyPaused
	return out, nil
}
