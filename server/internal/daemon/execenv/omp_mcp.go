package execenv

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

//go:embed pi_mcp_extension.js
var piMcpExtension []byte

func prepareOmpMcpConfig(workDir, provider string, raw json.RawMessage, manifest *sidecarManifest) error {
	if provider != "omp" && provider != "pi" {
		return nil
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil
	}
	if workDir == "" {
		return errors.New("managed mcp_config requires a working directory")
	}
	configDir := filepath.Join(workDir, "."+provider)
	if err := recordMkdirAll(configDir, 0o700, manifest); err != nil {
		return err
	}
	path := filepath.Join(configDir, "mcp.json")
	if err := recordWriteFile(path, raw, 0o600, manifest); err != nil {
		if errors.Is(err, errPathPreExists) {
			return errors.New("managed mcp_config would overwrite existing " + path)
		}
		return err
	}
	return nil
}

func preparePiMcpExtension(workDir string, raw json.RawMessage, manifest *sidecarManifest) (string, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return "", nil
	}
	if workDir == "" {
		return "", errors.New("managed mcp_config requires a working directory")
	}
	if !json.Valid(trimmed) {
		return "", fmt.Errorf("managed mcp_config is not valid JSON")
	}
	extensionDir := filepath.Join(workDir, ".pi", "extensions")
	if err := recordMkdirAll(extensionDir, 0o700, manifest); err != nil {
		return "", err
	}
	path := filepath.Join(extensionDir, "multica-mcp.js")
	if err := recordWriteFile(path, piMcpExtension, 0o700, manifest); err != nil {
		if errors.Is(err, errPathPreExists) {
			return "", errors.New("managed Pi MCP extension would overwrite existing " + path)
		}
		return "", err
	}
	if _, err := os.Stat(path); err != nil {
		return "", err
	}
	return path, nil
}
