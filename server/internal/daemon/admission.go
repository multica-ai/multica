package daemon

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
)

const defaultAdmissionOwner = "manual"

var admissionOwnerPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

type admissionPauseDiskState struct {
	Owners []string `json:"owners"`
}

func normalizeAdmissionOwner(owner string) (string, error) {
	if owner == "" {
		owner = defaultAdmissionOwner
	}
	if !admissionOwnerPattern.MatchString(owner) {
		return "", fmt.Errorf("invalid admission owner %q", owner)
	}
	return owner, nil
}

func (d *Daemon) admissionStatePath() string {
	if d.cfg.WorkspacesRoot == "" {
		return ""
	}
	return filepath.Join(d.cfg.WorkspacesRoot, ".multica-admission-pauses.json")
}

func cloneAdmissionOwners(source map[string]struct{}) map[string]struct{} {
	result := make(map[string]struct{}, len(source))
	for owner := range source {
		result[owner] = struct{}{}
	}
	return result
}

func sortedAdmissionOwners(source map[string]struct{}) []string {
	owners := make([]string, 0, len(source))
	for owner := range source {
		owners = append(owners, owner)
	}
	sort.Strings(owners)
	return owners
}

func (d *Daemon) persistAdmissionOwnersLocked(next map[string]struct{}) error {
	path := d.admissionStatePath()
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create admission state directory: %w", err)
	}
	payload, err := json.Marshal(admissionPauseDiskState{Owners: sortedAdmissionOwners(next)})
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".admission-pauses-*.tmp")
	if err != nil {
		return fmt.Errorf("create admission state temp file: %w", err)
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(append(payload, '\n')); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryName, path); err != nil {
		return fmt.Errorf("commit admission state: %w", err)
	}
	directory, err := os.Open(filepath.Dir(path))
	if err != nil {
		return fmt.Errorf("open admission state directory: %w", err)
	}
	defer directory.Close()
	if err := directory.Sync(); err != nil {
		return fmt.Errorf("sync admission state directory: %w", err)
	}
	return nil
}

func (d *Daemon) loadAdmissionOwners() error {
	path := d.admissionStatePath()
	if path == "" {
		return nil
	}
	payload, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var state admissionPauseDiskState
	if err := json.Unmarshal(payload, &state); err != nil {
		return err
	}
	owners := make(map[string]struct{}, len(state.Owners))
	for _, owner := range state.Owners {
		normalized, err := normalizeAdmissionOwner(owner)
		if err != nil {
			return err
		}
		owners[normalized] = struct{}{}
	}
	d.admissionPauseOwners = owners
	return nil
}
