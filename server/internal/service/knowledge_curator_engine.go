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

func NewWorkspaceConfiguredCuratorEngine(queries *db.Queries, base OpenAICompatibleCuratorConfig, draftService *CuratorDraftTaskService) CuratorEngine {
	return &WorkspaceConfiguredCuratorEngine{
		queries: queries,
		base:    normalizeOpenAICompatibleCuratorConfig(base),
	}
}

func NewOpenAICompatibleCuratorEngine(cfg OpenAICompatibleCuratorConfig) CuratorEngine {
	cfg = normalizeOpenAICompatibleCuratorConfig(cfg)
	if cfg.Provider == "" || cfg.BaseURL == "" || cfg.Model == "" {
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
	cfg = applyWorkspaceCuratorSettings(cfg, workspace.Settings)
	return NewOpenAICompatibleCuratorEngine(cfg)
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
	return normalizeOpenAICompatibleCuratorConfig(base)
}

func nonEmptySetting(settings map[string]any, key string) (string, bool) {
	value, ok := settings[key].(string)
	value = strings.TrimSpace(value)
	return value, ok && value != ""
}

// curatorSetting reads a single string value from the knowledge_curator block
// in the workspace settings JSONB.
func curatorSetting(rawSettings []byte, key string) (string, bool) {
	var settings map[string]any
	if len(rawSettings) == 0 || json.Unmarshal(rawSettings, &settings) != nil {
		return "", false
	}
	raw, ok := settings["knowledge_curator"]
	if !ok || raw == nil {
		return "", false
	}
	curator, ok := raw.(map[string]any)
	if !ok {
		return "", false
	}
	return nonEmptySetting(curator, key)
}

type CuratorEndpointProbeInput struct {
	BaseURL        string
	APIKey         string
	Model          string
	EmbeddingModel string
	Timeout        time.Duration
}

type CuratorEndpointProbeResult struct {
	Provider           string   `json:"provider"`
	Model              string   `json:"model"`
	EmbeddingModel     string   `json:"embedding_model"`
	ChatSupported      bool     `json:"chat_supported"`
	EmbeddingSupported bool     `json:"embedding_supported"`
	Warnings           []string `json:"warnings"`
}

type curatorModelInfo struct {
	ID string `json:"id"`
}

type curatorModelsResponse struct {
	Data []curatorModelInfo `json:"data"`
}

func ProbeCuratorEndpoint(ctx context.Context, input CuratorEndpointProbeInput) (CuratorEndpointProbeResult, error) {
	cfg := normalizeOpenAICompatibleCuratorConfig(OpenAICompatibleCuratorConfig{
		BaseURL:        input.BaseURL,
		APIKey:         input.APIKey,
		Model:          input.Model,
		EmbeddingModel: input.EmbeddingModel,
		Timeout:        input.Timeout,
	})
	if cfg.BaseURL == "" {
		return CuratorEndpointProbeResult{}, validationError("base_url is required")
	}
	client := &http.Client{Timeout: cfg.Timeout}
	models, err := fetchCuratorModels(ctx, client, cfg)
	if err != nil {
		return CuratorEndpointProbeResult{}, err
	}
	provider := detectCuratorProvider(cfg.BaseURL)
	modelIDs := curatorModelIDs(models)
	model := chooseCuratorChatModel(provider, cfg.Model, modelIDs)
	embeddingModel := chooseCuratorEmbeddingModel(provider, cfg.EmbeddingModel, modelIDs)
	result := CuratorEndpointProbeResult{
		Provider:           provider,
		Model:              model,
		EmbeddingModel:     embeddingModel,
		ChatSupported:      model != "",
		EmbeddingSupported: false,
		Warnings:           []string{},
	}
	if !result.ChatSupported {
		result.Warnings = append(result.Warnings, "No likely chat model was found in /models. Select a chat-capable model manually.")
	}
	if embeddingModel == "" {
		result.Warnings = append(result.Warnings, "No likely embedding model was found in /models. Draft generation can work, but vectorization/RAG will be unavailable.")
		return result, nil
	}
	if err := probeCuratorEmbedding(ctx, client, cfg, embeddingModel); err != nil {
		result.Warnings = append(result.Warnings, err.Error())
		return result, nil
	}
	result.EmbeddingSupported = true
	return result, nil
}

func fetchCuratorModels(ctx context.Context, client *http.Client, cfg OpenAICompatibleCuratorConfig) ([]curatorModelInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cfg.BaseURL+"/models", nil)
	if err != nil {
		return nil, err
	}
	if cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cannot reach /models: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("/models returned %d. Check the API key and endpoint permissions.", resp.StatusCode)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("/models returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var out curatorModelsResponse
	if err := json.Unmarshal(body, &out); err != nil || out.Data == nil {
		return nil, errors.New("/models did not return an OpenAI-compatible model list")
	}
	return out.Data, nil
}

func probeCuratorEmbedding(ctx context.Context, client *http.Client, cfg OpenAICompatibleCuratorConfig, model string) error {
	raw, err := json.Marshal(map[string]any{"model": model, "input": "multica probe"})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.BaseURL+"/embeddings", bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("/embeddings is not reachable. Draft generation can work, but vectorization/RAG will be unavailable: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("/embeddings returned %d. Draft generation can work, but vectorization/RAG will be unavailable.", resp.StatusCode)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("/embeddings returned %d. Draft generation can work, but vectorization/RAG will be unavailable.", resp.StatusCode)
	}
	var out struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &out); err != nil || len(out.Data) == 0 {
		return errors.New("/embeddings did not return an OpenAI-compatible embedding response. Draft generation can work, but vectorization/RAG will be unavailable.")
	}
	if len(out.Data[0].Embedding) != KnowledgeEmbeddingDimensions {
		return fmt.Errorf("Embedding dimension is %d, but Multica expects %d. Draft generation can work, but vectorization/RAG will be unavailable.", len(out.Data[0].Embedding), KnowledgeEmbeddingDimensions)
	}
	return nil
}

func detectCuratorProvider(baseURL string) string {
	value := strings.ToLower(baseURL)
	switch {
	case strings.Contains(value, "api.openai.com"):
		return "openai"
	case strings.Contains(value, "deepseek"):
		return "deepseek"
	case strings.Contains(value, "ollama") || strings.Contains(value, ":11434"):
		return "ollama"
	default:
		return "custom"
	}
}

func curatorModelIDs(models []curatorModelInfo) []string {
	ids := make([]string, 0, len(models))
	for _, model := range models {
		if id := strings.TrimSpace(model.ID); id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}

func chooseCuratorChatModel(provider, current string, ids []string) string {
	current = strings.TrimSpace(current)
	if current != "" && curatorModelExists(ids, current) {
		return current
	}
	preferred := map[string][]string{
		"openai":   {"gpt-4.1-mini", "gpt-4o-mini", "gpt-4o"},
		"deepseek": {"deepseek-chat", "deepseek-reasoner"},
	}
	for _, id := range preferred[provider] {
		if curatorModelExists(ids, id) {
			return id
		}
	}
	for _, id := range ids {
		if !looksLikeEmbeddingModel(id) {
			return id
		}
	}
	return ""
}

func chooseCuratorEmbeddingModel(provider, current string, ids []string) string {
	current = strings.TrimSpace(current)
	if current != "" && curatorModelExists(ids, current) {
		return current
	}
	preferred := map[string][]string{
		"openai": {"text-embedding-3-small", "text-embedding-3-large"},
		"ollama": {"nomic-embed-text", "mxbai-embed-large", "bge-m3"},
	}
	for _, id := range preferred[provider] {
		if curatorModelExists(ids, id) {
			return id
		}
	}
	for _, id := range ids {
		if looksLikeEmbeddingModel(id) {
			return id
		}
	}
	return ""
}

func curatorModelExists(ids []string, expected string) bool {
	for _, id := range ids {
		if id == expected {
			return true
		}
	}
	return false
}

func looksLikeEmbeddingModel(id string) bool {
	value := strings.ToLower(id)
	return strings.Contains(value, "embed") ||
		strings.Contains(value, "bge") ||
		strings.Contains(value, "nomic") ||
		strings.Contains(value, "e5-")
}

func (e *OpenAICompatibleCuratorEngine) GenerateDraft(ctx context.Context, input CuratorDraftInput) (CuratorDraft, error) {
	prompt := strings.Join([]string{
		"Generate a reusable Multica knowledge draft from the issue evidence.",
		curatorOutputLanguageInstruction(input.OutputLanguage),
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
		curatorOutputLanguageInstruction(inferCuratorOutputLanguage(source)),
		"Focus on root cause, diagnostic path, fix, anti-patterns, and applicability. Return plain text only.",
		"Issue:", issueText(source.Issue),
		"Evidence:", sourceEvidenceText(source),
	}, "\n\n")
	return e.chatText(ctx, prompt)
}

func (e *OpenAICompatibleCuratorEngine) BuildEmbedding(ctx context.Context, content string) ([]float32, error) {
	if e.cfg.EmbeddingModel == "" {
		return nil, ErrCuratorEngineUnavailable
	}
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
	return CuratorEngineInfo{Provider: e.cfg.Provider, Model: e.cfg.Model, EmbeddingModel: e.cfg.EmbeddingModel}
}

func (e *OpenAICompatibleCuratorEngine) chatJSON(ctx context.Context, prompt string, out any) error {
	text, err := e.chatText(ctx, prompt)
	if err != nil {
		return err
	}
	text = stripCuratorJSONFence(text)
	if err := json.Unmarshal([]byte(text), out); err == nil {
		return nil
	} else if object, ok := extractFirstJSONObject(text); ok {
		if objectErr := json.Unmarshal([]byte(object), out); objectErr == nil {
			return nil
		} else {
			return fmt.Errorf("%w: %v", ErrCuratorInvalidResponse, objectErr)
		}
	} else {
		return fmt.Errorf("%w: %v", ErrCuratorInvalidResponse, err)
	}
}

func stripCuratorJSONFence(text string) string {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "```") {
		return text
	}
	if newline := strings.IndexByte(text, '\n'); newline >= 0 {
		text = strings.TrimSpace(text[newline+1:])
	}
	if strings.HasSuffix(text, "```") {
		text = strings.TrimSpace(strings.TrimSuffix(text, "```"))
	}
	return text
}

func extractFirstJSONObject(text string) (string, bool) {
	start := strings.IndexByte(text, '{')
	if start < 0 {
		return "", false
	}
	depth := 0
	inString := false
	escaped := false
	for i := start; i < len(text); i++ {
		ch := text[i]
		if inString {
			if escaped {
				escaped = false
				continue
			}
			switch ch {
			case '\\':
				escaped = true
			case '"':
				inString = false
			}
			continue
		}
		switch ch {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return text[start : i+1], true
			}
		}
	}
	return "", false
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
	if e.cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+e.cfg.APIKey)
	}
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
		return fmt.Errorf("%w: returned %d: %s", ErrCuratorProvider, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("%w: decode response: %v", ErrCuratorProvider, err)
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
	if input.Governance != nil {
		parts = append(parts, fmt.Sprintf("Governance finding type=%s severity=%d reason=%s suggested_action=%s", input.Governance.FindingType, input.Governance.Severity, input.Governance.Reason, input.Governance.SuggestedAction))
	}
	if input.OriginalItem != nil {
		parts = append(parts, "Original knowledge:\n"+strings.Join([]string{
			"Title: " + input.OriginalItem.Title,
			"Problem: " + input.OriginalItem.ProblemPattern,
			"Recommended practice: " + input.OriginalItem.RecommendedPractice,
			"Anti-patterns: " + input.OriginalItem.AntiPatterns,
		}, "\n"))
	}
	for _, feedback := range input.NegativeFeedback {
		parts = append(parts, "Negative feedback:\n"+excerpt(strings.TrimSpace(feedback.Value+" "+feedback.Note.String), 800))
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
