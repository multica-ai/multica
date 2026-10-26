// Package issueworkflow owns project workflows (MUL-7420).
//
// A workflow is a whole pipeline: it selects and orders statuses from the
// workspace's shared issue_status catalog, and says who each column hands an
// issue to. Every workflow lists todo, done and cancelled; backlog and
// blocked are optional; only their positions are fixed (backlog, todo first;
// blocked, done, cancelled last). Statuses stay shared and peer-level, so
// issue.status keeps holding a catalog key.
//
// When an issue of a project enters a status through an ordinary issue write,
// it is assigned to the workflow's handler for that status and, for an agent,
// its run starts with the step's instructions. A relative handler (the project
// lead) resolves per project, so projects can share a workflow.
//
// A project without a workflow uses the workspace Default workflow. Default
// is a stored row once created; before that it is the built-in statuses.
// Status writes on a Default project are never rejected: issues already on
// statuses Default does not list keep them, as they always have.
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
	"github.com/multica-ai/multica/server/internal/issuestatus"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Handler types a project can hand an issue to when it enters a status.
const (
	HandlerNone        = "none"
	HandlerAgent       = "agent"
	HandlerSquad       = "squad"
	HandlerMember      = "member"
	HandlerProjectLead = "project_lead"
	HandlerCreator     = "creator"
)

const (
	MaxSteps             = 50
	MaxNameRunes         = 64
	MaxDescriptionRunes  = 256
	MaxInstructionsRunes = 8000
	DefaultName          = "Default"
)

// StartStatus is where a new issue lands unless its creator picks a status.
const StartStatus = issuestatus.Todo

// These statuses sit in fixed places around a workflow's own steps. Backlog
// and blocked are optional; the rest every workflow lists.
var (
	fixedHead = []string{issuestatus.Backlog, issuestatus.Todo}
	fixedTail = []string{issuestatus.Blocked, issuestatus.Done, issuestatus.Cancelled}
)

// IsRequired reports whether every workflow must list the status: new issues
// start on todo, and done and cancelled are how work ends.
func IsRequired(key string) bool {
	switch key {
	case issuestatus.Todo, issuestatus.Done, issuestatus.Cancelled:
		return true
	}
	return false
}

// IsFixed reports whether the status has a fixed place when listed.
func IsFixed(key string) bool {
	for _, k := range fixedHead {
		if k == key {
			return true
		}
	}
	for _, k := range fixedTail {
		if k == key {
			return true
		}
	}
	return false
}

// AcceptsHandoff reports whether entering the status can hand an issue off.
// Parking, finishing and cancelling an issue never reassign it.
func AcceptsHandoff(key string) bool {
	switch key {
	case issuestatus.Backlog, issuestatus.Done, issuestatus.Cancelled:
		return false
	}
	return true
}

// Arrange puts a workflow's steps in board order: the fixed statuses it
// lists in their places around its own steps, with the required ones added
// when missing. A repeated fixed status keeps its first step.
func Arrange(steps []Step) []Step {
	fixed := map[string]Step{}
	middle := make([]Step, 0, len(steps))
	for _, s := range steps {
		if !IsFixed(s.StatusKey) {
			middle = append(middle, s)
			continue
		}
		if _, ok := fixed[s.StatusKey]; !ok {
			fixed[s.StatusKey] = s
		}
	}
	place := func(out []Step, keys []string) []Step {
		for _, key := range keys {
			if s, ok := fixed[key]; ok {
				out = append(out, s)
			} else if IsRequired(key) {
				out = append(out, Step{StatusKey: key, Handler: Handler{Type: HandlerNone}})
			}
		}
		return out
	}
	out := place(make([]Step, 0, len(steps)+len(fixedHead)+len(fixedTail)), fixedHead)
	out = append(out, middle...)
	return place(out, fixedTail)
}

// ArrangeKeys is Arrange for bare status keys.
func ArrangeKeys(keys []string) []string {
	return keysOf(Arrange(stepsFor(keys)))
}

func keysOf(steps []Step) []string {
	keys := make([]string, len(steps))
	for i, s := range steps {
		keys[i] = s.StatusKey
	}
	return keys
}

// MiddleKeys returns the workflow's own steps, without the fixed statuses.
func MiddleKeys(keys []string) []string {
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		if !IsFixed(key) {
			out = append(out, key)
		}
	}
	return out
}

// BuiltInDefaultKeys is Default before a workspace stores its own.
func BuiltInDefaultKeys() []string {
	return ArrangeKeys([]string{issuestatus.Backlog, issuestatus.InProgress, issuestatus.InReview, issuestatus.Blocked})
}

// Handler names who receives an issue when it enters a step. ID is set only
// for the concrete types (agent, squad, member).
type Handler struct {
	Type string `json:"type"`
	ID   string `json:"id,omitempty"`
}

// Step is one status of a workflow with its handoff.
type Step struct {
	StatusKey     string  `json:"status_key"`
	Handler       Handler `json:"handler"`
	Instructions  string  `json:"instructions"`
	NextStatusKey string  `json:"next_status_key,omitempty"`
	BackStatusKey string  `json:"back_status_key,omitempty"`
}

// Definition is a decoded workflow.
type Definition struct {
	ID        pgtype.UUID
	Name      string
	IsDefault bool
	Steps     []Step
}

// HandsOff reports whether entering the step reassigns the issue.
func (s Step) HandsOff() bool {
	switch s.Handler.Type {
	case HandlerAgent, HandlerSquad, HandlerMember, HandlerProjectLead, HandlerCreator:
		return true
	}
	return false
}

// storedStep is a step as the workflow row stores it; a step without a
// handoff is just its status key.
type storedStep struct {
	StatusKey     string   `json:"status_key"`
	Handler       *Handler `json:"handler,omitempty"`
	Instructions  string   `json:"instructions,omitempty"`
	NextStatusKey string   `json:"next_status_key,omitempty"`
	BackStatusKey string   `json:"back_status_key,omitempty"`
}

// Decode parses a stored workflow row.
func Decode(row db.IssueWorkflow) (Definition, error) {
	steps, err := DecodeSteps(row.Steps)
	if err != nil {
		return Definition{}, err
	}
	return Definition{ID: row.ID, Name: row.Name, IsDefault: row.IsDefault, Steps: steps}, nil
}

// DecodeSteps parses a stored steps array, normalizing a missing handler to
// "none".
func DecodeSteps(raw []byte) ([]Step, error) {
	if len(raw) == 0 {
		return []Step{}, nil
	}
	var stored []storedStep
	if err := json.Unmarshal(raw, &stored); err != nil {
		return nil, fmt.Errorf("decode workflow steps: %w", err)
	}
	steps := make([]Step, len(stored))
	for i, s := range stored {
		steps[i] = Step{
			StatusKey:     s.StatusKey,
			Handler:       Handler{Type: HandlerNone},
			Instructions:  s.Instructions,
			NextStatusKey: s.NextStatusKey,
			BackStatusKey: s.BackStatusKey,
		}
		if s.Handler != nil && s.Handler.Type != "" {
			steps[i].Handler = *s.Handler
		}
	}
	return steps, nil
}

// DecodeKeys parses a stored steps array into its status keys, in order.
func DecodeKeys(raw []byte) ([]string, error) {
	steps, err := DecodeSteps(raw)
	if err != nil {
		return nil, err
	}
	return keysOf(steps), nil
}

// EncodeSteps serializes a workflow's steps for storage.
func EncodeSteps(steps []Step) ([]byte, error) {
	stored := make([]storedStep, len(steps))
	for i, s := range steps {
		stored[i] = storedStep{StatusKey: s.StatusKey, NextStatusKey: s.NextStatusKey, BackStatusKey: s.BackStatusKey}
		if s.HandsOff() {
			h := s.Handler
			stored[i].Handler = &h
			stored[i].Instructions = s.Instructions
		}
	}
	return json.Marshal(stored)
}

// EncodeKeys serializes statuses without handoffs for storage.
func EncodeKeys(keys []string) ([]byte, error) {
	return EncodeSteps(stepsFor(keys))
}

func stepsFor(keys []string) []Step {
	steps := make([]Step, len(keys))
	for i, key := range keys {
		steps[i] = Step{StatusKey: key, Handler: Handler{Type: HandlerNone}}
	}
	return steps
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
	return keysOf(d.Steps)
}

// HandsOffAnywhere reports whether any step hands an issue off.
func (d Definition) HandsOffAnywhere() bool {
	for _, s := range d.Steps {
		if s.HandsOff() {
			return true
		}
	}
	return false
}

// NextStatus is where a step's handler moves the issue when done: the step's
// own choice, or else the next status on the board that is not blocked.
func (d Definition) NextStatus(key string) string {
	step, ok := d.Step(key)
	if !ok {
		return ""
	}
	if step.NextStatusKey != "" {
		return step.NextStatusKey
	}
	for i, s := range d.Steps {
		if s.StatusKey != key {
			continue
		}
		for _, next := range d.Steps[i+1:] {
			if next.StatusKey != issuestatus.Blocked {
				return next.StatusKey
			}
		}
	}
	return ""
}

// handlerlessSpan is the stretch of work an assignee owns from a step that
// has no handler: entering such steps reassigns nothing and starts no run, so
// the same assignee carries the issue until it reaches a step that hands off
// (or the end of the board).
type handlerlessSpan struct {
	// ok is set when the current step has no handler and the span applies.
	ok bool
	// start is the status that marks the work as started — in_progress when
	// the span passes through it — or empty.
	start string
	// deliver is the status to set once all of the work is done.
	deliver string
	// until is where the span ends: the next step that hands off, done or
	// cancelled, or empty when the board ends first.
	until string
}

// handlerlessSpan walks from key along NextStatus over the steps without a
// handler. When the walk reaches a step that hands off, delivering moves the
// issue there. Otherwise it reaches the end of the board, and delivering sets
// the last status before done other than in_progress, leaving done to a
// human as without a workflow; only when no such status exists does it set
// what follows (done).
func (d Definition) handlerlessSpan(key string) handlerlessSpan {
	current, ok := d.Step(key)
	if !ok || current.HandsOff() {
		return handlerlessSpan{}
	}
	switch key {
	case issuestatus.Blocked, issuestatus.Done, issuestatus.Cancelled:
		return handlerlessSpan{}
	}
	var span handlerlessSpan
	span.ok = true
	var passed []string
	seen := map[string]bool{key: true}
	for at := key; ; {
		next := d.NextStatus(at)
		if next == "" || seen[next] {
			break
		}
		seen[next] = true
		step, _ := d.Step(next)
		if step.HandsOff() || next == issuestatus.Done || next == issuestatus.Cancelled {
			span.until = next
			break
		}
		passed = append(passed, next)
		at = next
	}
	for _, k := range passed {
		if k == issuestatus.InProgress {
			span.start = k
		}
	}
	if target, ok := d.Step(span.until); ok && target.HandsOff() {
		span.deliver = span.until
		return span
	}
	for i := len(passed) - 1; i >= 0; i-- {
		if passed[i] != issuestatus.InProgress {
			span.deliver = passed[i]
			return span
		}
	}
	// Nothing but done lies ahead. From a step where work starts, done is the
	// only way out; from a delivery status such as in_review the work is
	// already handed in, and done stays a human's call as without a workflow.
	switch key {
	case issuestatus.Backlog, issuestatus.Todo, issuestatus.InProgress:
	default:
		return span
	}
	span.deliver = span.until
	return span
}

// CatalogEntry is the slice of an issue_status row validation needs.
type CatalogEntry struct {
	Key      string
	Name     string
	Category string
	Archived bool
}

// Normalize trims free text and handoffs and puts the steps in board order.
func Normalize(name, description string, steps []Step) (string, string, []Step) {
	normalized := make([]Step, len(steps))
	for i, s := range steps {
		normalized[i] = NormalizeHandoff(s)
	}
	return strings.TrimSpace(name), strings.TrimSpace(description), Arrange(normalized)
}

// ValidationError is a user-correctable problem with a workflow definition.
type ValidationError struct{ Message string }

func (e *ValidationError) Error() string { return e.Message }

func invalid(format string, args ...any) error {
	return &ValidationError{Message: fmt.Sprintf(format, args...)}
}

// Validate checks a normalized workflow and its handoffs against the status
// catalog. The Default workflow keeps its reserved name. Handler ids are
// checked by the caller, which owns the workspace lookups.
func Validate(name, description string, steps []Step, isDefault bool, catalog map[string]CatalogEntry) error {
	if !isDefault {
		if name == "" || utf8.RuneCountInString(name) > MaxNameRunes {
			return invalid("name must be 1-%d characters", MaxNameRunes)
		}
		if strings.EqualFold(name, DefaultName) {
			return invalid("%q is reserved for the workspace default workflow", DefaultName)
		}
	}
	if utf8.RuneCountInString(description) > MaxDescriptionRunes {
		return invalid("description must be at most %d characters", MaxDescriptionRunes)
	}
	if len(steps) > MaxSteps {
		return invalid("a workflow can have at most %d statuses", MaxSteps)
	}
	seen := make(map[string]bool, len(steps))
	for _, key := range keysOf(steps) {
		entry, ok := catalog[key]
		if !ok && !issuestatus.IsBuiltIn(key) {
			return invalid("status %q does not exist", key)
		}
		if ok && entry.Archived {
			return invalid("status %q is archived", key)
		}
		if seen[key] {
			return invalid("status %q appears more than once", key)
		}
		seen[key] = true
	}
	def := Definition{Steps: steps}
	for _, s := range steps {
		if err := ValidateHandoff(s, def); err != nil {
			return err
		}
	}
	return nil
}

// NormalizeHandoff trims a handoff and drops fields its handler type does
// not use.
func NormalizeHandoff(s Step) Step {
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
		// by a type switch in the form would otherwise be stored.
		s.Handler.ID = ""
	}
	s.Instructions = strings.TrimSpace(s.Instructions)
	if s.Handler.Type == HandlerNone {
		s.Instructions = ""
	}
	s.NextStatusKey = strings.TrimSpace(s.NextStatusKey)
	s.BackStatusKey = strings.TrimSpace(s.BackStatusKey)
	return s
}

// ValidateHandoff checks one normalized handoff against its workflow.
// Handler ids are checked by the caller, which owns the workspace lookups.
func ValidateHandoff(s Step, def Definition) error {
	if !def.Has(s.StatusKey) {
		return invalid("status %q is not part of this workflow", s.StatusKey)
	}
	switch s.Handler.Type {
	case HandlerNone, HandlerProjectLead:
	case HandlerAgent, HandlerMember:
		if s.Handler.ID == "" {
			return invalid("choose a %s to hand off to", s.Handler.Type)
		}
	case HandlerSquad, HandlerCreator:
		// A squad routes work itself, so a handoff to one would compete with
		// its own assignment. (MUL-7420)
		return invalid("hand off to an agent, a member or the project lead")
	default:
		return invalid("unknown handler type %q", s.Handler.Type)
	}
	if s.HandsOff() && !AcceptsHandoff(s.StatusKey) {
		return invalid("issues entering %q cannot be handed off", s.StatusKey)
	}
	if utf8.RuneCountInString(s.Instructions) > MaxInstructionsRunes {
		return invalid("instructions must be at most %d characters", MaxInstructionsRunes)
	}
	for _, ref := range []string{s.NextStatusKey, s.BackStatusKey} {
		if ref != "" && (ref == s.StatusKey || !def.Has(ref)) {
			return invalid("%q is not another status of this workflow", ref)
		}
	}
	return nil
}

// Querier is the read surface workflow resolution needs.
type Querier interface {
	GetIssueWorkflow(ctx context.Context, arg db.GetIssueWorkflowParams) (db.IssueWorkflow, error)
	GetDefaultIssueWorkflow(ctx context.Context, workspaceID pgtype.UUID) (db.IssueWorkflow, error)
	GetProjectInWorkspace(ctx context.Context, arg db.GetProjectInWorkspaceParams) (db.Project, error)
}

// DefaultFor loads the workspace Default workflow, or the built-in one when
// the workspace has not stored its own yet.
func DefaultFor(ctx context.Context, q Querier, workspaceID pgtype.UUID) (Definition, error) {
	row, err := q.GetDefaultIssueWorkflow(ctx, workspaceID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Definition{Name: DefaultName, IsDefault: true, Steps: stepsFor(BuiltInDefaultKeys())}, nil
		}
		return Definition{}, err
	}
	return Decode(row)
}

// ForProject loads the workflow a project uses. def is nil only for an issue
// outside any project.
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

// ForProjectRow loads the workflow of an already-loaded project. A dangling
// workflow reference falls back to Default.
func ForProjectRow(ctx context.Context, q Querier, project db.Project) (*Definition, error) {
	var def Definition
	var err error
	if project.WorkflowID.Valid {
		var row db.IssueWorkflow
		row, err = q.GetIssueWorkflow(ctx, db.GetIssueWorkflowParams{ID: project.WorkflowID, WorkspaceID: project.WorkspaceID})
		if err == nil {
			def, err = Decode(row)
			return &def, err
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
	}
	def, err = DefaultFor(ctx, q, project.WorkspaceID)
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
// first step of the same lifecycle category, otherwise the start status.
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
	return StartStatus
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

// CheckStatus rejects a status a named workflow does not list. Default
// accepts every status, so issues already on statuses it no longer lists
// keep working.
func CheckStatus(def *Definition, key string) error {
	if def == nil || def.IsDefault || def.Has(key) {
		return nil
	}
	return &NotInWorkflowError{StatusKey: key, WorkflowName: def.Name, Allowed: def.StatusKeys()}
}

// BriefInput carries what the handoff brief names.
type BriefInput struct {
	Workflow        Definition
	ProjectName     string
	StatusKey       string
	IssueIdentifier string
	// StatusName resolves a catalog key to its display name.
	StatusName func(key string) string
	// HandlerName describes who a step hands off to, e.g. "Sentinel" or
	// "Lin (project lead)". Empty means the step does not hand off.
	HandlerName func(step Step) string
	// Observer renders the brief for an agent working on the issue without
	// being its assignee — mentioned in a comment, say: the statuses, and to
	// leave the status to AssigneeName. The zero value is the assignee's
	// brief, with the step's instructions and the commands that finish it.
	Observer bool
	// AssigneeName names the issue's assignee in an observer's brief; empty
	// means unassigned.
	AssigneeName string
	// LegacyRuntimeRules renders for a daemon whose runtime brief still
	// states the built-in in_progress / in_review rules (one without
	// protocol.DaemonCapabilityWorkflowStatusRulesV1): the brief then says it
	// replaces them, rules out the built-in moves, and carries the exit check.
	// A current daemon drops those rules and states the exit check itself, so
	// the zero value — also what the editor and timeline show — leaves these
	// lines out rather than forbid what nothing asks for (MUL-8145).
	LegacyRuntimeRules bool
}

// Brief renders what an agent reads about the issue's project workflow: the
// workflow's statuses, who each step hands off to, and — for the assignee —
// the current step's instructions and the commands for the next move. The
// runtime brief points at this block instead of stating built-in status rules;
// for an older daemon that still states them, LegacyRuntimeRules adds the
// lines that override them.
//
// It is the one rendering of a workflow for agents: the claim carries it into
// every run's prompt, and the editor and timeline show the same text. Names
// and instructions are user-authored, so control characters are dropped and
// names are kept to one line.
func Brief(in BriefInput) string {
	step, _ := in.Workflow.Step(in.StatusKey)
	name := func(key string) string {
		if in.StatusName == nil {
			return key
		}
		if n := briefLine(in.StatusName(key)); n != "" {
			return n
		}
		return key
	}
	var b strings.Builder
	title := briefLine(in.Workflow.Name)
	if project := briefLine(in.ProjectName); project != "" {
		title = fmt.Sprintf("%s (%s)", title, project)
	}
	fmt.Fprintf(&b, "This project's workflow — %s\n", title)
	// Each move names who the target step hands off to, so the list needs no
	// preamble about handoffs.
	handsOffTo := func(key string) string {
		target, ok := in.Workflow.Step(key)
		if !ok || in.HandlerName == nil {
			return ""
		}
		if who := briefLine(in.HandlerName(target)); who != "" {
			return " — hands off to " + who
		}
		return ""
	}
	// An older daemon still states the built-in rules itself, so its brief
	// keeps the per-step wording rather than add a second in_progress rule.
	var span handlerlessSpan
	if !in.Observer && !in.LegacyRuntimeRules {
		span = in.Workflow.handlerlessSpan(in.StatusKey)
	}
	if in.LegacyRuntimeRules {
		b.WriteString("This workflow replaces the status rules in your runtime instructions: use only the status keys below")
		if !in.Workflow.Has(issuestatus.InReview) {
			fmt.Fprintf(&b, ", never `%s`", issuestatus.InReview)
		}
		b.WriteString(".\n")
	}
	if in.Observer {
		assignee := "is unassigned"
		if who := briefLine(in.AssigneeName); who != "" {
			assignee = "is assigned to " + who
		}
		fmt.Fprintf(&b, "%s is at the %q step and %s. You are not its assignee: leave the status to them unless you are explicitly asked to change it.\n",
			in.IssueIdentifier, name(in.StatusKey), assignee)
	} else if span.ok {
		// A step without a handler is a progress marker, not a stage: naming
		// it "the step you handle" made agents split one task across columns
		// and stop after the first half (MUL-8231).
		fmt.Fprintf(&b, "You are handling %s. Its %q step has no handler, so the work the issue asks for is yours ", in.IssueIdentifier, name(in.StatusKey))
		if target, ok := in.Workflow.Step(span.until); ok && target.HandsOff() {
			fmt.Fprintf(&b, "until it reaches the %q step%s.\n", name(span.until), handsOffTo(span.until))
		} else {
			b.WriteString("to finish.\n")
		}
	} else {
		fmt.Fprintf(&b, "You are handling the %q step of %s.\n", name(in.StatusKey), in.IssueIdentifier)
		if instructions := briefText(step.Instructions); instructions != "" {
			b.WriteString("\nStep instructions:\n")
			for _, line := range strings.Split(instructions, "\n") {
				b.WriteString("> ")
				b.WriteString(line)
				b.WriteString("\n")
			}
		}
	}
	b.WriteString("\nStatuses in this workflow:\n")
	for _, s := range in.Workflow.Steps {
		// Built-in statuses have no stored name; their key reads on its own.
		fmt.Fprintf(&b, "- `%s`", s.StatusKey)
		if n := name(s.StatusKey); n != s.StatusKey {
			fmt.Fprintf(&b, " %s", n)
		}
		if in.HandlerName != nil {
			if who := briefLine(in.HandlerName(s)); who != "" {
				fmt.Fprintf(&b, " — hands off to %s", who)
			}
		}
		if s.StatusKey == in.StatusKey {
			b.WriteString("   ← current")
		}
		b.WriteString("\n")
	}
	if in.Observer {
		b.WriteString("\nMoving the issue into a step that hands off reassigns it to that step's handler and starts their run.\n")
		return b.String()
	}
	b.WriteString("\nYour moves:\n")
	switch {
	case span.ok:
		if span.start != "" {
			fmt.Fprintf(&b, "Starting work  → multica issue status %s %s\n", in.IssueIdentifier, span.start)
		}
		if span.deliver != "" {
			fmt.Fprintf(&b, "Step done      → multica issue status %s %s%s\n", in.IssueIdentifier, span.deliver, handsOffTo(span.deliver))
		} else {
			fmt.Fprintf(&b, "Step done      → leave the issue at `%s`; `done` is set by a human\n", in.StatusKey)
		}
	default:
		if next := in.Workflow.NextStatus(in.StatusKey); next != "" {
			fmt.Fprintf(&b, "Step done      → multica issue status %s %s%s\n", in.IssueIdentifier, next, handsOffTo(next))
		}
	}
	if step.BackStatusKey != "" {
		fmt.Fprintf(&b, "Needs changes  → multica issue status %s %s%s\n", in.IssueIdentifier, step.BackStatusKey, handsOffTo(step.BackStatusKey))
	}
	switch {
	case in.Workflow.Has(issuestatus.Blocked):
		fmt.Fprintf(&b, "Can't proceed  → multica issue status %s blocked, then comment why\n", in.IssueIdentifier)
	case step.BackStatusKey != "":
		b.WriteString("Can't proceed  → comment why, then run Needs changes\n")
	default:
		b.WriteString("Can't proceed  → comment why and leave the status as it is\n")
	}
	if span.ok {
		b.WriteString("Steps without a handler hand nothing off and start no run: finish all of the issue's work in this run before Step done.\n")
	}
	if in.LegacyRuntimeRules {
		// An older daemon's brief still says to set in_progress first and
		// in_review on delivery, and checks the status on exit in those terms.
		b.WriteString("These commands are the only status moves for your step: set no status to mark that you started, and none to mark delivery — no separate `in_progress` or `in_review`.\n")
		b.WriteString("Before your run ends, either run one of them or leave the issue at this step on purpose because its work continues. The system does not advance the status when your run ends.\n")
	}
	b.WriteString("When the issue enters a step you handle, you get a run automatically — do not add a wakeup for that. Wakeups for anything else (a time, a PR, another issue, a step someone else handles) are fine.\n")
	return b.String()
}

// briefLine keeps a user-authored name on one line of a brief: line breaks
// and tabs become single spaces and other control characters are dropped.
func briefLine(s string) string {
	return strings.Join(strings.Fields(briefText(s)), " ")
}

// briefText drops the control characters of user-authored text, keeping its
// line breaks, so it cannot reshape the brief around it.
func briefText(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return r
		}
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) || r == '\u2028' || r == '\u2029' {
			return -1
		}
		return r
	}, s)
	return strings.TrimSpace(s)
}
