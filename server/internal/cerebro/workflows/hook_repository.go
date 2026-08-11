package workflows

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

var (
	ErrHookPolicyNotFound      = errors.New("workflow hook policy not found")
	ErrHookPublishPrerequisite = errors.New("workflow hook requires an observed run and fresh baseline before publish")
	ErrHookDraftRevisionStale  = errors.New("workflow hook Draft revision is stale")
	ErrHookEventNotFound       = errors.New("workflow hook retained event not found")
	ErrHookEventNotRetainable  = errors.New("workflow hook event type is not retainable")
	ErrManagedHookLocked       = errors.New("managed workflow hook is locked")
	ErrHookContractRequired    = errors.New("workflow hook requires a plain-language contract before publish")
)

type HookJournalEvent struct {
	ID            string        `json:"id"`
	EventID       string        `json:"event_id"`
	EventType     HookEventType `json:"event_type"`
	SchemaVersion int           `json:"schema_version"`
	EventHash     string        `json:"-"`
	OccurredAt    time.Time     `json:"occurred_at"`
	ExpiresAt     time.Time     `json:"expires_at"`
	replayEvent   HookEvent
}

type HookRunRecord struct {
	ID            string       `json:"id"`
	PolicyID      string       `json:"policy_id"`
	PolicyVersion int          `json:"policy_version"`
	Event         HookEvent    `json:"event"`
	Result        HookResult   `json:"result"`
	SourceScope   HookBinding  `json:"source_scope"`
	FailMode      HookFailMode `json:"fail_mode"`
	LatencyMS     int          `json:"latency_ms"`
	CreatedAt     time.Time    `json:"created_at"`
}

// HookRunSummary is one line of the workspace-wide hook history: enough to see
// which hook ran, on whose work, and whether it actually changed anything —
// without loading every action result for every hook in the workspace.
type HookRunSummary struct {
	ID            string       `json:"id"`
	FamilyID      string       `json:"family_id"`
	HookName      string       `json:"hook_name"`
	PolicyVersion int          `json:"policy_version"`
	Event         HookEvent    `json:"event"`
	SourceScope   HookBinding  `json:"source_scope"`
	Decision      HookDecision `json:"decision"`
	WouldDecision HookDecision `json:"would_decision,omitempty"`
	// Enforced is false for a Dry run or a replayed test — the run was observed
	// but nothing was stopped, changed, or started.
	Enforced     bool         `json:"enforced"`
	FailMode     HookFailMode `json:"fail_mode"`
	Requirements []string     `json:"requirements,omitempty"`
	LatencyMS    int          `json:"latency_ms"`
	TimedOut     bool         `json:"timed_out"`
	CreatedAt    time.Time    `json:"created_at"`
}

type HookRepository interface {
	List(context.Context, string) ([]HookPolicy, error)
	ListEffective(context.Context, string) ([]HookPolicy, error)
	Get(context.Context, string, string) (HookPolicy, error)
	GetEffective(context.Context, string, string) (HookPolicy, error)
	Create(context.Context, string, HookPermissionActor, HookPolicy) (HookPolicy, error)
	Update(context.Context, string, HookPermissionActor, string, HookPolicy) (HookPolicy, error)
	DiscardDraft(context.Context, string, HookPermissionActor, string) (HookPolicy, error)
	Disable(context.Context, string, HookPermissionActor, string) (HookPolicy, error)
	Delete(context.Context, string, HookPermissionActor, string) error
	Publish(context.Context, string, string, string) (HookPolicy, error)
	Runs(context.Context, string, string) ([]HookRunRecord, error)
	RecentRuns(context.Context, string, int) ([]HookRunSummary, error)
	RecordRun(context.Context, string, HookRunRecord) error
	RefreshBaseline(context.Context, string, string) (time.Time, error)
	RecordTestEvidence(context.Context, string, string, HookRunRecord) (time.Time, error)
	CaptureEvent(context.Context, string, HookEvent) (HookJournalEvent, error)
	CompatibleEvents(context.Context, string, string, int) ([]HookJournalEvent, error)
	ReplayEvent(context.Context, string, string) (HookEvent, error)
}

type memoryHookPolicy struct {
	live         *HookPolicy
	draft        *HookPolicy
	disabled     bool
	aliases      map[string]struct{}
	observedRuns int
	baselineAt   time.Time
	evidenceRev  int
}

type MemoryHookRepository struct {
	mu         sync.Mutex
	workspaces map[string]map[string]*memoryHookPolicy
	runs       map[string][]HookRunRecord
	journal    map[string][]HookJournalEvent
	now        func() time.Time
}

func NewMemoryHookRepository() *MemoryHookRepository {
	return &MemoryHookRepository{
		workspaces: make(map[string]map[string]*memoryHookPolicy),
		runs:       make(map[string][]HookRunRecord),
		journal:    make(map[string][]HookJournalEvent),
		now:        time.Now,
	}
}

func (r *MemoryHookRepository) List(_ context.Context, workspaceID string) ([]HookPolicy, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	rows := r.workspaces[workspaceID]
	out := make([]HookPolicy, 0, len(rows))
	for _, row := range rows {
		out = append(out, memoryPolicyResponse(row))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	return out, nil
}

func (r *MemoryHookRepository) ListEffective(_ context.Context, workspaceID string) ([]HookPolicy, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []HookPolicy
	for _, row := range r.workspaces[workspaceID] {
		if row.live != nil && !row.disabled {
			out = append(out, *row.live)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	return out, nil
}

func (r *MemoryHookRepository) Get(_ context.Context, workspaceID, id string) (HookPolicy, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	row := r.find(workspaceID, id)
	if row == nil {
		return HookPolicy{}, ErrHookPolicyNotFound
	}
	return memoryPolicyResponse(row), nil
}

func (r *MemoryHookRepository) GetEffective(_ context.Context, workspaceID, id string) (HookPolicy, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	row := r.find(workspaceID, id)
	if row == nil || row.live == nil {
		return HookPolicy{}, ErrHookPolicyNotFound
	}
	policy := *row.live
	policy.FamilyID = memoryFamilyID(row)
	policy.Lifecycle = memoryLifecycle(row)
	return policy, nil
}

func (r *MemoryHookRepository) Create(_ context.Context, workspaceID string, actor HookPermissionActor, policy HookPolicy) (HookPolicy, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if policy.ID == "" {
		policy.ID = uuid.NewString()
	}
	if policy.FamilyID == "" {
		policy.FamilyID = uuid.NewString()
	}
	policy.DraftSeriesID = uuid.NewString()
	policy.Revision = 1
	policy.Version = 1
	policy.Mode = HookModeDryRun
	policy.CreatedByID = actor.ID
	policy.CreatedByType = actor.Type
	policy.UpdatedAt = time.Now().UTC()
	if r.workspaces[workspaceID] == nil {
		r.workspaces[workspaceID] = make(map[string]*memoryHookPolicy)
	}
	row := &memoryHookPolicy{draft: &policy, aliases: map[string]struct{}{policy.ID: {}}}
	r.workspaces[workspaceID][policy.FamilyID] = row
	return memoryPolicyResponse(row), nil
}

func (r *MemoryHookRepository) Update(_ context.Context, workspaceID string, actor HookPermissionActor, id string, policy HookPolicy) (HookPolicy, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	current := r.find(workspaceID, id)
	if current == nil {
		return HookPolicy{}, ErrHookPolicyNotFound
	}
	if current.live != nil && current.live.Mode == HookModeManaged && !actor.IsOwner {
		return HookPolicy{}, ErrManagedHookLocked
	}
	if current.draft != nil && policy.Revision > 0 && policy.Revision != current.draft.Revision {
		return HookPolicy{}, ErrHookDraftRevisionStale
	}
	policy.ID = uuid.NewString()
	policy.FamilyID = memoryFamilyID(current)
	if current.draft != nil {
		policy.DraftSeriesID = current.draft.DraftSeriesID
		policy.Revision = current.draft.Revision + 1
		policy.Version = current.draft.Version
	} else {
		policy.DraftSeriesID = uuid.NewString()
		policy.Revision = 1
		policy.Version = 1
		if current.live != nil {
			policy.Version = current.live.Version + 1
		}
	}
	policy.Mode = HookModeDryRun
	policy.CreatedByID = actor.ID
	policy.CreatedByType = actor.Type
	policy.UpdatedAt = time.Now().UTC()
	current.draft = &policy
	if current.aliases == nil {
		current.aliases = make(map[string]struct{})
	}
	current.aliases[policy.ID] = struct{}{}
	current.observedRuns = 0
	current.baselineAt = time.Time{}
	current.evidenceRev = 0
	return memoryPolicyResponse(current), nil
}

func (r *MemoryHookRepository) Disable(_ context.Context, workspaceID string, actor HookPermissionActor, id string) (HookPolicy, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	row := r.find(workspaceID, id)
	if row == nil {
		return HookPolicy{}, ErrHookPolicyNotFound
	}
	if row.live != nil && row.live.Mode == HookModeManaged && !actor.IsOwner {
		return HookPolicy{}, ErrManagedHookLocked
	}
	if row.live == nil {
		return HookPolicy{}, ErrHookPolicyNotFound
	}
	row.disabled = true
	row.live.Mode = HookModeOff
	row.live.UpdatedAt = time.Now().UTC()
	return memoryPolicyResponse(row), nil
}

func (r *MemoryHookRepository) DiscardDraft(_ context.Context, workspaceID string, actor HookPermissionActor, id string) (HookPolicy, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	row := r.find(workspaceID, id)
	if row == nil || row.draft == nil {
		return HookPolicy{}, ErrHookPolicyNotFound
	}
	if row.live != nil && row.live.Mode == HookModeManaged && !actor.IsOwner {
		return HookPolicy{}, ErrManagedHookLocked
	}
	familyID := memoryFamilyID(row)
	row.draft = nil
	row.observedRuns = 0
	row.baselineAt = time.Time{}
	row.evidenceRev = 0
	if row.live == nil {
		delete(r.workspaces[workspaceID], familyID)
		return HookPolicy{}, ErrHookPolicyNotFound
	}
	return memoryPolicyResponse(row), nil
}

func (r *MemoryHookRepository) Delete(_ context.Context, workspaceID string, actor HookPermissionActor, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	row := r.find(workspaceID, id)
	if row == nil {
		return ErrHookPolicyNotFound
	}
	if row.live != nil && row.live.Mode == HookModeManaged && !actor.IsOwner {
		return ErrManagedHookLocked
	}
	delete(r.workspaces[workspaceID], memoryFamilyID(row))
	delete(r.runs, workspaceID+":"+id)
	return nil
}

func (r *MemoryHookRepository) Publish(_ context.Context, workspaceID, id, _ string) (HookPolicy, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	row := r.find(workspaceID, id)
	if row == nil || row.draft == nil {
		return HookPolicy{}, ErrHookPolicyNotFound
	}
	if row.observedRuns == 0 || row.baselineAt.IsZero() {
		return HookPolicy{}, ErrHookPublishPrerequisite
	}
	if !hasReadableHookContract(*row.draft) {
		return HookPolicy{}, ErrHookContractRequired
	}
	published := *row.draft
	canonicalizeWorkspaceBindings(&published, workspaceID)
	published.ID = uuid.NewString()
	published.DraftSeriesID = ""
	published.Revision = 0
	published.Mode = HookModeEnforce
	published.UpdatedAt = time.Now().UTC()
	row.live = &published
	row.draft = nil
	row.disabled = false
	return memoryPolicyResponse(row), nil
}

func hasReadableHookContract(policy HookPolicy) bool {
	return strings.TrimSpace(policy.ContractRule) != "" && strings.TrimSpace(policy.ContractSatisfy) != ""
}

func memoryPolicyResponse(row *memoryHookPolicy) HookPolicy {
	var policy HookPolicy
	if row.draft != nil {
		policy = *row.draft
	} else if row.live != nil {
		policy = *row.live
	}
	policy.ObservedRuns = row.observedRuns
	if !row.baselineAt.IsZero() {
		baseline := row.baselineAt
		policy.BaselineAt = &baseline
	}
	policy.CanPublish = row.draft != nil && row.observedRuns > 0 && !row.baselineAt.IsZero()
	if row.draft != nil {
		policy.CanPublish = policy.CanPublish && row.evidenceRev == row.draft.Revision
	}
	policy.Lifecycle = memoryLifecycle(row)
	return policy
}

func (r *MemoryHookRepository) Runs(_ context.Context, workspaceID, policyID string) ([]HookRunRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := workspaceID + ":" + policyID
	return append([]HookRunRecord(nil), r.runs[key]...), nil
}

func (r *MemoryHookRepository) RecentRuns(_ context.Context, workspaceID string, limit int) ([]HookRunSummary, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []HookRunSummary
	for key, runs := range r.runs {
		if !strings.HasPrefix(key, workspaceID+":") {
			continue
		}
		for _, run := range runs {
			name, familyID := "", run.PolicyID
			if row := r.find(workspaceID, run.PolicyID); row != nil && row.live != nil {
				name, familyID = row.live.Name, row.live.FamilyID
			}
			out = append(out, HookRunSummary{
				ID: run.ID, FamilyID: familyID, HookName: name, PolicyVersion: run.PolicyVersion,
				Event: run.Event, SourceScope: run.SourceScope,
				Decision: run.Result.Decision, WouldDecision: run.Result.WouldDecision,
				Enforced: run.Result.WouldDecision == "", FailMode: run.FailMode,
				Requirements: run.Result.Requirements, LatencyMS: run.LatencyMS,
				TimedOut: run.Result.TimedOut, CreatedAt: run.CreatedAt,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (r *MemoryHookRepository) RecordRun(_ context.Context, workspaceID string, run HookRunRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	row := r.find(workspaceID, run.PolicyID)
	if row == nil {
		return ErrHookPolicyNotFound
	}
	if run.ID == "" {
		run.ID = uuid.NewString()
	}
	if run.CreatedAt.IsZero() {
		run.CreatedAt = time.Now().UTC()
	}
	key := workspaceID + ":" + run.PolicyID
	r.runs[key] = append([]HookRunRecord{run}, r.runs[key]...)
	row.observedRuns++
	lastRun := run.CreatedAt
	if row.draft != nil {
		row.draft.LastRunAt = &lastRun
	} else if row.live != nil {
		row.live.LastRunAt = &lastRun
	}
	return nil
}

func (r *MemoryHookRepository) RefreshBaseline(_ context.Context, workspaceID, policyID string) (time.Time, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	row := r.find(workspaceID, policyID)
	if row == nil {
		return time.Time{}, ErrHookPolicyNotFound
	}
	if row.observedRuns == 0 {
		return time.Time{}, ErrHookPublishPrerequisite
	}
	row.baselineAt = time.Now().UTC()
	if row.draft != nil {
		row.evidenceRev = row.draft.Revision
	}
	return row.baselineAt, nil
}

func (r *MemoryHookRepository) RecordTestEvidence(_ context.Context, workspaceID, eventJournalID string, run HookRunRecord) (time.Time, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	row := r.find(workspaceID, run.PolicyID)
	if row == nil || row.draft == nil || row.draft.ID != run.PolicyID {
		return time.Time{}, ErrHookPolicyNotFound
	}
	var retained bool
	now := r.now().UTC()
	for _, event := range r.journal[workspaceID] {
		if event.ID == eventJournalID && event.ExpiresAt.After(now) {
			retained = true
			break
		}
	}
	if !retained {
		return time.Time{}, ErrHookEventNotFound
	}
	if run.PolicyVersion != row.draft.Version || run.Result.TimedOut || len(run.Result.Matches) == 0 {
		return time.Time{}, ErrHookPublishPrerequisite
	}
	if run.ID == "" {
		run.ID = uuid.NewString()
	}
	run.CreatedAt = now
	r.runs[workspaceID+":"+run.PolicyID] = append([]HookRunRecord{run}, r.runs[workspaceID+":"+run.PolicyID]...)
	row.observedRuns = 1
	row.baselineAt = now
	row.evidenceRev = row.draft.Revision
	row.draft.LastRunAt = &now
	return now, nil
}

func (r *MemoryHookRepository) CaptureEvent(_ context.Context, workspaceID string, event HookEvent) (HookJournalEvent, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := hookJournalSectionsByEvent[event.Type]; !ok {
		return HookJournalEvent{}, ErrHookEventNotRetainable
	}
	now := r.now().UTC()
	sanitized := sanitizeHookJournalEvent(workspaceID, event)
	raw, _ := json.Marshal(sanitized)
	hash := sha256.Sum256(raw)
	eventHash := fmt.Sprintf("%x", hash)
	for _, retained := range r.journal[workspaceID] {
		if retained.EventHash == eventHash && retained.ExpiresAt.After(now) {
			return retained, nil
		}
	}
	retained := HookJournalEvent{
		ID:            uuid.NewString(),
		EventID:       sanitized.EventID,
		EventType:     sanitized.Type,
		SchemaVersion: 1,
		EventHash:     eventHash,
		OccurredAt:    now,
		ExpiresAt:     now.Add(7 * 24 * time.Hour),
		replayEvent:   sanitized,
	}
	r.journal[workspaceID] = append([]HookJournalEvent{retained}, r.journal[workspaceID]...)
	return retained, nil
}

func (r *MemoryHookRepository) CompatibleEvents(_ context.Context, workspaceID, policyID string, limit int) ([]HookJournalEvent, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	row := r.find(workspaceID, policyID)
	if row == nil {
		return nil, ErrHookPolicyNotFound
	}
	policy := memoryPolicyResponse(row)
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	now := r.now().UTC()
	out := make([]HookJournalEvent, 0, limit)
	for _, retained := range r.journal[workspaceID] {
		if !retained.ExpiresAt.After(now) || !eventListed(policy.Events, retained.EventType) || !policyMatches(policy, retained.replayEvent) {
			continue
		}
		out = append(out, retained)
		if len(out) == limit {
			break
		}
	}
	return out, nil
}

func (r *MemoryHookRepository) ReplayEvent(_ context.Context, workspaceID, eventID string) (HookEvent, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now().UTC()
	for _, retained := range r.journal[workspaceID] {
		if retained.ID == eventID && retained.ExpiresAt.After(now) {
			return retained.replayEvent, nil
		}
	}
	return HookEvent{}, ErrHookEventNotFound
}

// Allowlist follows the canonical field manifest (hook_field_manifest.json):
// every key a condition can match on is retained so journal replay and the
// Test → publish evidence flow work. Content-bearing values (message text,
// prompts, failure messages, tool output) stay out — they are sensitive and
// never condition fields.
var hookJournalContextAllowlist = map[string]map[string]struct{}{
	"issue":      {"id": {}, "identifier": {}, "status": {}, "priority": {}, "terminal": {}},
	"task":       {"id": {}, "status": {}, "outcome": {}},
	"tool":       {"name": {}, "status": {}, "error": {}},
	"wakeup":     {"id": {}, "status": {}, "trigger_type": {}, "trigger_enabled": {}, "active_count": {}, "max_active": {}, "min_interval_seconds": {}, "seconds_until_fire": {}, "has_last_fire": {}, "seconds_after_last_fire": {}, "loop_limit_enabled": {}, "consecutive_without_progress": {}, "max_without_progress": {}, "since_member_reply": {}, "since_status_change": {}, "since_progress_update": {}, "since_pull_request_update": {}, "expected_continuation": {}},
	"workflow":   {"id": {}, "step": {}, "status": {}, "phase_id": {}, "block_id": {}, "block_type": {}, "step_number": {}, "step_status": {}},
	"session":    {"id": {}, "status": {}},
	"agent":      {"id": {}, "model": {}},
	"error":      {"code": {}, "kind": {}},
	"message":    {"kind": {}, "channel": {}, "agent_authored": {}, "has_recipient": {}, "has_active_wakeup": {}, "promises_continuation": {}, "thread_required": {}, "correct_thread": {}, "required_parent_id": {}, "no_action": {}, "is_sub_issue": {}, "mentions_initiator": {}, "mentions_agent": {}, "posted_on_parent": {}},
	"continuation": {"present": {}, "kind": {}, "evidence_id": {}},
	"chain":      {"active": {}, "approved_for_done": {}},
	"status":     {"from": {}, "to": {}},
	"failure":    {"reason": {}, "attempt": {}, "max_attempts": {}, "consecutive_postpones": {}, "next_consecutive_postpone": {}},
	"assignment": {"agent_id": {}, "reason": {}},
	"actor":      {"type": {}, "id": {}},
	"handoff":    {"root_comment_id": {}, "start_new": {}},
}

var hookJournalSectionsByEvent = map[HookEventType][]string{
	HookBeforeSessionStart:   {"session", "agent", "issue"},
	HookAfterSessionStart:    {"session", "agent", "issue"},
	HookBeforeSessionEnd:     {"session", "agent", "issue", "handoff"},
	HookAfterSessionEnd:      {"session", "agent", "issue"},
	HookBeforePromptAssemble: {"session", "agent", "issue"},
	HookBeforeToolCall:       {"tool", "session", "agent", "issue"},
	HookAfterToolCall:        {"tool", "session", "agent", "issue"},
	HookOnToolFailure:        {"tool", "error", "session", "agent", "issue"},
	HookBeforeTaskComplete:   {"task", "workflow", "session", "agent", "issue", "continuation"},
	HookBeforeAgentStop:      {"task", "session", "agent", "issue", "continuation"},
	HookBeforeSubagentStart:  {"task", "session", "agent", "issue"},
	HookAfterSubagentStop:    {"task", "session", "agent", "issue"},
	HookOnError:              {"error", "task", "session", "agent", "issue"},
	HookOnTaskFailure:        {"error", "failure", "task", "workflow", "session", "agent", "issue"},
	HookBeforeWakeupCreate:   {"wakeup", "session", "agent", "issue"},
	HookOnWakeupFireFailure:  {"wakeup", "error", "failure", "session", "agent", "issue"},
	HookBeforeIssueStatus:    {"issue", "session", "agent", "status", "chain"},
	HookBeforeIssueAssigned:  {"assignment", "actor", "session", "agent", "issue"},
	HookBeforeMessageSend:    {"message", "session", "agent", "issue"},
	HookAfterWorkflowStep:    {"workflow", "task", "session", "agent", "issue"},
}

func sanitizeHookJournalEvent(workspaceID string, event HookEvent) HookEvent {
	contextValues := make(map[string]any)
	for _, section := range hookJournalSectionsByEvent[event.Type] {
		allowed := hookJournalContextAllowlist[section]
		source, ok := event.Context[section].(map[string]any)
		if !ok {
			continue
		}
		target := make(map[string]any)
		for key := range allowed {
			if value, exists := source[key]; exists && isHookJournalScalar(value) {
				target[key] = value
			}
		}
		if len(target) > 0 {
			contextValues[section] = target
		}
	}
	return HookEvent{
		EventID: event.EventID, Type: event.Type, WorkspaceID: workspaceID,
		ProjectID: event.ProjectID, WorkflowID: event.WorkflowID, AgentID: event.AgentID,
		Model: event.Model, IssueID: event.IssueID, SessionID: event.SessionID,
		Context: contextValues, Attempt: event.Attempt, NoProgress: event.NoProgress,
		HookDepth: event.HookDepth,
	}
}

func isHookJournalScalar(value any) bool {
	switch value.(type) {
	case nil, bool, string, float64, int, int32, int64:
		return true
	default:
		return false
	}
}

func (r *MemoryHookRepository) Seed(workspaceID string, policy HookPolicy) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.workspaces[workspaceID] == nil {
		r.workspaces[workspaceID] = make(map[string]*memoryHookPolicy)
	}
	if policy.FamilyID == "" {
		policy.FamilyID = policy.ID
	}
	row := &memoryHookPolicy{aliases: map[string]struct{}{policy.ID: {}}}
	if policy.Mode == HookModeDryRun {
		policy.DraftSeriesID = uuid.NewString()
		policy.Revision = 1
		row.draft = &policy
	} else {
		row.live = &policy
		row.disabled = policy.Mode == HookModeOff
	}
	r.workspaces[workspaceID][policy.FamilyID] = row
}

func (r *MemoryHookRepository) RecordObservedRun(workspaceID, policyID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if row := r.find(workspaceID, policyID); row != nil {
		row.observedRuns++
	}
}

func (r *MemoryHookRepository) MarkBaselineFresh(workspaceID, policyID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if row := r.find(workspaceID, policyID); row != nil {
		row.baselineAt = time.Now().UTC()
		if row.draft != nil {
			row.evidenceRev = row.draft.Revision
		}
	}
}

func (r *MemoryHookRepository) find(workspaceID, id string) *memoryHookPolicy {
	if row := r.workspaces[workspaceID][id]; row != nil {
		return row
	}
	for _, row := range r.workspaces[workspaceID] {
		if (row.live != nil && row.live.ID == id) || (row.draft != nil && row.draft.ID == id) {
			return row
		}
		if _, ok := row.aliases[id]; ok {
			return row
		}
	}
	return nil
}

func memoryFamilyID(row *memoryHookPolicy) string {
	if row.draft != nil {
		return row.draft.FamilyID
	}
	if row.live != nil {
		return row.live.FamilyID
	}
	return ""
}

func memoryLifecycle(row *memoryHookPolicy) HookLifecycle {
	lifecycle := HookLifecycle{State: HookLifecycleDraft, LiveUnchangedByDraft: row.live != nil && row.draft != nil}
	if row.live != nil {
		lifecycle.LivePolicyID = row.live.ID
		lifecycle.LiveVersion = row.live.Version
		switch {
		case row.live.Mode == HookModeManaged:
			lifecycle.State = HookLifecycleManaged
		case row.disabled && row.draft != nil:
			lifecycle.State = HookLifecycleOffWithDraft
		case row.disabled:
			lifecycle.State = HookLifecycleOff
		case row.draft != nil:
			lifecycle.State = HookLifecycleLiveWithDraft
		default:
			lifecycle.State = HookLifecycleLive
		}
	}
	if row.draft != nil {
		lifecycle.DraftID = row.draft.ID
		lifecycle.DraftSeriesID = row.draft.DraftSeriesID
		lifecycle.DraftRevision = row.draft.Revision
	}
	return lifecycle
}
