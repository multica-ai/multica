package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// bbBackend delegates to the companion BB plugin using BB's public CLI.
// Task credentials travel on stdin and are injected by BB's provider-env API.
type bbBackend struct{ cfg Config }

type bbRequest struct {
	RunKey         string            `json:"runKey"`
	Prompt         string            `json:"prompt"`
	Cwd            string            `json:"cwd"`
	Env            map[string]string `json:"env"`
	Model          string            `json:"model,omitempty"`
	ReasoningLevel string            `json:"reasoningLevel,omitempty"`
	SessionID      string            `json:"sessionId,omitempty"`
	Title          string            `json:"title,omitempty"`
}

type bbResponse struct {
	Status    string `json:"status"`
	SessionID string `json:"session_id"`
	Output    string `json:"output"`
}

func (b *bbBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	var mcp struct {
		Servers map[string]json.RawMessage `json:"mcpServers"`
	}
	if len(opts.McpConfig) != 0 && json.Unmarshal(opts.McpConfig, &mcp) != nil {
		return nil, fmt.Errorf("bb: invalid MCP configuration")
	}
	if len(opts.CustomArgs) != 0 || len(opts.ExtraArgs) != 0 || len(mcp.Servers) != 0 {
		return nil, fmt.Errorf("bb: configure provider arguments and MCP in BB")
	}
	env := make(map[string]string, len(b.cfg.Env))
	for name, value := range b.cfg.Env {
		env[name] = value
	}
	if opts.TaskToken != "" {
		env["MULTICA_TOKEN"] = opts.TaskToken
	}
	runKey := uuid.NewString()
	request, err := json.Marshal(bbRequest{
		RunKey: runKey,
		Prompt: prompt, Cwd: opts.Cwd, Env: env, Model: opts.Model,
		ReasoningLevel: opts.ThinkingLevel, SessionID: opts.ResumeSessionID, Title: opts.ThreadName,
	})
	if err != nil {
		return nil, err
	}
	executable := b.cfg.ExecutablePath
	if executable == "" {
		executable = "bb"
	}
	runCtx, cancel := runContext(ctx, opts.Timeout)
	cmd := b.cfg.commandAt(executable).exec(runCtx, "multica", "run", "--request-stdin")
	cmd.Dir = opts.Cwd
	cmd.Env = buildEnv(b.cfg.Env)
	cmd.Stdin = bytes.NewReader(request)
	kill := cmd.Cancel
	var cleanupErr error
	cmd.Cancel = func() error {
		cleanupCtx, done := context.WithTimeout(context.Background(), 30*time.Second)
		defer done()
		stop := b.cfg.commandAt(executable).exec(cleanupCtx, "multica", "cancel", "--run-key", runKey)
		stop.Env = buildEnv(b.cfg.Env)
		cleanupErr = startOwnedProcessTree(stop, b.cfg.Logger)
		if cleanupErr == nil {
			cleanupErr = stop.Wait()
			releaseProcessGroup(stop)
		}
		return kill()
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	if err := startOwnedProcessTree(cmd, b.cfg.Logger); err != nil {
		cancel()
		return nil, fmt.Errorf("start bb: %w", err)
	}
	messages := make(chan Message, 2)
	results := make(chan Result, 1)
	go func() {
		defer cancel()
		defer close(results)
		defer releaseProcessGroup(cmd)
		started := time.Now()
		var response bbResponse
		decodeErr := json.NewDecoder(stdout).Decode(&response)
		waitErr := cmd.Wait()
		result := Result{Status: "completed", SessionID: response.SessionID, Output: response.Output, DurationMs: time.Since(started).Milliseconds()}
		if runCtx.Err() != nil {
			result.Status = "aborted"
			result.Error = "BB execution cancelled"
			if cleanupErr != nil {
				result.Status = "cleanup_unconfirmed"
				result.Error = "BB did not confirm the task stopped; inspect its thread before retrying"
			}
		} else if response.Status == "cleanup_unconfirmed" {
			result.Status = "cleanup_unconfirmed"
			result.Error = "BB did not confirm the task stopped; inspect its thread before retrying"
		} else if waitErr != nil || decodeErr != nil || response.SessionID == "" || response.Output == "" {
			result.Status = "failed"
			result.Error = "BB execution failed; install and configure the Multica BB plugin and inspect its thread"
		} else {
			messages <- Message{Type: MessageStatus, Status: "completed", SessionID: response.SessionID}
			messages <- Message{Type: MessageText, Content: response.Output}
		}
		close(messages)
		results <- result
	}()
	return &Session{CleanupRequired: true, Messages: messages, Result: results}, nil
}

func discoverBBModels(ctx context.Context, runtimeCmd Command) (Catalog, error) {
	output, err := runtimeCmd.exec(ctx, "multica", "models").Output()
	if err != nil {
		return Catalog{}, fmt.Errorf("bb model discovery: install and configure the Multica BB plugin: %w", err)
	}
	var models []Model
	if err := json.Unmarshal(output, &models); err != nil {
		return Catalog{}, fmt.Errorf("bb model catalog: %w", err)
	}
	for _, model := range models {
		if strings.TrimSpace(model.ID) == "" {
			return Catalog{}, fmt.Errorf("bb model catalog contains an empty model ID")
		}
	}
	return Catalog{Models: models}, nil
}

// ProbeBBRuntime checks the companion plugin, not just the presence of bb.
func ProbeBBRuntime(ctx context.Context, runtimeCmd Command) (string, error) {
	output, err := runtimeCmd.exec(ctx, "multica", "status").Output()
	if err != nil {
		return "", fmt.Errorf("install and configure integrations/bb in BB")
	}
	var status struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(output, &status); err != nil || status.Version == "" {
		return "", fmt.Errorf("invalid BB Multica runtime status")
	}
	return status.Version, nil
}
