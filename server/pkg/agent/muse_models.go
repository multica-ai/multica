package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// museDiscoveryTimeout bounds the whole catalog read. `muse serve` answers
// model/list immediately after the handshake, so this ceiling only ever covers
// a wedged or unauthenticated host.
var museDiscoveryTimeout = 20 * time.Second

// discoverMuseModels reads the catalog `muse serve` will accept in
// `session/setModel`. It is a short-lived MSP connection: initialize, then the
// model/list query, then a reap. The catalog is a query and not a command — it
// creates no session and writes no durable record — so no session/start is
// needed.
//
// Every failure degrades to an empty catalog with Fallback set, which is what
// keeps the picker's manual-entry field usable on a host that is logged out.
func discoverMuseModels(ctx context.Context, runtimeCmd Command) (Catalog, error) {
	if runtimeCmd.Path == "" {
		runtimeCmd.Path = "muse"
	}
	if _, err := exec.LookPath(runtimeCmd.Path); err != nil {
		return Catalog{Models: []Model{}, Fallback: true}, nil
	}

	runCtx, cancel := context.WithTimeout(ctx, museDiscoveryTimeout)
	defer cancel()

	cmd := runtimeCmd.exec(runCtx, "serve", "--no-session-log")
	hideAgentWindow(cmd)
	cmd.Env = buildEnv(nil)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return Catalog{Models: []Model{}, Fallback: true}, nil
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return Catalog{Models: []Model{}, Fallback: true}, nil
	}
	cmd.Stderr = io.Discard
	if err := startOwnedProcessTree(cmd, runtimeCmd.logger); err != nil {
		return Catalog{Models: []Model{}, Fallback: true}, nil
	}
	// This probe runs on a discovery schedule, so a leaked host would
	// accumulate. Always reap the process group.
	defer func() {
		_ = stdin.Close()
		signalProcessGroup(cmd, syscall.SIGKILL)
		_, _ = cmd.Process.Wait()
		releaseProcessGroup(cmd)
	}()

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 1024*1024), 4*1024*1024)

	write := func(payload map[string]any) error {
		data, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		_, err = stdin.Write(append(data, '\n'))
		return err
	}

	call := func(method string, params map[string]any) (json.RawMessage, error) {
		id := museUUIDv7()
		payload := map[string]any{"jsonrpc": "2.0", "id": id, "method": method}
		if params != nil {
			payload["params"] = params
		}
		if err := write(payload); err != nil {
			return nil, err
		}
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			var env struct {
				ID     json.RawMessage `json:"id"`
				Method string          `json:"method"`
				Result json.RawMessage `json:"result"`
				Error  *museRPCError   `json:"error"`
			}
			if err := json.Unmarshal([]byte(line), &env); err != nil {
				continue
			}
			// Server-initiated requests still need an answer, even here.
			if env.Method != "" && len(env.ID) > 0 && string(env.ID) != "null" {
				_ = write(map[string]any{"jsonrpc": "2.0", "id": env.ID, "result": map[string]any{}})
				continue
			}
			if museDecodeID(env.ID) != id {
				continue
			}
			if env.Error != nil {
				return nil, env.Error
			}
			return env.Result, nil
		}
		if err := scanner.Err(); err != nil {
			return nil, err
		}
		return nil, errMuseProcessExited
	}

	if _, err := call("initialize", map[string]any{
		"clientInfo":   map[string]any{"name": "multica_model_discovery", "version": museLauncherVersion},
		"capabilities": map[string]any{},
	}); err != nil {
		return Catalog{Models: []Model{}, Fallback: true}, nil
	}
	if err := write(map[string]any{"jsonrpc": "2.0", "method": "initialized", "params": map[string]any{}}); err != nil {
		return Catalog{Models: []Model{}, Fallback: true}, nil
	}

	result, err := call("model/list", map[string]any{})
	if err != nil {
		return Catalog{Models: []Model{}, Fallback: true}, nil
	}

	var payload struct {
		ProviderID string `json:"providerId"`
		Source     string `json:"source"`
		Models     []struct {
			ModelID     string  `json:"modelId"`
			DisplayName string  `json:"displayLabel"`
			Description *string `json:"description"`
			IsDefault   bool    `json:"isDefault"`
			ProviderID  string  `json:"providerId"`
		} `json:"models"`
	}
	if err := json.Unmarshal(result, &payload); err != nil {
		return Catalog{Models: []Model{}, Fallback: true}, nil
	}

	models := make([]Model, 0, len(payload.Models))
	for _, entry := range payload.Models {
		if strings.TrimSpace(entry.ModelID) == "" {
			continue
		}
		label := entry.DisplayName
		if label == "" {
			label = entry.ModelID
		}
		provider := entry.ProviderID
		if provider == "" {
			provider = payload.ProviderID
		}
		models = append(models, Model{
			ID:       entry.ModelID,
			Label:    label,
			Provider: provider,
			Default:  entry.IsDefault,
		})
	}
	if len(models) == 0 {
		return Catalog{Models: []Model{}, Fallback: true}, nil
	}
	return Catalog{Models: models}, nil
}
