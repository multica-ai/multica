package agent

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

const codexChatGPTTokenEnv = "MULTICA_CHATGPT_ACCESS_TOKEN"

// CodexChatGPTToken is an ephemeral, local-only credential for one registration.
// The daemon owns refresh and storage; the backend owns process injection.
type CodexChatGPTToken struct {
	AccessToken string `json:"-"`
	ClientID    string
	Subject     string
	Scopes      []string
}

func (t CodexChatGPTToken) String() string   { return "CodexChatGPTToken{[REDACTED]}" }
func (t CodexChatGPTToken) GoString() string { return t.String() }

// CodexChatGPTPlan supplies a fresh token for each app-server process and checks
// that the registration is still selected before starting or steering a turn.
type CodexChatGPTPlan struct {
	AccessToken  func(context.Context) (CodexChatGPTToken, error)
	AssertActive func(context.Context, CodexChatGPTToken) error
}

func (b *codexBackend) prepareChatGPTPlan(ctx context.Context, opts ExecOptions) (CodexChatGPTToken, ExecOptions, error) {
	var token CodexChatGPTToken
	if plan := b.cfg.CodexChatGPTPlan; plan != nil {
		if plan.AccessToken == nil || plan.AssertActive == nil || b.cfg.CodexSessionOwnerDir == "" {
			return token, opts, fmt.Errorf("ChatGPT plan requires local credentials and a session ownership directory")
		}
		var err error
		token, err = plan.AccessToken(ctx)
		if err != nil {
			return token, opts, err
		}
		if token.AccessToken == "" || token.ClientID == "" || token.Subject == "" || !slices.Contains(token.Scopes, "chatgpt.tokens.use.direct") {
			return CodexChatGPTToken{}, opts, fmt.Errorf("ChatGPT plan access is not granted; sign in with chatgpt.tokens.use.direct")
		}
		if strings.TrimSpace(opts.Model) == "" {
			return CodexChatGPTToken{}, opts, fmt.Errorf("ChatGPT plan requires a model from the account's current model catalog")
		}
	}
	if opts.ResumeSessionID != "" && b.cfg.CodexSessionOwnerDir != "" {
		owner, err := os.ReadFile(codexChatGPTOwnerPath(b.cfg.CodexSessionOwnerDir, opts.ResumeSessionID))
		if err != nil && !os.IsNotExist(err) {
			return token, opts, fmt.Errorf("read Codex thread ownership: %w", err)
		}
		if (b.cfg.CodexChatGPTPlan != nil && string(owner) != codexChatGPTOwner(token)) || (b.cfg.CodexChatGPTPlan == nil && err == nil) {
			opts.ResumeSessionID = ""
		}
	}
	return token, opts, nil
}

func codexChatGPTOwner(token CodexChatGPTToken) string {
	sum := sha256.Sum256([]byte(token.ClientID + "\x00" + token.Subject))
	return hex.EncodeToString(sum[:])
}

func codexChatGPTOwnerPath(dir, threadID string) string {
	sum := sha256.Sum256([]byte(threadID))
	return filepath.Join(dir, hex.EncodeToString(sum[:])+".owner")
}

func bindCodexChatGPTThread(dir, threadID string, token CodexChatGPTToken) error {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create Codex thread ownership directory: %w", err)
	}
	path := codexChatGPTOwnerPath(dir, threadID)
	owner := codexChatGPTOwner(token)
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if os.IsExist(err) {
		data, readErr := os.ReadFile(path)
		if readErr == nil && string(data) == owner {
			return nil
		}
		return fmt.Errorf("Codex thread belongs to another ChatGPT registration; start a new conversation")
	}
	if err != nil {
		return fmt.Errorf("record Codex thread ownership: %w", err)
	}
	_, writeErr := f.WriteString(owner)
	closeErr := f.Close()
	if writeErr != nil || closeErr != nil {
		_ = os.Remove(path)
		return fmt.Errorf("record Codex thread ownership: write failed")
	}
	return nil
}

func newCodexChatGPTProviderID() (string, error) {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return "", fmt.Errorf("create isolated ChatGPT provider: %w", err)
	}
	return "multica_chatgpt_" + hex.EncodeToString(id[:]), nil
}

func codexChatGPTArgs(providerID string) []string {
	return []string{
		"-c", fmt.Sprintf(`model_provider="%s"`, providerID),
		// Codex recursively merges config tables. A fresh name on every launch
		// avoids inheriting saved auth, headers, or endpoints from another layer.
		"-c", fmt.Sprintf(`model_providers.%s={name="ChatGPT plan",base_url="https://api.openai.com/v1",env_key="MULTICA_CHATGPT_ACCESS_TOKEN",wire_api="responses",requires_openai_auth=false,supports_websockets=false}`, providerID),
		// The credential is for the app-server only, never its shell tools.
		"-c", `shell_environment_policy.exclude=["MULTICA_CHATGPT_ACCESS_TOKEN"]`,
	}
}

func redactCodexChatGPTToken(value string, token CodexChatGPTToken) string {
	if token.AccessToken != "" {
		value = strings.ReplaceAll(value, token.AccessToken, "[REDACTED]")
	}
	return value
}

func withoutCodexChatGPTToken(env []string) []string {
	clean := make([]string, 0, len(env))
	for _, value := range env {
		if !strings.HasPrefix(value, codexChatGPTTokenEnv+"=") {
			clean = append(clean, value)
		}
	}
	return clean
}
