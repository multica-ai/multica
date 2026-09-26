// Package issueworkflow owns project workflows (MUL-7420).
//
// A workflow selects and orders statuses from the workspace's shared
// issue_status catalog. Each step may name a handler: when an issue enters the
// step through an ordinary issue write, the issue is assigned to that handler
// and, for an agent or squad, its run starts with the step's instructions.
// Statuses themselves stay shared, so issue.status keeps holding a catalog key.
//
// A project without a workflow uses the implicit Default workflow: every
// active catalog status and no handoffs, which is exactly the behavior that
// predates workflows.
package issueworkflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Handler types a step can hand an issue to.
const (
	HandlerNone        = "none"
	HandlerAgent       = "agent"
	HandlerSquad       = "squad"
	HandlerMember      = "member"
	HandlerProjectLead = "project_lead"
	HandlerCreator     = "creator"
)

const (
	MaxSteps               = 50
	MaxNameRunes           = 64
	MaxDescriptionRunes    = 256
	MaxInstructionsRunes   = 8000
	defaultWorkflowDisplay = "Default"
)

// Handler names who receives an issue when it enters a step. ID is set only
// for the concrete types (agent, squad, member).
type Handler struct {
	Type string `json:"type"`
	ID   string `json:"id,omitempty"`
}

// Step is one status of a workflow, in board order.
type Step struct {
	StatusKey     string  `json:"status_key"`
	Handler       Handler `json:"handler"`
	Instructions  string  `json:"instructions"`
	NextStatusKey string  `json:"next_status_key,omitempty"`
	BackStatusKey string  `json:"back_status_key,omitempty"`
}

// Definition is a decoded workflow row.
type Definition struct {
	ID               pgtype.UUID
	Name             string
	InitialStatusKey string
	Steps            []Step
}

// HandsOff reports whether entering the step reassigns the issue.
func (s Step) HandsOff() bool {
	switch s.Handler.Type {
	case HandlerAgent, HandlerSquad, HandlerMember, HandlerProjectLead, HandlerCreator:
		return true
	}
	return false
}

// Decode parses a stored workflow row.
func Decode(row db.IssueWorkflow) (Definition, error) {
	steps, err := DecodeSteps(row.Steps)
	if err != nil {
		return Definition{}, err
	}
	return Definition{
		ID:               row.ID,
		Name:             row.Name,
		InitialStatusKey: row.InitialStatusKey,
		Steps:            steps,
	}, nil
}

// DecodeSteps parses the stored steps array, normalizing an empty handler to
// "none" so older or hand-written rows read the same as editor-saved ones.
func DecodeSteps(raw []byte) ([]Step, error) {
	if len(raw) == 0 {
		return []Step{}, nil
	}
	var steps []Step
	if err := json.Unmarshal(raw, &steps); err != nil {
		return nil, fmt.Errorf("decode workflow steps: %w", err)
	}
	for i := range steps {
		if steps[i].Handler.Type == "" {
			steps[i].Handler.Type = HandlerNone
		}
	}
	return steps, nil
}

// EncodeSteps serializes steps for storage.
func EncodeSteps(steps []Step) ([]byte, error) {
	if steps == nil {
		steps = []Step{}
	}
	return json.Marshal(steps)
}

// Has reports whether the workflow lists the status key.
func (d Definition) Has(key string) bool {
	_, ok := d.Step(key)
	return ok
}

// Step returns the step for a status key.
func (d Definition) Step(key string) (Step, bool) {
	for _, s := range d.Steps {
		if s.StatusKey == key {
			return s, true
		}
	}
	return Step{}, false
}

// StatusKeys lists the workflow's statuses in board order.
func (d Definition) StatusKeys() []string {
	keys := make([]string, len(d.Steps))
	for i, s := range d.Steps {
		keys[i] = s.StatusKey
	}
	return keys
}

// CatalogEntry is the slice of an issue_status row validation needs.
type CatalogEntry struct {
	Key      string
	Name     string
	Category string
	Archived bool
}

// Normalize trims free text and fills defaults before validation.
func Normalize(name, description, initial string, steps []Step) (string, string, string, []Step) {
	out := make([]Step, len(steps))
	for i, s := range steps {
		s.StatusKey = strings.TrimSpace(s.StatusKey)
		s.Handler.Type = strings.TrimSpace(s.Handler.Type)
		s.Handler.ID = strings.TrimSpace(s.Handler.ID)
		if s.Handler.Type == "" {
			s.Handler.Type = HandlerNone
		}
		switch s.Handler.Type {
		case HandlerAgent, HandlerSquad, HandlerMember:
		default:
			// Only the concrete handler types carry an id; a stale one left
			// by a type switch in the editor would otherwise be stored.
			s.Handler.ID = ""
		}
		if s.Handler.Type == HandlerNone {
			s.Instructions = ""
		}
		s.Instructions = strings.TrimSpace(s.Instructions)
		s.NextStatusKey = strings.TrimSpace(s.NextStatusKey)
		s.BackStatusKey = strings.TrimSpace(s.BackStatusKey)
		out[i] = s
	}
	return strings.TrimSpace(name), strings.TrimSpace(description), strings.TrimSpace(initial), out
}

// ValidationError is a user-correctable problem with a workflow definition.
type ValidationError struct{ Message string }

func (e *ValidationError) Error() string { return e.Message }

func invalid(format string, args ...any) error {
	return &ValidationError{Message: fmt.Sprintf(format, args...)}
}

// Validate checks a normalized definition against the status catalog. Handler
// ids are checked by the caller, which owns the workspace lookups.
func Validate(name, description, initial string, steps []Step, catalog map[string]CatalogEntry) error {
	if name == "" || utf8.RuneCountInString(name) > MaxNameRunes {
		return invalid("name must be 1-%d characters", MaxNameRunes)
	}
	if strings.EqualFold(name, defaultWorkflowDisplay) {
		return invalid("%q is reserved for the workspace default workflow", defaultWorkflowDisplay)
	}
	if utf8.RuneCountInString(description) > MaxDescriptionRunes {
		return invalid("description must be at most %d characters", MaxDescriptionRunes)
	}
	if len(steps) == 0 || len(steps) > MaxSteps {
		return invalid("a workflow needs 1-%d steps", MaxSteps)
	}
	seen := make(map[string]bool, len(steps))
	for _, s := range steps {
		entry, ok := catalog[s.StatusKey]
		if !ok {
			return invalid("status %q does not exist", s.StatusKey)
		}
		if entry.Archived {
			return invalid("status %q is archived", s.StatusKey)
		}
		if seen[s.StatusKey] {
			return invalid("status %q appears more than once", s.StatusKey)
		}
		seen[s.StatusKey] = true
	}
	if !seen[initial] {
		return invalid("the starting status %q must be one of the workflow's steps", initial)
	}
	for _, s := range steps {
		switch s.Handler.Type {
		case HandlerNone, HandlerProjectLead, HandlerCreator:
		case HandlerAgent, HandlerSquad, HandlerMember:
			if s.Handler.ID == "" {
				return invalid("step %q needs a %s to hand off to", s.StatusKey, s.Handler.Type)
			}
		default:
			return invalid("step %q has an unknown handler type %q", s.StatusKey, s.Handler.Type)
		}
		if utf8.RuneCountInString(s.Instructions) > MaxInstructionsRunes {
			return invalid("instructions for %q must be at most %d characters", s.StatusKey, MaxInstructionsRunes)
		}
		for _, ref := range []string{s.NextStatusKey, s.BackStatusKey} {
			if ref != "" && !seen[ref] {
				return invalid("step %q points to %q, which is not a step of this workflow", s.StatusKey, ref)
			}
		}
	}
	return nil
}

// Querier is the read surface workflow resolution needs.
type Querier interface {
	GetIssueWorkflow(ctx context.Context, arg db.GetIssueWorkflowParams) (db.IssueWorkflow, error)
	GetProjectInWorkspace(ctx context.Context, arg db.GetProjectInWorkspaceParams) (db.Project, error)
}

// ErrWorkflowMissing means a project points at a workflow that no longer
// exists. Callers treat the project as using the Default workflow.
var ErrWorkflowMissing = errors.New("project workflow not found")

// ForProject loads the workflow a project uses, or nil for the Default
// workflow (no project, no workflow, or a dangling reference).
func ForProject(ctx context.Context, q Querier, workspaceID, projectID pgtype.UUID) (*Definition, db.Project, error) {
	if !projectID.Valid {
		return nil, db.Project{}, nil
	}
	project, err := q.GetProjectInWorkspace(ctx, db.GetProjectInWorkspaceParams{ID: projectID, WorkspaceID: workspaceID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, db.Project{}, nil
		}
		return nil, db.Project{}, err
	}
	def, err := ForProjectRow(ctx, q, project)
	return def, project, err
}

// ForProjectRow loads the workflow of an already-loaded project.
func ForProjectRow(ctx context.Context, q Querier, project db.Project) (*Definition, error) {
	if !project.WorkflowID.Valid {
		return nil, nil
	}
	row, err := q.GetIssueWorkflow(ctx, db.GetIssueWorkflowParams{ID: project.WorkflowID, WorkspaceID: project.WorkspaceID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	def, err := Decode(row)
	if err != nil {
		return nil, err
	}
	return &def, nil
}

// ResolveHandler turns a step's handler into a concrete assignee for one
// issue. ok is false when the step does not hand off, or when a relative
// handler (project lead, creator) has nobody to resolve to.
func ResolveHandler(step Step, project db.Project, issueCreatorType string, issueCreatorID pgtype.UUID) (assigneeType string, assigneeID pgtype.UUID, ok bool) {
	switch step.Handler.Type {
	case HandlerAgent, HandlerSquad, HandlerMember:
		var id pgtype.UUID
		if err := id.Scan(step.Handler.ID); err != nil || !id.Valid {
			return "", pgtype.UUID{}, false
		}
		return step.Handler.Type, id, true
	case HandlerProjectLead:
		if !project.LeadType.Valid || !project.LeadID.Valid {
			return "", pgtype.UUID{}, false
		}
		return project.LeadType.String, project.LeadID, true
	case HandlerCreator:
		if issueCreatorType == "" || !issueCreatorID.Valid {
			return "", pgtype.UUID{}, false
		}
		return issueCreatorType, issueCreatorID, true
	}
	return "", pgtype.UUID{}, false
}

// MoveTarget picks the status an issue keeps when it moves into a project
// using def: its current status when the workflow lists it, otherwise the
// first step of the same lifecycle category, otherwise the starting status.
func MoveTarget(def Definition, currentKey string, categoryOf func(string) string) string {
	if def.Has(currentKey) {
		return currentKey
	}
	category := categoryOf(currentKey)
	for _, s := range def.Steps {
		if category != "" && categoryOf(s.StatusKey) == category {
			return s.StatusKey
		}
	}
	return def.InitialStatusKey
}

// NotInWorkflowError explains a status write the project's workflow rejects.
// Its message lists the allowed keys because agents read it verbatim.
type NotInWorkflowError struct {
	StatusKey    string
	WorkflowName string
	Allowed      []string
}

func (e *NotInWorkflowError) Error() string {
	return fmt.Sprintf("status %q is not part of this project's workflow %q; use one of: %s",
		e.StatusKey, e.WorkflowName, strings.Join(e.Allowed, ", "))
}

// StepMovedError refuses an agent's status change on an issue that has left
// the step the agent's run works on: someone else moved it while the run
// worked, so the run's decision no longer applies, and applying it would undo
// that move.
type StepMovedError struct {
	IssueIdentifier string
	// Step is the status the run works on; Current is where the issue is now.
	Step, StepName       string
	Current, CurrentName string
	// MovedBy names whoever last changed the status; empty when unknown.
	MovedBy string
}

func (e *StepMovedError) Error() string {
	mover := e.MovedBy
	if mover == "" {
		mover = "someone else"
	}
	return fmt.Sprintf("%s is no longer at %s: %s moved it to %s while this run was working, so this status change was not applied. "+
		"Do not change the status again in this run; leave a comment if something still needs attention.",
		e.IssueIdentifier, statusLabel(e.StepName, e.Step), mover, statusLabel(e.CurrentName, e.Current))
}

// statusLabel names a status for an agent: its display name with the key the
// CLI takes, or the bare key when the two are the same.
func statusLabel(name, key string) string {
	if name == "" || name == key {
		return fmt.Sprintf("%q", key)
	}
	return fmt.Sprintf("%q (%s)", name, key)
}

// CheckStatus rejects a status the workflow does not list.
func CheckStatus(def *Definition, key string) error {
	if def == nil || def.Has(key) {
		return nil
	}
	return &NotInWorkflowError{StatusKey: key, WorkflowName: def.Name, Allowed: def.StatusKeys()}
}

// BriefInput carries what the handoff brief names.
type BriefInput struct {
	Workflow        Definition
	StatusKey       string
	IssueIdentifier string
	// StatusName resolves a catalog key to its display name.
	StatusName func(key string) string
	// HandlerName describes who a step hands off to, e.g. "Sentinel" or
	// "Lin (project lead)". Empty means the step does not hand off.
	HandlerName func(step Step) string
}

// Brief renders the handoff note an agent or squad leader receives when an
// issue enters a step that hands off to it. The runtime brief's built-in
// status rules (for example in_review on delivery) name statuses this project
// may not use, so the note spells out the workflow's own statuses and the
// exact commands for the next move.
func Brief(in BriefInput) string {
	step, _ := in.Workflow.Step(in.StatusKey)
	name := func(key string) string {
		if in.StatusName == nil {
			return key
		}
		if n := in.StatusName(key); n != "" {
			return n
		}
		return key
	}
	var b strings.Builder
	fmt.Fprintf(&b, "This project's workflow — %s\n", in.Workflow.Name)
	fmt.Fprintf(&b, "You are handling the %q step of %s. You were assigned because the issue entered this status.\n",
		name(in.StatusKey), in.IssueIdentifier)
	if step.Instructions != "" {
		b.WriteString("\nStep instructions:\n")
		for _, line := range strings.Split(step.Instructions, "\n") {
			b.WriteString("> ")
			b.WriteString(line)
			b.WriteString("\n")
		}
	}
	b.WriteString("\nStatuses in this workflow (use only these keys):\n")
	for _, s := range in.Workflow.Steps {
		// Built-in statuses have no stored name; their key reads on its own.
		fmt.Fprintf(&b, "- `%s`", s.StatusKey)
		if n := name(s.StatusKey); n != s.StatusKey {
			fmt.Fprintf(&b, " %s", n)
		}
		if in.HandlerName != nil {
			if who := in.HandlerName(s); who != "" {
				fmt.Fprintf(&b, " — hands off to %s", who)
			}
		}
		if s.StatusKey == in.StatusKey {
			b.WriteString("   ← current")
		}
		b.WriteString("\n")
	}
	b.WriteString("\nHand off by changing the status:\n")
	if step.NextStatusKey != "" {
		fmt.Fprintf(&b, "Step done      → multica issue status %s %s\n", in.IssueIdentifier, step.NextStatusKey)
	}
	if step.BackStatusKey != "" {
		fmt.Fprintf(&b, "Needs changes  → multica issue status %s %s\n", in.IssueIdentifier, step.BackStatusKey)
	}
	if in.Workflow.Has("blocked") {
		fmt.Fprintf(&b, "Can't proceed  → multica issue status %s blocked, then comment why\n", in.IssueIdentifier)
	}
	b.WriteString("The system does not advance the status when your run ends. Where your runtime instructions name a status this list does not include, use the commands above instead.\n")
	return b.String()
}
