// Package kagent implements an opt-in DEV integration for Kagent alpha7
// (source 542e0a7a82f0f32d09c398f9c56319a3f617c93a).
// Shared harnesses, models and worker pools remain platform-managed.
package kagent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

const (
	// GroupVersionAPI is the deployed Kagent API group/version this
	// reconciler writes (v1alpha3, Kagent v1.0.0-alpha7).
	GroupVersionAPI = "api.kagent.dev/v1alpha3"

	// KindAgent is the Agent resource kind.
	KindAgent = "Agent"

	// LabelManagedBy marks resources Multica owns. A resource is only ever
	// adopted, modified or deleted when it carries this label with the
	// expected value, matching agent identity and durable enrollment token,
	// and a non-empty pinned UID. Labels alone never authorize changes.
	LabelManagedBy      = "app.kubernetes.io/managed-by"
	LabelManagedByValue = "multica"

	// LabelAgentID pins a resource to exactly one Multica agent UUID.
	LabelAgentID = "multica.io/agent-id"

	// AnnotationAgentName is informational; enrollment supplies identity authority.
	AnnotationAgentName = "multica.io/agent-name"

	// AnnotationOwnerToken binds enrollment to durable Multica state, beyond labels.
	AnnotationOwnerToken = "multica.io/owner-token"

	// AnnotationDesiredRevision records the observed desired-state revision
	// so operators and callers can compare Multica's desired revision with
	// what is on the cluster.
	AnnotationDesiredRevision = "multica.io/desired-revision"

	// ResourceNamePrefix prefixes deterministic per-agent resource names.
	// Names are always derived from the immutable agent UUID, never from the
	// mutable agent name, so rename does not orphan resources.
	ResourceNamePrefix = "multica-agent-"

	// ConditionReady is the readiness condition type reported by the Kagent
	// controller (v1alpha3 constants: Accepted, ResolvedRefs, Compatible,
	// Ready).
	ConditionReady = "Ready"
)

// agentIDPattern: Multica agent IDs are UUIDs; the reconciler derives the
// resource name from them, so only name-safe lowercase IDs are accepted.
var agentIDPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// Sentinel errors. Callers must match with errors.Is.
var (
	// ErrTerminating prevents new work on a resource pending deletion.
	ErrTerminating = errors.New("kagent: resource is terminating")

	// ErrNotFound is returned by Store lookups and as a wrapping target by
	// the reconciler when the resource disappeared mid-reconcile.
	ErrNotFound = errors.New("kagent: resource not found")

	// ErrConflict signals an optimistic-concurrency (resourceVersion)
	// conflict; the reconciler re-reads and retries a bounded number of
	// times.
	ErrConflict = errors.New("kagent: resource version conflict")

	// ErrPreconditionFailed is returned by Store.Delete when the object no
	// longer matches the caller-observed UID/resourceVersion (it was
	// modified, replaced or recreated in between). Deletion fails closed.
	ErrPreconditionFailed = errors.New("kagent: delete preconditions failed (uid/resourceVersion mismatch)")

	// ErrNotOwned means the resource exists but is not owned by Multica
	// (mismatched labels, enrollment token, identity or UID). The reconciler
	// refuses to modify or delete it.
	ErrNotOwned = errors.New("kagent: resource exists and is not owned by Multica")

	// ErrNamespaceNotAllowed means the namespace is outside the configured
	// allowlist. Nothing is ever read or written outside the allowlist.
	ErrNamespaceNotAllowed = errors.New("kagent: namespace is not in the allowlist")

	// ErrDrainRequired means deletion was requested without a confirmed
	// drain of active work. Deletion fails closed.
	ErrDrainRequired = errors.New("kagent: deletion requires confirmed drain of active work")

	// ErrUnsupportedField means desired or observed state uses a template field this
	// slice does not manage (e.g. tools, skills, plugins). The reconciler
	// fails closed instead of silently writing an unqualified capability
	// surface onto a cluster.
	ErrUnsupportedField = errors.New("kagent: template uses a field this slice does not manage")

	// ErrInvalidDesired means the desired state itself is invalid.
	ErrInvalidDesired = errors.New("kagent: invalid desired state")
)

// Agent is the subset of the api.kagent.dev/v1alpha3 Agent CR the reconciler
// reads and writes. Field names and JSON shapes mirror the v1.0.0-alpha7
// v1alpha3 schema.
type Agent struct {
	APIVersion string      `json:"apiVersion,omitempty"`
	Kind       string      `json:"kind,omitempty"`
	Metadata   ObjectMeta  `json:"metadata,omitempty"`
	Spec       AgentSpec   `json:"spec,omitempty"`
	Status     AgentStatus `json:"status,omitempty"`
}

// ObjectMeta is the subset of metav1.ObjectMeta this slice needs.
type ObjectMeta struct {
	Name              string            `json:"name,omitempty"`
	Namespace         string            `json:"namespace,omitempty"`
	Labels            map[string]string `json:"labels,omitempty"`
	Annotations       map[string]string `json:"annotations,omitempty"`
	ResourceVersion   string            `json:"resourceVersion,omitempty"`
	UID               string            `json:"uid,omitempty"`
	DeletionTimestamp *string           `json:"deletionTimestamp,omitempty"`
	Generation        int64             `json:"generation,omitempty"`
}

// LocalObjectReference mirrors corev1.LocalObjectReference (name only).
type LocalObjectReference struct {
	Name string `json:"name,omitempty"`
}

// AgentSpec mirrors v1alpha3.AgentSpec: exactly one of Template/TemplateRef
// (CEL: has(self.template) != has(self.templateRef)) and exactly one of
// Harness/HarnessRef (CEL: has(self.harness) != has(self.harnessRef)).
// All references are local to the Agent's namespace.
type AgentSpec struct {
	// Template is the inline portable behavior; mutually exclusive with
	// TemplateRef per the schema. This slice always writes this field.
	// +optional
	Template *AgentTemplateSpec `json:"template,omitempty"`
	// TemplateRef is a flat same-namespace AgentTemplate reference
	// (alpha7 shape: {"name": "..."}). It is platform-managed and this
	// slice never authors it; observed presence on an owned resource is
	// drift and is cleared in favor of Template so the exactly-one rule
	// holds.
	// +optional
	TemplateRef *LocalObjectReference `json:"templateRef,omitempty"`
	// Harness is the inline harness variant; it is never authored by this
	// slice (platform-managed). It is modeled only as raw presence so a
	// real binding can detect and clear it when observed on an owned
	// resource; internals are never interpreted here.
	// +optional
	Harness map[string]any `json:"harness,omitempty"`
	// HarnessRef references the platform-managed Harness by name; this
	// reconciler never creates or modifies the Harness itself.
	// +optional
	HarnessRef *LocalObjectReference `json:"harnessRef,omitempty"`
}

// AgentTemplateSpec models the managed alpha7 template fields. ExtraFields
// retains every other field, including tools, skills, plugins, promptTemplate,
// outputSchema and outputSchemaFrom. Reconciliation rejects their presence.
type AgentTemplateSpec struct {
	ExtraFields map[string]json.RawMessage `json:"-"`
	// ModelConfig references the ModelConfig by name in the SAME namespace
	// as the Agent (namespace-local in this schema; there is no namespace
	// field). Required by managed harnesses, optional for BYO harnesses.
	// +optional
	ModelConfig *LocalObjectReference `json:"modelConfig,omitempty"`
	// +optional
	Description string `json:"description,omitempty"`
	// SystemPrompt is mutually exclusive with SystemPromptFrom (schema CEL:
	// !(has(self.systemPrompt) && has(self.systemPromptFrom))).
	// +optional
	SystemPrompt string `json:"systemPrompt,omitempty"`
	// SystemPromptFrom references prompt text in a same-namespace ConfigMap.
	// +optional
	SystemPromptFrom *ConfigMapKeyReference `json:"systemPromptFrom,omitempty"`
}

// ConfigMapKeyReference mirrors v1alpha3.ConfigMapKeyReference. Name and Key
// are both required by the schema (MinLength=1); enforced by ValidateDesired.
type ConfigMapKeyReference struct {
	Name string `json:"name"`
	Key  string `json:"key"`
}

// AgentStatus carries controller conditions. Fields not used by this slice
// (observedGeneration, revisions, warnings) are deliberately not modeled.
type AgentStatus struct {
	Conditions []Condition `json:"conditions,omitempty"`
}

// Condition mirrors metav1.Condition as exposed on the Agent CR.
type Condition struct {
	Type               string `json:"type,omitempty"`
	Status             string `json:"status,omitempty"`
	Reason             string `json:"reason,omitempty"`
	Message            string `json:"message,omitempty"`
	ObservedGeneration int64  `json:"observedGeneration,omitempty"`
}

// Desired is the Multica-owned desired state for one agent's Kagent
// resource. The reconciler owns the mapping from these knobs to the Agent
// spec, so unsupported capability fields cannot sneak in.
type Desired struct {
	// AgentID is the immutable Multica agent UUID; it determines the
	// resource name and ownership label. Lifecycle binds it to a token and UID.
	AgentID string

	// OwnerToken is a random UUID persisted before provisioning. ExpectedUID is
	// pinned after creation. Use Lifecycle to serialize desired state and deletion.
	OwnerToken  string
	ExpectedUID string

	// AgentName is informational (annotation only).
	AgentName string

	// Namespace must be in the configured allowlist.
	Namespace string

	// HarnessRefName references the shared platform-managed Harness. This
	// reconciler never creates or modifies the Harness itself.
	HarnessRefName string

	// Inline is the inline template written to spec.template. It is the
	// v1alpha3 AgentTemplateSpec restricted to managed fields; only
	// Description/ModelConfig/SystemPrompt(+SystemPromptFrom) are supported.
	// A templateRef is never written by this slice.
	Inline *AgentTemplateSpec
}

// DeletePreconditions gate destructive deletes: the store must only delete
// the object if it is still the exact object the caller observed. UID guards
// against delete-and-recreate (a brand-new object at the same name);
// ResourceVersion additionally guards against any concurrent modification
// between the caller's Get and the Delete call. Empty preconditions are a
// caller error and fail closed.
type DeletePreconditions struct {
	UID             string
	ResourceVersion string
}

// Store is the resource transport contract implemented by KubernetesStore.
// Create and Update return server state through their pointer argument.
type Store interface {
	// Get returns ErrNotFound (errors.Is) when the resource is absent.
	Get(ctx context.Context, namespace, name string) (*Agent, error)
	// Create stores a new resource; ErrConflict if it already exists.
	// Implementations replace agent with the server-returned object, including UID,
	// resourceVersion and generation; they never reuse pre-write status.
	Create(ctx context.Context, agent *Agent) error
	// Update stores the resource; ErrConflict if ResourceVersion no longer
	// matches the stored one. Implementations replace agent with the server response.
	// Implementations MUST NOT prune spec fields
	// outside the modeled set: this reconciler never owns any other spec
	// surface (e.g. a deployed variant's egress policy or tools), so a
	// production binding must merge-patch only owned paths or round-trip
	// unmodeled fields verbatim — a full-object replace is unsafe.
	Update(ctx context.Context, agent *Agent) error
	// Delete removes the resource ONLY when both preconditions hold:
	// metadata.uid == pre.UID AND metadata.resourceVersion ==
	// pre.ResourceVersion. Any mismatch — including a resource replaced
	// under the same name (different UID) — returns ErrPreconditionFailed
	// and leaves the stored object intact. ErrNotFound if absent. Empty
	// preconditions fail closed.
	Delete(ctx context.Context, namespace, name string, pre DeletePreconditions) error
	// List returns all Agents in a namespace.
	List(ctx context.Context, namespace string) ([]*Agent, error)
}

// ResourceName derives the deterministic per-agent resource name.
func ResourceName(agentID string) (string, error) {
	id := strings.ToLower(agentID)
	if !agentIDPattern.MatchString(id) {
		return "", fmt.Errorf("%w: agent id %q is not a name-safe id (lowercase [0-9a-f-], UUID)", ErrInvalidDesired, agentID)
	}
	return ResourceNamePrefix + id, nil
}

// DesiredRevision computes a stable short hash of the desired state so the
// annotation records what was last applied.
func DesiredRevision(d Desired) string {
	type rev struct {
		HarnessRefName string             `json:"harnessRefName"`
		Inline         *AgentTemplateSpec `json:"inline,omitempty"`
	}
	b, err := json.Marshal(rev{HarnessRefName: d.HarnessRefName, Inline: d.Inline})
	if err != nil {
		// Invalid raw fields cannot be applied: ValidateDesired rejects them.
		return "unknown"
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])[:16]
}
