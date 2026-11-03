package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/multica-ai/multica/server/internal/daemon/localhost"
	"github.com/multica-ai/multica/server/internal/localconversation/catalog"
	"github.com/multica-ai/multica/server/internal/localconversation/computer"
	"github.com/multica-ai/multica/server/internal/localconversation/contextmcp"
	"github.com/multica-ai/multica/server/internal/localconversation/provider/claude"
	"github.com/multica-ai/multica/server/internal/localconversation/provider/codex"
	"github.com/multica-ai/multica/server/internal/localconversation/service"
	"github.com/multica-ai/multica/server/internal/localconversation/store"
	"github.com/multica-ai/multica/server/internal/localconversation/store/sqlitedb"
	"github.com/multica-ai/multica/server/internal/localconversation/timing"
)

// openLocalConversations opens the local conversation service and its
// /local/v2 API. Claude conversations run Desktop's Claude adapter with
// Desktop's Electron in Node mode.
func (d *Daemon) openLocalConversations(ctx context.Context, sidecar localhost.Sidecar) error {
	d.conversationPreheat = newConversationPreheat(d.cfg.ProfileDir, d.cfg.LocalAPIToken)
	svc, err := service.Open(ctx, service.Options{
		Computer:                computer.Command{Executable: sidecar.Electron, Entry: filepath.Join(filepath.Dir(sidecar.ClaudeEntry), "computer-host.cjs")},
		Path:                    filepath.Join(d.cfg.ProfileDir, "local-conversations", "conversations.sqlite"),
		TimingPath:              filepath.Join(d.cfg.ProfileDir, "local-conversations", "diagnostics", "daemon-timing.jsonl"),
		Claude:                  d.claudeLauncher(sidecar),
		Codex:                   d.codexLauncher(),
		CodexSpare:              d.codexSpareLauncher(),
		ClaudeSpare:             d.claudeSpareLauncher(sidecar),
		PreheatOldConversations: d.conversationPreheat.enabled,
		Cloud:                   d.contextCloud,
		Catalog: catalog.New(func(ctx context.Context, provider string) (string, error) {
			entry, ok := d.cfg.Agents[provider]
			if !ok {
				return "", fmt.Errorf("%s is not installed on this computer", provider)
			}
			resolved, _, err := d.resolveAgentEntryForLaunch(ctx, provider, entry)
			if err != nil {
				return "", err
			}
			return resolved.Path, nil
		}, "", ""),
		Logger: d.logger.With("component", "local-conversations"),
	})
	if err != nil {
		return err
	}
	d.conversations = svc
	d.localV2 = localhost.NewV2(svc, d.cfg.LocalAPIToken, d.cfg.ProfileDir)
	d.localV2.SetServerOrigin(d.cfg.ServerBaseURL)
	d.reportsTiming = timing.Open(filepath.Join(d.cfg.ProfileDir, "local-conversations", "diagnostics", reportsTimingFile), timing.Options{})
	d.localV2.SetTiming(timing.NewMarks(d.openDeviceTiming(), timing.SourceDaemon), d.reportsTiming)
	d.deviceLink = newDeviceLink(d.cfg.ProfileDir, d.localV2, d.cfg.LocalAPIToken, d.logger.With("component", "device-link"))
	return nil
}

// claudeLauncher starts one Claude adapter per conversation with the
// packaged Electron in Node mode, using the Claude Code executable the
// daemon resolves for cloud tasks.
func (d *Daemon) claudeLauncher(sidecar localhost.Sidecar) service.ClaudeLauncher {
	return func(ctx context.Context, conv sqlitedb.Conversation) (service.ClaudeSession, service.StartClaudeAdapter, error) {
		entry, ok := d.cfg.Agents["claude"]
		if !ok {
			return service.ClaudeSession{}, nil, errors.New("Claude Code is not installed on this computer")
		}
		resolved, _, err := d.resolveAgentEntryForLaunch(ctx, "claude", entry)
		if err != nil {
			return service.ClaudeSession{}, nil, err
		}
		fields := map[string]any{"executable": resolved.Path, "cwd": conv.Cwd, "env": service.ClaudeEnv(os.Environ())}
		logger := d.logger.With("component", "claude-adapter", "conversation_id", conv.ID)
		start := func(ctx context.Context, handler claude.Handler) (service.ClaudeAdapter, error) {
			p, err := claude.Start(ctx, claude.AdapterCommand(sidecar.Electron, sidecar.ClaudeEntry, os.Environ()), handler, logger)
			if err != nil {
				return nil, err
			}
			return p, nil
		}
		return service.ClaudeSession{Fields: fields}, start, nil
	}
}

// codexLauncher runs one app-server per Codex conversation with the Codex
// executable the daemon resolves for cloud tasks.
func (d *Daemon) codexLauncher() service.CodexLauncher {
	return func(ctx context.Context, conv sqlitedb.Conversation) (service.CodexSession, service.StartCodexServer, error) {
		entry, ok := d.cfg.Agents["codex"]
		if !ok {
			return service.CodexSession{}, nil, errors.New("Codex is not installed on this computer")
		}
		resolved, _, err := d.resolveAgentEntryForLaunch(ctx, "codex", entry)
		if err != nil {
			return service.CodexSession{}, nil, err
		}
		logger := d.logger.With("component", "codex-app-server", "conversation_id", conv.ID)
		start := func(ctx context.Context, handler codex.Handler, env []string) (service.CodexServer, error) {
			s, err := codex.Start(ctx, codex.Command{Path: resolved.Path, Dir: conv.Cwd, Env: append(os.Environ(), env...)}, handler, logger)
			if err != nil {
				return nil, err
			}
			return s, nil
		}
		return service.CodexSession{Cwd: conv.Cwd}, start, nil
	}
}

// spareConditions says whether a provider may keep a spare: only with its CLI
// resolved and someone signed in. The hash stands for the environment a parked
// process inherited, so no value of it is kept.
func (d *Daemon) spareConditions(ctx context.Context, provider string) service.SpareConditions {
	entry, ok := d.cfg.Agents[provider]
	user := localhost.DesktopUser(d.cfg.ProfileDir)
	if !ok || user == "" || d.client == nil || d.client.Token() == "" {
		return service.SpareConditions{}
	}
	resolved, version, err := d.resolveAgentEntryForLaunch(ctx, provider, entry)
	if err != nil {
		return service.SpareConditions{}
	}
	environ := slices.Sorted(slices.Values(os.Environ()))
	sum := sha256.Sum256([]byte(strings.Join(environ, "\x00")))
	identity := user + "\x00" + hex.EncodeToString(sum[:])
	if provider == "claude" {
		identity += "\x00" + claudeAccountStamp(claudeAccountFile(os.Getenv, os.UserHomeDir))
	}
	return service.SpareConditions{
		Ready: true, Executable: resolved.Path, Binary: resolved.Path + "\x00" + version,
		Identity: identity, ConfigFiles: providerConfigFiles(provider, os.Getenv, os.UserHomeDir),
	}
}

// providerConfigFiles are the user configuration a parked process has read; any
// change to them discards it. Claude's .claude.json is not among them: it also
// holds bookkeeping rewritten constantly, see claudeAccountStamp.
func providerConfigFiles(provider string, getenv func(string) string, home func() (string, error)) []string {
	userHome, err := home()
	if err != nil {
		userHome = ""
	}
	switch provider {
	case "codex":
		dir := getenv("CODEX_HOME")
		if dir == "" {
			dir = filepath.Join(userHome, ".codex")
		}
		return []string{filepath.Join(dir, "config.toml"), filepath.Join(dir, "auth.json")}
	case "claude":
		dir := getenv("CLAUDE_CONFIG_DIR")
		if dir == "" {
			dir = filepath.Join(userHome, ".claude")
		}
		return []string{filepath.Join(dir, "settings.json"), filepath.Join(dir, ".credentials.json")}
	}
	return nil
}

func claudeAccountFile(getenv func(string) string, home func() (string, error)) string {
	if dir := getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return filepath.Join(dir, ".claude.json")
	}
	userHome, err := home()
	if err != nil {
		userHome = ""
	}
	return filepath.Join(userHome, ".claude.json")
}

// claudeAccountStamp identifies the Claude login recorded in .claude.json, the only
// file that shows a new login where credentials live in the system keychain. It is
// a hash so the key is not kept, and empty for a file that is missing or unreadable,
// which reads as a change of account and costs one spare at most.
func claudeAccountStamp(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var file struct {
		OAuthAccount struct {
			AccountUUID      string `json:"accountUuid"`
			OrganizationUUID string `json:"organizationUuid"`
		} `json:"oauthAccount"`
		PrimaryAPIKey string `json:"primaryApiKey"`
	}
	if json.Unmarshal(data, &file) != nil {
		return ""
	}
	sum := sha256.Sum256([]byte(file.OAuthAccount.AccountUUID + "\x00" + file.OAuthAccount.OrganizationUUID + "\x00" + file.PrimaryAPIKey))
	return hex.EncodeToString(sum[:])
}

// codexSpareLauncher keeps an app-server ready outside any project: the thread
// gets its directory when a conversation claims the process.
func (d *Daemon) codexSpareLauncher() *service.CodexSpareLauncher {
	logger := d.logger.With("component", "codex-app-server", "spare", true)
	return &service.CodexSpareLauncher{
		Conditions: func(ctx context.Context) service.SpareConditions { return d.spareConditions(ctx, "codex") },
		Start: func(ctx context.Context, conditions service.SpareConditions, handler codex.Handler, env []string) (service.CodexServer, error) {
			dir := filepath.Join(d.cfg.ProfileDir, "local-conversations", "spare")
			if err := os.MkdirAll(dir, 0o700); err != nil {
				return nil, err
			}
			s, err := codex.Start(ctx, codex.Command{Path: conditions.Executable, Dir: dir, Env: append(os.Environ(), env...)}, handler, logger)
			if err != nil {
				return nil, err
			}
			return s, nil
		},
	}
}

// claudeSpareLauncher keeps an adapter with a parked Claude Code ready.
func (d *Daemon) claudeSpareLauncher(sidecar localhost.Sidecar) *service.ClaudeSpareLauncher {
	logger := d.logger.With("component", "claude-adapter", "spare", true)
	return &service.ClaudeSpareLauncher{
		Conditions: func(ctx context.Context) service.SpareConditions { return d.spareConditions(ctx, "claude") },
		Fields: func(conditions service.SpareConditions) map[string]any {
			return map[string]any{"executable": conditions.Executable, "env": service.ClaudeEnv(os.Environ()), "allowed_tools": []string{"mcp__" + contextmcp.Name + "__*"}}
		},
		Start: func(ctx context.Context, handler claude.Handler) (service.ClaudeAdapter, error) {
			p, err := claude.Start(ctx, claude.AdapterCommand(sidecar.Electron, sidecar.ClaudeEntry, os.Environ()), handler, logger)
			if err != nil {
				return nil, err
			}
			return p, nil
		},
	}
}

// contextCloud gives context tools official data with the profile's
// credentials, only for conversations of the server this daemon signs in
// to: the token is not valid for another API origin.
func (d *Daemon) contextCloud(scope store.Scope, dir string) contextmcp.Cloud {
	if normalizeOrigin(scope.APIOrigin) != normalizeOrigin(d.cfg.ServerBaseURL) || d.client == nil {
		return nil
	}
	return &contextmcp.CloudClient{APIURL: d.cfg.ServerBaseURL, WorkspaceID: scope.WorkspaceID, Token: d.client.Token, CLIPath: d.cfg.LocalHostCLIPath, Dir: dir}
}

func normalizeOrigin(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return ""
	}
	return strings.ToLower(u.Scheme + "://" + u.Host)
}
