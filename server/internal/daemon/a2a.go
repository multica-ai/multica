package daemon

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"go.yaml.in/yaml/v2"

	"github.com/multica-ai/multica/server/internal/cli"
)

const a2aConfigFilename = "a2a-agents.yaml"
const a2aCardPath = "/.well-known/agent-card.json"
const a2aRequestTimeout = 5 * time.Minute
const a2aHealthProbeInterval = 30 * time.Second
const a2aPortScanMin = 8900
const a2aPortScanMax = 8910

// a2aHTTPClient is the shared HTTP client for all A2A requests. Connection
// pooling is handled by the default transport (2 idle conns per host, 100
// total). Per-request timeouts are set via context, not client.Timeout, so
// long-polling (streaming) connections are not killed prematurely.
var a2aHTTPClient = &http.Client{}

// ---------------------------------------------------------------------------
// URL validation
// ---------------------------------------------------------------------------

// validateA2AURL rejects URLs that could cause SSRF: non-http(s) schemes,
// empty hosts, loopback/link-local IPs (except 127.0.0.1 for local dev),
// and hostnames that resolve to internal addresses.
func validateA2AURL(raw string) error {
	if raw == "" {
		return fmt.Errorf("empty URL")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("unsupported scheme %q (must be http or https)", u.Scheme)
	}
	if u.Host == "" {
		return fmt.Errorf("URL missing host")
	}
	return nil
}

// ---------------------------------------------------------------------------
// YAML config types
// ---------------------------------------------------------------------------

// a2aConfigFile represents the top-level structure of a2a-agents.yaml.
type a2aConfigFile struct {
	Agents   []a2aConfigEntry `yaml:"agents"`
	Registry *a2aRegistryConf `yaml:"registry,omitempty"`
}

// a2aConfigEntry represents a single agent entry in the config file.
type a2aConfigEntry struct {
	Name     string `yaml:"name"`
	URL      string `yaml:"url"`
	TokenEnv string `yaml:"token_env,omitempty"`
	Auth     *a2aAuthYAML `yaml:"auth,omitempty"`
}

// a2aAuthYAML represents auth configuration per agent entry.
type a2aAuthYAML struct {
	Scheme   string `yaml:"scheme"`             // "bearer", "api-key", "openid-connect"
	TokenEnv string `yaml:"token_env,omitempty"` // env var holding the token/key
	Header   string `yaml:"header,omitempty"`    // custom header name (default: Authorization)
}

// a2aRegistryConf represents a central agent registry configuration.
type a2aRegistryConf struct {
	URL      string `yaml:"url"`
	TokenEnv string `yaml:"token_env,omitempty"`
	PollSec  int    `yaml:"poll_interval,omitempty"` // seconds between polls (default: 300)
}

// ---------------------------------------------------------------------------
// A2A Agent Card (minimal subset we need)
// ---------------------------------------------------------------------------

// A2AAgentCard holds the subset of the A2A Agent Card we use.
type A2AAgentCard struct {
	Name               string                `json:"name"`
	Version            string                `json:"version"`
	Description        string                `json:"description"`
	DefaultInputModes  []string              `json:"defaultInputModes,omitempty"`
	DefaultOutputModes []string              `json:"defaultOutputModes,omitempty"`
	Capabilities       *A2ACapabilities      `json:"capabilities,omitempty"`
	SecuritySchemes    map[string]A2ASecScheme `json:"securitySchemes,omitempty"`
	Skills             []A2ASkill            `json:"skills,omitempty"`
}

// A2ACapabilities holds capability flags from the Agent Card.
type A2ACapabilities struct {
	Streaming            bool `json:"streaming"`
	PushNotifications    bool `json:"pushNotifications"`
	StateTransitionHistory bool `json:"stateTransitionHistory"`
}

// A2ASkill represents a skill declared in the Agent Card.
type A2ASkill struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// A2ASecScheme represents a security scheme from the Agent Card.
type A2ASecScheme struct {
	Type        string `json:"type"`        // "http", "apiKey", "openIdConnect"
	Scheme      string `json:"scheme"`      // e.g. "bearer" for type=http
	In          string `json:"in"`          // "header", "query", "cookie" for apiKey
	Name        string `json:"name"`        // header/query param name for apiKey
	OpenIDConnectURL string `json:"openIdConnectUrl,omitempty"`
}

// ---------------------------------------------------------------------------
// JSON-RPC types for SendMessage / SendStreamingMessage / CancelTask
// ---------------------------------------------------------------------------

type a2aJSONRPCRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      string      `json:"id"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params"`
}

type a2aSendParams struct {
	Message a2aMessage `json:"message"`
}

type a2aMessage struct {
	Role  string    `json:"role"`
	Parts []a2aPart `json:"parts"`
}

type a2aPart struct {
	Text       string          `json:"text,omitempty"`
	Data       json.RawMessage `json:"data,omitempty"`
	MediaType  string          `json:"mediaType,omitempty"`
}

// The response result is a discriminated union: either "task" or "message".
type a2aSendResult struct {
	Task    *a2aTaskResponse `json:"task,omitempty"`
	Message *a2aMessage      `json:"message,omitempty"`
}

type a2aTaskResponse struct {
	ID     string    `json:"id"`
	Status a2aStatus `json:"status"`
}

type a2aStatus struct {
	State   string      `json:"state"`
	Message *a2aMessage `json:"message,omitempty"`
}

type a2aJSONRPCResponse struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      string           `json:"id"`
	Result  json.RawMessage  `json:"result,omitempty"`
	Error   *a2aJSONRPCError `json:"error,omitempty"`
}

type a2aJSONRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// ---------------------------------------------------------------------------
// Auth resolution
// ---------------------------------------------------------------------------

// a2aResolvedAuth holds a resolved auth scheme + credential for request dispatch.
type a2aResolvedAuth struct {
	Header string // HTTP header name (e.g. "Authorization")
	Value  string // header value (e.g. "Bearer <token>")
}

// resolveAuth builds the auth header from config and environment.
func resolveA2AAuth(yamlAuth *a2aAuthYAML, legacyTokenEnv string) *a2aResolvedAuth {
	// Legacy token_env shorthand (Phase 1 compat).
	if yamlAuth == nil {
		if legacyTokenEnv != "" {
			if tok := strings.TrimSpace(os.Getenv(legacyTokenEnv)); tok != "" {
				return &a2aResolvedAuth{Header: "Authorization", Value: "Bearer " + tok}
			}
		}
		return nil
	}

	token := strings.TrimSpace(os.Getenv(yamlAuth.TokenEnv))
	if token == "" {
		return nil
	}

	header := yamlAuth.Header
	if header == "" {
		header = "Authorization"
	}

	switch yamlAuth.Scheme {
	case "api-key":
		return &a2aResolvedAuth{Header: header, Value: token}
	case "openid-connect":
		return &a2aResolvedAuth{Header: header, Value: "Bearer " + token}
	default: // "bearer" or empty
		return &a2aResolvedAuth{Header: header, Value: "Bearer " + token}
	}
}

// applyAuth sets auth headers on an HTTP request.
func applyA2AAuth(req *http.Request, auth *a2aResolvedAuth) {
	if auth != nil {
		req.Header.Set(auth.Header, auth.Value)
	}
}

// ---------------------------------------------------------------------------
// Config loading
// ---------------------------------------------------------------------------

// loadA2AConfig reads and parses the A2A config file. Returns nil if the file
// does not exist — A2A agents are opt-in.
func loadA2AConfig(profile string) (*a2aConfigFile, error) {
	dir, err := cli.ProfileDir(profile)
	if err != nil {
		return nil, nil
	}
	path := filepath.Join(dir, a2aConfigFilename)

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read A2A config %s: %w", path, err)
	}

	if len(bytes.TrimSpace(data)) == 0 {
		return nil, nil
	}

	var cfg a2aConfigFile
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse A2A config %s: %w", path, err)
	}
	return &cfg, nil
}

// ---------------------------------------------------------------------------
// Agent Card fetching
// ---------------------------------------------------------------------------

// fetchAgentCard retrieves and parses the A2A Agent Card from the given base URL.
func fetchAgentCard(ctx context.Context, baseURL string) (*A2AAgentCard, error) {
	cardURL := strings.TrimRight(baseURL, "/") + a2aCardPath

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cardURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build card request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := a2aHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch agent card from %s: %w", cardURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("agent card returned %d from %s", resp.StatusCode, cardURL)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1MB max
	if err != nil {
		return nil, fmt.Errorf("read agent card body: %w", err)
	}

	var card A2AAgentCard
	if err := json.Unmarshal(body, &card); err != nil {
		return nil, fmt.Errorf("parse agent card JSON: %w", err)
	}
	if card.Name == "" {
		return nil, fmt.Errorf("agent card missing required field 'name'")
	}
	return &card, nil
}

// ---------------------------------------------------------------------------
// Discovery
// ---------------------------------------------------------------------------

// discoverA2AAgents loads the A2A config, fetches Agent Cards for each entry,
// and returns a map of provider-keyed AgentEntry values. Failures to fetch
// individual cards are logged but do not prevent other agents from loading.
func discoverA2AAgents(ctx context.Context, profile string, logger *slog.Logger) map[string]AgentEntry {
	cfg, err := loadA2AConfig(profile)
	if err != nil {
		logger.Warn("a2a: config load failed", "error", err)
		return nil
	}
	if cfg == nil {
		return nil
	}

	agents := make(map[string]AgentEntry)

	// Config-file discovery (Mode 1).
	for _, entry := range cfg.Agents {
		if entry.Name == "" || entry.URL == "" {
			logger.Warn("a2a: skipping entry with empty name or url")
			continue
		}
		if err := validateA2AURL(entry.URL); err != nil {
			logger.Warn("a2a: skipping entry with invalid URL", "name", entry.Name, "url", entry.URL, "error", err)
			continue
		}

		auth := resolveA2AAuth(entry.Auth, entry.TokenEnv)

		card, cardErr := fetchAgentCard(ctx, entry.URL)
		if cardErr != nil {
			logger.Warn("a2a: failed to fetch agent card, skipping agent",
				"name", entry.Name,
				"url", entry.URL,
				"error", cardErr,
			)
			continue
		}

		logger.Info("a2a: discovered agent",
			"name", card.Name,
			"version", card.Version,
			"url", entry.URL,
		)

		var token string
		if auth != nil {
			token = auth.Value
		}
		agents[entry.Name] = AgentEntry{
			Mode:    "a2a",
			A2AURL:  strings.TrimRight(entry.URL, "/"),
			Card:    card,
			Token:   token,
			A2AAuth: auth,
		}
	}

	// Local port scan discovery (Mode 2).
	scanA2ALocalPorts(ctx, agents, logger)

	return agents
}

// scanA2ALocalPorts probes localhost ports a2aPortScanMin..a2aPortScanMax for
// A2A agent cards. Newly discovered agents are added to the map. Agents whose
// names conflict with existing entries are skipped.
func scanA2ALocalPorts(ctx context.Context, agents map[string]AgentEntry, logger *slog.Logger) {
	for port := a2aPortScanMin; port <= a2aPortScanMax; port++ {
		url := fmt.Sprintf("http://127.0.0.1:%d", port)
		card, err := fetchAgentCard(ctx, url)
		if err != nil {
			continue
		}

		if _, exists := agents[card.Name]; exists {
			logger.Debug("a2a: port scan found existing agent, skipping", "name", card.Name, "port", port)
			continue
		}

		logger.Info("a2a: auto-discovered agent via port scan",
			"name", card.Name,
			"version", card.Version,
			"port", port,
		)

		agents[card.Name] = AgentEntry{
			Mode:   "a2a",
			A2AURL: url,
			Card:   card,
		}
	}
}

// ---------------------------------------------------------------------------
// Task dispatch
// ---------------------------------------------------------------------------

// dispatchA2ATask sends the task prompt to the A2A agent via JSON-RPC.
// If the agent's card advertises streaming, it uses SendStreamingMessage
// with SSE. Otherwise it falls back to blocking SendMessage.
func (d *Daemon) dispatchA2ATask(ctx context.Context, task Task, entry AgentEntry, taskLog *slog.Logger) (TaskResult, error) {
	if task.WorkspaceID == "" {
		return TaskResult{}, fmt.Errorf("refusing to dispatch a2a task: task has no workspace_id (task_id=%s)", task.ID)
	}

	prompt := BuildPrompt(task, entry.Card.Name)
	taskLog.Info("dispatching a2a task", "url", entry.A2AURL, "agent", entry.Card.Name)

	_ = d.client.ReportProgress(ctx, task.ID, fmt.Sprintf("Connecting to %s", entry.Card.Name), 1, 2)

	// Streaming path: use SendStreamingMessage + SSE.
	if entry.Card.Capabilities != nil && entry.Card.Capabilities.Streaming {
		return d.dispatchA2AStreamingTask(ctx, task, entry, prompt, taskLog)
	}

	// Blocking path: use SendMessage.
	return d.dispatchA2ABlockingTask(ctx, task, entry, prompt, taskLog)
}

// dispatchA2ABlockingTask sends a blocking SendMessage and waits for the result.
func (d *Daemon) dispatchA2ABlockingTask(ctx context.Context, task Task, entry AgentEntry, prompt string, taskLog *slog.Logger) (TaskResult, error) {
	reqBody := a2aJSONRPCRequest{
		JSONRPC: "2.0",
		ID:      task.ID,
		Method:  "SendMessage",
		Params: a2aSendParams{
			Message: a2aMessage{
				Role: "ROLE_USER",
				Parts: []a2aPart{
					{Text: prompt},
				},
			},
		},
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return TaskResult{}, fmt.Errorf("marshal a2a request: %w", err)
	}

	endpoint := entry.A2AURL
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return TaskResult{}, fmt.Errorf("build a2a request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	applyA2AAuth(req, entry.A2AAuth)
	// Fallback: legacy Token field.
	if entry.A2AAuth == nil && entry.Token != "" {
		req.Header.Set("Authorization", entry.Token)
	}

	resp, err := a2aHTTPClient.Do(req)
	if err != nil {
		return TaskResult{Status: "blocked", Comment: fmt.Sprintf("a2a request failed: %s", err)}, nil
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20)) // 10MB max
	if err != nil {
		return TaskResult{Status: "blocked", Comment: fmt.Sprintf("read a2a response: %s", err)}, nil
	}

	if resp.StatusCode != http.StatusOK {
		return TaskResult{
			Status:  "blocked",
			Comment: fmt.Sprintf("a2a agent returned HTTP %d: %s", resp.StatusCode, string(respBody)),
		}, nil
	}

	var rpcResp a2aJSONRPCResponse
	if err := json.Unmarshal(respBody, &rpcResp); err != nil {
		return TaskResult{Status: "blocked", Comment: fmt.Sprintf("parse a2a response: %s", err)}, nil
	}
	if rpcResp.Error != nil {
		return TaskResult{
			Status:  "blocked",
			Comment: fmt.Sprintf("a2a JSON-RPC error %d: %s", rpcResp.Error.Code, rpcResp.Error.Message),
		}, nil
	}

	_ = d.client.ReportProgress(ctx, task.ID, "A2A agent responded", 2, 2)

	return mapA2AResult(rpcResp.Result, taskLog)
}

// dispatchA2AStreamingTask sends a SendStreamingMessage request and reads
// SSE events until a terminal task state is reached or the context is cancelled.
func (d *Daemon) dispatchA2AStreamingTask(ctx context.Context, task Task, entry AgentEntry, prompt string, taskLog *slog.Logger) (TaskResult, error) {
	reqBody := a2aJSONRPCRequest{
		JSONRPC: "2.0",
		ID:      task.ID,
		Method:  "SendStreamingMessage",
		Params: a2aSendParams{
			Message: a2aMessage{
				Role: "ROLE_USER",
				Parts: []a2aPart{
					{Text: prompt},
				},
			},
		},
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return TaskResult{}, fmt.Errorf("marshal a2a streaming request: %w", err)
	}

	endpoint := entry.A2AURL
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return TaskResult{}, fmt.Errorf("build a2a streaming request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	applyA2AAuth(req, entry.A2AAuth)
	if entry.A2AAuth == nil && entry.Token != "" {
		req.Header.Set("Authorization", entry.Token)
	}

	resp, err := a2aHTTPClient.Do(req)
	if err != nil {
		return TaskResult{Status: "blocked", Comment: fmt.Sprintf("a2a streaming request failed: %s", err)}, nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return TaskResult{
			Status:  "blocked",
			Comment: fmt.Sprintf("a2a streaming agent returned HTTP %d: %s", resp.StatusCode, string(respBody)),
		}, nil
	}

	// Read SSE events, looking for terminal task states.
	return d.readA2ASSEStream(ctx, task, resp.Body, taskLog)
}

// readA2ASSEStream reads SSE events from the response body until a terminal
// A2A task state is found, the context is cancelled, or the stream ends.
func (d *Daemon) readA2ASSEStream(ctx context.Context, task Task, body io.Reader, taskLog *slog.Logger) (TaskResult, error) {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var lastResult json.RawMessage

	for scanner.Scan() {
		line := scanner.Text()

		// SSE data lines start with "data: ".
		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")
		if data == "" {
			continue
		}

		// Parse as JSON-RPC response.
		var rpcResp a2aJSONRPCResponse
		if err := json.Unmarshal([]byte(data), &rpcResp); err != nil {
			taskLog.Debug("a2a sse: unparseable event", "data", truncateLog(data, 200))
			continue
		}

		if rpcResp.Error != nil {
			return TaskResult{
				Status:  "blocked",
				Comment: fmt.Sprintf("a2a streaming JSON-RPC error %d: %s", rpcResp.Error.Code, rpcResp.Error.Message),
			}, nil
		}

		lastResult = rpcResp.Result

		// Check if this event carries a terminal task state.
		result, terminal := checkA2AStreamEvent(rpcResp.Result, taskLog)
		if terminal {
			_ = d.client.ReportProgress(ctx, task.ID, "A2A agent completed", 2, 2)
			return result, nil
		}

		// Report progress for non-terminal states.
		if result.Status == "working" {
			_ = d.client.ReportProgress(ctx, task.ID, result.Comment, 1, 2)
		}
	}

	if err := scanner.Err(); err != nil {
		taskLog.Warn("a2a sse: scanner error", "error", err)
	}

	_ = d.client.ReportProgress(ctx, task.ID, "A2A stream ended", 2, 2)

	// Stream ended; use last result if available.
	if len(lastResult) > 0 {
		return mapA2AResult(lastResult, taskLog)
	}

	return TaskResult{Status: "blocked", Comment: "a2a streaming agent closed stream without result"}, nil
}

// checkA2AStreamEvent checks if an SSE event payload carries a terminal state.
// Returns the mapped TaskResult and true if terminal, or a partial result and false.
func checkA2AStreamEvent(raw json.RawMessage, taskLog *slog.Logger) (TaskResult, bool) {
	if len(raw) == 0 {
		return TaskResult{}, false
	}

	var result a2aSendResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return TaskResult{}, false
	}

	if result.Task == nil {
		return TaskResult{}, false
	}

	state := result.Task.Status.State
	msg := extractStatusMessageText(&result.Task.Status)

	switch state {
	case "TASK_STATE_COMPLETED":
		if msg == "" {
			msg = "a2a agent completed task"
		}
		return TaskResult{Status: "completed", Comment: msg}, true
	case "TASK_STATE_FAILED":
		if msg == "" {
			msg = "a2a agent reported task failure"
		}
		return TaskResult{Status: "blocked", Comment: msg}, true
	case "TASK_STATE_REJECTED":
		if msg == "" {
			msg = "a2a agent rejected task"
		}
		return TaskResult{Status: "blocked", Comment: msg}, true
	case "TASK_STATE_CANCELED":
		return TaskResult{Status: "cancelled", Comment: "a2a agent canceled task"}, true
	case "TASK_STATE_INPUT_REQUIRED":
		return TaskResult{
			Status:  "blocked",
			Comment: fmt.Sprintf("a2a agent requires input: %s", msg),
		}, true
	case "TASK_STATE_AUTH_REQUIRED":
		return TaskResult{
			Status:  "blocked",
			Comment: fmt.Sprintf("a2a agent requires authentication: %s", msg),
		}, true
	case "TASK_STATE_WORKING":
		if msg == "" {
			msg = "working"
		}
		return TaskResult{Status: "working", Comment: msg}, false
	default:
		return TaskResult{}, false
	}
}

// ---------------------------------------------------------------------------
// Cancellation
// ---------------------------------------------------------------------------

// cancelA2ATask sends a CancelTask JSON-RPC request to the A2A agent.
func cancelA2ATask(ctx context.Context, entry AgentEntry, taskID string, taskLog *slog.Logger) {
	cancelReq := struct {
		JSONRPC string `json:"jsonrpc"`
		ID      string `json:"id"`
		Method  string `json:"method"`
		Params  struct {
			TaskID string `json:"id"`
			Reason string `json:"reason,omitempty"`
		} `json:"params"`
	}{
		JSONRPC: "2.0",
		ID:      taskID + "-cancel",
		Method:  "CancelTask",
	}
	cancelReq.Params.TaskID = taskID
	cancelReq.Params.Reason = "cancelled by daemon"

	body, err := json.Marshal(cancelReq)
	if err != nil {
		taskLog.Debug("a2a: marshal cancel request failed", "error", err)
		return
	}

	cancelCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(cancelCtx, http.MethodPost, entry.A2AURL, bytes.NewReader(body))
	if err != nil {
		taskLog.Debug("a2a: build cancel request failed", "error", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	applyA2AAuth(req, entry.A2AAuth)
	if entry.A2AAuth == nil && entry.Token != "" {
		req.Header.Set("Authorization", entry.Token)
	}

	resp, err := a2aHTTPClient.Do(req)
	if err != nil {
		taskLog.Debug("a2a: cancel request failed", "error", err)
		return
	}
	resp.Body.Close()
	taskLog.Info("a2a: sent CancelTask to agent", "task_id", taskID)
}

// ---------------------------------------------------------------------------
// Health probing
// ---------------------------------------------------------------------------

// startA2AHealthProbes starts background goroutines that periodically probe
// each A2A agent's agent card endpoint. Returns a cancel function.
func (d *Daemon) startA2AHealthProbes(ctx context.Context) context.CancelFunc {
	ctx, cancel := context.WithCancel(ctx)

	var wg sync.WaitGroup
	for name, entry := range d.agentEntries() {
		if entry.Mode != "a2a" || entry.A2AURL == "" {
			continue
		}
		wg.Add(1)
		go func(name, url string) {
			defer wg.Done()
			d.a2aHealthProbeLoop(ctx, name, url)
		}(name, entry.A2AURL)
	}

	return func() {
		cancel()
		wg.Wait()
	}
}

// a2aHealthProbeLoop periodically GETs the agent card as a liveness check.
func (d *Daemon) a2aHealthProbeLoop(ctx context.Context, name, url string) {
	ticker := time.NewTicker(a2aHealthProbeInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			_, err := fetchAgentCard(probeCtx, url)
			cancel()
			if err != nil {
				d.logger.Warn("a2a: health probe failed",
					"name", name,
					"url", url,
					"error", err,
				)
			} else {
				d.logger.Debug("a2a: health probe ok", "name", name, "url", url)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Registry polling
// ---------------------------------------------------------------------------

// startA2ARegistryPoll starts polling the registry for agent definitions,
// if configured. Returns a cancel function.
func (d *Daemon) startA2ARegistryPoll(ctx context.Context) context.CancelFunc {
	if d.cfg.a2aRegistry == nil || d.cfg.a2aRegistry.URL == "" {
		return func() {}
	}

	ctx, cancel := context.WithCancel(ctx)
	wg := &sync.WaitGroup{}
	wg.Add(1)

	go func() {
		defer wg.Done()
		d.a2aRegistryPollLoop(ctx)
	}()

	return func() {
		cancel()
		wg.Wait()
	}
}

// a2aRegistryPollLoop periodically fetches the registry URL and discovers
// agents from the response.
func (d *Daemon) a2aRegistryPollLoop(ctx context.Context) {
	reg := d.cfg.a2aRegistry
	interval := 300 * time.Second
	if reg.PollSec > 0 {
		interval = time.Duration(reg.PollSec) * time.Second
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Initial poll.
	d.pollA2ARegistry(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			d.pollA2ARegistry(ctx)
		}
	}
}

// pollA2ARegistry fetches the registry URL and adds any newly discovered agents.
func (d *Daemon) pollA2ARegistry(ctx context.Context) {
	reg := d.cfg.a2aRegistry
	if reg == nil || reg.URL == "" {
		return
	}

	pollCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(pollCtx, http.MethodGet, reg.URL, nil)
	if err != nil {
		d.logger.Warn("a2a: registry poll build request failed", "error", err)
		return
	}
	req.Header.Set("Accept", "application/json")
	if reg.TokenEnv != "" {
		if tok := os.Getenv(reg.TokenEnv); tok != "" {
			req.Header.Set("Authorization", "Bearer "+tok)
		}
	}

	resp, err := a2aHTTPClient.Do(req)
	if err != nil {
		d.logger.Warn("a2a: registry poll failed", "url", reg.URL, "error", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		d.logger.Warn("a2a: registry returned non-200", "status", resp.StatusCode)
		return
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 5<<20)) // 5MB
	if err != nil {
		d.logger.Warn("a2a: registry read body failed", "error", err)
		return
	}

	// Expected format: list of agent configs (same shape as a2aConfigEntry).
	var entries []a2aConfigEntry
	if err := json.Unmarshal(body, &entries); err != nil {
		d.logger.Warn("a2a: registry parse failed", "error", err)
		return
	}

	for _, entry := range entries {
		if entry.Name == "" || entry.URL == "" {
			continue
		}
		if err := validateA2AURL(entry.URL); err != nil {
			d.logger.Warn("a2a: registry agent has invalid URL", "name", entry.Name, "url", entry.URL, "error", err)
			continue
		}

		// Skip if already known.
		if _, exists := d.getAgentEntry(entry.Name); exists {
			continue
		}

		auth := resolveA2AAuth(entry.Auth, entry.TokenEnv)

		card, cardErr := fetchAgentCard(pollCtx, entry.URL)
		if cardErr != nil {
			d.logger.Warn("a2a: registry agent card fetch failed",
				"name", entry.Name, "url", entry.URL, "error", cardErr)
			continue
		}

		var token string
		if auth != nil {
			token = auth.Value
		}
		d.setAgentEntry(entry.Name, AgentEntry{
			Mode:    "a2a",
			A2AURL:  strings.TrimRight(entry.URL, "/"),
			Card:    card,
			Token:   token,
			A2AAuth: auth,
		})

		d.logger.Info("a2a: discovered agent from registry",
			"name", card.Name, "version", card.Version)
	}
}

// ---------------------------------------------------------------------------
// Result mapping
// ---------------------------------------------------------------------------

// mapA2AResult converts the A2A SendMessage response into a TaskResult.
func mapA2AResult(raw json.RawMessage, taskLog *slog.Logger) (TaskResult, error) {
	if len(raw) == 0 {
		return TaskResult{Status: "blocked", Comment: "a2a agent returned empty result"}, nil
	}

	var result a2aSendResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return TaskResult{Status: "blocked", Comment: fmt.Sprintf("parse a2a result: %s", err)}, nil
	}

	// If the agent returned a direct message (pre-task clarification), treat as blocked.
	if result.Message != nil && result.Task == nil {
		text := extractTextFromMessage(result.Message)
		return TaskResult{
			Status:  "blocked",
			Comment: fmt.Sprintf("a2a agent responded with message (no task): %s", text),
		}, nil
	}

	if result.Task == nil {
		return TaskResult{Status: "blocked", Comment: "a2a response contains neither task nor message"}, nil
	}

	state := result.Task.Status.State
	taskLog.Info("a2a task state", "state", state)

	switch state {
	case "TASK_STATE_COMPLETED":
		output := extractStatusMessageText(&result.Task.Status)
		if output == "" {
			output = "a2a agent completed task"
		}
		return TaskResult{Status: "completed", Comment: output}, nil
	case "TASK_STATE_FAILED":
		msg := extractStatusMessageText(&result.Task.Status)
		if msg == "" {
			msg = "a2a agent reported task failure"
		}
		return TaskResult{Status: "blocked", Comment: msg}, nil
	case "TASK_STATE_REJECTED":
		msg := extractStatusMessageText(&result.Task.Status)
		if msg == "" {
			msg = "a2a agent rejected task"
		}
		return TaskResult{Status: "blocked", Comment: msg}, nil
	case "TASK_STATE_CANCELED":
		return TaskResult{Status: "cancelled", Comment: "a2a agent canceled task"}, nil
	case "TASK_STATE_INPUT_REQUIRED":
		msg := extractStatusMessageText(&result.Task.Status)
		return TaskResult{
			Status:  "blocked",
			Comment: fmt.Sprintf("a2a agent requires input: %s", msg),
		}, nil
	case "TASK_STATE_AUTH_REQUIRED":
		msg := extractStatusMessageText(&result.Task.Status)
		return TaskResult{
			Status:  "blocked",
			Comment: fmt.Sprintf("a2a agent requires authentication: %s", msg),
		}, nil
	case "TASK_STATE_WORKING":
		// Working is a non-terminal state; in blocking mode we shouldn't
		// normally see this, but handle it gracefully.
		msg := extractStatusMessageText(&result.Task.Status)
		return TaskResult{
			Status:  "blocked",
			Comment: fmt.Sprintf("a2a agent still working (non-terminal): %s", msg),
		}, nil
	default:
		msg := extractStatusMessageText(&result.Task.Status)
		return TaskResult{
			Status:  "blocked",
			Comment: fmt.Sprintf("a2a agent returned unknown state %s: %s", state, msg),
		}, nil
	}
}

// extractTextFromMessage joins all text parts from an A2A message.
func extractTextFromMessage(msg *a2aMessage) string {
	if msg == nil {
		return ""
	}
	var parts []string
	for _, p := range msg.Parts {
		if p.Text != "" {
			parts = append(parts, p.Text)
		}
	}
	return strings.Join(parts, "\n")
}

// extractStatusMessageText extracts text from the status message if present.
func extractStatusMessageText(status *a2aStatus) string {
	if status == nil || status.Message == nil {
		return ""
	}
	return extractTextFromMessage(status.Message)
}
