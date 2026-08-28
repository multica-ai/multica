package execenv

import (
	"bytes"
	"encoding/json"
	"errors"
	"path/filepath"
)

func prepareOmpMcpConfig(workDir, provider string, raw json.RawMessage, manifest *sidecarManifest) error {
	if provider != "omp" {
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
