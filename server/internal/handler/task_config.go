package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/internal/middleware"
)

const taskConfigSourceType = "control_plane_managed"

// TaskConfigSelectors are non-secret facts that bind a config version to the
// task's intended deployment. They are compared as a complete tuple.
type TaskConfigSelectors struct {
	Repo    string `json:"repo,omitempty"`
	Target  string `json:"target,omitempty"`
	Account string `json:"account,omitempty"`
	Region  string `json:"region,omitempty"`
}

type TaskConfigResolveRequest struct {
	WorkspaceID string
	TaskID      string
	RuntimeID   string
	AgentID     string
	Provider    string
	ProviderRef string
	Version     string
	Selectors   TaskConfigSelectors
}

// TaskConfigProvider returns bytes only to the authenticated daemon request;
// implementations must not log or persist the returned value.
type TaskConfigProvider interface {
	Resolve(context.Context, TaskConfigResolveRequest) ([]byte, error)
}

// SecretsManagerAPI is the narrow AWS surface needed by the adapter.
type SecretsManagerAPI interface {
	GetSecretValue(context.Context, *secretsmanager.GetSecretValueInput, ...func(*secretsmanager.Options)) (*secretsmanager.GetSecretValueOutput, error)
}

type SecretsManagerTaskConfigProvider struct {
	client SecretsManagerAPI
}

type lazySecretsManagerTaskConfigProvider struct {
	once   sync.Once
	client SecretsManagerAPI
	err    error
}

// NewDefaultTaskConfigProvider defers AWS credential/configuration loading
// until a task_config is actually requested. This keeps server startup
// independent of AWS while still wiring the provider contract in production.
func NewDefaultTaskConfigProvider() TaskConfigProvider {
	return &lazySecretsManagerTaskConfigProvider{}
}

func (p *lazySecretsManagerTaskConfigProvider) Resolve(ctx context.Context, req TaskConfigResolveRequest) ([]byte, error) {
	p.once.Do(func() {
		cfg, err := awsconfig.LoadDefaultConfig(ctx)
		if err != nil {
			p.err = errors.New("provider configuration unavailable")
			return
		}
		p.client = secretsmanager.NewFromConfig(cfg)
	})
	if p.err != nil {
		return nil, p.err
	}
	return (&SecretsManagerTaskConfigProvider{client: p.client}).Resolve(ctx, req)
}

func NewSecretsManagerTaskConfigProvider(client SecretsManagerAPI) *SecretsManagerTaskConfigProvider {
	return &SecretsManagerTaskConfigProvider{client: client}
}

func (p *SecretsManagerTaskConfigProvider) Resolve(ctx context.Context, req TaskConfigResolveRequest) ([]byte, error) {
	if p == nil || p.client == nil {
		return nil, errors.New("provider unavailable")
	}
	if err := validateTaskConfigResolveRequest(req); err != nil {
		return nil, errors.New("invalid provider binding")
	}
	out, err := p.client.GetSecretValue(ctx, &secretsmanager.GetSecretValueInput{
		SecretId:  aws.String(req.ProviderRef),
		VersionId: aws.String(req.Version),
	})
	if err != nil {
		return nil, errors.New("provider read failed")
	}
	if out == nil {
		return nil, errors.New("provider returned no value")
	}
	if out.SecretString != nil {
		return []byte(*out.SecretString), nil
	}
	if len(out.SecretBinary) > 0 {
		return append([]byte(nil), out.SecretBinary...), nil
	}
	return nil, errors.New("provider returned empty value")
}

func validateTaskConfigResolveRequest(req TaskConfigResolveRequest) error {
	if req.Provider != "aws_secrets_manager" || strings.TrimSpace(req.ProviderRef) == "" || strings.TrimSpace(req.Version) == "" {
		return errors.New("provider reference is incomplete")
	}
	if strings.TrimSpace(req.WorkspaceID) == "" || strings.TrimSpace(req.TaskID) == "" || strings.TrimSpace(req.RuntimeID) == "" || strings.TrimSpace(req.AgentID) == "" {
		return errors.New("task identity is incomplete")
	}
	if strings.TrimSpace(req.Selectors.Repo) == "" || strings.TrimSpace(req.Selectors.Target) == "" || strings.TrimSpace(req.Selectors.Account) == "" || strings.TrimSpace(req.Selectors.Region) == "" {
		return errors.New("selector tuple is incomplete")
	}
	return nil
}

type taskConfigResolvePayload struct {
	Provider    string              `json:"provider"`
	ProviderRef string              `json:"provider_ref"`
	Version     string              `json:"version"`
	Path        string              `json:"path"`
	Mode        uint32              `json:"mode"`
	Selectors   TaskConfigSelectors `json:"selectors"`
}

// ResolveTaskConfig resolves one project binding for the currently claimed
// task. The response is raw bytes by design: JSON/base64 would make secret
// material observable in API payloads and logs.
func (h *Handler) ResolveTaskConfig(w http.ResponseWriter, r *http.Request) {
	runtimeID := chi.URLParam(r, "runtimeId")
	taskID := chi.URLParam(r, "taskId")
	resourceID := chi.URLParam(r, "resourceId")
	runtime, ok := h.requireDaemonRuntimeAccess(w, r, runtimeID)
	if !ok {
		return
	}
	// Config bytes are never available through a member PAT/JWT. Require the
	// daemon token's executor identity and bind it to the runtime row, so a
	// workspace member cannot resolve another machine's provider reference.
	if daemonID := middleware.DaemonIDFromContext(r.Context()); daemonID == "" || !runtime.DaemonID.Valid || runtime.DaemonID.String != daemonID {
		writeError(w, http.StatusNotFound, "task_config binding not found")
		return
	}
	task, workspaceID, ok := h.requireDaemonTaskAccessWithWorkspace(w, r, taskID)
	if !ok || workspaceID != uuidToString(runtime.WorkspaceID) || uuidToString(task.RuntimeID) != runtimeID {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}
	if task.Status != "dispatched" && task.Status != "waiting_local_directory" {
		writeError(w, http.StatusConflict, "task is not preparing")
		return
	}
	resourceUUID, ok := parseUUIDOrBadRequest(w, resourceID, "resource id")
	if !ok {
		return
	}
	var payload taskConfigResolvePayload
	if err := json.NewDecoder(io.LimitReader(r.Body, 16<<10)).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid task_config request")
		return
	}

	if !task.IssueID.Valid {
		writeError(w, http.StatusNotFound, "task_config binding not found")
		return
	}
	issue, err := h.Queries.GetIssue(r.Context(), task.IssueID)
	if err != nil || !issue.ProjectID.Valid {
		writeError(w, http.StatusNotFound, "task_config binding not found")
		return
	}
	resources, err := h.Queries.ListProjectResources(r.Context(), issue.ProjectID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load task_config binding")
		return
	}
	var binding taskConfigRef
	found := false
	for _, resource := range resources {
		if resource.ID != resourceUUID || resource.ResourceType != "task_config" {
			continue
		}
		if err := json.Unmarshal(resource.ResourceRef, &binding); err != nil {
			writeError(w, http.StatusNotFound, "task_config binding not found")
			return
		}
		found = true
		break
	}
	if !found {
		writeError(w, http.StatusNotFound, "task_config binding not found")
		return
	}
	want := TaskConfigSelectors{Repo: binding.Repo, Target: binding.Target, Account: binding.Account, Region: binding.Region}
	if payload.Provider != binding.Provider || payload.ProviderRef != binding.ProviderRef || payload.Version != binding.Version || payload.Path != binding.Path || payload.Mode != binding.Mode || !taskConfigSelectorsEqual(payload.Selectors, want) {
		writeError(w, http.StatusForbidden, "task_config selector mismatch")
		return
	}
	if h.TaskConfigProvider == nil {
		writeError(w, http.StatusServiceUnavailable, "task_config provider unavailable")
		return
	}
	content, err := h.TaskConfigProvider.Resolve(r.Context(), TaskConfigResolveRequest{
		WorkspaceID: workspaceID,
		TaskID:      taskID,
		RuntimeID:   runtimeID,
		AgentID:     uuidToString(task.AgentID),
		Provider:    binding.Provider,
		ProviderRef: binding.ProviderRef,
		Version:     binding.Version,
		Selectors:   want,
	})
	if err != nil {
		writeError(w, http.StatusBadGateway, "task_config provider failed")
		return
	}
	if len(content) == 0 {
		writeError(w, http.StatusBadGateway, "task_config provider returned empty value")
		return
	}
	defer clear(content)
	w.Header().Set("Content-Type", "application/octet-stream")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(content)
}

func taskConfigSelectorsEqual(a, b TaskConfigSelectors) bool {
	return strings.TrimSpace(a.Repo) == strings.TrimSpace(b.Repo) && strings.TrimSpace(a.Target) == strings.TrimSpace(b.Target) && strings.TrimSpace(a.Account) == strings.TrimSpace(b.Account) && strings.TrimSpace(a.Region) == strings.TrimSpace(b.Region)
}
