// Package runtimeinstance defines the user-local Desktop/CLI discovery contract.
// Registration files locate an instance; commands and events use its Local API.
package runtimeinstance

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/multica-ai/multica/server/internal/verificationenv"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

const SchemaVersion = 1
const TokenFile = "local-api.token"
const InstanceID = "desktop"
const ProfileName = "desktop-runtime"
const OwnerEnv = "MULTICA_DESKTOP_OWNER_ID"

var validID = regexp.MustCompile(`^[a-f0-9]{32}$`)
var validCommit = regexp.MustCompile(`^[a-f0-9]{7,40}$`)

type Launch struct {
	SchemaVersion int    `json:"schemaVersion"`
	InstanceID    string `json:"instanceId"`
	OwnerID       string `json:"ownerId"`
	Kind          string `json:"kind"`
	AppPath       string `json:"appPath"`
	CLIPath       string `json:"cliPath"`
	CLIVersion    string `json:"cliVersion"`
	CLICommit     string `json:"cliCommit"`
	ElectronPath  string `json:"electronPath"`
	// ClaudeEntryPath is the per-conversation Claude adapter the daemon runs
	// with ElectronPath for local conversations.
	ClaudeEntryPath string `json:"claudeEntryPath"`
	Profile         string `json:"profile"`
	APIURL          string `json:"apiUrl"`
	ProtocolVersion int    `json:"protocolVersion"`
}

type Runtime struct {
	SchemaVersion          int    `json:"schemaVersion"`
	InstanceID             string `json:"instanceId"`
	OwnerID                string `json:"ownerId"`
	RunID                  string `json:"runId"`
	PID                    int    `json:"pid"`
	Port                   int    `json:"port"`
	Profile                string `json:"profile"`
	APIURL                 string `json:"apiUrl"`
	CLIVersion             string `json:"cliVersion"`
	CLICommit              string `json:"cliCommit"`
	APIVersion             int    `json:"apiVersion"`
	SidecarProtocolVersion int    `json:"sidecarProtocolVersion"`
	LocalConversations     bool   `json:"localConversations"`
	StartedAt              string `json:"startedAt"`
}

type Store struct{ home string }

func New(home string) Store { return Store{home: home} }
func Default() (Store, error) {
	home, err := os.UserHomeDir()
	return New(verificationenv.Home(home)), err
}
func ValidID(id string) bool  { return id == InstanceID }
func Profile(_ string) string { return ProfileName }
func IDFromProfile(profile string) string {
	if profile == ProfileName {
		return InstanceID
	}
	return ""
}
func (s Store) Root() string { return filepath.Join(s.home, ".multica") }
func (s Store) Dir(id string) (string, error) {
	if !ValidID(id) {
		return "", errors.New("invalid runtime instance ID")
	}
	return filepath.Join(s.Root(), ProfileName), nil
}
func (s Store) path(id, name string) (string, error) {
	dir, err := s.Dir(id)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, name), nil
}

func (l Launch) Validate() error {
	if l.SchemaVersion != SchemaVersion || !ValidID(l.InstanceID) || !validID.MatchString(l.OwnerID) || l.Profile != ProfileName || l.ProtocolVersion != 2 || (l.Kind != "development" && l.Kind != "packaged") {
		return errors.New("incompatible runtime launch registration")
	}
	if l.CLIVersion == "" || !validCommit.MatchString(l.CLICommit) {
		return errors.New("runtime CLI version cannot be verified")
	}
	u, err := url.Parse(l.APIURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return errors.New("invalid runtime API origin")
	}
	for _, path := range []string{l.AppPath, l.CLIPath, l.ElectronPath, l.ClaudeEntryPath} {
		if !filepath.IsAbs(path) || strings.ContainsRune(path, 0) {
			return errors.New("runtime paths must be absolute")
		}
	}
	return nil
}

func (r Runtime) Validate() error {
	if r.SchemaVersion != SchemaVersion || !ValidID(r.InstanceID) || !validID.MatchString(r.OwnerID) || !validID.MatchString(r.RunID) || r.Profile != ProfileName || r.PID < 1 || r.Port < 1 || r.Port > 65535 || r.APIVersion != 2 || r.SidecarProtocolVersion != 2 || r.CLIVersion == "" || !validCommit.MatchString(r.CLICommit) {
		return errors.New("incompatible runtime registration")
	}
	if _, err := time.Parse(time.RFC3339Nano, r.StartedAt); err != nil {
		return errors.New("invalid runtime start time")
	}
	u, err := url.Parse(r.APIURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" {
		return errors.New("invalid runtime API origin")
	}
	return nil
}

func (s Store) ReadLaunch(id string) (Launch, error) {
	var record Launch
	path, err := s.path(id, "launch.json")
	if err == nil {
		err = readPrivateJSON(path, &record)
	}
	if err == nil {
		err = record.Validate()
	}
	if err == nil && record.InstanceID != id {
		err = errors.New("runtime launch identity mismatch")
	}
	return record, err
}

func (s Store) ReadRuntime(id string) (Runtime, error) {
	var record Runtime
	path, err := s.path(id, "runtime.json")
	if err == nil {
		err = readPrivateJSON(path, &record)
	}
	if err == nil {
		err = record.Validate()
	}
	if err == nil && record.InstanceID != id {
		err = errors.New("runtime identity mismatch")
	}
	return record, err
}

func (s Store) PublishLaunch(l Launch) error {
	if err := l.Validate(); err != nil {
		return err
	}
	path, _ := s.path(l.InstanceID, "launch.json")
	return writePrivateJSON(path, l)
}

func (s Store) PublishRuntime(r Runtime) error {
	if err := r.Validate(); err != nil {
		return err
	}
	l, err := s.ReadLaunch(r.InstanceID)
	if err != nil {
		return err
	}
	if l.OwnerID != r.OwnerID || l.Profile != r.Profile || l.APIURL != r.APIURL || !l.MatchesVersion(r.CLIVersion, r.CLICommit) {
		return errors.New("runtime launch and daemon disagree")
	}
	path, _ := s.path(r.InstanceID, "runtime.json")
	return writePrivateJSON(path, r)
}

// The daemon holds the instance's profile lock until this cleanup completes.
func (s Store) RemoveRuntimeIfOwned(id, runID string, pid int) error {
	r, err := s.ReadRuntime(id)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if r.RunID != runID || r.PID != pid {
		return nil
	}
	path, _ := s.path(id, "runtime.json")
	return os.Remove(path)
}

func (s Store) IDs() ([]string, error) {
	_, err := s.ReadLaunch(InstanceID)
	if errors.Is(err, os.ErrNotExist) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	return []string{InstanceID}, nil
}

func NewRunID() (string, error) {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf[:]), nil
}

func SameFile(a, b string) bool {
	x, err := filepath.EvalSymlinks(a)
	if err != nil {
		return false
	}
	y, err := filepath.EvalSymlinks(b)
	return err == nil && x == y
}

func (l Launch) ValidateExecutable(exe string) error {
	if !SameFile(exe, l.CLIPath) {
		return errors.New("runtime instance belongs to another CLI; use its registered executable")
	}
	for _, path := range []string{l.CLIPath, l.ElectronPath, l.ClaudeEntryPath} {
		info, err := os.Stat(path)
		if err != nil {
			return fmt.Errorf("runtime component unavailable: %w", err)
		}
		if !info.Mode().IsRegular() {
			return errors.New("runtime component is not a regular file")
		}
	}
	return nil
}

func (l Launch) MatchesVersion(version, commit string) bool {
	return l.CLIVersion == version && validCommit.MatchString(commit) &&
		(strings.HasPrefix(l.CLICommit, commit) || strings.HasPrefix(commit, l.CLICommit))
}

func readPrivateJSON(path string, out any) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() > 64<<10 || (runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0) {
		return errors.New("runtime registration must be a private regular file")
	}
	dir, err := os.Lstat(filepath.Dir(path))
	if err != nil || !dir.IsDir() || (runtime.GOOS != "windows" && dir.Mode().Perm()&0o077 != 0) {
		return errors.New("runtime instance directory must be private")
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	decoder := json.NewDecoder(io.LimitReader(f, 64<<10))
	if err := decoder.Decode(out); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return errors.New("trailing runtime registration data")
	}
	return nil
}

func writePrivateJSON(path string, value any) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("invalid runtime instance directory")
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".registration-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if err := f.Chmod(0o600); err != nil {
		return err
	}
	if err := json.NewEncoder(f).Encode(value); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	for attempt := 0; ; attempt++ {
		err := os.Rename(f.Name(), path)
		if err == nil || runtime.GOOS != "windows" || attempt == 9 {
			return err
		}
		time.Sleep(50 * time.Millisecond)
	}
}
