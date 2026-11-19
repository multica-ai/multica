package execenv

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/pkg/agent"
	"github.com/pelletier/go-toml/v2"
)

type codexForkReceipt struct {
	RequestID string `json:"request_id"`
	ChildID   string `json:"child_id"`
}

// publishCodexFork stages the child and its archived ancestors in the same
// filesystem as the conversation store. The callback must finish writing and
// close the provider process before returning the child ID.
func publishCodexFork(storeDir, requestID string, stage func(string) (string, error)) (string, error) {
	if storeDir == "" || requestID == "" {
		return "", errors.New("Codex fork requires a destination store and request ID")
	}
	if err := os.MkdirAll(storeDir, 0o700); err != nil {
		return "", fmt.Errorf("create Codex fork destination: %w", err)
	}
	lock, err := openLockFile(filepath.Join(storeDir, ".fork.lock"))
	if err != nil {
		return "", fmt.Errorf("open Codex fork lock: %w", err)
	}
	defer lock.Close()
	locked, err := lockFileExclusiveNonBlocking(lock)
	if err != nil {
		return "", fmt.Errorf("acquire Codex fork lock: %w", err)
	}
	if !locked {
		return "", errors.New("another Codex fork is in progress for this chat")
	}

	destination := filepath.Join(storeDir, "fork")
	if childID, err := LookupPublishedCodexFork(storeDir, requestID); err != nil || childID != "" {
		return childID, err
	}

	staging, err := os.MkdirTemp(storeDir, ".fork-staging-")
	if err != nil {
		return "", fmt.Errorf("create Codex fork staging directory: %w", err)
	}
	defer os.RemoveAll(staging)
	for _, name := range []string{"sessions", "archived_sessions"} {
		if err := os.Mkdir(filepath.Join(staging, name), 0o700); err != nil {
			return "", fmt.Errorf("create Codex fork staging subdirectory: %w", err)
		}
	}
	childID, err := stage(staging)
	if err != nil {
		return "", fmt.Errorf("stage Codex fork: %w", err)
	}
	if !validPublishedCodexFork(staging, childID) {
		return "", errors.New("staged Codex fork history is unavailable")
	}
	publication := filepath.Join(staging, "publication")
	if err := os.Mkdir(publication, 0o700); err != nil {
		return "", fmt.Errorf("create Codex fork publication directory: %w", err)
	}
	for _, name := range []string{"sessions", "archived_sessions"} {
		if err := os.Rename(filepath.Join(staging, name), filepath.Join(publication, name)); err != nil {
			return "", fmt.Errorf("stage Codex fork publication %s: %w", name, err)
		}
	}
	data, err := json.Marshal(codexForkReceipt{RequestID: requestID, ChildID: childID})
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(publication, "receipt.json"), data, 0o600); err != nil {
		return "", fmt.Errorf("write Codex fork receipt: %w", err)
	}
	if err := os.Rename(publication, destination); err != nil {
		return "", fmt.Errorf("publish Codex fork: %w", err)
	}
	return childID, nil
}

// LookupPublishedCodexFork returns the child of an already published request.
// Callers hold the destination conversation writer lock.
func LookupPublishedCodexFork(storeDir, requestID string) (string, error) {
	destination := filepath.Join(storeDir, "fork")
	receiptPath := filepath.Join(destination, "receipt.json")
	if _, err := os.Lstat(destination); !os.IsNotExist(err) {
		if err != nil {
			return "", err
		}
		data, err := os.ReadFile(receiptPath)
		if err != nil {
			return "", fmt.Errorf("read published Codex fork receipt: %w", err)
		}
		var receipt codexForkReceipt
		if err := json.Unmarshal(data, &receipt); err != nil {
			return "", fmt.Errorf("decode published Codex fork receipt: %w", err)
		}
		if receipt.RequestID != requestID {
			return "", errors.New("a different Codex fork already owns the destination")
		}
		if !validPublishedCodexFork(destination, receipt.ChildID) {
			return "", errors.New("published Codex fork history is unavailable")
		}
		return receipt.ChildID, nil
	}
	return "", nil
}

func validPublishedCodexFork(dir, childID string) bool {
	if err := uuid.Validate(childID); err != nil {
		return false
	}
	for _, name := range []string{"sessions", "archived_sessions"} {
		info, err := os.Lstat(filepath.Join(dir, name))
		if err != nil || !info.IsDir() {
			return false
		}
	}
	return CodexResumeRolloutPresent(dir, childID)
}

// ForkCodexChat publishes a native fork from daemon-resolved conversation paths.
// sourceArchiveDir is set only for a source fork whose archive is owned by that
// conversation. The caller must pin both stores and resolve source access.
func ForkCodexChat(ctx context.Context, cfg agent.Config, cwd, sourceSessionsDir, sourceArchiveDir, sourceConfigPath, sourceID, destinationStore, requestID string) (string, error) {
	return publishCodexFork(destinationStore, requestID, func(staging string) (string, error) {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		stagedSource, err := stageCodexForkSource(sourceSessionsDir, staging, sourceID)
		if err != nil {
			return "", err
		}
		archive := filepath.Join(staging, "archived_sessions")
		if sourceArchiveDir != "" {
			if err := copyCodexForkArchive(sourceArchiveDir, archive); err != nil {
				return "", err
			}
		}

		cfg.Env = maps.Clone(cfg.Env)
		if cfg.Env == nil {
			cfg.Env = make(map[string]string, 1)
		}
		cfg.Env["CODEX_HOME"] = staging
		stagingConfig := filepath.Join(staging, "config.toml")
		if sourceConfigPath != "" {
			if err := writeCodexForkProviders(sourceConfigPath, stagingConfig); err != nil {
				return "", err
			}
		}
		childID, err := agent.ForkCodexThread(ctx, cfg, cwd, sourceID)
		if err != nil {
			return "", err
		}
		if err := os.Remove(stagingConfig); err != nil && !os.IsNotExist(err) {
			return "", fmt.Errorf("remove temporary Codex provider config: %w", err)
		}
		if err := os.Rename(stagedSource, filepath.Join(archive, filepath.Base(stagedSource))); err != nil {
			return "", fmt.Errorf("archive staged Codex source: %w", err)
		}
		return childID, nil
	})
}

// Copy only provider definitions from the daemon-resolved runtime config.
func writeCodexForkProviders(source, destination string) error {
	data, err := os.ReadFile(source)
	if err != nil {
		return fmt.Errorf("read Codex fork provider config: %w", err)
	}
	var providers struct {
		ModelProviders map[string]any `toml:"model_providers,omitempty"`
	}
	if err := toml.Unmarshal(data, &providers); err != nil {
		return errors.New("decode Codex fork provider config")
	}
	encoded, err := toml.Marshal(providers)
	if err != nil {
		return errors.New("encode Codex fork provider config")
	}
	return os.WriteFile(destination, encoded, 0o600)
}

func stageCodexForkSource(sourceSessionsDir, staging, sourceID string) (string, error) {
	if err := uuid.Validate(sourceID); err != nil || sourceSessionsDir == "" {
		return "", errors.New("Codex fork source history is unavailable")
	}
	matches := findCodexRollouts(sourceSessionsDir, sourceID)
	if len(matches) != 1 {
		return "", errors.New("Codex fork requires one source rollout")
	}
	relative, err := filepath.Rel(sourceSessionsDir, matches[0])
	if err != nil {
		return "", err
	}
	dest := filepath.Join(staging, "sessions", relative)
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		return "", err
	}
	root, err := os.OpenRoot(sourceSessionsDir)
	if err != nil {
		return "", err
	}
	defer root.Close()
	return dest, copyCodexForkRollout(root, relative, dest)
}

func copyCodexForkArchive(sourceDir, dest string) error {
	root, err := os.OpenRoot(sourceDir)
	if err != nil {
		return err
	}
	defer root.Close()
	dir, err := root.Open(".")
	if err != nil {
		return err
	}
	defer dir.Close()
	entries, err := dir.ReadDir(-1)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, "rollout-") || (!strings.HasSuffix(name, ".jsonl") && !strings.HasSuffix(name, ".jsonl.zst")) {
			continue
		}
		if err := copyCodexForkRollout(root, name, filepath.Join(dest, name)); err != nil {
			return err
		}
	}
	return nil
}

func copyCodexForkRollout(root *os.Root, name, dest string) error {
	info, err := root.Lstat(name)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("Codex fork rollout is not a regular file")
	}
	source, err := root.Open(name)
	if err != nil {
		return err
	}
	defer source.Close()
	target, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(target, source)
	return errors.Join(copyErr, target.Close())
}

// CodexForkSource describes the history already owned by the source chat.
type CodexForkSource struct {
	SessionsDir string
	ArchiveDir  string
	ConfigPath  string
}

// ResolveCodexForkSource takes an optional environment root whose workspace,
// agent and chat provenance the daemon has already verified.
func ResolveCodexForkSource(profile string, task TaskContextForEnv, ownedEnvRoot, sessionID string) (CodexForkSource, error) {
	if uuid.Validate(sessionID) != nil || task.ChatSessionID == "" || task.AgentID == "" || task.IssueID != "" {
		return CodexForkSource{}, errors.New("Codex fork source chat and session are required")
	}
	sharedHome := resolveSharedCodexHome()
	store := CodexSessionStorePath(profile, task)
	source := CodexForkSource{SessionsDir: store}
	config := filepath.Join(sharedHome, "config.toml")
	if task.RequireNativeResume {
		fork := filepath.Join(store, "fork")
		source.SessionsDir = filepath.Join(fork, "sessions")
		source.ArchiveDir = filepath.Join(fork, "archived_sessions")
		if !validPublishedCodexFork(fork, sessionID) {
			return CodexForkSource{}, errors.New("published Codex fork history is unavailable")
		}
	} else if !hasScopedCodexForkRollout(store, sessionID) {
		if ownedEnvRoot == "" {
			return CodexForkSource{}, errors.New("Codex source chat history is unavailable")
		}
		home := filepath.Join(ownedEnvRoot, codexHomeDirName)
		if info, err := os.Lstat(home); err != nil || !info.IsDir() {
			return CodexForkSource{}, errors.New("Codex source chat home is unavailable")
		}
		source.SessionsDir = filepath.Join(home, "sessions")
		if !hasScopedCodexForkRollout(source.SessionsDir, sessionID) {
			return CodexForkSource{}, errors.New("Codex source chat history is unavailable")
		}
		config = filepath.Join(home, "config.toml")
	}
	if _, err := os.Stat(config); err == nil {
		source.ConfigPath = config
	} else if !os.IsNotExist(err) {
		return CodexForkSource{}, fmt.Errorf("inspect Codex fork provider config: %w", err)
	}
	return source, nil
}

func hasScopedCodexForkRollout(directory, sessionID string) bool {
	info, err := os.Lstat(directory)
	return err == nil && info.IsDir() && len(findCodexRollouts(directory, sessionID)) == 1
}
