// Package persona is the cerebro-side scaffolding for the Persona grant
// control plane (JEH-978 / JEH-1179 / JEH-1181).
//
// JEH-1181 (this MCP-tools task) ships ahead of JEH-1179 (Fætta's HTTP API).
// To unblock the MCP-tool work — and to let Sara/Tine sanity-check tool
// shapes before the API lands — the tools here are wired against a local
// in-memory `MockBackend` that matches the API shape proposed in JEH-1179:
//
//   - subject (member | agent | group | role | workspace_default | anonymous)
//   - resource_pattern (glob over typed resource strings, e.g. `issue:*`)
//   - capability (canonical action, e.g. `issue.read`)
//   - classification_ceiling (public | internal | confidential | restricted)
//   - validity window (starts_at / ends_at)
//   - approval requirement (requires_approval + approval_role)
//   - effect (allow | deny)
//   - status (active | revoked)
//
// When JEH-1179 lands, swap the `MockBackend` wiring in
// `cmd_mcp_tools_grants.go` for an HTTP-backed `Backend` against
// `/api/workspaces/{id}/grants` without changing the MCP tool surface.
package persona

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Classification levels, ordered from least to most sensitive.
// Ordering matters for the ceiling check in policy evaluation.
const (
	ClassificationPublic       = "public"
	ClassificationInternal     = "internal"
	ClassificationConfidential = "confidential"
	ClassificationRestricted   = "restricted"
)

var classificationOrder = map[string]int{
	ClassificationPublic:       0,
	ClassificationInternal:     1,
	ClassificationConfidential: 2,
	ClassificationRestricted:   3,
}

// Effect values.
const (
	EffectAllow = "allow"
	EffectDeny  = "deny"
)

// Status values.
const (
	StatusActive  = "active"
	StatusRevoked = "revoked"
)

// SubjectType values mirror the audit-table from JEH-978.
const (
	SubjectMember           = "member"
	SubjectAgent            = "agent"
	SubjectGroup            = "group"
	SubjectRole             = "role"
	SubjectWorkspaceDefault = "workspace_default"
	SubjectAnonymous        = "anonymous"
)

// Grant is one row of the proposed JEH-1179 grants table, re-expressed in
// Go. Fields stay in the order the API design doc lists them.
type Grant struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspace_id"`

	SubjectType string `json:"subject_type"`
	SubjectID   string `json:"subject_id,omitempty"`

	ResourcePattern string `json:"resource_pattern"`
	Capability      string `json:"capability"`

	ClassificationCeiling string `json:"classification_ceiling,omitempty"`

	StartsAt *time.Time `json:"starts_at,omitempty"`
	EndsAt   *time.Time `json:"ends_at,omitempty"`

	RequiresApproval bool   `json:"requires_approval"`
	ApprovalRole     string `json:"approval_role,omitempty"`

	Effect string `json:"effect"`
	Status string `json:"status"`

	Note string `json:"note,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	CreatedBy string    `json:"created_by,omitempty"`
	UpdatedBy string    `json:"updated_by,omitempty"`
}

// GrantInput is the create/update payload. Pointer fields preserve
// "not provided" semantics on update — nil means leave unchanged.
type GrantInput struct {
	SubjectType *string `json:"subject_type,omitempty"`
	SubjectID   *string `json:"subject_id,omitempty"`

	ResourcePattern *string `json:"resource_pattern,omitempty"`
	Capability      *string `json:"capability,omitempty"`

	ClassificationCeiling *string `json:"classification_ceiling,omitempty"`

	StartsAt *time.Time `json:"starts_at,omitempty"`
	EndsAt   *time.Time `json:"ends_at,omitempty"`

	RequiresApproval *bool   `json:"requires_approval,omitempty"`
	ApprovalRole     *string `json:"approval_role,omitempty"`

	Effect *string `json:"effect,omitempty"`
	Status *string `json:"status,omitempty"`

	Note *string `json:"note,omitempty"`
}

// ListFilter narrows down `list_grants` results.
type ListFilter struct {
	SubjectType  string
	SubjectID    string
	ResourceLike string
	Capability   string
	Effect       string
	Status       string
	Limit        int
	Offset       int
}

// EvaluationQuery describes a hypothetical action for `evaluate_policy`.
type EvaluationQuery struct {
	ActorType            string `json:"actor_type"`
	ActorID              string `json:"actor_id"`
	Resource             string `json:"resource"`
	Capability           string `json:"capability"`
	ResourceClassification string `json:"resource_classification,omitempty"`
	At                   *time.Time `json:"at,omitempty"`
}

// EvaluationResult is what `evaluate_policy` returns.
type EvaluationResult struct {
	Decision       string      `json:"decision"` // allow | deny
	Reason         string      `json:"reason"`
	MatchedGrants  []Grant     `json:"matched_grants"`
	WinningGrant   *Grant      `json:"winning_grant,omitempty"`
	EvaluatedAt    time.Time   `json:"evaluated_at"`
	Query          EvaluationQuery `json:"query"`
}

// AuditEntry records one mutation or evaluation through the MCP surface.
// JEH-1181 acceptance criterion: "Hver tool-call audit-logges som
// 'actor X via MCP-session Y ændrede grant Z'".
type AuditEntry struct {
	ID        string    `json:"id"`
	Action    string    `json:"action"`
	GrantID   string    `json:"grant_id,omitempty"`
	Actor     string    `json:"actor"`
	ActorType string    `json:"actor_type"`
	Surface   string    `json:"surface"`
	SessionID string    `json:"session_id"`
	At        time.Time `json:"at"`
	Before    *Grant    `json:"before,omitempty"`
	After     *Grant    `json:"after,omitempty"`
	Detail    string    `json:"detail,omitempty"`
}

// AuditContext captures the calling agent for audit-log entries.
// Passed into every backend mutation/read so audit lines stay accurate
// without making the backend re-discover the actor.
type AuditContext struct {
	Actor     string
	ActorType string
	Surface   string
	SessionID string
}

// ErrGrantNotFound is returned by Get/Update/Delete on a missing id.
var ErrGrantNotFound = errors.New("grant not found")

// ErrInvalidGrant covers shape/value validation errors at the backend boundary.
type ErrInvalidGrant struct{ Reason string }

func (e *ErrInvalidGrant) Error() string { return "invalid grant: " + e.Reason }

// Backend is the surface the MCP tools talk to. The mock implements
// everything in-process; the eventual HTTP backend will implement the same
// methods by translating to /api/workspaces/{id}/grants calls.
type Backend interface {
	List(ctx context.Context, f ListFilter, ac AuditContext) ([]Grant, error)
	Get(ctx context.Context, id string, ac AuditContext) (*Grant, error)
	Create(ctx context.Context, in GrantInput, ac AuditContext) (*Grant, error)
	Update(ctx context.Context, id string, in GrantInput, ac AuditContext) (*Grant, error)
	Delete(ctx context.Context, id string, ac AuditContext) error

	Evaluate(ctx context.Context, q EvaluationQuery, ac AuditContext) (*EvaluationResult, error)

	ListAudit(ctx context.Context, grantID string, limit int) ([]AuditEntry, error)
}

// MockBackend is the in-memory stand-in used until JEH-1179 lands.
type MockBackend struct {
	mu          sync.Mutex
	workspaceID string
	grants      map[string]*Grant
	audit       []AuditEntry
	now         func() time.Time
}

// NewMockBackend returns an empty in-memory backend bound to workspaceID.
func NewMockBackend(workspaceID string) *MockBackend {
	return &MockBackend{
		workspaceID: workspaceID,
		grants:      map[string]*Grant{},
		now:         func() time.Time { return time.Now().UTC() },
	}
}

// SetClock is for tests — replaces the time source.
func (m *MockBackend) SetClock(now func() time.Time) { m.now = now }

func (m *MockBackend) List(_ context.Context, f ListFilter, ac AuditContext) ([]Grant, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Grant, 0, len(m.grants))
	for _, g := range m.grants {
		if f.SubjectType != "" && g.SubjectType != f.SubjectType {
			continue
		}
		if f.SubjectID != "" && g.SubjectID != f.SubjectID {
			continue
		}
		if f.Capability != "" && g.Capability != f.Capability {
			continue
		}
		if f.Effect != "" && g.Effect != f.Effect {
			continue
		}
		if f.Status != "" && g.Status != f.Status {
			continue
		}
		if f.ResourceLike != "" && !strings.Contains(g.ResourcePattern, f.ResourceLike) {
			continue
		}
		out = append(out, *g)
	}
	// Stable order: created_at desc, id asc as a tiebreaker.
	sortGrantsNewestFirst(out)
	if f.Offset > 0 {
		if f.Offset >= len(out) {
			out = nil
		} else {
			out = out[f.Offset:]
		}
	}
	if f.Limit > 0 && len(out) > f.Limit {
		out = out[:f.Limit]
	}
	m.recordLocked(AuditEntry{Action: "grant.list", Detail: fmt.Sprintf("filter=%+v", f)}, ac)
	return out, nil
}

func (m *MockBackend) Get(_ context.Context, id string, ac AuditContext) (*Grant, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	g, ok := m.grants[id]
	if !ok {
		return nil, ErrGrantNotFound
	}
	cp := *g
	m.recordLocked(AuditEntry{Action: "grant.read", GrantID: id, After: &cp}, ac)
	return &cp, nil
}

func (m *MockBackend) Create(_ context.Context, in GrantInput, ac AuditContext) (*Grant, error) {
	if err := validateCreate(in); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	g := &Grant{
		ID:               uuid.NewString(),
		WorkspaceID:      m.workspaceID,
		SubjectType:      derefStr(in.SubjectType),
		SubjectID:        derefStr(in.SubjectID),
		ResourcePattern:  derefStr(in.ResourcePattern),
		Capability:       derefStr(in.Capability),
		ClassificationCeiling: derefStr(in.ClassificationCeiling),
		StartsAt:         in.StartsAt,
		EndsAt:           in.EndsAt,
		RequiresApproval: derefBool(in.RequiresApproval),
		ApprovalRole:     derefStr(in.ApprovalRole),
		Effect:           defaultStr(derefStr(in.Effect), EffectAllow),
		Status:           defaultStr(derefStr(in.Status), StatusActive),
		Note:             derefStr(in.Note),
		CreatedAt:        now,
		UpdatedAt:        now,
		CreatedBy:        ac.Actor,
		UpdatedBy:        ac.Actor,
	}
	m.grants[g.ID] = g
	cp := *g
	m.recordLocked(AuditEntry{Action: "grant.create", GrantID: g.ID, After: &cp}, ac)
	return &cp, nil
}

func (m *MockBackend) Update(_ context.Context, id string, in GrantInput, ac AuditContext) (*Grant, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	g, ok := m.grants[id]
	if !ok {
		return nil, ErrGrantNotFound
	}
	before := *g
	applyUpdate(g, in)
	g.UpdatedAt = m.now()
	g.UpdatedBy = ac.Actor
	after := *g
	if err := validateGrant(*g); err != nil {
		// Revert.
		*g = before
		return nil, err
	}
	m.recordLocked(AuditEntry{Action: "grant.update", GrantID: id, Before: &before, After: &after}, ac)
	return &after, nil
}

func (m *MockBackend) Delete(_ context.Context, id string, ac AuditContext) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	g, ok := m.grants[id]
	if !ok {
		return ErrGrantNotFound
	}
	before := *g
	g.Status = StatusRevoked
	g.UpdatedAt = m.now()
	g.UpdatedBy = ac.Actor
	after := *g
	m.recordLocked(AuditEntry{Action: "grant.delete", GrantID: id, Before: &before, After: &after}, ac)
	return nil
}

func (m *MockBackend) Evaluate(_ context.Context, q EvaluationQuery, ac AuditContext) (*EvaluationResult, error) {
	if q.ActorType == "" || q.Capability == "" || q.Resource == "" {
		return nil, &ErrInvalidGrant{Reason: "actor_type, capability, and resource are required"}
	}
	at := m.now()
	if q.At != nil {
		at = *q.At
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	var matched []Grant
	for _, g := range m.grants {
		if !grantApplies(*g, q, at) {
			continue
		}
		matched = append(matched, *g)
	}
	sortGrantsNewestFirst(matched)

	result := &EvaluationResult{
		Decision:    EffectDeny,
		Reason:      "no matching grants — default deny",
		MatchedGrants: matched,
		EvaluatedAt: at,
		Query:       q,
	}
	// Deny wins over allow (mest specifik / mest restriktiv-first).
	for i := range matched {
		if matched[i].Effect == EffectDeny {
			result.Decision = EffectDeny
			result.Reason = fmt.Sprintf("explicit deny grant %s", matched[i].ID)
			g := matched[i]
			result.WinningGrant = &g
			m.recordLocked(AuditEntry{Action: "policy.evaluate", GrantID: matched[i].ID, Detail: result.Reason}, ac)
			return result, nil
		}
	}
	for i := range matched {
		if matched[i].Effect == EffectAllow {
			// Classification ceiling check: if the resource carries a
			// classification higher than the grant allows, the grant does
			// NOT cover this action.
			if q.ResourceClassification != "" && matched[i].ClassificationCeiling != "" {
				rc, okR := classificationOrder[q.ResourceClassification]
				gc, okG := classificationOrder[matched[i].ClassificationCeiling]
				if okR && okG && rc > gc {
					continue
				}
			}
			result.Decision = EffectAllow
			result.Reason = fmt.Sprintf("allow grant %s matched", matched[i].ID)
			if matched[i].RequiresApproval {
				result.Reason += " (requires approval before execution)"
			}
			g := matched[i]
			result.WinningGrant = &g
			m.recordLocked(AuditEntry{Action: "policy.evaluate", GrantID: matched[i].ID, Detail: result.Reason}, ac)
			return result, nil
		}
	}
	m.recordLocked(AuditEntry{Action: "policy.evaluate", Detail: result.Reason}, ac)
	return result, nil
}

func (m *MockBackend) ListAudit(_ context.Context, grantID string, limit int) ([]AuditEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]AuditEntry, 0, len(m.audit))
	for i := len(m.audit) - 1; i >= 0; i-- {
		e := m.audit[i]
		if grantID != "" && e.GrantID != grantID {
			continue
		}
		out = append(out, e)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (m *MockBackend) recordLocked(e AuditEntry, ac AuditContext) {
	e.ID = uuid.NewString()
	e.At = m.now()
	e.Actor = ac.Actor
	e.ActorType = ac.ActorType
	e.Surface = ac.Surface
	if e.Surface == "" {
		e.Surface = "mcp"
	}
	e.SessionID = ac.SessionID
	m.audit = append(m.audit, e)
}

// --- helpers ---------------------------------------------------------------

func validateCreate(in GrantInput) error {
	if in.SubjectType == nil || *in.SubjectType == "" {
		return &ErrInvalidGrant{Reason: "subject_type is required"}
	}
	if !validSubjectType(*in.SubjectType) {
		return &ErrInvalidGrant{Reason: "unknown subject_type: " + *in.SubjectType}
	}
	if in.ResourcePattern == nil || *in.ResourcePattern == "" {
		return &ErrInvalidGrant{Reason: "resource_pattern is required"}
	}
	if in.Capability == nil || *in.Capability == "" {
		return &ErrInvalidGrant{Reason: "capability is required"}
	}
	// SubjectID can be empty for workspace_default / anonymous.
	if *in.SubjectType != SubjectWorkspaceDefault && *in.SubjectType != SubjectAnonymous {
		if in.SubjectID == nil || *in.SubjectID == "" {
			return &ErrInvalidGrant{Reason: "subject_id is required for subject_type " + *in.SubjectType}
		}
	}
	if in.Effect != nil && *in.Effect != "" && *in.Effect != EffectAllow && *in.Effect != EffectDeny {
		return &ErrInvalidGrant{Reason: "effect must be allow or deny"}
	}
	if in.ClassificationCeiling != nil && *in.ClassificationCeiling != "" {
		if _, ok := classificationOrder[*in.ClassificationCeiling]; !ok {
			return &ErrInvalidGrant{Reason: "unknown classification_ceiling: " + *in.ClassificationCeiling}
		}
	}
	if in.StartsAt != nil && in.EndsAt != nil && in.EndsAt.Before(*in.StartsAt) {
		return &ErrInvalidGrant{Reason: "ends_at must be after starts_at"}
	}
	return nil
}

func validateGrant(g Grant) error {
	if !validSubjectType(g.SubjectType) {
		return &ErrInvalidGrant{Reason: "unknown subject_type: " + g.SubjectType}
	}
	if g.ResourcePattern == "" {
		return &ErrInvalidGrant{Reason: "resource_pattern is required"}
	}
	if g.Capability == "" {
		return &ErrInvalidGrant{Reason: "capability is required"}
	}
	if g.Effect != EffectAllow && g.Effect != EffectDeny {
		return &ErrInvalidGrant{Reason: "effect must be allow or deny"}
	}
	if g.Status != StatusActive && g.Status != StatusRevoked {
		return &ErrInvalidGrant{Reason: "status must be active or revoked"}
	}
	return nil
}

func applyUpdate(g *Grant, in GrantInput) {
	if in.SubjectType != nil {
		g.SubjectType = *in.SubjectType
	}
	if in.SubjectID != nil {
		g.SubjectID = *in.SubjectID
	}
	if in.ResourcePattern != nil {
		g.ResourcePattern = *in.ResourcePattern
	}
	if in.Capability != nil {
		g.Capability = *in.Capability
	}
	if in.ClassificationCeiling != nil {
		g.ClassificationCeiling = *in.ClassificationCeiling
	}
	if in.StartsAt != nil {
		g.StartsAt = in.StartsAt
	}
	if in.EndsAt != nil {
		g.EndsAt = in.EndsAt
	}
	if in.RequiresApproval != nil {
		g.RequiresApproval = *in.RequiresApproval
	}
	if in.ApprovalRole != nil {
		g.ApprovalRole = *in.ApprovalRole
	}
	if in.Effect != nil {
		g.Effect = *in.Effect
	}
	if in.Status != nil {
		g.Status = *in.Status
	}
	if in.Note != nil {
		g.Note = *in.Note
	}
}

func validSubjectType(s string) bool {
	switch s {
	case SubjectMember, SubjectAgent, SubjectGroup, SubjectRole, SubjectWorkspaceDefault, SubjectAnonymous:
		return true
	}
	return false
}

// grantApplies returns true if grant g could match the evaluation query at
// the given timestamp. Subject matching is exact for the mock (no group
// expansion); a real Persona deployment will expand group/role membership.
func grantApplies(g Grant, q EvaluationQuery, at time.Time) bool {
	if g.Status != StatusActive {
		return false
	}
	if g.StartsAt != nil && at.Before(*g.StartsAt) {
		return false
	}
	if g.EndsAt != nil && at.After(*g.EndsAt) {
		return false
	}
	if !subjectMatches(g, q) {
		return false
	}
	if g.Capability != q.Capability && !globMatch(g.Capability, q.Capability) {
		return false
	}
	if !globMatch(g.ResourcePattern, q.Resource) {
		return false
	}
	return true
}

func subjectMatches(g Grant, q EvaluationQuery) bool {
	if g.SubjectType == SubjectAnonymous {
		return true
	}
	if g.SubjectType == SubjectWorkspaceDefault {
		return true
	}
	// member/agent: actor must match exactly.
	if g.SubjectType == q.ActorType && g.SubjectID == q.ActorID {
		return true
	}
	// group/role: the mock cannot expand membership; treat as no match.
	// A real backend will expand q.ActorID's groups/roles before this check.
	return false
}

// globMatch supports a single `*` at the end of pattern (`issue:*`) and
// exact match. Enough for stub-validation; real engine will reuse Persona's
// matcher.
func globMatch(pattern, value string) bool {
	if pattern == "" {
		return false
	}
	if pattern == "*" {
		return true
	}
	if strings.HasSuffix(pattern, ":*") {
		prefix := strings.TrimSuffix(pattern, "*")
		return strings.HasPrefix(value, prefix)
	}
	if strings.HasSuffix(pattern, "*") {
		prefix := strings.TrimSuffix(pattern, "*")
		return strings.HasPrefix(value, prefix)
	}
	return pattern == value
}

func sortGrantsNewestFirst(gs []Grant) {
	// Simple insertion sort — n is small in the mock.
	for i := 1; i < len(gs); i++ {
		for j := i; j > 0 && grantLess(gs[j], gs[j-1]); j-- {
			gs[j], gs[j-1] = gs[j-1], gs[j]
		}
	}
}

func grantLess(a, b Grant) bool {
	if !a.CreatedAt.Equal(b.CreatedAt) {
		return a.CreatedAt.After(b.CreatedAt)
	}
	return a.ID < b.ID
}

func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func derefBool(b *bool) bool {
	if b == nil {
		return false
	}
	return *b
}

func defaultStr(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
