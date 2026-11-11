package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"sort"
	"time"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
)

// Context receipt (MUL-9013): a per-run record of which context sources this
// daemon handed the agent, how big each was, how it was delivered, and how
// sure we are the runtime actually loaded it. Metadata only — sizes, short
// hashes, workdir-relative paths and names. Content never leaves the machine.

// ContextReceiptSchemaVersion is bumped when the receipt's meaning changes in a
// way readers must branch on. Additive fields do not bump it.
const ContextReceiptSchemaVersion = 1

// Source kinds.
const (
	ReceiptKindRuntimeBrief         = "runtime_brief"
	ReceiptKindAgentInstructions    = "agent_instructions"
	ReceiptKindProjectDescription   = "project_description"
	ReceiptKindExistingInstructions = "existing_instructions"
	ReceiptKindTaskPrompt           = "task_prompt"
	ReceiptKindSkill                = "skill"
)

// Delivery modes.
const (
	ReceiptDeliveryFile     = "file"     // written to a file the runtime is expected to discover
	ReceiptDeliveryInline   = "inline"   // passed as the turn's system prompt
	ReceiptDeliveryPrompt   = "prompt"   // the turn input itself
	ReceiptDeliveryEmbedded = "embedded" // a section inside another source (see Parent)
	ReceiptDeliveryNone     = "none"     // built but the runtime has no channel for it
)

// Load states, from strongest evidence to weakest.
const (
	// The daemon put it in the request the runtime received.
	ReceiptLoadDelivered = "delivered"
	// Written to a file this runtime, at this version, was probed to load.
	ReceiptLoadVerified = "verified"
	// Written to a file this runtime was probed to load, but at a different
	// (or unknown) version than the one that ran.
	ReceiptLoadVerifiedOtherVersion = "verified_other_version"
	// Written to disk; nothing confirms the runtime reads it.
	ReceiptLoadUnverified = "unverified"
	// Built but not delivered through any channel.
	ReceiptLoadNotDelivered = "not_delivered"
)

// ContextReceipt is the wire shape POSTed to the server.
type ContextReceipt struct {
	SchemaVersion  int    `json:"schema_version"`
	Runtime        string `json:"runtime"`
	RuntimeVersion string `json:"runtime_version,omitempty"`
	Model          string `json:"model,omitempty"`
	PromptBytes    int    `json:"prompt_bytes"`
	BriefBytes     int    `json:"brief_bytes"`
	BriefDelivery  string `json:"brief_delivery"`
	// ResumedSession reports the turn continued a prior provider session, so
	// the model also carries that session's history beyond what is listed.
	ResumedSession bool `json:"resumed_session"`
	// FreshRetry marks the receipt rewritten for the cold retry after a
	// resume was rejected; it replaces the first attempt's receipt.
	FreshRetry bool                   `json:"fresh_retry,omitempty"`
	MCPServers []string               `json:"mcp_servers"`
	Sources    []ContextReceiptSource `json:"sources"`
}

// ContextReceiptSource is one row of the receipt.
type ContextReceiptSource struct {
	Kind            string `json:"kind"`
	Name            string `json:"name,omitempty"`
	Bytes           int    `json:"bytes"`
	SHA256          string `json:"sha256,omitempty"`
	Delivery        string `json:"delivery"`
	Path            string `json:"path,omitempty"`
	Load            string `json:"load"`
	VerifiedVersion string `json:"verified_version,omitempty"`
	Parent          string `json:"parent,omitempty"`
}

// contextFileLoadProbes records runtimes confirmed to load the context file
// the daemon writes, and the CLI version each probe ran against. Source: the
// MUL-5392 canary probes documented on providerNeedsInlineSystemPrompt, plus
// the kiro-cli ACP smoke. Add a row only with a probe behind it; an absent
// runtime reads "unverified" on the receipt, which is the honest answer.
var contextFileLoadProbes = map[string]string{
	"claude":   "2.1.220",
	"codex":    "0.144.6",
	"opencode": "1.17.7",
	"pi":       "0.67.2",
	"hermes":   "0.18.2",
	"kiro":     "2.13.0",
	"mcode":    "0.1.2",
}

var receiptVersionPattern = regexp.MustCompile(`\d+\.\d+\.\d+`)

// contextFileLoadState grades a file-delivered source for this runtime.
func contextFileLoadState(provider, runtimeVersion string) (state, verifiedVersion string) {
	probed, ok := contextFileLoadProbes[provider]
	if !ok {
		return ReceiptLoadUnverified, ""
	}
	if receiptVersionPattern.FindString(runtimeVersion) == probed {
		return ReceiptLoadVerified, probed
	}
	return ReceiptLoadVerifiedOtherVersion, probed
}

// receiptHash is a short content fingerprint: enough to tell two copies of a
// rule apart at a glance, too short to be useful for anything else.
func receiptHash(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])[:12]
}

// contextReceiptInput is everything buildContextReceipt reads. Gathered at the
// launch site so the builder stays a pure function of its arguments.
type contextReceiptInput struct {
	Provider       string
	RuntimeVersion string
	CustomRuntime  bool
	Model          string
	WorkDir        string
	Prompt         string
	RuntimeBrief   string
	InlineBrief    bool
	BriefWritten   bool
	ResumedSession bool
	FreshRetry     bool
	MCPConfig      json.RawMessage
	TaskCtx        execenv.TaskContextForEnv
	Skills         []execenv.SkillReceipt
	// ExistingInstructions is the repository's own content in the runtime
	// config file, outside the managed block.
	ExistingInstructions string
}

func buildContextReceipt(in contextReceiptInput) ContextReceipt {
	r := ContextReceipt{
		SchemaVersion:  ContextReceiptSchemaVersion,
		Runtime:        in.Provider,
		RuntimeVersion: in.RuntimeVersion,
		Model:          in.Model,
		PromptBytes:    len(in.Prompt),
		BriefBytes:     len(in.RuntimeBrief),
		ResumedSession: in.ResumedSession,
		FreshRetry:     in.FreshRetry,
		MCPServers:     receiptMCPServerNames(in.MCPConfig),
	}

	configPath := execenv.RuntimeConfigRelPath(in.WorkDir, in.Provider)
	fileLoad, fileVerifiedAt := contextFileLoadState(in.Provider, in.RuntimeVersion)
	if in.CustomRuntime {
		// A vendor CLI probe does not establish how an arbitrary command
		// speaking the same protocol discovers its instruction files.
		fileLoad, fileVerifiedAt = ReceiptLoadUnverified, ""
	}

	brief := ContextReceiptSource{
		Kind:   ReceiptKindRuntimeBrief,
		Bytes:  len(in.RuntimeBrief),
		SHA256: receiptHash(in.RuntimeBrief),
	}
	switch {
	case in.InlineBrief:
		// Inline wins over the file: providerNeedsInlineSystemPrompt exists
		// because these runtimes do not read the file the daemon also writes.
		brief.Delivery, brief.Load = ReceiptDeliveryInline, ReceiptLoadDelivered
	case configPath != "" && in.BriefWritten:
		brief.Delivery, brief.Path = ReceiptDeliveryFile, configPath
		brief.Load, brief.VerifiedVersion = fileLoad, fileVerifiedAt
	default:
		brief.Delivery, brief.Load = ReceiptDeliveryNone, ReceiptLoadNotDelivered
	}
	r.BriefDelivery = brief.Delivery
	r.Sources = append(r.Sources, brief)

	// Sections the owner configured that travel inside the brief. Listed on
	// their own so a reader can see their size and fingerprint without
	// diffing the brief; their load state is the brief's.
	embedded := func(kind, content string) {
		if content == "" {
			return
		}
		r.Sources = append(r.Sources, ContextReceiptSource{
			Kind:            kind,
			Bytes:           len(content),
			SHA256:          receiptHash(content),
			Delivery:        ReceiptDeliveryEmbedded,
			Parent:          ReceiptKindRuntimeBrief,
			Load:            brief.Load,
			VerifiedVersion: brief.VerifiedVersion,
		})
	}
	embedded(ReceiptKindAgentInstructions, in.TaskCtx.AgentInstructions)
	embedded(ReceiptKindProjectDescription, in.TaskCtx.ProjectDescription)

	if in.ExistingInstructions != "" && configPath != "" {
		r.Sources = append(r.Sources, ContextReceiptSource{
			Kind:            ReceiptKindExistingInstructions,
			Bytes:           len(in.ExistingInstructions),
			SHA256:          receiptHash(in.ExistingInstructions),
			Delivery:        ReceiptDeliveryFile,
			Path:            configPath,
			Load:            fileLoad,
			VerifiedVersion: fileVerifiedAt,
		})
	}

	r.Sources = append(r.Sources, ContextReceiptSource{
		Kind:     ReceiptKindTaskPrompt,
		Bytes:    len(in.Prompt),
		SHA256:   receiptHash(in.Prompt),
		Delivery: ReceiptDeliveryPrompt,
		Load:     ReceiptLoadDelivered,
	})

	for _, skill := range in.Skills {
		source := ContextReceiptSource{
			Kind: ReceiptKindSkill, Name: skill.Name, Bytes: skill.Bytes,
			SHA256: skill.SHA256, Path: skill.Path,
			Delivery: ReceiptDeliveryNone, Load: ReceiptLoadNotDelivered,
		}
		if skill.Available {
			// Materialization is confirmed, discovery has not been probed.
			source.Delivery, source.Load = ReceiptDeliveryFile, ReceiptLoadUnverified
		}
		r.Sources = append(r.Sources, source)
	}
	return r
}

// receiptMCPServerNames lists the server names in an mcpServers config. Names
// only: commands, URLs, args and env can carry credentials.
func receiptMCPServerNames(raw json.RawMessage) []string {
	names := []string{}
	if len(raw) == 0 {
		return names
	}
	var cfg struct {
		MCPServers map[string]json.RawMessage `json:"mcpServers"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return names
	}
	for name := range cfg.MCPServers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// reportContextReceipt sends the receipt best-effort. The receipt is
// diagnostic: a failure — including a server that predates the endpoint —
// is logged and never affects the run.
func (d *Daemon) reportContextReceipt(ctx context.Context, taskID string, receipt ContextReceipt, taskLog *slog.Logger) {
	reportCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	err := d.client.ReportTaskContextReceipt(reportCtx, taskID, receipt)
	if err == nil {
		return
	}
	var reqErr *requestError
	if errors.As(err, &reqErr) && (reqErr.StatusCode == http.StatusNotFound || reqErr.StatusCode == http.StatusMethodNotAllowed) {
		taskLog.Debug("server does not accept context receipts; skipping", "error", err)
		return
	}
	taskLog.Warn("failed to report context receipt", "error", err)
}
