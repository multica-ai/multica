package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Find resolves a provider's executable; it fails when the provider is not
// installed.
type Find func(ctx context.Context, provider string) (string, error)

// Catalog answers provider questions with short-lived caches, so opening a
// menu does not start a CLI every time.
type Catalog struct {
	find      Find
	claudeDir string
	codexDir  string
	now       func() time.Time

	mu      sync.Mutex
	models  map[string]cached[Models]
	account map[string]cached[accountRead]
	paths   map[string]string
	// codexDefault is the default a running Codex last reported; its model cache
	// does not say.
	codexDefault string
}

type cached[T any] struct {
	at    time.Time
	value T
	err   error
}

// New returns a catalog. claudeDir and codexDir are the CLIs' home
// directories; empty values use their defaults.
func New(find Find, claudeDir, codexDir string) *Catalog {
	home, _ := os.UserHomeDir()
	if claudeDir == "" {
		claudeDir = envOr("CLAUDE_CONFIG_DIR", filepath.Join(home, ".claude"))
	}
	if codexDir == "" {
		codexDir = envOr("CODEX_HOME", filepath.Join(home, ".codex"))
	}
	return &Catalog{
		find: find, claudeDir: claudeDir, codexDir: codexDir, now: time.Now,
		models: map[string]cached[Models]{}, account: map[string]cached[accountRead]{}, paths: map[string]string{},
	}
}

// Provider is one supported provider and whether it is installed.
type Provider struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Available bool   `json:"available"`
}

// Providers lists the supported providers.
func (c *Catalog) Providers(ctx context.Context) []Provider {
	out := []Provider{{ID: "codex", Name: "Codex"}, {ID: "claude", Name: "Claude Code"}}
	for i := range out {
		_, err := c.find(ctx, out[i].ID)
		out[i].Available = err == nil
	}
	return out
}

// Models is a provider's model catalog, in the shape Desktop shares with
// cloud runtimes.
type Models struct {
	Supported bool    `json:"supported"`
	Models    []Model `json:"models"`
}

// Model is one selectable model.
type Model struct {
	ID       string    `json:"id"`
	Label    string    `json:"label"`
	Thinking *Thinking `json:"thinking,omitempty"`
	// IsDefault marks what following the default runs now. It is only a label:
	// the setting stays "follow the default", so it can change underneath.
	IsDefault bool `json:"isDefault,omitempty"`
}

// Thinking lists a model's reasoning efforts.
type Thinking struct {
	SupportedLevels []Level `json:"supported_levels"`
	DefaultLevel    string  `json:"default_level,omitempty"`
}

// Level is one reasoning effort.
type Level struct {
	Value       string `json:"value"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

const (
	modelsTTL       = time.Minute
	modelsTimeout   = 10 * time.Second
	usageTimeout    = 20 * time.Second
	accountTTL      = time.Minute
	forcedTTL       = 15 * time.Second
	transcriptBytes = 2_000_000
)

// ErrUnknownProvider rejects a provider this catalog does not support.
var ErrUnknownProvider = errors.New("catalog: unknown provider")

// NoteCodexDefault remembers the default model a running Codex reported.
func (c *Catalog) NoteCodexDefault(model string) {
	c.mu.Lock()
	c.codexDefault = model
	c.mu.Unlock()
}

func markDefault(models Models, id string) Models {
	for i := range models.Models {
		if models.Models[i].ID == id {
			models.Models[i].IsDefault = true
		}
	}
	return models
}

// Models returns a provider's models. Claude's come from the CLI's
// initialize response and are cached for a minute unless force is set;
// Codex's come from its local model cache.
func (c *Catalog) Models(ctx context.Context, provider string, force bool) (Models, error) {
	switch provider {
	case "codex":
		c.mu.Lock()
		reported := c.codexDefault
		c.mu.Unlock()
		return markDefault(codexModels(c.codexDir), reported), nil
	case "claude":
	default:
		return Models{}, ErrUnknownProvider
	}
	c.mu.Lock()
	entry, ok := c.models[provider]
	c.mu.Unlock()
	if ok && !force && entry.err == nil && c.now().Sub(entry.at) < modelsTTL {
		return entry.value, nil
	}
	exe, err := c.find(ctx, provider)
	if err != nil {
		return Models{}, err
	}
	result, err := probeClaude(ctx, probeRequest{executable: exe, dir: os.TempDir(), env: probeEnv(), timeout: modelsTimeout})
	var models Models
	if err == nil {
		models = claudeModels(result.initialize)
		c.mu.Lock()
		c.models[provider] = cached[Models]{at: c.now(), value: models}
		c.mu.Unlock()
	}
	return models, err
}

// Usage is a provider's account limits and a conversation's context use.
type Usage struct {
	Provider string       `json:"provider"`
	Context  ContextUsage `json:"context"`
	Account  AccountUsage `json:"account"`
}

// ContextUsage is the latest main request of a conversation.
type ContextUsage struct {
	UsedTokens  *int64  `json:"used_tokens"`
	LimitTokens *int64  `json:"limit_tokens"`
	Model       *string `json:"model"`
	UpdatedAt   *string `json:"updated_at"`
}

// AccountUsage is the account's rate-limit windows.
type AccountUsage struct {
	Status    string   `json:"status"`
	Plan      *string  `json:"plan"`
	Windows   []Window `json:"windows"`
	UpdatedAt *string  `json:"updated_at"`
	Stale     bool     `json:"stale,omitempty"`
}

// Window is one rate-limit window.
type Window struct {
	ID          string  `json:"id"`
	Label       string  `json:"label"`
	UsedPercent float64 `json:"used_percent"`
	ResetsAt    *string `json:"resets_at"`
}

// Session names the conversation whose context use to read.
type Session struct {
	ProviderSessionID string
	Cwd               string
}

type accountRead struct {
	account AccountUsage
	// limits are Claude's per-model limits, for the context window.
	limits map[string]json.RawMessage
}

// Usage reads account limits (cached for a minute, or 15 seconds when
// forced) and, for a conversation, its latest context use.
func (c *Catalog) Usage(ctx context.Context, provider string, session *Session, force bool) (Usage, error) {
	if provider != "claude" && provider != "codex" {
		return Usage{}, ErrUnknownProvider
	}
	read := c.readAccount(ctx, provider, session, force)
	usage := Usage{Provider: provider, Account: read.account, Context: ContextUsage{}}
	if session != nil && validSessionID(session.ProviderSessionID) {
		usage.Context = parseContext(provider, c.transcript(provider, session.ProviderSessionID))
		if usage.Context.Model != nil {
			var limit struct {
				ContextWindow *int64 `json:"contextWindow"`
			}
			if json.Unmarshal(read.limits[*usage.Context.Model], &limit) == nil {
				usage.Context.LimitTokens = limit.ContextWindow
			}
		}
	}
	return usage, nil
}

func (c *Catalog) readAccount(ctx context.Context, provider string, session *Session, force bool) accountRead {
	key, resume, dir := provider+":account", "", os.TempDir()
	if session != nil && provider == "claude" && validSessionID(session.ProviderSessionID) {
		key, resume = provider+":"+session.ProviderSessionID, session.ProviderSessionID
	}
	if session != nil && session.Cwd != "" {
		dir = session.Cwd
	}
	ttl := accountTTL
	if force {
		ttl = forcedTTL
	}
	c.mu.Lock()
	entry, ok := c.account[key]
	c.mu.Unlock()
	if ok && c.now().Sub(entry.at) < ttl && (!force || entry.value.account.Status != "error") {
		return entry.value
	}
	exe, err := c.find(ctx, provider)
	if err != nil {
		return accountRead{account: AccountUsage{Status: "unavailable", Windows: []Window{}}}
	}
	probe := probeCodex
	if provider == "claude" {
		probe = probeClaude
	}
	result, err := probe(ctx, probeRequest{executable: exe, dir: dir, env: probeEnv(), resume: resume, usage: true, timeout: usageTimeout})
	var read accountRead
	if err != nil {
		read = accountRead{account: AccountUsage{Status: "error", Windows: []Window{}}}
		if ok {
			read = entry.value
			read.account.Status, read.account.Stale = "error", true
		}
	} else {
		read = accountRead{account: parseAccount(provider, result.usage, c.now()), limits: claudeModelLimits(result.usage)}
	}
	c.mu.Lock()
	c.account[key] = cached[accountRead]{at: c.now(), value: read}
	c.mu.Unlock()
	return read
}

// sessionFile finds the provider's own session file, or "" when it has none.
func (c *Catalog) sessionFile(provider, id string) string {
	root, depth := filepath.Join(c.claudeDir, "projects"), 1
	if provider == "codex" {
		root, depth = filepath.Join(c.codexDir, "sessions"), 4
	}
	key := root + ":" + id
	c.mu.Lock()
	path := c.paths[key]
	c.mu.Unlock()
	if path == "" {
		path = findSessionFile(root, id+".jsonl", depth)
		if path == "" {
			return ""
		}
		c.mu.Lock()
		c.paths[key] = path
		c.mu.Unlock()
	}
	return path
}

// ClaudeTranscriptSize is the size of the session file a resume reloads.
func (c *Catalog) ClaudeTranscriptSize(id string) (int64, bool) {
	path := c.sessionFile("claude", id)
	if path == "" {
		return 0, false
	}
	info, err := os.Stat(path)
	if err != nil {
		return 0, false
	}
	return info.Size(), true
}

// transcript reads the tail of the provider's own session file.
func (c *Catalog) transcript(provider, id string) [][]byte {
	path := c.sessionFile(provider, id)
	if path == "" {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil
	}
	length := min(info.Size(), transcriptBytes)
	buf := make([]byte, length)
	if _, err := f.ReadAt(buf, info.Size()-length); err != nil {
		return nil
	}
	var lines [][]byte
	for _, line := range strings.Split(string(buf), "\n") {
		if json.Valid([]byte(line)) {
			lines = append(lines, []byte(line))
		}
	}
	return lines
}

func findSessionFile(dir, suffix string, depth int) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if e.Type().IsRegular() && strings.HasSuffix(e.Name(), suffix) {
			return filepath.Join(dir, e.Name())
		}
	}
	if depth > 0 {
		for _, e := range entries {
			if e.IsDir() {
				if found := findSessionFile(filepath.Join(dir, e.Name()), suffix, depth-1); found != "" {
					return found
				}
			}
		}
	}
	return ""
}

func validSessionID(id string) bool {
	if id == "" || len(id) > 128 {
		return false
	}
	for _, r := range id {
		if !(r == '-' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z') {
			return false
		}
	}
	return true
}

func envOr(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}

// Executable resolves a provider's executable.
func (c *Catalog) Executable(ctx context.Context, provider string) (string, error) {
	return c.find(ctx, provider)
}
