package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

// CodeBuddy 2.151 emits per-model-message OpenAI-shaped usage in
// usage_update._meta.usage, not the cumulative ACP usage field. A later
// context-window update repeats the message id without any token counters.
// Reconcile by message id before summing so neither repeat charges twice.
// Vendor credit and an empty-currency cost are not USD and are left unset.
type codebuddyUsageTracker struct {
	mu       sync.Mutex
	messages map[string]TokenUsage
}

func (u *codebuddyUsageTracker) observe(method string, params json.RawMessage) {
	if method != "session/update" && method != "session/notification" {
		return
	}
	var event struct {
		Update struct {
			Meta struct {
				MessageID string                     `json:"codebuddy.ai/messageId"`
				Usage     map[string]json.RawMessage `json:"usage"`
			} `json:"_meta"`
		} `json:"update"`
	}
	if json.Unmarshal(params, &event) != nil {
		return
	}
	meta := event.Update.Meta

	if meta.MessageID == "" || len(meta.Usage) == 0 {
		return
	}
	u.record(meta.MessageID, meta.Usage)
}

func (u *codebuddyUsageTracker) record(messageID string, raw map[string]json.RawMessage) {
	input, hasInput := acpUsageInt64(raw, "prompt_tokens")
	output, hasOutput := acpUsageInt64(raw, "completion_tokens")
	if !hasInput || !hasOutput {
		return
	}
	read, _ := acpUsageInt64(raw, "cache_read_input_tokens", "prompt_cache_hit_tokens")
	write, _ := acpUsageInt64(raw, "cache_creation_input_tokens", "prompt_cache_write_tokens")
	if miss, ok := acpUsageInt64(raw, "prompt_cache_miss_tokens"); ok {
		input = miss
	} else if input >= read+write {
		input -= read + write
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.messages == nil {
		u.messages = make(map[string]TokenUsage)
	}
	u.messages[messageID] = TokenUsage{InputTokens: input, OutputTokens: output, CacheReadTokens: read, CacheWriteTokens: write}
}

func (u *codebuddyUsageTracker) total() TokenUsage {
	u.mu.Lock()
	defer u.mu.Unlock()
	var total TokenUsage
	for _, v := range u.messages {
		total.InputTokens += v.InputTokens
		total.OutputTokens += v.OutputTokens
		total.CacheReadTokens += v.CacheReadTokens
		total.CacheWriteTokens += v.CacheWriteTokens
	}
	return total
}

// reconcileTranscript reads only the exact CLI session and exact prompt request.
// CodeBuddy 2.151 can omit wire usage after session/load even though it persists
// providerData.rawUsage. Message IDs reconcile this external metering source
// with wire events; history, sibling runs and duplicate blocks are not summed.
func (u *codebuddyUsageTracker) reconcileTranscript(env []string, cwd, sessionID, requestID string) error {
	if requestID == "" {
		return nil
	}
	if sessionID == "" || strings.IndexFunc(sessionID, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-')
	}) >= 0 {
		return fmt.Errorf("invalid CodeBuddy session id")
	}
	var root, home string
	for _, entry := range env {
		k, v, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		switch k {
		case "CODEBUDDY_CONFIG_DIR":
			root = v
		case "HOME":
			if runtime.GOOS != "windows" {
				home = v
			}
		case "USERPROFILE":
			if runtime.GOOS == "windows" {
				home = v
			}
		}
	}
	if strings.TrimSpace(root) == "" {
		if home == "" {
			return fmt.Errorf("CodeBuddy home unavailable")
		}
		root = filepath.Join(home, ".codebuddy")
	}
	if !filepath.IsAbs(root) {
		root = filepath.Join(cwd, root)
	}
	// The CLI hashes long and platform-specific workspace names. Enumerate only
	// directory names, then open the unique exact session filename, never infer
	// its project key or read other sessions to discover an identity.
	dirs, err := os.ReadDir(filepath.Join(root, "projects"))
	if err != nil {
		return err
	}
	var path string
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		candidate := filepath.Join(root, "projects", d.Name(), sessionID+".jsonl")
		info, e := os.Lstat(candidate)
		if os.IsNotExist(e) {
			continue
		}
		if e != nil {
			return e
		}
		if !info.Mode().IsRegular() {
			continue
		}
		if path != "" {
			return fmt.Errorf("ambiguous CodeBuddy session transcript")
		}
		path = candidate
	}
	if path == "" {
		return fmt.Errorf("CodeBuddy session transcript unavailable")
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	scan := newAgentStreamScanner(f)
	for scan.Scan() {
		var row struct {
			SessionID string `json:"sessionId"`
			Provider  struct {
				RequestID string                     `json:"conversationRequestId"`
				MessageID string                     `json:"messageId"`
				Usage     map[string]json.RawMessage `json:"rawUsage"`
			} `json:"providerData"`
		}
		if err := json.Unmarshal(scan.Bytes(), &row); err != nil {
			return fmt.Errorf("invalid CodeBuddy transcript record: %w", err)
		}
		if row.SessionID == sessionID && row.Provider.RequestID == requestID && row.Provider.MessageID != "" {
			u.record(row.Provider.MessageID, row.Provider.Usage)
		}
	}
	return scan.Err()
}
