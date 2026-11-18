package kagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
)

// fakeStore is an in-memory Store used to exercise the real reconciliation
// logic. It is a test double for the Kubernetes transport only; the
// reconciliation, ownership and safety logic under test is production code.
// It enforces the same contract a real binding must: optimistic concurrency
// on Update and UID+resourceVersion preconditions on Delete.
type fakeStore struct {
	mu   sync.Mutex
	objs map[string]*Agent // key: namespace/name
	ops  []string          // operation log: "verb:ns/name"
	uids int               // monotonically assigned uid counter

	createConflicts int // number of Create calls to fail with ErrConflict
	updateConflicts int // number of Update calls to fail with ErrConflict
	deleteConflicts int // number of Delete calls to fail with ErrPreconditionFailed (simulates a concurrent writer bumping the RV between Get and Delete)
}

func newFakeStore() *fakeStore { return &fakeStore{objs: map[string]*Agent{}} }

func key(ns, name string) string { return ns + "/" + name }

func (s *fakeStore) Get(_ context.Context, ns, name string) (*Agent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.objs[key(ns, name)]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *a
	return &cp, nil
}

func (s *fakeStore) Create(_ context.Context, a *Agent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ops = append(s.ops, "create:"+a.Metadata.Namespace+"/"+a.Metadata.Name)
	if _, ok := s.objs[key(a.Metadata.Namespace, a.Metadata.Name)]; ok {
		return ErrConflict
	}
	if s.createConflicts > 0 {
		s.createConflicts--
		return ErrConflict
	}
	s.uids++
	cp := *a
	cp.Metadata.UID = fmt.Sprintf("uid-%d", s.uids)
	cp.Metadata.ResourceVersion = "rv-1"
	s.objs[key(a.Metadata.Namespace, a.Metadata.Name)] = &cp
	*a = cp
	return nil
}

func (s *fakeStore) Update(_ context.Context, a *Agent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ops = append(s.ops, "update:"+a.Metadata.Namespace+"/"+a.Metadata.Name)
	stored, ok := s.objs[key(a.Metadata.Namespace, a.Metadata.Name)]
	if !ok {
		return ErrNotFound
	}
	if s.updateConflicts > 0 {
		s.updateConflicts--
		stored.Metadata.ResourceVersion = "rv-bump"
		return ErrConflict
	}
	if a.Metadata.ResourceVersion != stored.Metadata.ResourceVersion {
		return ErrConflict
	}
	cp := *a
	cp.Metadata.ResourceVersion = "rv-" + a.Metadata.Annotations[AnnotationDesiredRevision]
	s.objs[key(a.Metadata.Namespace, a.Metadata.Name)] = &cp
	*a = cp
	return nil
}

func (s *fakeStore) Delete(_ context.Context, ns, name string, pre DeletePreconditions) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ops = append(s.ops, "delete:"+ns+"/"+name)
	stored, ok := s.objs[key(ns, name)]
	if !ok {
		return ErrNotFound
	}
	// Fail closed on empty preconditions: a caller that did not observe the
	// object must not be able to delete blind.
	if pre.UID == "" || pre.ResourceVersion == "" {
		return fmt.Errorf("kagent fake: delete requires non-empty uid and resourceVersion preconditions")
	}
	if s.deleteConflicts > 0 {
		s.deleteConflicts--
		stored.Metadata.ResourceVersion = "rv-bump" // concurrent writer between Get and Delete
		return ErrPreconditionFailed
	}
	// UID guards delete-and-recreate; resourceVersion guards concurrent
	// modification. Mismatch leaves the stored object untouched.
	if stored.Metadata.UID != pre.UID || stored.Metadata.ResourceVersion != pre.ResourceVersion {
		return ErrPreconditionFailed
	}
	delete(s.objs, key(ns, name))
	return nil
}

func (s *fakeStore) List(_ context.Context, ns string) ([]*Agent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*Agent
	for k, a := range s.objs {
		if strings.HasPrefix(k, ns+"/") {
			cp := *a
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (s *fakeStore) count(verb string) int {
	n := 0
	for _, op := range s.ops {
		if strings.HasPrefix(op, verb+":") {
			n++
		}
	}
	return n
}

func testConfig() Config {
	return Config{AllowedNamespaces: []string{"kagent-system"}}
}

func testDesired() Desired {
	return Desired{
		AgentID:        "0f0a3c2e-1111-2222-3333-444455556666",
		AgentName:      "builder",
		OwnerToken:     "11111111-2222-3333-4444-555555555555",
		Namespace:      "kagent-system",
		HarnessRefName: "my-harness",
		Inline: &AgentTemplateSpec{
			Description:  "Multica builder",
			SystemPrompt: "You are a builder.",
			ModelConfig:  &LocalObjectReference{Name: "shared-model"},
		},
	}
}

func newTestReconciler(t *testing.T) (*Reconciler, *fakeStore) {
	t.Helper()
	s := newFakeStore()
	r, err := New(s, testConfig())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return r, s
}

// mustCreate reconciles once and returns the created object's name.
func mustCreate(t *testing.T, r *Reconciler, d *Desired) string {
	t.Helper()
	out, err := r.reconcile(context.Background(), *d)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if out.Action != ActionCreated {
		t.Fatalf("action = %q, want created", out.Action)
	}
	name, err := ResourceName(d.AgentID)
	if err != nil {
		t.Fatalf("ResourceName: %v", err)
	}
	d.ExpectedUID = out.UID
	return name
}

func TestReconcileCreateThenUnchanged(t *testing.T) {
	r, s := newTestReconciler(t)
	d := testDesired()

	out, err := r.reconcile(context.Background(), d)
	if err != nil {
		t.Fatalf("first reconcile: %v", err)
	}
	if out.Action != ActionCreated {
		t.Fatalf("first action = %q, want created", out.Action)
	}
	if s.count("create") != 1 || s.count("update") != 0 {
		t.Fatalf("ops after create = %v", s.ops)
	}

	d.ExpectedUID = out.UID
	out2, err := r.reconcile(context.Background(), d)
	if err != nil {
		t.Fatalf("second reconcile: %v", err)
	}
	if out2.Action != ActionUnchanged {
		t.Fatalf("second action = %q, want unchanged", out2.Action)
	}
	if s.count("create") != 1 || s.count("update") != 0 {
		t.Fatalf("second reconcile mutated store: %v", s.ops)
	}
	if out2.Ready {
		t.Fatalf("no controller conditions reported yet; Ready must be false")
	}
	if out2.DesiredRevision != out.DesiredRevision {
		t.Fatalf("desired revision drifted between calls: %q vs %q", out.DesiredRevision, out2.DesiredRevision)
	}
}

// TestAlpha7SpecJSONShape is a schema regression: the written spec must be
// exactly the v1.0.0-alpha7 v1alpha3 shape — a direct spec.template with a
// namespace-local modelConfig (name only) and a flat spec.harnessRef. No
// templateRef, no inline harness, and absolutely no egressPolicyRefs key
// (prior experimental code invented and then dropped that field).
func TestAlpha7SpecJSONShape(t *testing.T) {
	r, s := newTestReconciler(t)
	d := testDesired()
	name := mustCreate(t, r, &d)

	raw, err := json.Marshal(s.objs[key("kagent-system", name)])
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var doc struct {
		Spec map[string]json.RawMessage `json:"spec"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	allowed := map[string]bool{"template": true, "harnessRef": true}
	for k := range doc.Spec {
		if !allowed[k] {
			t.Fatalf("spec key %q is not alpha7 for this slice (spec=%s)", k, raw)
		}
	}
	if len(doc.Spec) != 2 {
		t.Fatalf("spec must have exactly template+harnessRef, got: %s", raw)
	}
	var template struct {
		Description      string           `json:"description"`
		ModelConfig      map[string]any   `json:"modelConfig"`
		SystemPrompt     string           `json:"systemPrompt"`
		SystemPromptFrom *json.RawMessage `json:"systemPromptFrom"`
	}
	if err := json.Unmarshal(doc.Spec["template"], &template); err != nil {
		t.Fatalf("template unmarshal: %v", err)
	}
	if template.Description != "Multica builder" || template.SystemPrompt != "You are a builder." {
		t.Fatalf("template content wrong: %s", doc.Spec["template"])
	}
	if v, ok := template.ModelConfig["name"]; !ok || v != "shared-model" || len(template.ModelConfig) != 1 {
		t.Fatalf("modelConfig must be a namespace-local {name} ref only: %v", template.ModelConfig)
	}
	if template.SystemPromptFrom != nil {
		t.Fatalf("systemPromptFrom must be absent when systemPrompt is set")
	}
	var harnessRef struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(doc.Spec["harnessRef"], &harnessRef); err != nil || harnessRef.Name != "my-harness" {
		t.Fatalf("harnessRef must be a flat {name} ref: %s (%v)", doc.Spec["harnessRef"], err)
	}
}

// TestReconcileClearsTemplateRefAndHarnessDrift verifies the v1alpha3
// exactly-one rules are repaired on owned resources: a templateRef or an
// inline harness left on the object (by anyone) is replaced by this slice's
// inline template + harnessRef.
func TestReconcileClearsTemplateRefAndHarnessDrift(t *testing.T) {
	r, s := newTestReconciler(t)
	d := testDesired()
	name := mustCreate(t, r, &d)

	// Simulate drift by a platform admin: templateRef instead of inline
	// template, and an inline harness instead of harnessRef.
	stored := s.objs[key("kagent-system", name)]
	stored.Spec.Template = nil
	stored.Spec.TemplateRef = &LocalObjectReference{Name: "external-template"}
	stored.Spec.Harness = map[string]any{"kagent": map[string]any{"memory": "configured-by-platform"}}

	out, err := r.reconcile(context.Background(), d)
	if err != nil {
		t.Fatalf("drift reconcile: %v", err)
	}
	if out.Action != ActionUpdated {
		t.Fatalf("action = %q, want updated", out.Action)
	}
	after := s.objs[key("kagent-system", name)]
	if after.Spec.TemplateRef != nil {
		t.Fatalf("templateRef must be cleared (exactly-one with spec.template)")
	}
	if after.Spec.Harness != nil {
		t.Fatalf("inline harness must be cleared (exactly-one with spec.harnessRef)")
	}
	if after.Spec.Template == nil || after.Spec.Template.SystemPrompt != "You are a builder." {
		t.Fatalf("inline template not written: %+v", after.Spec.Template)
	}
	if after.Spec.HarnessRef == nil || after.Spec.HarnessRef.Name != "my-harness" {
		t.Fatalf("harnessRef not set: %+v", after.Spec.HarnessRef)
	}
}

func TestReconcileRepairsOwnedDriftPreservesForeign(t *testing.T) {
	r, s := newTestReconciler(t)
	d := testDesired()
	name := mustCreate(t, r, &d)

	// Simulate drift: owned field mutated, foreign metadata added.
	stored := s.objs[key("kagent-system", name)]
	stored.Spec.Template.SystemPrompt = "tampered"
	stored.Spec.HarnessRef = &LocalObjectReference{Name: "wrong-harness"}
	stored.Metadata.Labels["team"] = "platform"                 // foreign
	stored.Metadata.Annotations["audit.example/observed"] = "1" // foreign
	stored.Metadata.ResourceVersion = "rv-1"

	out, err := r.reconcile(context.Background(), d)
	if err != nil {
		t.Fatalf("drift reconcile: %v", err)
	}
	if out.Action != ActionUpdated {
		t.Fatalf("action = %q, want updated", out.Action)
	}
	after := s.objs[key("kagent-system", name)]
	if after.Spec.Template.SystemPrompt != "You are a builder." {
		t.Fatalf("owned spec field not repaired: %q", after.Spec.Template.SystemPrompt)
	}
	if after.Spec.HarnessRef == nil || after.Spec.HarnessRef.Name != "my-harness" {
		t.Fatalf("owned harnessRef not repaired: %+v", after.Spec.HarnessRef)
	}
	if after.Metadata.Labels["team"] != "platform" {
		t.Fatalf("foreign label must be preserved")
	}
	if after.Metadata.Annotations["audit.example/observed"] != "1" {
		t.Fatalf("foreign annotation must be preserved")
	}
	if after.Metadata.Labels[LabelManagedBy] != LabelManagedByValue ||
		after.Metadata.Labels[LabelAgentID] != d.AgentID {
		t.Fatalf("ownership labels must remain: %+v", after.Metadata.Labels)
	}
}

func TestReconcileRefusesUnownedResource(t *testing.T) {
	r, s := newTestReconciler(t)
	d := testDesired()
	name, _ := ResourceName(d.AgentID)

	// Pre-existing resource at the deterministic name, NOT owned by Multica.
	s.objs[key("kagent-system", name)] = &Agent{
		Metadata: ObjectMeta{Name: name, Namespace: "kagent-system",
			Labels: map[string]string{"owner": "someone-else"}},
		Spec: AgentSpec{HarnessRef: &LocalObjectReference{Name: "their-harness"}},
	}

	_, err := r.reconcile(context.Background(), d)
	if !errors.Is(err, ErrNotOwned) {
		t.Fatalf("want ErrNotOwned, got %v", err)
	}
	if len(s.ops) != 0 {
		t.Fatalf("unowned resource must not be modified: %v", s.ops)
	}
}

func TestReconcileRefusesForeignAgentOwner(t *testing.T) {
	r, s := newTestReconciler(t)
	d := testDesired()
	name, _ := ResourceName(d.AgentID)

	// Owned by Multica, but by a DIFFERENT agent.
	s.objs[key("kagent-system", name)] = &Agent{
		Metadata: ObjectMeta{Name: name, Namespace: "kagent-system",
			Labels: map[string]string{
				LabelManagedBy: LabelManagedByValue,
				LabelAgentID:   "99999999-9999-9999-9999-999999999999",
			}},
	}

	_, err := r.reconcile(context.Background(), d)
	if !errors.Is(err, ErrNotOwned) {
		t.Fatalf("want ErrNotOwned, got %v", err)
	}
	if len(s.ops) != 0 {
		t.Fatalf("foreign-owned resource must not be modified: %v", s.ops)
	}
}

func TestDeleteRequiresDrainConfirmation(t *testing.T) {
	r, s := newTestReconciler(t)
	d := testDesired()
	name, _ := ResourceName(d.AgentID)
	ctx := context.Background()

	mustCreate(t, r, &d)
	if err := r.delete(ctx, d, false); !errors.Is(err, ErrDrainRequired) {
		t.Fatalf("want ErrDrainRequired, got %v", err)
	}
	if _, ok := s.objs[key("kagent-system", name)]; !ok {
		t.Fatalf("resource must survive unconfirmed delete")
	}
	if err := r.delete(ctx, d, true); err != nil {
		t.Fatalf("confirmed delete: %v", err)
	}
	if _, ok := s.objs[key("kagent-system", name)]; ok {
		t.Fatalf("resource should be deleted")
	}
	// Idempotent: second delete is a no-op.
	if err := r.delete(ctx, d, true); err != nil {
		t.Fatalf("second delete: %v", err)
	}
}

// TestStoreDeletePreconditionsEnforced verifies the fake store (the
// reference contract for the real binding) refuses to delete unless BOTH the
// observed UID and resourceVersion match. UID mismatches cover
// delete-and-recreate under the same name.
func TestStoreDeletePreconditionsEnforced(t *testing.T) {
	r, s := newTestReconciler(t)
	d := testDesired()
	name, _ := ResourceName(d.AgentID)
	mustCreate(t, r, &d)

	stored := s.objs[key("kagent-system", name)]
	// Wrong UID: precondition fails, object intact.
	if err := s.Delete(context.Background(), "kagent-system", name, DeletePreconditions{
		UID:             "uid-wrong",
		ResourceVersion: stored.Metadata.ResourceVersion,
	}); !errors.Is(err, ErrPreconditionFailed) {
		t.Fatalf("wrong uid: want ErrPreconditionFailed, got %v", err)
	}
	if _, ok := s.objs[key("kagent-system", name)]; !ok {
		t.Fatalf("object must survive failed precondition delete")
	}
	// Wrong resourceVersion: precondition fails, object intact.
	if err := s.Delete(context.Background(), "kagent-system", name, DeletePreconditions{
		UID:             stored.Metadata.UID,
		ResourceVersion: "rv-wrong",
	}); !errors.Is(err, ErrPreconditionFailed) {
		t.Fatalf("wrong rv: want ErrPreconditionFailed, got %v", err)
	}
	if _, ok := s.objs[key("kagent-system", name)]; !ok {
		t.Fatalf("object must survive failed precondition delete")
	}
	// Empty preconditions fail closed.
	if err := s.Delete(context.Background(), "kagent-system", name, DeletePreconditions{}); err == nil {
		t.Fatalf("empty preconditions must fail closed")
	}
	if _, ok := s.objs[key("kagent-system", name)]; !ok {
		t.Fatalf("object must survive failed precondition delete")
	}
	// Correct UID + resourceVersion deletes.
	if err := s.Delete(context.Background(), "kagent-system", name, DeletePreconditions{
		UID:             stored.Metadata.UID,
		ResourceVersion: stored.Metadata.ResourceVersion,
	}); err != nil {
		t.Fatalf("matching preconditions delete: %v", err)
	}
	if _, ok := s.objs[key("kagent-system", name)]; ok {
		t.Fatalf("object should be deleted with matching preconditions")
	}
	// Unknown resource: ErrNotFound.
	if err := s.Delete(context.Background(), "kagent-system", name, DeletePreconditions{
		UID:             stored.Metadata.UID,
		ResourceVersion: stored.Metadata.ResourceVersion,
	}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound on absent resource, got %v", err)
	}
}

// TestDeleteSurvivesConcurrentModification verifies the reconciler's delete
// retries on precondition failure: the first delete loses (the store bumps
// the RV to simulate a concurrent writer between Get and Delete), then the
// reconciler re-reads the fresh RV and deletes successfully.
func TestDeleteSurvivesConcurrentModification(t *testing.T) {
	r, s := newTestReconciler(t)
	d := testDesired()
	name, _ := ResourceName(d.AgentID)
	mustCreate(t, r, &d)

	s.deleteConflicts = 1
	if err := r.delete(context.Background(), d, true); err != nil {
		t.Fatalf("delete with one concurrent modification: %v", err)
	}
	if _, ok := s.objs[key("kagent-system", name)]; ok {
		t.Fatalf("resource should be deleted after retry")
	}
	if s.count("delete") != 2 {
		t.Fatalf("want one failed precondition delete plus one successful, ops: %v", s.ops)
	}
}

// TestDeleteFailsClosedWhenPreconditionsNeverSatisfied: if a concurrent
// writer keeps bumping the resource version, the delete must fail closed and
// leave the resource in place.
func TestDeleteFailsClosedWhenPreconditionsNeverSatisfied(t *testing.T) {
	r, s := newTestReconciler(t)
	d := testDesired()
	name, _ := ResourceName(d.AgentID)
	mustCreate(t, r, &d)

	s.deleteConflicts = 99
	if err := r.delete(context.Background(), d, true); err == nil {
		t.Fatalf("delete must fail closed when preconditions can never be satisfied")
	}
	if _, ok := s.objs[key("kagent-system", name)]; !ok {
		t.Fatalf("resource must survive when delete fails closed")
	}
}

func TestDeleteRefusesUnowned(t *testing.T) {
	r, s := newTestReconciler(t)
	d := testDesired()
	name, _ := ResourceName(d.AgentID)
	s.objs[key("kagent-system", name)] = &Agent{
		Metadata: ObjectMeta{Name: name, Namespace: "kagent-system",
			Labels: map[string]string{"owner": "someone-else"}},
	}
	if err := r.delete(context.Background(), d, true); !errors.Is(err, ErrNotOwned) {
		t.Fatalf("want ErrNotOwned, got %v", err)
	}
	if _, ok := s.objs[key("kagent-system", name)]; !ok {
		t.Fatalf("unowned resource must never be deleted")
	}
}

// authRecord returns the authoritative enrollment record matching a desired
// state, bound to the UID of the object currently in the store.
func authRecord(t *testing.T, s *fakeStore, d Desired) EnrollmentRecord {
	t.Helper()
	name, err := ResourceName(d.AgentID)
	if err != nil {
		t.Fatalf("ResourceName: %v", err)
	}
	obj, ok := s.objs[key(d.Namespace, name)]
	if !ok {
		t.Fatalf("authRecord: no resource at %s/%s", d.Namespace, name)
	}
	return EnrollmentRecord{
		AgentID:     d.AgentID,
		Namespace:   d.Namespace,
		OwnerToken:  d.OwnerToken,
		ExpectedUID: obj.Metadata.UID,
	}
}

func TestRecoverListsOwnedOnly(t *testing.T) {
	r, s := newTestReconciler(t)
	d := testDesired()
	if _, err := r.reconcile(context.Background(), d); err != nil {
		t.Fatalf("create: %v", err)
	}
	// Foreign resource in the same namespace must be ignored.
	s.objs["kagent-system/foreign-agent"] = &Agent{
		Metadata: ObjectMeta{Name: "foreign-agent", Namespace: "kagent-system"},
	}

	// Fresh reconciler instance: recovery must depend only on the store and
	// the authoritative persisted records, not in-memory reconcile state.
	r2, err := New(s, testConfig())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	states, err := r2.Recover(context.Background(), []EnrollmentRecord{authRecord(t, s, d)})
	if err != nil {
		t.Fatalf("recover: %v", err)
	}
	if len(states) != 1 {
		t.Fatalf("want 1 owned state, got %d: %+v", len(states), states)
	}
	st := states[0]
	if st.AgentID != d.AgentID || st.Namespace != "kagent-system" || st.Ready {
		t.Fatalf("unexpected state: %+v", st)
	}
	if st.ObservedRevision != DesiredRevision(d) {
		t.Fatalf("observed revision = %q, want %q", st.ObservedRevision, DesiredRevision(d))
	}
}

// forgedResource builds a resource that copies Multica's ownership labels AND
// the owner-token annotation from a legitimately reconciled resource — the
// strongest forgery available to a plain cluster writer, since labels and
// annotations share identical write authority.
func forgedResource(d Desired, name, uid string) *Agent {
	return &Agent{
		APIVersion: GroupVersionAPI, Kind: KindAgent,
		Metadata: ObjectMeta{
			Name: name, Namespace: d.Namespace, UID: uid, ResourceVersion: "rv-forged", Generation: 1,
			Labels: map[string]string{
				LabelManagedBy: LabelManagedByValue,
				LabelAgentID:   d.AgentID,
			},
			Annotations: map[string]string{
				AnnotationOwnerToken:      d.OwnerToken,
				AnnotationAgentName:       d.AgentName,
				AnnotationDesiredRevision: DesiredRevision(d),
			},
		},
		Spec: AgentSpec{
			HarnessRef: &LocalObjectReference{Name: d.HarnessRefName},
			Template:   cloneTemplate(d.Inline),
		},
	}
}

// TestRecoverRequiresAuthoritativeEnrollment is the recovery-ownership
// regression: a resource that copies Multica's labels and owner-token
// annotation is not owned state unless the authoritative persisted record
// matches — including the record's pinned server-assigned UID (which a
// forger of cluster metadata cannot set) and the enrolled namespace.
func TestRecoverRequiresAuthoritativeEnrollment(t *testing.T) {
	r, s := newTestReconciler(t)
	d := testDesired()
	ctx := context.Background()
	name, _ := ResourceName(d.AgentID)
	if _, err := r.reconcile(ctx, d); err != nil {
		t.Fatalf("create: %v", err)
	}
	storedUID := s.objs[key(d.Namespace, name)].Metadata.UID
	auth := EnrollmentRecord{
		AgentID:     d.AgentID,
		Namespace:   d.Namespace,
		OwnerToken:  d.OwnerToken,
		ExpectedUID: storedUID,
	}

	// (a) Copied labels + token under a different name: never claimed, even
	// though the record matches and every observable annotation is correct.
	s.objs["kagent-system/forged-agent"] = forgedResource(d, "forged-agent", storedUID)
	states, err := r.Recover(ctx, []EnrollmentRecord{auth})
	if err != nil {
		t.Fatalf("recover (a): %v", err)
	}
	if len(states) != 1 || states[0].Name != name {
		t.Fatalf("forged resource recovered as owned: %+v", states)
	}

	// (b) Same deterministic name (recreated under the same name) with a
	// different UID: never claimed — the record pins the original UID.
	s.objs[key(d.Namespace, name)] = forgedResource(d, name, "forged-uid")
	if states, err = r.Recover(ctx, []EnrollmentRecord{auth}); err != nil || len(states) != 0 {
		t.Fatalf("UID-forged resource recovered as owned: %+v err=%v", states, err)
	}

	// (c) No authoritative record at all: nothing is claimed, even when the
	// resource carries the exact labels and token annotation Multica wrote.
	s.objs[key(d.Namespace, name)] = forgedResource(d, name, storedUID)
	if states, err = r.Recover(ctx, nil); err != nil || len(states) != 0 {
		t.Fatalf("unrecorded resource recovered as owned: %+v err=%v", states, err)
	}

	// (d) Tombstoned (deleted) record claims nothing: a stale writer cannot
	// resurrect a deleted agent's ownership through a forged resource.
	tomb := auth
	tomb.Deleted = true
	if states, err = r.Recover(ctx, []EnrollmentRecord{tomb}); err != nil || len(states) != 0 {
		t.Fatalf("tombstoned record recovered as owned: %+v err=%v", states, err)
	}

	// (e) Unbound record (enrolled, never reconciled: ExpectedUID empty)
	// claims nothing; there is no authoritative UID to match.
	unbound := auth
	unbound.ExpectedUID = ""
	if states, err = r.Recover(ctx, []EnrollmentRecord{unbound}); err != nil || len(states) != 0 {
		t.Fatalf("unbound record recovered as owned: %+v err=%v", states, err)
	}

	// (f) Token annotation and record must agree; a record with a different
	// token does not claim the resource.
	otherToken := auth
	otherToken.OwnerToken = "00000000-0000-4000-8000-000000000000"
	if states, err = r.Recover(ctx, []EnrollmentRecord{otherToken}); err != nil || len(states) != 0 {
		t.Fatalf("mismatched token record recovered as owned: %+v err=%v", states, err)
	}

	// (g) Record enrolled in a different namespace does not claim the copy.
	otherNS := auth
	otherNS.Namespace = "other-namespace"
	if states, err = r.Recover(ctx, []EnrollmentRecord{otherNS}); err != nil || len(states) != 0 {
		t.Fatalf("cross-namespace record recovered as owned: %+v err=%v", states, err)
	}

	// (h) Duplicate authoritative records are ambiguous: fail closed.
	if _, err = r.Recover(ctx, []EnrollmentRecord{auth, auth}); err == nil {
		t.Fatal("duplicate authoritative records must fail closed")
	}

	// The legitimately reconciled resource remains recoverable throughout.
	s.objs["kagent-system/forged-agent"] = forgedResource(d, "forged-agent", storedUID)
	if states, err = r.Recover(ctx, []EnrollmentRecord{auth}); err != nil {
		t.Fatalf("recover (final): %v", err)
	}
	if len(states) != 1 || states[0].UID != storedUID {
		t.Fatalf("legitimate resource not recovered: %+v", states)
	}
}

// TestReadinessStrictGeneration verifies strict generation readiness: a
// Ready=True condition observed against an OLDER generation is stale and
// must never be reported ready; only a condition covering the current
// generation counts.
func TestReadinessStrictGeneration(t *testing.T) {
	r, s := newTestReconciler(t)
	d := testDesired()
	ctx := context.Background()
	name, _ := ResourceName(d.AgentID)
	mustCreate(t, r, &d)
	obj := s.objs[key("kagent-system", name)]

	// Stale Ready=True: condition observed generation 2, object at 3.
	obj.Metadata.Generation = 3
	obj.Status.Conditions = []Condition{
		{Type: "Accepted", Status: "True", ObservedGeneration: 3},
		{Type: ConditionReady, Status: "True", ObservedGeneration: 2},
	}
	out, err := r.reconcile(ctx, d)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if out.Ready {
		t.Fatalf("stale Ready condition must not report ready")
	}
	if !strings.Contains(out.ReadySummary, "stale") || !strings.Contains(out.ReadySummary, "observedGeneration=2") {
		t.Fatalf("summary must surface staleness: %q", out.ReadySummary)
	}

	// Current Ready=True: condition observed generation matches.
	obj.Status.Conditions = []Condition{
		{Type: "Accepted", Status: "True", ObservedGeneration: 3},
		{Type: ConditionReady, Status: "True", ObservedGeneration: 3},
	}
	out, err = r.reconcile(ctx, d)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if !out.Ready || out.ReadySummary != "Ready=True" {
		t.Fatalf("want Ready=true, got ready=%v summary=%q", out.Ready, out.ReadySummary)
	}

	// Missing generation is unverifiable and must fail closed.
	obj.Metadata.Generation = 0
	obj.Status.Conditions = []Condition{{Type: ConditionReady, Status: "True"}}
	out, err = r.reconcile(ctx, d)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if out.Ready {
		t.Fatalf("without generation info readiness must be false")
	}

	// Recover path must apply the same strictness.
	obj.Metadata.Generation = 5
	obj.Status.Conditions = []Condition{{Type: ConditionReady, Status: "True", ObservedGeneration: 4}}
	states, err := r.Recover(ctx, []EnrollmentRecord{authRecord(t, s, d)})
	if err != nil {
		t.Fatalf("recover: %v", err)
	}
	if len(states) != 1 || states[0].Ready {
		t.Fatalf("stale condition must not report ready in recovery: %+v", states)
	}
}

func TestRecoverReadinessFromConditions(t *testing.T) {
	r, s := newTestReconciler(t)
	d := testDesired()
	if _, err := r.reconcile(context.Background(), d); err != nil {
		t.Fatalf("create: %v", err)
	}
	name, _ := ResourceName(d.AgentID)
	s.objs[key("kagent-system", name)].Metadata.Generation = 1
	s.objs[key("kagent-system", name)].Status.Conditions = []Condition{
		{Type: "Accepted", Status: "True", ObservedGeneration: 1},
		{Type: "Ready", Status: "True", ObservedGeneration: 1},
	}
	states, err := r.Recover(context.Background(), []EnrollmentRecord{authRecord(t, s, d)})
	if err != nil {
		t.Fatalf("recover: %v", err)
	}
	if len(states) != 1 || !states[0].Ready {
		t.Fatalf("want ready state, got %+v", states)
	}

	d.ExpectedUID = s.objs[key(d.Namespace, name)].Metadata.UID
	// Non-ready: Ready=False must surface the blocking condition.
	s.objs[key("kagent-system", name)].Status.Conditions = []Condition{
		{Type: "Accepted", Status: "True"},
		{Type: ConditionReady, Status: "False", Reason: "HarnessCompatibility", Message: "harness incompatible"},
	}
	out, err := r.reconcile(context.Background(), d)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if out.Ready {
		t.Fatalf("Ready=False condition must not report ready")
	}
	if !strings.Contains(out.ReadySummary, "HarnessCompatibility") {
		t.Fatalf("summary should surface the blocking reason: %q", out.ReadySummary)
	}
}

func TestConcurrentReconcilesCreateOnce(t *testing.T) {
	r, s := newTestReconciler(t)
	d := testDesired()

	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := r.reconcile(context.Background(), d)
			if err == nil {
				errs <- nil
			} else {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	successes := 0
	for err := range errs {
		if err == nil {
			successes++
		} else if !errors.Is(err, ErrNotOwned) {
			t.Fatalf("concurrent reconcile: %v", err)
		}
	}
	if successes != 1 {
		t.Fatalf("want only confirmed creator to succeed, got %d", successes)
	}
	if s.count("create") != 1 {
		t.Fatalf("want exactly one create, got %d (ops: %v)", s.count("create"), s.ops)
	}
	if s.count("update") != 0 {
		t.Fatalf("concurrent identical desired states must not update, ops: %v", s.ops)
	}
}

func TestNamespaceAllowlistFailClosed(t *testing.T) {
	r, s := newTestReconciler(t)
	d := testDesired()
	d.Namespace = "kube-system" // not in allowlist

	if _, err := r.reconcile(context.Background(), d); !errors.Is(err, ErrNamespaceNotAllowed) {
		t.Fatalf("want ErrNamespaceNotAllowed, got %v", err)
	}
	if err := r.delete(context.Background(), d, true); !errors.Is(err, ErrNamespaceNotAllowed) {
		t.Fatalf("delete: want ErrNamespaceNotAllowed, got %v", err)
	}
	if len(s.ops) != 0 {
		t.Fatalf("nothing may be touched outside the allowlist: %v", s.ops)
	}
}

func TestUpdateConflictRetryConverges(t *testing.T) {
	r, s := newTestReconciler(t)
	s.updateConflicts = 1 // first Update attempt conflicts
	d := testDesired()
	ctx := context.Background()

	if _, err := r.reconcile(ctx, d); err != nil {
		t.Fatalf("create: %v", err)
	}
	d.ExpectedUID = s.objs[key(d.Namespace, ResourceNamePrefix+d.AgentID)].Metadata.UID
	// Force drift by editing the store directly.
	name, _ := ResourceName(d.AgentID)
	s.objs[key("kagent-system", name)].Spec.HarnessRef = &LocalObjectReference{Name: "wrong"}
	s.objs[key("kagent-system", name)].Metadata.ResourceVersion = "rv-1"

	out, err := r.reconcile(ctx, d)
	if err != nil {
		t.Fatalf("reconcile after conflict: %v", err)
	}
	if out.Action != ActionUpdated {
		t.Fatalf("action = %q, want updated", out.Action)
	}
	if got := s.objs[key("kagent-system", name)].Spec.HarnessRef.Name; got != "my-harness" {
		t.Fatalf("drift not repaired after conflict retry: %q", got)
	}
	if s.count("update") != 2 {
		t.Fatalf("want one conflicted update plus one applied update, ops: %v", s.ops)
	}
}

// TestValidateDesiredFailClosed covers desired-state validation, including
// the v1alpha3 mutual-exclusion rule between systemPrompt and
// systemPromptFrom.
func TestValidateDesiredFailClosed(t *testing.T) {
	d := testDesired()
	if err := ValidateDesired(d); err != nil {
		t.Fatalf("valid desired rejected: %v", err)
	}

	noHarness := d
	noHarness.HarnessRefName = ""
	if err := ValidateDesired(noHarness); !errors.Is(err, ErrInvalidDesired) {
		t.Fatalf("want ErrInvalidDesired for empty harness ref, got %v", err)
	}

	badID := d
	badID.AgentID = "../etc/passwd"
	if _, err := ResourceName(badID.AgentID); !errors.Is(err, ErrInvalidDesired) {
		t.Fatalf("want ErrInvalidDesired for unsafe id, got %v", err)
	}

	empty := d
	empty.Inline = &AgentTemplateSpec{}
	if err := ValidateDesired(empty); !errors.Is(err, ErrInvalidDesired) {
		t.Fatalf("want ErrInvalidDesired for empty inline template, got %v", err)
	}

	// systemPrompt XOR systemPromptFrom (v1alpha3 CEL).
	both := d
	both.Inline.SystemPrompt = "You are a builder."
	both.Inline.SystemPromptFrom = &ConfigMapKeyReference{Name: "prompts", Key: "builder"}
	if err := ValidateDesired(both); !errors.Is(err, ErrInvalidDesired) {
		t.Fatalf("want ErrInvalidDesired for systemPrompt+systemPromptFrom, got %v", err)
	}
	bothOnly := d
	bothOnly.Inline.SystemPrompt = ""
	bothOnly.Inline.SystemPromptFrom = &ConfigMapKeyReference{Name: "prompts", Key: "builder"}
	if err := ValidateDesired(bothOnly); err != nil {
		t.Fatalf("systemPromptFrom alone must be valid: %v", err)
	}
	bothPrompt := d
	bothPrompt.Inline.SystemPrompt = "You are a builder."
	bothPrompt.Inline.SystemPromptFrom = nil
	if err := ValidateDesired(bothPrompt); err != nil {
		t.Fatalf("systemPrompt alone must be valid: %v", err)
	}

	// modelConfig is a namespace-local {name} ref; empty name fails closed.
	emptyModelName := d
	emptyModelName.Inline.ModelConfig = &LocalObjectReference{Name: ""}
	if err := ValidateDesired(emptyModelName); !errors.Is(err, ErrInvalidDesired) {
		t.Fatalf("want ErrInvalidDesired for empty modelConfig name, got %v", err)
	}

	// systemPromptFrom requires both name and key.
	badPromptRef := d
	badPromptRef.Inline.SystemPrompt = ""
	badPromptRef.Inline.SystemPromptFrom = &ConfigMapKeyReference{Name: "prompts", Key: ""}
	if err := ValidateDesired(badPromptRef); !errors.Is(err, ErrInvalidDesired) {
		t.Fatalf("want ErrInvalidDesired for systemPromptFrom without key, got %v", err)
	}
}
