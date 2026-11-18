package kagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"sort"
	"strings"
	"sync"
)

// Action reports what the last reconcile did.
type Action string

const (
	ActionCreated   Action = "created"
	ActionUpdated   Action = "updated"
	ActionUnchanged Action = "unchanged"
)

// Outcome is the result of one reconcile pass.
type Outcome struct {
	unsupportedTemplate bool
	Terminating         bool
	Action              Action
	Ready               bool   // true only when a current-generation Ready=True condition is present
	ReadySummary        string // e.g. "Ready=True" or "Ready=False (HarnessCompatibility reason: ...)"
	ResourceVersion     string
	UID                 string
	DesiredRevision     string
}

// AgentState is the observed cluster state of one Multica-owned resource,
// as reported by recovery. It carries no in-memory reconciliation state:
// a fresh process recovers purely from the cluster (restart recovery).
type AgentState struct {
	AgentID          string
	Name             string
	Namespace        string
	Ready            bool
	ObservedRevision string // AnnotationDesiredRevision on the resource
	ResourceVersion  string
	UID              string
}

// Config configures the reconciler.
type Config struct {
	// AllowedNamespaces is the namespace allowlist. Empty means deny all:
	// nothing is read or written outside the allowlist (fail closed).
	AllowedNamespaces []string

	// MaxUpdateRetries bounds optimistic-concurrency retries for update and
	// delete; <=0 uses 3.
	MaxUpdateRetries int

	Logger *slog.Logger
}

// Reconciler applies one already-authorized desired state to Kubernetes.
// Its write surface is deliberately unexported: Lifecycle is the only
// sanctioned path for reconcile and delete. Low-level direct writes cannot
// fence stale queued desired state against enrollment updates, tombstoned
// deletion, or concurrent enrollment holders; the unexported methods are
// reachable only in-package (unit tests) and always through Lifecycle in
// production.
type Reconciler struct {
	store  Store
	cfg    Config
	logger *slog.Logger

	mu    sync.Mutex
	locks map[string]*sync.Mutex // per-agent serialization key: ns/name
}

func New(store Store, cfg Config) (*Reconciler, error) {
	if store == nil {
		return nil, fmt.Errorf("kagent: store is required")
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Reconciler{
		store:  store,
		cfg:    cfg,
		logger: logger,
		locks:  map[string]*sync.Mutex{},
	}, nil
}

// lockFor serializes reconciliation per resource key so two concurrent
// passes for the same agent never race Get/Create.
func (r *Reconciler) lockFor(key string) *sync.Mutex {
	r.mu.Lock()
	defer r.mu.Unlock()
	m, ok := r.locks[key]
	if !ok {
		m = &sync.Mutex{}
		r.locks[key] = m
	}
	return m
}

func (r *Reconciler) namespaceAllowed(ns string) bool {
	for _, allowed := range r.cfg.AllowedNamespaces {
		if allowed == ns {
			return true
		}
	}
	return false
}

// ValidateDesired fails closed on anything this slice does not support.
func ValidateDesired(d Desired) error {
	if _, err := ResourceName(d.AgentID); err != nil {
		return err
	}
	if !agentIDPattern.MatchString(d.OwnerToken) {
		return fmt.Errorf("%w: persisted owner token UUID is required", ErrInvalidDesired)
	}
	if d.HarnessRefName == "" {
		return fmt.Errorf("%w: HarnessRefName is required (the reconciler references platform-managed harnesses but never creates them)", ErrInvalidDesired)
	}
	if d.Inline == nil {
		return fmt.Errorf("%w: exactly one of template/templateRef is required; this slice only writes inline templates (spec.template)", ErrInvalidDesired)
	}
	if len(d.Inline.ExtraFields) != 0 {
		return fmt.Errorf("%w: unsupported inline template fields", ErrUnsupportedField)
	}
	if d.Inline.Description == "" && d.Inline.SystemPrompt == "" && d.Inline.SystemPromptFrom == nil {
		return fmt.Errorf("%w: inline template must set at least Description or SystemPrompt[SystemPromptFrom]", ErrInvalidDesired)
	}
	// v1alpha3 CEL: systemPrompt and systemPromptFrom are mutually exclusive.
	if d.Inline.SystemPrompt != "" && d.Inline.SystemPromptFrom != nil {
		return fmt.Errorf("%w: systemPrompt and systemPromptFrom are mutually exclusive (v1alpha3 CEL)", ErrInvalidDesired)
	}
	if d.Inline.ModelConfig != nil && d.Inline.ModelConfig.Name == "" {
		return fmt.Errorf("%w: modelConfig.name must not be empty", ErrInvalidDesired)
	}
	if d.Inline.SystemPromptFrom != nil && (d.Inline.SystemPromptFrom.Name == "" || d.Inline.SystemPromptFrom.Key == "") {
		return fmt.Errorf("%w: systemPromptFrom requires name and key (v1alpha3 ConfigMapKeyReference)", ErrInvalidDesired)
	}
	if strings.Contains(d.AgentName, "\x00") {
		return fmt.Errorf("%w: AgentName contains NUL", ErrInvalidDesired)
	}
	return nil
}

// reconcile idempotently drives the cluster to the desired state for one
// agent: create if absent, update owned fields if drifted, report readiness.
// It never touches resources it does not own.
//
// Unexported on purpose: Lifecycle.Reconcile is the only production path.
// Calling this directly (in-package only) bypasses the enrollment journal
// tombstone, which is why no exported write surface exists on the reconciler.
func (r *Reconciler) reconcile(ctx context.Context, d Desired) (Outcome, error) {
	if err := ValidateDesired(d); err != nil {
		return Outcome{}, err
	}
	if !r.namespaceAllowed(d.Namespace) {
		return Outcome{}, fmt.Errorf("%w: %q", ErrNamespaceNotAllowed, d.Namespace)
	}
	name, err := ResourceName(d.AgentID)
	if err != nil {
		return Outcome{}, err
	}
	rev := DesiredRevision(d)

	key := d.Namespace + "/" + name
	lock := r.lockFor(key)
	lock.Lock()
	defer lock.Unlock()

	const maxAttempts = 8 // bounded; generous headroom over MaxUpdateRetries
	maxRetries := r.cfg.MaxUpdateRetries
	if maxRetries <= 0 {
		maxRetries = 3
	}
	var lastConflict error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		cur, err := r.store.Get(ctx, d.Namespace, name)
		if errors.Is(err, ErrNotFound) {
			if d.ExpectedUID != "" {
				return Outcome{}, fmt.Errorf("%w: bound resource disappeared; explicit enrollment is required", ErrNotFound)
			}
			created := r.buildAgent(d, name, rev)
			err := r.store.Create(ctx, created)
			if errors.Is(err, ErrConflict) {
				lastConflict = err
				continue // re-read; an unbound enrollment cannot adopt the winner
			}
			if err != nil {
				return Outcome{}, err
			}
			if created.Metadata.UID == "" {
				return Outcome{}, fmt.Errorf("kagent: create response missing resource UID")
			}
			confirmed := d
			confirmed.ExpectedUID = created.Metadata.UID
			if !ownedBy(created, confirmed) {
				return Outcome{}, ErrNotOwned
			}
			out := outcomeFrom(created, rev)
			out.Action = ActionCreated
			r.logger.Info("kagent reconciler: created agent resource",
				"namespace", d.Namespace, "name", name, "agent_id", d.AgentID, "revision", rev)
			return out, nil
		}
		if err != nil {
			return Outcome{}, err
		}
		if !ownedBy(cur, d) {
			return Outcome{}, fmt.Errorf("%w: namespace=%s name=%s", ErrNotOwned, cur.Metadata.Namespace, cur.Metadata.Name)
		}
		if cur.Metadata.DeletionTimestamp != nil {
			return Outcome{}, ErrTerminating
		}
		if hasUnsupportedTemplate(cur) {
			return Outcome{}, fmt.Errorf("%w: observed template has unsupported fields", ErrUnsupportedField)
		}
		if ownedFieldsEqual(cur, d, rev) {
			out := outcomeFrom(cur, rev)
			out.Action = ActionUnchanged
			return out, nil
		}
		merged := r.applyDesired(cur, d, name, rev)
		if err := r.store.Update(ctx, merged); errors.Is(err, ErrConflict) {
			lastConflict = err
			if attempt >= maxRetries {
				break
			}
			continue
		} else if err != nil {
			return Outcome{}, err
		}
		out := outcomeFrom(merged, rev)
		out.Action = ActionUpdated
		r.logger.Info("kagent reconciler: updated agent resource",
			"namespace", d.Namespace, "name", name, "agent_id", d.AgentID, "revision", rev)
		return out, nil
	}
	return Outcome{}, fmt.Errorf("kagent: reconcile did not converge after %d attempts: %w", maxAttempts, lastConflict)
}

// delete removes a Multica-owned resource. It fails closed unless the
// resource is owned AND the caller has confirmed drain of active work
// (Multica owns the decision that no active session/task remains).
//
// The delete carries UID+resourceVersion preconditions: the store must only
// delete the exact object observed here. If the object changed in between
// (concurrent update, or delete-and-recreate under the same name), the
// precondition fails closed and the pass re-reads, re-validates ownership and
// drains, and retries a bounded number of times. The per-agent lock held here
// does not protect against other controllers or platform admins writing
// concurrently — the preconditions are the protection.
//
// Unexported on purpose: Lifecycle.Delete is the only production path; it
// persists the tombstone before invoking this, so a stale writer holding an
// old Desired cannot delete or resurrect after the enrollment is gone.
func (r *Reconciler) delete(ctx context.Context, d Desired, drainConfirmed bool) error {
	if !r.namespaceAllowed(d.Namespace) {
		return fmt.Errorf("%w: %q", ErrNamespaceNotAllowed, d.Namespace)
	}
	name, err := ResourceName(d.AgentID)
	if err != nil {
		return err
	}
	lock := r.lockFor(d.Namespace + "/" + name)
	lock.Lock()
	defer lock.Unlock()

	const maxAttempts = 8 // bounded; generous headroom over MaxUpdateRetries
	maxRetries := r.cfg.MaxUpdateRetries
	if maxRetries <= 0 {
		maxRetries = 3
	}
	var lastPreconditionErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		cur, err := r.store.Get(ctx, d.Namespace, name)
		if errors.Is(err, ErrNotFound) {
			return nil // already gone: idempotent
		}
		if err != nil {
			return err
		}
		if !ownedBy(cur, d) {
			return fmt.Errorf("%w: namespace=%s name=%s", ErrNotOwned, cur.Metadata.Namespace, cur.Metadata.Name)
		}
		if !drainConfirmed {
			return fmt.Errorf("%w: agent_id=%s name=%s", ErrDrainRequired, d.AgentID, name)
		}
		// Strict preconditions: never delete blind. An empty observed
		// uid/resourceVersion means the transport did not report identity;
		// refuse rather than guess.
		if cur.Metadata.UID == "" || cur.Metadata.ResourceVersion == "" {
			return fmt.Errorf("kagent: refusing delete: observed uid/resourceVersion missing (uid=%q rv=%q)", cur.Metadata.UID, cur.Metadata.ResourceVersion)
		}
		err = r.store.Delete(ctx, d.Namespace, name, DeletePreconditions{
			UID:             cur.Metadata.UID,
			ResourceVersion: cur.Metadata.ResourceVersion,
		})
		if errors.Is(err, ErrNotFound) {
			return nil // deleted between Get and Delete: done
		}
		if errors.Is(err, ErrPreconditionFailed) || errors.Is(err, ErrConflict) {
			lastPreconditionErr = err
			if attempt >= maxRetries {
				break
			}
			continue // object changed under us: re-read, re-validate, retry
		}
		if err != nil {
			return err
		}
		r.logger.Info("kagent reconciler: deleted agent resource",
			"namespace", d.Namespace, "name", name, "agent_id", d.AgentID,
			"uid", cur.Metadata.UID, "resource_version", cur.Metadata.ResourceVersion)
		return nil
	}
	return fmt.Errorf("kagent: delete did not converge after %d attempts: %w", maxAttempts, lastPreconditionErr)
}

// EnrollmentRecord is the authoritative persisted enrollment state for one
// agent, loaded from durable Multica storage (the enrollment journal today;
// the server DB when wired). Recover compares cluster observations against
// these records and never trusts the observed resource's own annotations:
// labels and annotations can be copied by any writer with cluster access,
// while the persisted record cannot. ExpectedUID is the enrollment's pinned
// server-assigned UID — the one field a forger of labels/token annotations
// cannot replicate on a fabricated resource.
type EnrollmentRecord struct {
	AgentID     string
	Namespace   string
	OwnerToken  string
	ExpectedUID string
	Deleted     bool
}

// Recover lists resources in allowed namespaces and reports the observed
// state of those matching an AUTHORITATIVE persisted enrollment record.
// Ownership is never inferred from the resource's own annotation: the
// records argument is the only ownership authority.
//
// A resource is reported only when ALL hold against the authoritative record:
//   - the record exists, is live (not tombstoned), and carries a token;
//   - the record is bound (ExpectedUID non-empty) and the resource's
//     server-assigned metadata.uid matches it exactly — copied labels and a
//     copied owner-token annotation alone never grant recovery claims;
//   - managed-by label, agent-id label, token annotation, deterministic
//     name and the enrolled namespace all match (ownedBy).
//
// This observation is read-only: drift repair happens when the desired-state
// owner (Multica DB) replays Desired through Lifecycle. A fresh process
// recovers observation purely from the cluster cross-checked against the
// persisted enrollment records.
func (r *Reconciler) Recover(ctx context.Context, authoritative []EnrollmentRecord) ([]AgentState, error) {
	byAgent := make(map[string]EnrollmentRecord, len(authoritative))
	for _, rec := range authoritative {
		key := strings.ToLower(rec.AgentID)
		if key == "" {
			continue // identity-less record claims nothing
		}
		if _, dup := byAgent[key]; dup {
			return nil, fmt.Errorf("kagent: duplicate authoritative enrollment record for agent %q", rec.AgentID)
		}
		byAgent[key] = rec
	}
	namespaces := append([]string(nil), r.cfg.AllowedNamespaces...)
	sort.Strings(namespaces)
	var out []AgentState
	for _, ns := range namespaces {
		agents, err := r.store.List(ctx, ns)
		if err != nil {
			return nil, err
		}
		for _, a := range agents {
			agentID := a.Metadata.Labels[LabelAgentID]
			rec, ok := byAgent[strings.ToLower(agentID)]
			if !ok || rec.Deleted || rec.OwnerToken == "" || rec.ExpectedUID == "" {
				continue // no authoritative, bound, live enrollment: no ownership claim
			}
			if !ownedBy(a, Desired{AgentID: rec.AgentID, Namespace: rec.Namespace, OwnerToken: rec.OwnerToken, ExpectedUID: rec.ExpectedUID}) {
				continue // copied labels or a forged token annotation are not ownership
			}
			ready, _ := readiness(a)
			out = append(out, AgentState{
				AgentID:          rec.AgentID,
				Name:             a.Metadata.Name,
				Namespace:        a.Metadata.Namespace,
				Ready:            ready,
				ObservedRevision: a.Metadata.Annotations[AnnotationDesiredRevision],
				ResourceVersion:  a.Metadata.ResourceVersion,
				UID:              a.Metadata.UID,
			})
		}
	}
	return out, nil
}

// buildAgent constructs a new Agent for create.
func (r *Reconciler) buildAgent(d Desired, name, rev string) *Agent {
	a := &Agent{
		APIVersion: GroupVersionAPI,
		Kind:       KindAgent,
		Metadata: ObjectMeta{
			Name:      name,
			Namespace: d.Namespace,
			Labels: map[string]string{
				LabelManagedBy: LabelManagedByValue,
				LabelAgentID:   strings.ToLower(d.AgentID),
			},
			Annotations: map[string]string{
				AnnotationAgentName:       d.AgentName,
				AnnotationOwnerToken:      d.OwnerToken,
				AnnotationDesiredRevision: rev,
			},
		},
	}
	a.Spec.HarnessRef = &LocalObjectReference{Name: d.HarnessRefName}
	a.Spec.Template = cloneTemplate(d.Inline)
	// TemplateRef and Harness are never written on create: this slice always
	// writes the inline template and references the platform harness.
	return a
}

// applyDesired merges the desired state into the current object, touching
// ONLY owned fields: spec.harnessRef, spec.template, the exactly-one
// counterparts (spec.templateRef, spec.harness) and the managed
// labels/annotations. Everything else (foreign labels, annotations, status,
// and any spec surface not modeled here — egress policy, tools, skills,
// plugins, outputSchema) is preserved by the store: this function must never
// zero a field it does not own, and Store implementations must not prune
// unmodeled fields on update. The status is never written by Multica.
func (r *Reconciler) applyDesired(cur *Agent, d Desired, name, rev string) *Agent {
	out := *cur // shallow copy; deep-copy anything we mutate below
	out.Metadata.Labels = cloneMap(cur.Metadata.Labels)
	out.Metadata.Annotations = cloneMap(cur.Metadata.Annotations)
	out.Metadata.Labels[LabelManagedBy] = LabelManagedByValue
	out.Metadata.Labels[LabelAgentID] = strings.ToLower(d.AgentID)
	out.Metadata.Annotations[AnnotationAgentName] = d.AgentName
	out.Metadata.Annotations[AnnotationDesiredRevision] = rev
	out.Spec.HarnessRef = &LocalObjectReference{Name: d.HarnessRefName}
	out.Spec.Harness = nil // owned: inline harness is never authored by Multica
	out.Spec.Template = cloneTemplate(d.Inline)
	out.Spec.TemplateRef = nil // owned: exactly-one with spec.template
	return &out
}

// ownedBy requires enrollment identity, token and a pinned server UID.
func ownedBy(a *Agent, d Desired) bool {
	name, err := ResourceName(d.AgentID)
	return err == nil && a.APIVersion == GroupVersionAPI && a.Kind == KindAgent &&
		a.Metadata.Name == name && a.Metadata.Namespace == d.Namespace &&
		a.Metadata.Labels[LabelManagedBy] == LabelManagedByValue &&
		strings.EqualFold(a.Metadata.Labels[LabelAgentID], d.AgentID) &&
		d.OwnerToken != "" && a.Metadata.Annotations[AnnotationOwnerToken] == d.OwnerToken &&
		(d.ExpectedUID != "" && a.Metadata.UID == d.ExpectedUID)
}

// ownedFieldsEqual compares only the fields this reconciler owns. Returns
// true when the cluster already matches the desired state.
func ownedFieldsEqual(cur *Agent, d Desired, rev string) bool {
	if cur.Spec.HarnessRef == nil || *cur.Spec.HarnessRef != (LocalObjectReference{Name: d.HarnessRefName}) {
		return false
	}
	if cur.Spec.TemplateRef != nil || cur.Spec.Harness != nil {
		return false // exactly-one drift: clear to inline template + harnessRef
	}
	if !reflect.DeepEqual(cur.Spec.Template, cloneTemplate(d.Inline)) {
		return false
	}
	if cur.Metadata.Labels[LabelManagedBy] != LabelManagedByValue ||
		!strings.EqualFold(cur.Metadata.Labels[LabelAgentID], d.AgentID) {
		return false
	}
	if cur.Metadata.Annotations[AnnotationAgentName] != d.AgentName {
		return false
	}
	if cur.Metadata.Annotations[AnnotationDesiredRevision] != rev {
		return false
	}
	return true
}

// cloneTemplate deep-copies the inline template, including retained fields.
func cloneTemplate(t *AgentTemplateSpec) *AgentTemplateSpec {
	if t == nil {
		return nil
	}
	c := *t
	if t.ExtraFields != nil {
		c.ExtraFields = make(map[string]json.RawMessage, len(t.ExtraFields))
		for k, v := range t.ExtraFields {
			c.ExtraFields[k] = append(json.RawMessage(nil), v...)
		}
	}
	if t.ModelConfig != nil {
		mc := *t.ModelConfig
		c.ModelConfig = &mc
	}
	if t.SystemPromptFrom != nil {
		s := *t.SystemPromptFrom
		c.SystemPromptFrom = &s
	}
	return &c
}

func cloneMap(m map[string]string) map[string]string {
	out := make(map[string]string, len(m)+2)
	for k, v := range m {
		out[k] = v
	}
	return out
}

// readiness reports the Ready condition strictly: Ready=True only counts
// when the condition reflects the object's CURRENT metadata.generation. A
// Ready=True observed against an older generation (controller has not yet
// re-observed the newest spec) is stale and must never be reported as ready.
// Missing generation information means readiness cannot be verified.
func hasUnsupportedTemplate(a *Agent) bool {
	return a.Spec.Template != nil && len(a.Spec.Template.ExtraFields) != 0
}

func readiness(a *Agent) (bool, string) {
	if a.Metadata.DeletionTimestamp != nil {
		return false, "Ready=False (terminating)"
	}
	if hasUnsupportedTemplate(a) {
		return false, "Ready=False (unsupported template fields)"
	}
	gen := a.Metadata.Generation
	if gen <= 0 {
		return false, "Ready=False (missing metadata generation)"
	}
	var readyCond, nonReady *Condition
	readyCount := 0
	for i := range a.Status.Conditions {
		c := a.Status.Conditions[i]
		if c.Type == ConditionReady {
			readyCount++
			if readyCount > 1 {
				return false, "Ready=False (duplicate Ready conditions)"
			}
			if c.Status == "True" {
				rc := c
				readyCond = &rc
				continue
			}
			nc := c
			nonReady = &nc
			continue
		}
		if nonReady == nil && c.Status != "True" {
			nc := c
			nonReady = &nc
		}
	}
	if readyCond != nil {
		if gen > 0 && readyCond.ObservedGeneration != gen {
			return false, fmt.Sprintf("Ready=False (stale: condition observedGeneration=%d != generation=%d)",
				readyCond.ObservedGeneration, gen)
		}
		return true, "Ready=True"
	}
	if nonReady == nil {
		return false, "Ready=False (no conditions reported)"
	}
	return false, fmt.Sprintf("Ready=False (%s reason=%q: %s)", nonReady.Type, nonReady.Reason, nonReady.Message)
}

func outcomeFrom(a *Agent, rev string) Outcome {
	ready, summary := readiness(a)
	return Outcome{
		unsupportedTemplate: hasUnsupportedTemplate(a),
		Terminating:         a.Metadata.DeletionTimestamp != nil,
		Ready:               ready,
		ReadySummary:        summary,
		ResourceVersion:     a.Metadata.ResourceVersion,
		UID:                 a.Metadata.UID,
		DesiredRevision:     rev,
	}
}
