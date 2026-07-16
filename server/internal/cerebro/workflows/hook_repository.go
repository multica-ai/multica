package workflows

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
)

var (
	ErrHookPolicyNotFound      = errors.New("workflow hook policy not found")
	ErrHookPublishPrerequisite = errors.New("workflow hook requires an observed run and fresh baseline before publish")
	ErrManagedHookLocked       = errors.New("managed workflow hook is locked")
)

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

type HookRepository interface {
	List(context.Context, string) ([]HookPolicy, error)
	Get(context.Context, string, string) (HookPolicy, error)
	Create(context.Context, string, HookPermissionActor, HookPolicy) (HookPolicy, error)
	Update(context.Context, string, HookPermissionActor, string, HookPolicy) (HookPolicy, error)
	Publish(context.Context, string, string, string) (HookPolicy, error)
	Runs(context.Context, string, string) ([]HookRunRecord, error)
	RecordRun(context.Context, string, HookRunRecord) error
	RefreshBaseline(context.Context, string, string) (time.Time, error)
}

type memoryHookPolicy struct {
	policy       HookPolicy
	observedRuns int
	baselineAt   time.Time
}

type MemoryHookRepository struct {
	mu         sync.Mutex
	workspaces map[string]map[string]*memoryHookPolicy
	runs       map[string][]HookRunRecord
}

func NewMemoryHookRepository() *MemoryHookRepository {
	return &MemoryHookRepository{workspaces: make(map[string]map[string]*memoryHookPolicy), runs: make(map[string][]HookRunRecord)}
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

func (r *MemoryHookRepository) Get(_ context.Context, workspaceID, id string) (HookPolicy, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	row, ok := r.workspaces[workspaceID][id]
	if !ok {
		return HookPolicy{}, ErrHookPolicyNotFound
	}
	return memoryPolicyResponse(row), nil
}

func (r *MemoryHookRepository) Create(_ context.Context, workspaceID string, actor HookPermissionActor, policy HookPolicy) (HookPolicy, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if policy.ID == "" {
		policy.ID = uuid.NewString()
	}
	policy.Version = 1
	policy.Mode = HookModeDryRun
	policy.CreatedByID = actor.ID
	policy.CreatedByType = actor.Type
	policy.UpdatedAt = time.Now().UTC()
	if r.workspaces[workspaceID] == nil {
		r.workspaces[workspaceID] = make(map[string]*memoryHookPolicy)
	}
	r.workspaces[workspaceID][policy.ID] = &memoryHookPolicy{policy: policy}
	return policy, nil
}

func (r *MemoryHookRepository) Update(_ context.Context, workspaceID string, actor HookPermissionActor, id string, policy HookPolicy) (HookPolicy, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	current, ok := r.workspaces[workspaceID][id]
	if !ok {
		return HookPolicy{}, ErrHookPolicyNotFound
	}
	if current.policy.Mode == HookModeManaged && !actor.IsOwner {
		return HookPolicy{}, ErrManagedHookLocked
	}
	policy.ID = id
	policy.Version = current.policy.Version + 1
	policy.Mode = HookModeDryRun
	policy.CreatedByID = actor.ID
	policy.CreatedByType = actor.Type
	policy.UpdatedAt = time.Now().UTC()
	r.workspaces[workspaceID][id] = &memoryHookPolicy{policy: policy}
	return policy, nil
}

func (r *MemoryHookRepository) Publish(_ context.Context, workspaceID, id, _ string) (HookPolicy, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	row, ok := r.workspaces[workspaceID][id]
	if !ok {
		return HookPolicy{}, ErrHookPolicyNotFound
	}
	if row.observedRuns == 0 || row.baselineAt.IsZero() {
		return HookPolicy{}, ErrHookPublishPrerequisite
	}
	row.policy.Mode = HookModeEnforce
	row.policy.UpdatedAt = time.Now().UTC()
	return row.policy, nil
}

func memoryPolicyResponse(row *memoryHookPolicy) HookPolicy {
	policy := row.policy
	policy.ObservedRuns = row.observedRuns
	if !row.baselineAt.IsZero() {
		baseline := row.baselineAt
		policy.BaselineAt = &baseline
	}
	policy.CanPublish = policy.Mode == HookModeDryRun && row.observedRuns > 0 && !row.baselineAt.IsZero()
	return policy
}

func (r *MemoryHookRepository) Runs(_ context.Context, workspaceID, policyID string) ([]HookRunRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := workspaceID + ":" + policyID
	return append([]HookRunRecord(nil), r.runs[key]...), nil
}

func (r *MemoryHookRepository) RecordRun(_ context.Context, workspaceID string, run HookRunRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	row, ok := r.workspaces[workspaceID][run.PolicyID]
	if !ok {
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
	return nil
}

func (r *MemoryHookRepository) RefreshBaseline(_ context.Context, workspaceID, policyID string) (time.Time, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	row, ok := r.workspaces[workspaceID][policyID]
	if !ok {
		return time.Time{}, ErrHookPolicyNotFound
	}
	if row.observedRuns == 0 {
		return time.Time{}, ErrHookPublishPrerequisite
	}
	row.baselineAt = time.Now().UTC()
	return row.baselineAt, nil
}

func (r *MemoryHookRepository) Seed(workspaceID string, policy HookPolicy) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.workspaces[workspaceID] == nil {
		r.workspaces[workspaceID] = make(map[string]*memoryHookPolicy)
	}
	r.workspaces[workspaceID][policy.ID] = &memoryHookPolicy{policy: policy}
}

func (r *MemoryHookRepository) RecordObservedRun(workspaceID, policyID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if row := r.workspaces[workspaceID][policyID]; row != nil {
		row.observedRuns++
	}
}

func (r *MemoryHookRepository) MarkBaselineFresh(workspaceID, policyID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if row := r.workspaces[workspaceID][policyID]; row != nil {
		row.baselineAt = time.Now().UTC()
	}
}
