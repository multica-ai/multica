package execenv

import (
	"bytes"
	"crypto/rand"
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
	if ctx.Commands == nil {
		return errors.New("platform agent commands must be an array")
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
	if err := ValidateNoDuplicateJSONKeys(raw); err != nil {
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

// ValidateNoDuplicateJSONKeys rejects ambiguous JSON objects at every nesting
// depth. encoding/json otherwise silently accepts the last value for a repeated
// key, which would let the daemon and CLI disagree about the signed runtime
// snapshot they are validating.
func ValidateNoDuplicateJSONKeys(raw []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var walkValue func() error
	walkValue = func() error {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		delim, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			seen := make(map[string]struct{})
			for decoder.More() {
				keyToken, err := decoder.Token()
				if err != nil {
					return err
				}
				key, ok := keyToken.(string)
				if !ok {
					return fmt.Errorf("JSON object key is not a string")
				}
				if _, exists := seen[key]; exists {
					return fmt.Errorf("duplicate object key %q", key)
				}
				seen[key] = struct{}{}
				if err := walkValue(); err != nil {
					return err
				}
			}
			_, err = decoder.Token()
			return err
		case '[':
			for decoder.More() {
				if err := walkValue(); err != nil {
					return err
				}
			}
			_, err = decoder.Token()
			return err
		default:
			return fmt.Errorf("unexpected JSON delimiter %q", delim)
		}
	}
	if err := walkValue(); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("JSON must contain one JSON value")
		}
		return err
	}
	return nil
}

func writePlatformAgentContext(workDir string, ctx *PlatformAgentContextForEnv, manifest *sidecarManifest) error {
	if err := ValidatePlatformAgentContext(ctx); err != nil {
		return err
	}
	if manifest == nil {
		return errors.New("platform agent context requires a sidecar manifest")
	}
	if err := manifest.bindRoot(workDir); err != nil {
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
	parentRel, err := pathRelativeToRoot(workDir, dir)
	if err != nil {
		return fmt.Errorf("resolve platform agent context directory: %w", err)
	}
	expectedParent, err := manifest.root.Lstat(parentRel)
	if err != nil {
		return fmt.Errorf("stat platform agent context directory: %w", err)
	}
	if !expectedParent.IsDir() || expectedParent.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("platform agent context directory must be a real directory: %s", dir)
	}
	parentRoot, err := manifest.root.OpenRoot(parentRel)
	if err != nil {
		return fmt.Errorf("open platform agent context directory: %w", err)
	}
	defer parentRoot.Close()
	if err := atomicWriteFileNoClobberAt(manifest.root, parentRoot, expectedParent, parentRel, "context.json", data, 0o600); err != nil {
		return fmt.Errorf("write platform agent context: %w", err)
	}
	manifest.Files = append(manifest.Files, filepath.Join(parentRel, "context.json"))
	return nil
}

// atomicWriteFileNoClobberAt publishes through fixed workdir and parent
// directory handles. A concurrent rename/symlink swap cannot redirect either
// the temporary file, the hard-link publication, or rollback outside workDir.
func atomicWriteFileNoClobberAt(workRoot, parentRoot *os.Root, expectedParent os.FileInfo, parentRel, name string, data []byte, perm os.FileMode) error {
	if err := validateFixedPlatformParent(workRoot, parentRoot, expectedParent, parentRel); err != nil {
		return err
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return fmt.Errorf("generate temporary sidecar name: %w", err)
	}
	tempName := fmt.Sprintf(".context.json.tmp-%x", random[:])
	temp, err := parentRoot.OpenFile(tempName, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		return fmt.Errorf("create temporary sidecar: %w", err)
	}
	defer func() {
		_ = temp.Close()
		_ = parentRoot.Remove(tempName)
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
	if err := parentRoot.Link(tempName, name); err != nil {
		if _, statErr := parentRoot.Lstat(name); statErr == nil {
			return fmt.Errorf("%w: %s", errPathPreExists, filepath.Join(parentRel, name))
		}
		return fmt.Errorf("publish sidecar: %w", err)
	}
	if err := validateFixedPlatformParent(workRoot, parentRoot, expectedParent, parentRel); err != nil {
		_ = parentRoot.Remove(name)
		return fmt.Errorf("platform agent context parent changed during publish: %w", err)
	}
	if err := parentRoot.Remove(tempName); err != nil {
		_ = parentRoot.Remove(name)
		return fmt.Errorf("remove temporary sidecar after publish: %w", err)
	}
	return nil
}

func validateFixedPlatformParent(workRoot, parentRoot *os.Root, expectedParent os.FileInfo, parentRel string) error {
	current, err := workRoot.Lstat(parentRel)
	if err != nil {
		return fmt.Errorf("stat platform context parent: %w", err)
	}
	if current.Mode()&os.ModeSymlink != 0 || !current.IsDir() || !os.SameFile(expectedParent, current) {
		return errors.New("platform context parent changed or is not a real directory")
	}
	opened, err := parentRoot.Stat(".")
	if err != nil {
		return fmt.Errorf("stat opened platform context parent: %w", err)
	}
	if !os.SameFile(expectedParent, opened) {
		return errors.New("opened platform context parent does not match workdir path")
	}
	return nil
}
