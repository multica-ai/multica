package execenv

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const (
	// PlatformAgentRuntimeContextSchema is the sidecar contract shared with
	// platform-agent-cli's immutable Thread bootstrap.
	PlatformAgentRuntimeContextSchema = "platform-agent.runtime-context/v1"
	maxPlatformAgentCommands          = 256
	maxPlatformAgentContextBytes      = 1024 * 1024
)

// PlatformAgentContextForEnv is the non-secret Extension snapshot materialized
// for one platform-agent-cli task.
type PlatformAgentContextForEnv struct {
	SchemaVersion string                       `json:"schema_version"`
	Extension     PlatformAgentExtensionForEnv `json:"extension"`
	Agent         PlatformAgentIdentityForEnv  `json:"agent"`
	Commands      []PlatformAgentCommandForEnv `json:"commands"`
}

type PlatformAgentExtensionForEnv struct {
	Key       string `json:"key"`
	Version   string `json:"version"`
	ReleaseID string `json:"release_id"`
	Digest    string `json:"digest"`
}

type PlatformAgentIdentityForEnv struct {
	SourceKey string `json:"source_key"`
}

type PlatformAgentCommandForEnv struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Content     string          `json:"content"`
	Metadata    json.RawMessage `json:"metadata"`
}

// ValidatePlatformAgentContext applies the same identity and command checks
// that the CLI performs when it bootstraps a Thread from context.json.
func ValidatePlatformAgentContext(ctx *PlatformAgentContextForEnv) error {
	if ctx == nil {
		return errors.New("platform agent context is required")
	}
	if ctx.SchemaVersion != PlatformAgentRuntimeContextSchema {
		return fmt.Errorf("unsupported schema %q for platform agent context", ctx.SchemaVersion)
	}
	if strings.TrimSpace(ctx.Extension.Key) == "" ||
		strings.TrimSpace(ctx.Extension.Version) == "" ||
		strings.TrimSpace(ctx.Extension.ReleaseID) == "" ||
		strings.TrimSpace(ctx.Extension.Digest) == "" {
		return errors.New("platform agent extension identity is incomplete")
	}
	if strings.TrimSpace(ctx.Agent.SourceKey) == "" {
		return errors.New("platform agent source_key is required")
	}
	if len(ctx.Commands) > maxPlatformAgentCommands {
		return fmt.Errorf("platform agent commands exceed limit %d", maxPlatformAgentCommands)
	}
	seen := make(map[string]struct{}, len(ctx.Commands))
	for _, command := range ctx.Commands {
		if strings.TrimSpace(command.Name) == "" {
			return errors.New("platform agent command name is required")
		}
		if _, ok := seen[command.Name]; ok {
			return fmt.Errorf("duplicate command name %q", command.Name)
		}
		seen[command.Name] = struct{}{}
		if !containsOneJSONValue(command.Metadata) {
			return fmt.Errorf("platform agent command %q metadata must contain one JSON value", command.Name)
		}
	}
	data, err := json.MarshalIndent(ctx, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal platform agent context: %w", err)
	}
	if len(data) > maxPlatformAgentContextBytes {
		return fmt.Errorf("platform agent context exceeds %d bytes", maxPlatformAgentContextBytes)
	}
	return nil
}

func containsOneJSONValue(raw json.RawMessage) bool {
	if len(bytes.TrimSpace(raw)) == 0 {
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	var value json.RawMessage
	if err := decoder.Decode(&value); err != nil {
		return false
	}
	var extra any
	return decoder.Decode(&extra) == io.EOF
}

func writePlatformAgentContext(workDir string, ctx *PlatformAgentContextForEnv, manifest *sidecarManifest) error {
	if err := ValidatePlatformAgentContext(ctx); err != nil {
		return err
	}
	data, err := json.MarshalIndent(ctx, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal platform agent context: %w", err)
	}
	dir := filepath.Join(workDir, ".platform-agent")
	if err := recordMkdirAll(dir, 0o700, manifest); err != nil {
		return fmt.Errorf("create platform agent context directory: %w", err)
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return fmt.Errorf("stat platform agent context directory: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("platform agent context directory must be a real directory: %s", dir)
	}
	path := filepath.Join(dir, "context.json")
	if err := atomicWriteFileNoClobber(path, data, 0o600); err != nil {
		return fmt.Errorf("write platform agent context: %w", err)
	}
	if manifest != nil {
		manifest.Files = append(manifest.Files, path)
	}
	return nil
}

// atomicWriteFileNoClobber publishes a fully written same-directory temporary
// file with an atomic hard link. Link creation fails when target already
// exists, so no check-then-rename window can overwrite a user path.
func atomicWriteFileNoClobber(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	temp, err := os.CreateTemp(dir, ".context.json.tmp-*")
	if err != nil {
		return fmt.Errorf("create temporary sidecar: %w", err)
	}
	tempPath := temp.Name()
	defer func() {
		_ = temp.Close()
		_ = os.Remove(tempPath)
	}()
	if err := temp.Chmod(perm); err != nil {
		return fmt.Errorf("chmod temporary sidecar: %w", err)
	}
	if _, err := temp.Write(data); err != nil {
		return fmt.Errorf("write temporary sidecar: %w", err)
	}
	if err := temp.Sync(); err != nil {
		return fmt.Errorf("sync temporary sidecar: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close temporary sidecar before publish: %w", err)
	}
	if err := os.Link(tempPath, path); err != nil {
		if _, statErr := os.Lstat(path); statErr == nil {
			return fmt.Errorf("%w: %s", errPathPreExists, path)
		}
		return fmt.Errorf("publish sidecar: %w", err)
	}
	if err := os.Remove(tempPath); err != nil {
		// Do not report success with an untracked target when temporary-file
		// cleanup fails. Roll the target back so ownership stays fail closed.
		_ = os.Remove(path)
		return fmt.Errorf("remove temporary sidecar after publish: %w", err)
	}
	return nil
}
