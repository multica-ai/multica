package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// defaultZcodeDiscoveryTimeout bounds one throwaway app-server spawn during
// model discovery. The dominant cost is the Node CLI's cold module load (the
// same first-run latency the execution handshake pays), so it mirrors
// defaultZcodeHandshakeTimeout.
const defaultZcodeDiscoveryTimeout = 30 * time.Second

// discoverZcodeModels reads ZCode's real model catalog. The CLI has no
// models/list command or RPC — the catalog only exists inside a session
// snapshot (settings.model.available, assembled from the provider registry,
// i.e. the logged-in account). Discovery therefore spawns a throwaway
// app-server, creates a `persistence:"deferred"` draft session — deferred
// drafts never enter the session store (zcodeSessionService.createSession),
// so discovery leaves no residue — and reads the snapshot, modelled on
// discoverACPModels' process handling. Any failure degrades to the empty
// catalog so the UI keeps manual entry in the `providerId/modelId` format
// (splitZcodeModelSelection).
func discoverZcodeModels(ctx context.Context, runtimeCmd Command) (Catalog, error) {
	empty := Catalog{Models: []Model{}}
	if runtimeCmd.Path == "" {
		runtimeCmd.Path = "zcode"
	}
	if _, err := exec.LookPath(runtimeCmd.Path); err != nil {
		return empty, nil
	}

	runCtx, cancel := context.WithTimeout(ctx, defaultZcodeDiscoveryTimeout)
	defer cancel()

	// The discovery session's workspace is a temp dir: the server treats the
	// process cwd as the workspace, and running inside a real checkout would
	// both pollute it and pick up its dotenv (design doc §6 R8).
	tmp, err := os.MkdirTemp("", "multica-zcode-discovery-")
	if err != nil {
		return empty, nil
	}
	defer os.RemoveAll(tmp)

	cmd := runtimeCmd.exec(runCtx, "app-server")
	hideAgentWindow(cmd)
	cmd.Dir = tmp
	cmd.Env = os.Environ()
	// stderr noise from a discovery spawn helps nobody; the daemon re-runs
	// discovery on demand and the empty-catalog fallback is already silent.
	cmd.Stderr = io.Discard
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return empty, nil
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return empty, nil
	}
	if err := startOwnedProcessTree(cmd, runtimeCmd.logger); err != nil {
		return empty, nil
	}
	// This probe runs on every uncached catalog read, so a leaked app-server
	// here accumulates; always reap the whole tree (same contract as
	// discoverACPModels).
	defer func() {
		_ = stdin.Close()
		signalProcessGroup(cmd, syscall.SIGKILL)
		_, _ = cmd.Process.Wait()
		releaseProcessGroup(cmd)
	}()

	// Wire frames carry no `jsonrpc` member — the server's strict schema
	// rejects it (see zcodeBackend's doc). Sending one would fail the whole
	// create, so this loop deliberately does not reuse the ACP framing.
	params := map[string]any{
		"workspace": map[string]any{
			"workspacePath": tmp,
			// The strict schema rejects an empty key; the temp dir is
			// stable for the lifetime of the throwaway session, which is
			// all the key's identity role needs here.
			"workspaceKey": tmp,
		},
		"persistence": "deferred",
		"mode":        "yolo",
	}
	frame, err := json.Marshal(map[string]any{
		"id":     1,
		"method": "session/create",
		"params": params,
	})
	if err != nil {
		return empty, nil
	}
	frame = append(frame, '\n')
	if _, err := stdin.Write(frame); err != nil {
		return empty, nil
	}

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 1024*1024), 4*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var msg struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Result json.RawMessage `json:"result"`
			Error  *zcodeRPCError  `json:"error"`
		}
		if json.Unmarshal([]byte(line), &msg) != nil {
			continue
		}
		if len(msg.ID) > 0 && string(msg.ID) != "null" && msg.Method != "" {
			// Server-originated request. A real app-server asks for the
			// client's runtime preferences during session/create
			// (session/requestRuntimePreferences); leaving it unanswered is a
			// guaranteed -32022 after its 15s timeout, so answer with the
			// schema defaults (nativeSearchEnhancementsEnabled is the only
			// member without one). Everything else gets the shared reverse
			// handling.
			if msg.Method == "session/requestRuntimePreferences" {
				zcodeWriteReverseResult(stdin, msg.ID, zcodeRuntimePreferencesDefaults())
				continue
			}
			zcodeWriteReverseUnsupported(stdin, msg.ID, msg.Method)
			continue
		}
		// Notifications (startup/storage*, state.updated, session/event
		// replays) are skipped: only the response to our create carries the
		// snapshot.
		if len(msg.ID) == 0 || string(msg.ID) == "null" || string(msg.ID) != "1" {
			continue
		}
		if msg.Error != nil {
			if runtimeCmd.logger != nil {
				runtimeCmd.logger.Debug("zcode model discovery create rejected",
					"backend", "zcode", "code", msg.Error.Code, "message", msg.Error.Message)
			}
			return empty, nil
		}
		catalog, ok := zcodeCatalogFromSnapshot(msg.Result)
		if !ok {
			if runtimeCmd.logger != nil {
				runtimeCmd.logger.Debug("zcode model discovery found no catalog in create snapshot",
					"backend", "zcode",
					"result_keys", strings.Join(acpResultTopLevelKeys(msg.Result), ","))
			}
			return empty, nil
		}
		return catalog, nil
	}
	if err := scanner.Err(); err != nil && runtimeCmd.logger != nil {
		runtimeCmd.logger.Debug("zcode model discovery stream ended", "backend", "zcode", "error", err)
	}
	return empty, nil
}

// zcodeResolveThinkingLevel resolves the level an empty thinking_level ("let
// the runtime decide") actually resolves to. zcode is the one protocol with no
// implicit default — session/create rejects a reasoning-capable selection
// without options.reasoningLevel — so the runtime's decision has to be made
// here. The chain mirrors the CLI's own resolution (zcode-agent-model-state
// current ?? defaultLevel ?? available[0]): the model's advertised
// defaultLevel, else its first supported level. Hardcoding a token instead
// would break the day the registry's dynamic vocabulary changes. A missing
// catalog or entry returns "" — the server's own "Reasoning level is required"
// is then the clearest diagnosis, and no fabricated token is sent.
func zcodeResolveThinkingLevel(ctx context.Context, runtimeCmd Command, model string) string {
	providerID, modelID, ok := splitZcodeModelSelection(model)
	if !ok {
		return ""
	}
	catalog, err := ListModels(ctx, "zcode", runtimeCmd)
	if err != nil {
		return ""
	}
	want := providerID + "/" + modelID
	for _, m := range catalog.Models {
		if m.ID != want || m.Thinking == nil || len(m.Thinking.SupportedLevels) == 0 {
			continue
		}
		if m.Thinking.DefaultLevel != "" {
			return m.Thinking.DefaultLevel
		}
		return m.Thinking.SupportedLevels[0].Value
	}
	return ""
}

// zcodeModelOption mirrors the subset of zcodeModelOptionSchema
// (zcode-protocol index.ts:795-810) the catalog mapping consumes. Parsing is
// lenient: the server's own payloads are strict and evolve, so unknown fields
// are ignored and absent optionals degrade instead of failing the catalog.
type zcodeModelOption struct {
	Ref struct {
		ProviderID string `json:"providerId"`
		ModelID    string `json:"modelId"`
	} `json:"ref"`
	Label     string `json:"label"`
	Reasoning *struct {
		Levels []struct {
			Value       string `json:"value"`
			Label       string `json:"label"`
			Description string `json:"description"`
		} `json:"levels"`
		DefaultLevel string `json:"defaultLevel"`
	} `json:"reasoning"`
	DisabledReason string `json:"disabledReason"`
}

// zcodeCatalogFromSnapshot maps a session/create snapshot onto a Catalog.
// IDs use the runtime's own picker format `providerId/modelId` — the same
// string splitZcodeModelSelection turns back into a selection, so a picked
// entry round-trips into session/create's model param unchanged. Entries with
// a disabledReason land in Unavailable rather than Models: the registry named
// them but will not run them on this account, and UnavailableModel carries the
// runtime's own remedy copy. The second return reports whether the snapshot
// carried a parsable settings.model at all; an absent or malformed one is
// schema drift and must not be mistaken for an empty registry.
func zcodeCatalogFromSnapshot(result json.RawMessage) (Catalog, bool) {
	// Pointers distinguish a missing settings.model (schema drift, ok=false)
	// from a present-but-empty one (a genuinely empty registry): encoding/json
	// silently zero-fills absent fields, so value structs cannot tell the two
	// apart.
	var snap struct {
		Settings *struct {
			Model *struct {
				Current *struct {
					ProviderID string `json:"providerId"`
					ModelID    string `json:"modelId"`
				} `json:"current"`
				Available []zcodeModelOption `json:"available"`
			} `json:"model"`
		} `json:"settings"`
	}
	if json.Unmarshal(result, &snap) != nil || snap.Settings == nil || snap.Settings.Model == nil {
		return Catalog{}, false
	}
	currentRef := ""
	if c := snap.Settings.Model.Current; c != nil && c.ProviderID != "" && c.ModelID != "" {
		currentRef = c.ProviderID + "/" + c.ModelID
	}
	models := []Model{}
	unavailable := []UnavailableModel{}
	for _, opt := range snap.Settings.Model.Available {
		if opt.Ref.ProviderID == "" || opt.Ref.ModelID == "" {
			continue
		}
		id := opt.Ref.ProviderID + "/" + opt.Ref.ModelID
		label := opt.Label
		if label == "" {
			label = opt.Ref.ModelID
		}
		if opt.DisabledReason != "" {
			unavailable = append(unavailable, UnavailableModel{ID: id, Label: label, Reason: opt.DisabledReason})
			continue
		}
		m := Model{ID: id, Label: label, Provider: opt.Ref.ProviderID}
		// Default is a display badge only: the registry's current selection,
		// which for a fresh account session is its preferred model.
		if id == currentRef {
			m.Default = true
		}
		if opt.Reasoning != nil && len(opt.Reasoning.Levels) > 0 {
			levels := make([]ThinkingLevel, 0, len(opt.Reasoning.Levels))
			for _, l := range opt.Reasoning.Levels {
				levels = append(levels, ThinkingLevel{Value: l.Value, Label: l.Label, Description: l.Description})
			}
			m.Thinking = &ModelThinking{SupportedLevels: levels, DefaultLevel: opt.Reasoning.DefaultLevel}
		}
		models = append(models, m)
	}
	return Catalog{Models: models, Unavailable: unavailable}, true
}
