package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type OpenAICompatibleCuratorConfig struct {
	Provider       string
	BaseURL        string
	APIKey         string
	Model          string
	EmbeddingModel string
	RuntimeMode    string
	Timeout        time.Duration
}

type OpenAICompatibleCuratorEngine struct {
	cfg    OpenAICompatibleCuratorConfig
	client *http.Client
}

type WorkspaceConfiguredCuratorEngine struct {
	queries *db.Queries
	base    OpenAICompatibleCuratorConfig
}

func NewWorkspaceConfiguredCuratorEngine(queries *db.Queries, base OpenAICompatibleCuratorConfig) CuratorEngine {
	return &WorkspaceConfiguredCuratorEngine{queries: queries, base: normalizeOpenAICompatibleCuratorConfig(base)}
}

func NewOpenAICompatibleCuratorEngine(cfg OpenAICompatibleCuratorConfig) CuratorEngine {
	cfg = normalizeOpenAICompatibleCuratorConfig(cfg)
	if cfg.Provider == "" || cfg.BaseURL == "" || cfg.APIKey == "" || cfg.Model == "" || cfg.EmbeddingModel == "" {
		return MissingCuratorEngine{}
	}
	return &OpenAICompatibleCuratorEngine{cfg: cfg, client: &http.Client{Timeout: cfg.Timeout}}
}

func normalizeOpenAICompatibleCuratorConfig(cfg OpenAICompatibleCuratorConfig) OpenAICompatibleCuratorConfig {
	cfg.Provider = strings.TrimSpace(cfg.Provider)
	cfg.BaseURL = strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	cfg.APIKey = strings.TrimSpace(cfg.APIKey)
	cfg.Model = strings.TrimSpace(cfg.Model)
	cfg.EmbeddingModel = strings.TrimSpace(cfg.EmbeddingModel)
	cfg.RuntimeMode = strings.TrimSpace(cfg.RuntimeMode)
	if cfg.Timeout <= 0 {
		cfg.Timeout = 60 * time.Second
	}
	return cfg
}

func (e *WorkspaceConfiguredCuratorEngine) ForWorkspace(ctx context.Context, workspaceID pgtype.UUID) CuratorEngine {
	cfg := e.base
	if e == nil || e.queries == nil || !workspaceID.Valid {
		return NewOpenAICompatibleCuratorEngine(cfg)
	}
	workspace, err := e.queries.GetWorkspace(ctx, workspaceID)
	if err != nil {
		return NewOpenAICompatibleCuratorEngine(cfg)
	}
	return NewOpenAICompatibleCuratorEngine(applyWorkspaceCuratorSettings(cfg, workspace.Settings))
}

func (e *WorkspaceConfiguredCuratorEngine) GenerateDraft(ctx context.Context, input CuratorDraftInput) (CuratorDraft, error) {
	return e.ForWorkspace(ctx, input.WorkspaceID).GenerateDraft(ctx, input)
}

func (e *WorkspaceConfiguredCuratorEngine) SummarizeSource(ctx context.Context, source CuratorSourceBundle) (string, error) {
	return e.ForWorkspace(ctx, source.WorkspaceID).SummarizeSource(ctx, source)
}

func (e *WorkspaceConfiguredCuratorEngine) BuildEmbedding(ctx context.Context, content string) ([]float32, error) {
	return NewOpenAICompatibleCuratorEngine(e.base).BuildEmbedding(ctx, content)
}

func (e *WorkspaceConfiguredCuratorEngine) Info() CuratorEngineInfo {
	return NewOpenAICompatibleCuratorEngine(e.base).Info()
}

func applyWorkspaceCuratorSettings(base OpenAICompatibleCuratorConfig, rawSettings []byte) OpenAICompatibleCuratorConfig {
	var settings map[string]any
	if len(rawSettings) == 0 || json.Unmarshal(rawSettings, &settings) != nil {
		return base
	}
	raw, ok := settings["knowledge_curator"]
	if !ok || raw == nil {
		return base
	}
	curator, ok := raw.(map[string]any)
	if !ok {
		return base
	}
	if enabled, ok := curator["enabled"].(bool); ok && !enabled {
		return OpenAICompatibleCuratorConfig{Timeout: base.Timeout}
	}
	if value, ok := nonEmptySetting(curator, "provider"); ok {
		base.Provider = value
	}
	if value, ok := nonEmptySetting(curator, "base_url"); ok {
		base.BaseURL = value
	}
	if value, ok := nonEmptySetting(curator, "model"); ok {
		base.Model = value
	}
	if value, ok := nonEmptySetting(curator, "embedding_model"); ok {
		base.EmbeddingModel = value
	}
	if value, ok := nonEmptySetting(curator, "runtime_mode"); ok {
		base.RuntimeMode = value
	}
	return normalizeOpenAICompatibleCuratorConfig(base)
}

func nonEmptySetting(settings map[string]any, key string) (string, bool) {
	value, ok := settings[key].(string)
	value = strings.TrimSpace(value)
	return value, ok && value != ""
}

func (e *OpenAICompatibleCuratorEngine) GenerateDraft(ctx context.Context, input CuratorDraftInput) (CuratorDraft, error) {
	prompt := strings.Join([]string{
		"Generate a reusable Multica knowledge draft from the issue evidence.",
		"Return ONLY valid JSON with keys: title, type, domain_labels, problem_pattern, trigger_conditions, diagnostic_steps, recommended_practice, anti_patterns, applicability, confidence_status.",
		"Allowed type values: lesson, playbook, reference. Allowed confidence_status values: low, medium, high.",
		"Issue:", issueText(input.Issue),
		"Source summary:", input.SourceSummary,
		"Evidence:", curatorEvidenceText(input),
	}, "\n\n")
	var draft CuratorDraft
	if err := e.chatJSON(ctx, prompt, &draft); err != nil {
		return CuratorDraft{}, err
	}
	return draft, nil
}

func (e *OpenAICompatibleCuratorEngine) SummarizeSource(ctx context.Context, source CuratorSourceBundle) (string, error) {
	prompt := strings.Join([]string{
		"Summarize the reusable knowledge signals in this issue evidence in 3-6 concise bullet points.",
		"Focus on root cause, diagnostic path, fix, anti-patterns, and applicability. Return plain text only.",
		"Issue:", issueText(source.Issue),
		"Evidence:", sourceEvidenceText(source),
	}, "\n\n")
	return e.chatText(ctx, prompt)
}

func (e *OpenAICompatibleCuratorEngine) BuildEmbedding(ctx context.Context, content string) ([]float32, error) {
	var resp struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	payload := map[string]any{"model": e.cfg.EmbeddingModel, "input": content}
	if err := e.postJSON(ctx, "/embeddings", payload, &resp); err != nil {
		return nil, err
	}
	if resp.Error != nil && strings.TrimSpace(resp.Error.Message) != "" {
		return nil, errors.New(resp.Error.Message)
	}
	if len(resp.Data) == 0 {
		return nil, errors.New("embedding response contained no vectors")
	}
	if len(resp.Data[0].Embedding) != KnowledgeEmbeddingDimensions {
		return nil, validationError(fmt.Sprintf("embedding must have %d dimensions", KnowledgeEmbeddingDimensions))
	}
	return resp.Data[0].Embedding, nil
}

func (e *OpenAICompatibleCuratorEngine) Info() CuratorEngineInfo {
	return CuratorEngineInfo{Provider: e.cfg.Provider, Model: e.cfg.Model, EmbeddingModel: e.cfg.EmbeddingModel, RuntimeMode: e.cfg.RuntimeMode}
}

func (e *OpenAICompatibleCuratorEngine) chatJSON(ctx context.Context, prompt string, out any) error {
	text, err := e.chatText(ctx, prompt)
	if err != nil {
		return err
	}
	text = strings.TrimSpace(text)
	text = strings.TrimPrefix(text, "```json")
	text = strings.TrimPrefix(text, "```")
	text = strings.TrimSuffix(text, "```")
	if err := json.Unmarshal([]byte(strings.TrimSpace(text)), out); err != nil {
		return fmt.Errorf("curator returned invalid JSON: %w", err)
	}
	return nil
}

func (e *OpenAICompatibleCuratorEngine) chatText(ctx context.Context, prompt string) (string, error) {
	var resp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	payload := map[string]any{
		"model": e.cfg.Model,
		"messages": []map[string]string{
			{"role": "system", "content": "You are Multica's Knowledge Curator. Produce concise, structured, auditable operational knowledge."},
			{"role": "user", "content": prompt},
		},
		"temperature": 0.2,
	}
	if err := e.postJSON(ctx, "/chat/completions", payload, &resp); err != nil {
		return "", err
	}
	if resp.Error != nil && strings.TrimSpace(resp.Error.Message) != "" {
		return "", errors.New(resp.Error.Message)
	}
	if len(resp.Choices) == 0 || strings.TrimSpace(resp.Choices[0].Message.Content) == "" {
		return "", errors.New("curator response contained no content")
	}
	return strings.TrimSpace(resp.Choices[0].Message.Content), nil
}

func (e *OpenAICompatibleCuratorEngine) postJSON(ctx context.Context, path string, payload any, out any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.cfg.BaseURL+path, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+e.cfg.APIKey)
	resp, err := e.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("curator provider returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("decode curator provider response: %w", err)
	}
	return nil
}

func curatorEvidenceText(input CuratorDraftInput) string {
	return strings.Join([]string{sourceEvidenceText(CuratorSourceBundle{Issue: input.Issue, Project: input.Project, Labels: input.Labels, Comments: input.Comments, AgentTasks: input.AgentTasks, PullRequests: input.PullRequests}), candidateEvidenceText(input)}, "\n\n")
}

func candidateEvidenceText(input CuratorDraftInput) string {
	parts := []string{}
	if input.Candidate != nil {
		parts = append(parts, fmt.Sprintf("Candidate reason=%s strength=%s score=%d signals=%s", input.Candidate.TriggerReason, input.Candidate.SignalStrength, input.Candidate.Score, strings.Join(input.Candidate.Signals, ", ")))
	}
	if input.TriggerComment != nil {
		parts = append(parts, "Trigger comment:\n"+excerpt(input.TriggerComment.Content, 1200))
	}
	if input.TriggerTask != nil {
		parts = append(parts, "Trigger task:\n"+excerpt(taskText(*input.TriggerTask), 1200))
	}
	return strings.Join(parts, "\n\n")
}

func sourceEvidenceText(source CuratorSourceBundle) string {
	parts := []string{}
	if source.Project != nil {
		parts = append(parts, "Project: "+source.Project.Title)
	}
	if len(source.Labels) > 0 {
		labels := make([]string, 0, len(source.Labels))
		for _, label := range source.Labels {
			labels = append(labels, label.Name)
		}
		parts = append(parts, "Labels: "+strings.Join(labels, ", "))
	}
	for _, comment := range source.Comments {
		parts = append(parts, "Comment:\n"+excerpt(comment.Content, 800))
	}
	for _, task := range source.AgentTasks {
		parts = append(parts, "Agent task:\n"+excerpt(taskText(task), 1000))
	}
	for _, pr := range source.PullRequests {
		parts = append(parts, fmt.Sprintf("Pull request: %s/%s#%d %s state=%s", pr.RepoOwner, pr.RepoName, pr.PrNumber, pr.Title, pr.State))
	}
	return strings.Join(parts, "\n\n")
}
