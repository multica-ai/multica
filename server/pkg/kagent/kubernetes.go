package kagent

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
)

var namespacePattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

// KubernetesStore uses the namespace-scoped Kubernetes REST API. Its credential
// needs get/list/create/patch/delete on agents.api.kagent.dev in these namespaces.
// Harnesses, templates, secrets and cluster resources are never accessed.
type KubernetesStore struct {
	wire       *wireClient
	namespaces map[string]bool
}

func NewKubernetesStore(c HTTPConfig, namespaces []string) (*KubernetesStore, error) {
	wire, err := newWire(c)
	if err != nil {
		return nil, err
	}
	s := &KubernetesStore{wire: wire, namespaces: map[string]bool{}}
	for _, ns := range namespaces {
		if !namespacePattern.MatchString(ns) {
			return nil, fmt.Errorf("%w: invalid namespace", ErrInvalidDesired)
		}
		s.namespaces[ns] = true
	}
	if len(s.namespaces) == 0 {
		return nil, ErrNamespaceNotAllowed
	}
	return s, nil
}

func (s *KubernetesStore) path(ns, name string) (string, error) {
	if !s.namespaces[ns] {
		return "", ErrNamespaceNotAllowed
	}
	path := "/apis/" + GroupVersionAPI + "/namespaces/" + ns + "/agents"
	if name != "" {
		if !namespacePattern.MatchString(name) && !resourcePattern.MatchString(name) {
			return "", ErrInvalidDesired
		}
		path += "/" + name
	}
	return path, nil
}

var resourcePattern = regexp.MustCompile(`^multica-agent-[0-9a-f-]{36}$`)

func kubeError(err error, deleting bool) error {
	var he *HTTPError
	if errors.As(err, &he) {
		if he.Status == http.StatusNotFound {
			return ErrNotFound
		}
		if he.Status == http.StatusConflict {
			if deleting {
				return ErrPreconditionFailed
			}
			return ErrConflict
		}
	}
	return err
}
func (s *KubernetesStore) Get(ctx context.Context, ns, name string) (*Agent, error) {
	path, err := s.path(ns, name)
	if err != nil {
		return nil, err
	}
	var a Agent
	err = s.wire.do(ctx, "GET", path, "", nil, &a)
	if err != nil {
		return nil, kubeError(err, false)
	}
	if err = validateObserved(&a, ns, name); err != nil {
		return nil, err
	}
	return &a, nil
}
func validateObserved(a *Agent, ns, name string) error {
	if a.APIVersion != GroupVersionAPI || a.Kind != KindAgent || a.Metadata.Namespace != ns || a.Metadata.Name != name || a.Metadata.UID == "" || a.Metadata.ResourceVersion == "" {
		return fmt.Errorf("kagent: incomplete or mismatched Kubernetes Agent")
	}
	return nil
}
func (s *KubernetesStore) Create(ctx context.Context, a *Agent) error {
	path, err := s.path(a.Metadata.Namespace, "")
	if err != nil {
		return err
	}
	var result Agent
	if err = s.wire.do(ctx, "POST", path, "application/json", a, &result); err != nil {
		return kubeError(err, false)
	}
	if err = validateObserved(&result, a.Metadata.Namespace, a.Metadata.Name); err != nil {
		return err
	}
	*a = result
	return nil
}
func (s *KubernetesStore) Update(ctx context.Context, a *Agent) error {
	path, err := s.path(a.Metadata.Namespace, a.Metadata.Name)
	if err != nil {
		return err
	}
	if a.Metadata.UID == "" || a.Metadata.ResourceVersion == "" || a.Spec.Template == nil {
		return ErrInvalidDesired
	}
	t := a.Spec.Template
	// Merge individual owned leaves. Replacing the template would prune fields
	// such as tools, skills, plugins and outputSchema unknown to this client.
	patch := map[string]any{
		"metadata": map[string]any{
			"uid": a.Metadata.UID, "resourceVersion": a.Metadata.ResourceVersion,
			"labels":      map[string]string{LabelManagedBy: a.Metadata.Labels[LabelManagedBy], LabelAgentID: a.Metadata.Labels[LabelAgentID]},
			"annotations": map[string]string{AnnotationOwnerToken: a.Metadata.Annotations[AnnotationOwnerToken], AnnotationAgentName: a.Metadata.Annotations[AnnotationAgentName], AnnotationDesiredRevision: a.Metadata.Annotations[AnnotationDesiredRevision]},
		},
		"spec": map[string]any{
			"harness": nil, "harnessRef": a.Spec.HarnessRef, "templateRef": nil,
			"template": map[string]any{"description": nilIfEmpty(t.Description), "modelConfig": t.ModelConfig, "systemPrompt": nilIfEmpty(t.SystemPrompt), "systemPromptFrom": t.SystemPromptFrom},
		},
	}
	var result Agent
	if err = s.wire.do(ctx, "PATCH", path, "application/merge-patch+json", patch, &result); err != nil {
		return kubeError(err, false)
	}
	if err = validateObserved(&result, a.Metadata.Namespace, a.Metadata.Name); err != nil {
		return err
	}
	*a = result
	return nil
}
func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
func (s *KubernetesStore) Delete(ctx context.Context, ns, name string, pre DeletePreconditions) error {
	path, err := s.path(ns, name)
	if err != nil {
		return err
	}
	if pre.UID == "" || pre.ResourceVersion == "" {
		return ErrPreconditionFailed
	}
	body := map[string]any{"apiVersion": "v1", "kind": "DeleteOptions", "preconditions": map[string]string{"uid": pre.UID, "resourceVersion": pre.ResourceVersion}}
	return kubeError(s.wire.do(ctx, "DELETE", path, "application/json", body, nil), true)
}
func (s *KubernetesStore) List(ctx context.Context, ns string) ([]*Agent, error) {
	path, err := s.path(ns, "")
	if err != nil {
		return nil, err
	}
	var all []*Agent
	token := ""
	seen := map[string]bool{}
	for {
		var page struct {
			Items    []*Agent `json:"items"`
			Metadata struct {
				Continue string `json:"continue"`
			} `json:"metadata"`
		}
		query := url.Values{"limit": {"500"}, "labelSelector": {LabelManagedBy + "=" + LabelManagedByValue}}
		if token != "" {
			query.Set("continue", token)
		}
		if err = s.wire.do(ctx, "GET", path+"?"+query.Encode(), "", nil, &page); err != nil {
			return nil, kubeError(err, false)
		}
		for _, a := range page.Items {
			if a == nil {
				return nil, fmt.Errorf("kagent: null Agent")
			}
			if err = validateObserved(a, ns, a.Metadata.Name); err != nil {
				return nil, err
			}
		}
		all = append(all, page.Items...)
		token = page.Metadata.Continue
		if token == "" {
			return all, nil
		}
		if seen[token] {
			return nil, fmt.Errorf("kagent: repeated list continuation")
		}
		seen[token] = true
	}
}
