package taskauth

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

const (
	ManagedBy = "multica-daemon-task-authority"
	Version   = 1
	MaxBytes  = 16 * 1024
	FileName  = "task-authority.json"
	FixedPath = "/run/multica/task-authority.json"
)

type Authority struct {
	ManagedBy   string `json:"managed_by"`
	Version     int    `json:"version"`
	ServerURL   string `json:"server_url"`
	WorkspaceID string `json:"workspace_id"`
	Token       string `json:"token"`
	TaskID      string `json:"task_id"`
	AgentID     string `json:"agent_id"`
}

func Write(root string, authority Authority) (string, error) {
	validated, err := validate(authority)
	if err != nil {
		return "", err
	}
	root = filepath.Clean(root)
	if !filepath.IsAbs(root) {
		return "", fmt.Errorf("authority root must be absolute")
	}
	body, err := json.Marshal(validated)
	if err != nil {
		return "", fmt.Errorf("encode task authority: %w", err)
	}
	body = append(body, '\n')
	temp, err := os.CreateTemp(root, ".task-authority-*")
	if err != nil {
		return "", fmt.Errorf("create task authority: %w", err)
	}
	tempPath := temp.Name()
	keep := false
	defer func() {
		_ = temp.Close()
		if !keep {
			_ = os.Remove(tempPath)
		}
	}()
	if err := temp.Chmod(0o600); err != nil {
		return "", fmt.Errorf("secure task authority: %w", err)
	}
	if _, err := temp.Write(body); err != nil {
		return "", fmt.Errorf("write task authority: %w", err)
	}
	if err := temp.Sync(); err != nil {
		return "", fmt.Errorf("sync task authority: %w", err)
	}
	if err := temp.Close(); err != nil {
		return "", fmt.Errorf("close task authority: %w", err)
	}
	target := filepath.Join(root, FileName)
	if err := os.Rename(tempPath, target); err != nil {
		return "", fmt.Errorf("publish task authority: %w", err)
	}
	keep = true
	if _, err := Load(target); err != nil {
		_ = os.Remove(target)
		return "", fmt.Errorf("verify task authority: %w", err)
	}
	return target, nil
}

func Load(path string) (Authority, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return Authority{}, fmt.Errorf("inspect task authority: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		return Authority{}, fmt.Errorf("task authority must be a regular 0600 file")
	}
	file, err := os.Open(path)
	if err != nil {
		return Authority{}, fmt.Errorf("open task authority: %w", err)
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return Authority{}, fmt.Errorf("task authority changed while opening")
	}
	body, err := io.ReadAll(io.LimitReader(file, MaxBytes+1))
	if err != nil {
		return Authority{}, fmt.Errorf("read task authority: %w", err)
	}
	if len(body) > MaxBytes {
		return Authority{}, fmt.Errorf("task authority exceeds size limit")
	}
	if err := rejectDuplicateKeys(body); err != nil {
		return Authority{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	var authority Authority
	if err := decoder.Decode(&authority); err != nil {
		return Authority{}, fmt.Errorf("decode task authority: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return Authority{}, fmt.Errorf("task authority contains trailing JSON")
	}
	return validate(authority)
}

func rejectDuplicateKeys(body []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(body))
	token, err := decoder.Token()
	if err != nil {
		return fmt.Errorf("decode task authority: %w", err)
	}
	if delimiter, ok := token.(json.Delim); !ok || delimiter != '{' {
		return fmt.Errorf("task authority must be a JSON object")
	}
	seen := map[string]struct{}{}
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return fmt.Errorf("decode task authority: %w", err)
		}
		key, ok := token.(string)
		if !ok {
			return fmt.Errorf("task authority contains an invalid key")
		}
		if _, exists := seen[key]; exists {
			return fmt.Errorf("task authority contains duplicate field %q", key)
		}
		seen[key] = struct{}{}
		var discard json.RawMessage
		if err := decoder.Decode(&discard); err != nil {
			return fmt.Errorf("decode task authority: %w", err)
		}
	}
	if _, err := decoder.Token(); err != nil {
		return fmt.Errorf("decode task authority: %w", err)
	}
	return nil
}

func validate(authority Authority) (Authority, error) {
	if authority.ManagedBy != ManagedBy || authority.Version != Version {
		return Authority{}, fmt.Errorf("unsupported task authority identity")
	}
	normalized, err := normalizeServerURL(authority.ServerURL)
	if err != nil {
		return Authority{}, err
	}
	authority.ServerURL = normalized
	for name, value := range map[string]string{
		"workspace_id": authority.WorkspaceID,
		"task_id":      authority.TaskID,
		"agent_id":     authority.AgentID,
	} {
		parsed, err := uuid.Parse(value)
		if err != nil || parsed.String() != value {
			return Authority{}, fmt.Errorf("task authority %s must be a canonical UUID", name)
		}
	}
	if !strings.HasPrefix(authority.Token, "mat_") || strings.TrimSpace(authority.Token) != authority.Token || authority.Token == "mat_" {
		return Authority{}, fmt.Errorf("task authority token is invalid")
	}
	return authority, nil
}

func normalizeServerURL(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", fmt.Errorf("task authority server_url must be an absolute HTTP URL")
	}
	if parsed.User != nil || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") || parsed.RawQuery != "" {
		return "", fmt.Errorf("task authority server_url contains unsupported components")
	}
	parsed.Path = ""
	return strings.TrimSuffix(parsed.String(), "/"), nil
}
