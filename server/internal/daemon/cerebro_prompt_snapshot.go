package daemon

// CEREBRO-PATCH(run-prompt-snapshot): FIR-3212 — byte-exact per-run production
// prompt snapshot. After the daemon has composed exactly what the runtime will
// read (the runtime brief — written to the workdir or passed inline as the
// system prompt — and the task prompt delivered as the user turn), this module
// captures those layers, hashes the original bytes, redacts spawn-time secrets
// from the stored view, and ships the result to the server (best-effort; a
// snapshot failure never touches the task outcome).
//
// Hash provenance: sha256_original is computed over the PRE-redaction bytes —
// each layer's content in order, each terminated by a single 0x00 byte — so
// the record proves what was sent without persisting unredacted content.
// sha256_redacted uses the same serialization over the stored (redacted)
// contents, making every row self-verifying.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"strings"
	"time"
)

// redactedPlaceholder replaces every occurrence of a known spawn secret in the
// stored view. The original byte length is deliberately not preserved — hiding
// the secret's length is worth more than layer byte offsets staying exact,
// and byte_size records the original size per layer.
const redactedPlaceholder = "[REDACTED]"

// minRedactableSecretLen guards against shredding the prompt with 1-5 char
// "secrets" (e.g. a custom env var holding "1" or "on"), which would match all
// over ordinary text and destroy the display view.
const minRedactableSecretLen = 6

// PromptLayer is one ordered slice of what the runtime read, as delivered.
type PromptLayer struct {
	Name string `json:"name"`
	// Delivery records the transport: system_prompt (inline ExecOptions
	// SystemPrompt), workdir_file (brief written into CLAUDE.md/AGENTS.md),
	// or user_prompt (the task prompt sent as the user turn).
	Delivery        string `json:"delivery"`
	ByteSize        int    `json:"byte_size"`
	SHA256Original  string `json:"sha256_original"`
	SHA256Redacted  string `json:"sha256_redacted"`
	ContentRedacted string `json:"content_redacted"`
}

// PromptSnapshot is the run evidence uploaded to the server.
type PromptSnapshot struct {
	TaskID              string        `json:"task_id"`
	AgentID             string        `json:"agent_id"`
	IssueID             string        `json:"issue_id,omitempty"`
	AgentContextVersion string        `json:"agent_context_version,omitempty"`
	Provider            string        `json:"provider"`
	Model               string        `json:"model,omitempty"`
	RuntimeVersion      string        `json:"runtime_version,omitempty"`
	SystemPromptMode    string        `json:"system_prompt_mode,omitempty"`
	Layers              []PromptLayer `json:"layers"`
	SHA256Original      string        `json:"sha256_original"`
	SHA256Redacted      string        `json:"sha256_redacted"`
	TotalBytes          int           `json:"total_bytes"`
	Redacted            bool          `json:"redacted"`
}

// promptSnapshotInput carries everything runTask knows at the capture point.
type promptSnapshotInput struct {
	Task             Task
	Provider         string
	Model            string
	RuntimeVersion   string
	SystemPromptMode string
	RuntimeBrief     string
	BriefInline      bool // true when the brief went inline via ExecOptions.SystemPrompt
	Prompt           string
	Secrets          []string
}

// buildPromptSnapshot assembles the layered snapshot. It never errors: a
// snapshot is descriptive evidence and the inputs are already in memory.
func buildPromptSnapshot(in promptSnapshotInput) PromptSnapshot {
	type rawLayer struct {
		name     string
		delivery string
		content  string
	}
	var raws []rawLayer
	if in.RuntimeBrief != "" {
		delivery := "workdir_file"
		if in.BriefInline {
			delivery = "system_prompt"
		}
		raws = append(raws, rawLayer{name: "runtime_brief", delivery: delivery, content: in.RuntimeBrief})
	}
	if in.Prompt != "" {
		raws = append(raws, rawLayer{name: "task_prompt", delivery: "user_prompt", content: in.Prompt})
	}

	origHash := sha256.New()
	redHash := sha256.New()
	layers := make([]PromptLayer, 0, len(raws))
	total := 0
	redacted := false
	for _, r := range raws {
		red := redactSecrets(r.content, in.Secrets)
		if red != r.content {
			redacted = true
		}
		origHash.Write([]byte(r.content))
		origHash.Write([]byte{0})
		redHash.Write([]byte(red))
		redHash.Write([]byte{0})
		total += len(r.content)
		layers = append(layers, PromptLayer{
			Name:            r.name,
			Delivery:        r.delivery,
			ByteSize:        len(r.content),
			SHA256Original:  sha256Hex(r.content),
			SHA256Redacted:  sha256Hex(red),
			ContentRedacted: red,
		})
	}

	version := ""
	if in.Task.Agent != nil {
		version = in.Task.Agent.ContextVersion
	}
	return PromptSnapshot{
		TaskID:              in.Task.ID,
		AgentID:             in.Task.AgentID,
		IssueID:             in.Task.IssueID,
		AgentContextVersion: version,
		Provider:            in.Provider,
		Model:               in.Model,
		RuntimeVersion:      in.RuntimeVersion,
		SystemPromptMode:    in.SystemPromptMode,
		Layers:              layers,
		SHA256Original:      hex.EncodeToString(origHash.Sum(nil)),
		SHA256Redacted:      hex.EncodeToString(redHash.Sum(nil)),
		TotalBytes:          total,
		Redacted:            redacted,
	}
}

// redactSecrets removes every occurrence of each known secret value from the
// stored view. Value-based redaction is exact — the daemon knows the real
// spawn secrets, so no pattern guessing is needed or wanted.
func redactSecrets(content string, secrets []string) string {
	for _, s := range secrets {
		if len(s) < minRedactableSecretLen {
			continue
		}
		content = strings.ReplaceAll(content, s, redactedPlaceholder)
	}
	return content
}

// snapshotSecretsForTask collects every secret value injected into the spawn
// environment — agent custom env, Infisical-resolved secrets, and the
// task-scoped API token — so none of them can survive into the stored view.
func snapshotSecretsForTask(task Task, agentToken string) []string {
	var secrets []string
	if task.Agent != nil {
		for _, v := range task.Agent.CustomEnv {
			secrets = append(secrets, v)
		}
		for _, v := range task.Agent.InfisicalSecrets {
			secrets = append(secrets, v)
		}
	}
	if agentToken != "" {
		secrets = append(secrets, agentToken)
	}
	return secrets
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// shipPromptSnapshot uploads the snapshot asynchronously. It intentionally
// detaches from the task context: the evidence should still land if the run
// itself is cancelled moments later, and a slow server must never stall the
// spawn path. Failures are logged and dropped — the server insert is
// idempotent, so the next daemon with the same task could not double-write
// even if a retry were added later.
func (d *Daemon) shipPromptSnapshot(snap PromptSnapshot, taskLog *slog.Logger) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := d.client.ReportTaskPromptSnapshot(ctx, snap); err != nil {
			taskLog.Warn("prompt snapshot upload failed (non-fatal)", "error", err)
		}
	}()
}
