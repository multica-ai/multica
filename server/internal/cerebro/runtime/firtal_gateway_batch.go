package runtime

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/internal/cerebro/firtalgateway"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const defaultGatewayBatchPollInterval = 2 * time.Second

type gatewayBatchTaskContext struct {
	Type          string `json:"type"`
	BatchToolMode string `json:"batch_tool_mode"`
}

// gatewayBatchTaskEligible is deliberately narrow. Only an inbox Round that
// explicitly opted into a tool-less model call may use the asynchronous API,
// and a fresh tool-resolution result can always veto batching. CLI/ACP tasks
// never reach this server-owned executor.
func gatewayBatchTaskEligible(task db.AgentTaskQueue, hasCallableTools bool) bool {
	if hasCallableTools || len(task.Context) == 0 {
		return false
	}
	var tc gatewayBatchTaskContext
	if json.Unmarshal(task.Context, &tc) != nil {
		return false
	}
	return tc.Type == "inbox_round" && tc.BatchToolMode == "none"
}

type gatewayModelCatalog struct {
	Data []struct {
		ID            string `json:"id"`
		SupportsBatch bool   `json:"firtal_supports_batch"`
	} `json:"data"`
}

type gatewayBatch struct {
	ID               string `json:"id"`
	ProcessingStatus string `json:"processing_status"`
}

type gatewayBatchResult struct {
	CustomID string `json:"custom_id"`
	Result   struct {
		Type    string            `json:"type"`
		Message anthropicResponse `json:"message"`
		Error   json.RawMessage   `json:"error"`
	} `json:"result"`
}

// CompleteBatch submits exactly one tool-less request. The accepted return
// value distinguishes safe pre-accept failures (normal synchronous fallback)
// from an upstream job that must first be cancelled before fallback.
func (c *GatewayClient) CompleteBatch(ctx context.Context, model string, messages []GatewayMessage, meta GatewayRequestMeta, pollInterval time.Duration) (GatewayCompletion, bool, error) {
	model = strings.TrimSpace(model)
	if model == "" || model == "auto" {
		model = c.cfg.Model
	}
	if model == "" {
		model = defaultFirtalGatewayModel
	}
	baseURL, err := firtalgateway.NormalizeTrustedBaseURL(c.cfg.BaseURL)
	if err != nil {
		return GatewayCompletion{}, false, err
	}
	if pollInterval <= 0 {
		pollInterval = defaultGatewayBatchPollInterval
	}

	supported, err := c.modelSupportsBatch(ctx, baseURL, model)
	if err != nil {
		return GatewayCompletion{}, false, err
	}
	if !supported {
		return GatewayCompletion{}, false, fmt.Errorf("model %q does not advertise batch support", model)
	}

	systemText := ""
	start := 0
	if len(messages) > 0 && messages[0].Role == "system" {
		systemText = messages[0].Content
		start = 1
	}
	params := anthropicRequest{
		Model:     model,
		MaxTokens: c.cfg.MaxTokens,
		Messages:  ConvertGatewayMessagesToAnthropic(messages[start:]),
	}
	if systemText != "" {
		params.System = []AnthropicSystemBlock{{Type: "text", Text: systemText}}
	}
	customID := meta.TaskID
	if customID == "" {
		customID = "multica-task"
	}
	body, err := json.Marshal(map[string]any{"requests": []any{map[string]any{"custom_id": customID, "params": params}}})
	if err != nil {
		return GatewayCompletion{}, false, err
	}
	respBody, status, err := c.batchRequest(ctx, http.MethodPost, baseURL+"/api/ai/proxy/v1/messages/batches", body, meta)
	if err != nil {
		return GatewayCompletion{}, false, err
	}
	if status < 200 || status >= 300 {
		return GatewayCompletion{}, false, fmt.Errorf("batch create returned HTTP %d: %s", status, truncateGatewayError(string(respBody), 2048))
	}
	var batch gatewayBatch
	if json.Unmarshal(respBody, &batch) != nil || strings.TrimSpace(batch.ID) == "" {
		return GatewayCompletion{}, false, fmt.Errorf("batch create returned malformed response")
	}
	accepted := true

	for {
		select {
		case <-ctx.Done():
			c.cancelBatch(baseURL, batch.ID, meta)
			return GatewayCompletion{}, accepted, ctx.Err()
		case <-time.After(pollInterval):
		}
		respBody, status, err = c.batchRequest(ctx, http.MethodGet, baseURL+"/api/ai/proxy/v1/messages/batches/"+url.PathEscape(batch.ID), nil, meta)
		if err != nil || status < 200 || status >= 300 {
			c.cancelBatch(baseURL, batch.ID, meta)
			if err != nil {
				return GatewayCompletion{}, accepted, err
			}
			return GatewayCompletion{}, accepted, fmt.Errorf("batch poll returned HTTP %d", status)
		}
		if json.Unmarshal(respBody, &batch) != nil || batch.ProcessingStatus == "" {
			c.cancelBatch(baseURL, batch.ID, meta)
			return GatewayCompletion{}, accepted, fmt.Errorf("batch poll returned malformed response")
		}
		switch batch.ProcessingStatus {
		case "ended":
			return c.batchResults(ctx, baseURL, batch.ID, customID, model, meta, accepted)
		case "canceling", "canceled", "expired":
			return GatewayCompletion{}, accepted, fmt.Errorf("batch ended with status %s", batch.ProcessingStatus)
		}
	}
}

func (c *GatewayClient) modelSupportsBatch(ctx context.Context, baseURL, model string) (bool, error) {
	body, status, err := c.batchRequest(ctx, http.MethodGet, baseURL+"/api/ai/proxy/v1/models", nil, GatewayRequestMeta{})
	if err != nil {
		return false, err
	}
	if status < 200 || status >= 300 {
		return false, fmt.Errorf("model capability lookup returned HTTP %d", status)
	}
	var catalog gatewayModelCatalog
	if err := json.Unmarshal(body, &catalog); err != nil {
		return false, fmt.Errorf("parse model capabilities: %w", err)
	}
	for _, entry := range catalog.Data {
		if entry.ID == model {
			return entry.SupportsBatch, nil
		}
	}
	return false, nil
}

func (c *GatewayClient) batchResults(ctx context.Context, baseURL, batchID, customID, model string, meta GatewayRequestMeta, accepted bool) (GatewayCompletion, bool, error) {
	body, status, err := c.batchRequest(ctx, http.MethodGet, baseURL+"/api/ai/proxy/v1/messages/batches/"+url.PathEscape(batchID)+"/results", nil, meta)
	if err != nil || status < 200 || status >= 300 {
		if err != nil {
			return GatewayCompletion{}, accepted, err
		}
		return GatewayCompletion{}, accepted, fmt.Errorf("batch results returned HTTP %d", status)
	}
	scanner := bufio.NewScanner(bytes.NewReader(body))
	scanner.Buffer(make([]byte, 64*1024), 4<<20)
	for scanner.Scan() {
		var item gatewayBatchResult
		if json.Unmarshal(scanner.Bytes(), &item) != nil || item.CustomID != customID {
			continue
		}
		if item.Result.Type != "succeeded" {
			return GatewayCompletion{}, accepted, fmt.Errorf("batch result type %q", item.Result.Type)
		}
		completion := completionFromAnthropicResponse(item.Result.Message, model)
		if strings.TrimSpace(completion.Output) == "" {
			return GatewayCompletion{}, accepted, fmt.Errorf("batch result contained no assistant content")
		}
		return completion, accepted, nil
	}
	if err := scanner.Err(); err != nil {
		return GatewayCompletion{}, accepted, err
	}
	return GatewayCompletion{}, accepted, fmt.Errorf("batch results did not contain custom_id %q", customID)
}

func completionFromAnthropicResponse(parsed anthropicResponse, fallbackModel string) GatewayCompletion {
	model := strings.TrimSpace(parsed.Model)
	if model == "" {
		model = fallbackModel
	}
	completion := GatewayCompletion{Model: model}
	var text []string
	for _, block := range parsed.Content {
		if block.Type == "text" && strings.TrimSpace(block.Text) != "" {
			text = append(text, strings.TrimSpace(block.Text))
		}
	}
	completion.Output = strings.Join(text, "\n")
	if parsed.Usage != nil {
		completion.Usage.InputTokens = parsed.Usage.InputTokens
		completion.Usage.OutputTokens = parsed.Usage.OutputTokens
		completion.Usage.CacheReadTokens = parsed.Usage.CacheReadInputTokens
		completion.Usage.CacheWriteTokens = parsed.Usage.CacheCreationInputTokens
	}
	if parsed.Firtal != nil {
		completion.Usage.CostCents = parsed.Firtal.CostCents
	}
	return completion
}

func (c *GatewayClient) batchRequest(ctx context.Context, method, endpoint string, body []byte, meta GatewayRequestMeta) ([]byte, int, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Skill", "multica-server-runtime")
	req.Header.Set("X-Tags", "multica,cerebro,server-runtime,anthropic,batch")
	meta.applyContextHeaders(req)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	return respBody, resp.StatusCode, err
}

func (c *GatewayClient) cancelBatch(baseURL, batchID string, meta GatewayRequestMeta) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _, _ = c.batchRequest(ctx, http.MethodDelete, baseURL+"/api/ai/proxy/v1/messages/batches/"+url.PathEscape(batchID), nil, meta)
}
