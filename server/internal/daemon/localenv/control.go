package localenv

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// The sidebar controls an environment by the id in its key, possibly after
// the manager has forgotten it: the record stays on disk next to its log.

var idRE = regexp.MustCompile(`^[0-9a-f]{16}$`)

const (
	excerptLines    = 2
	excerptLineMax  = 240
	excerptReadSize = 16 * 1024
)

// IDFromKey returns the record id in a config environment's key.
func IDFromKey(key string) (string, bool) {
	id, ok := strings.CutPrefix(key, "config:")
	return id, ok && idRE.MatchString(id)
}

// lookup finds a record in memory, else on disk. The entry is nil for a
// record the manager no longer keeps.
func (m *Manager) lookup(id string) (record, *entry, error) {
	if !idRE.MatchString(id) {
		return record{}, nil, ErrUnknown
	}
	m.mu.Lock()
	e := m.envs[id]
	if e != nil {
		rec := e.rec
		m.mu.Unlock()
		return rec, e, nil
	}
	m.mu.Unlock()
	data, err := os.ReadFile(filepath.Join(m.dir, id, stateFile))
	if err != nil {
		return record{}, nil, ErrUnknown
	}
	var rec record
	if err := json.Unmarshal(data, &rec); err != nil || rec.ID != id {
		return record{}, nil, ErrUnknown
	}
	return rec, nil, nil
}

// RecordOwner reports the issue and workspace an environment belongs to.
func (m *Manager) RecordOwner(id string) (issueID, workspaceID string, err error) {
	rec, _, err := m.lookup(id)
	return rec.IssueID, rec.WorkspaceID, err
}

// StopID stops a live environment by id. Stopping one that already ended is
// not an error.
func (m *Manager) StopID(id string) (Environment, error) {
	rec, e, err := m.lookup(id)
	if err != nil {
		return Environment{}, err
	}
	if e == nil {
		return Environment{Key: "config:" + rec.ID, Name: rec.Name, State: rec.State}, nil
	}
	m.stop(e, "")
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.viewLocked(e), nil
}

// Restart stops the environment if it is still live and starts it again from
// the same repository, with the same name and PATH. resolve maps the
// repository to the task that now owns its directory; the restarted
// environment is reported under that task.
func (m *Manager) Restart(id string, resolve func(dir string) (Task, bool)) (Environment, error) {
	rec, e, err := m.lookup(id)
	if err != nil {
		return Environment{}, err
	}
	if e != nil {
		m.stop(e, "")
	}
	if _, err := os.Stat(filepath.Join(rec.RepoRoot, filepath.FromSlash(ConfigPath))); err != nil {
		return Environment{}, ErrGone
	}
	task, ok := resolve(rec.RepoRoot)
	if !ok {
		task = Task{TaskID: rec.TaskID, IssueID: rec.IssueID, WorkspaceID: rec.WorkspaceID, RootDir: rec.RepoRoot}
	}
	if task.IssueID != rec.IssueID || task.WorkspaceID != rec.WorkspaceID {
		return Environment{}, errors.New("the environment's directory now belongs to another issue")
	}
	return m.Start(task, rec.RepoRoot, rec.Name, rec.Path)
}

// LastPath returns the PATH the most recent environment with this name on
// the issue was started with, so a start from the sidebar finds the same
// tools as one an agent ran.
func (m *Manager) LastPath(issueID, workspaceID, name string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var best *record
	for _, e := range m.envs {
		rec := e.rec
		if rec.IssueID != issueID || rec.WorkspaceID != workspaceID || rec.Path == "" {
			continue
		}
		if best == nil || (rec.Name == name && best.Name != name) ||
			((rec.Name == name) == (best.Name == name) && rec.StartedAt.After(best.StartedAt)) {
			best = &rec
		}
	}
	if best == nil {
		return ""
	}
	return best.Path
}

// LogTail returns the end of an environment's output: at most maxLines lines
// and maxBytes bytes, starting on a line boundary.
func (m *Manager) LogTail(id string, maxLines, maxBytes int) (string, error) {
	if _, _, err := m.lookup(id); err != nil {
		return "", err
	}
	data, err := readTail(filepath.Join(m.dir, id, outputFile), int64(maxBytes))
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read environment log: %w", err)
	}
	return string(lastLines(data, maxLines)), nil
}

func readTail(path string, max int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	offset := int64(0)
	if info.Size() > max {
		offset = info.Size() - max
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return nil, err
	}
	data, err := io.ReadAll(io.LimitReader(f, max))
	if err != nil {
		return nil, err
	}
	if offset > 0 {
		// Drop the partial first line.
		if i := bytes.IndexByte(data, '\n'); i >= 0 {
			data = data[i+1:]
		}
	}
	return data, nil
}

func lastLines(data []byte, n int) []byte {
	data = bytes.TrimRight(data, "\n")
	count := 0
	for i := len(data) - 1; i >= 0; i-- {
		if data[i] == '\n' {
			count++
			if count == n {
				return data[i+1:]
			}
		}
	}
	return data
}

// logExcerpt returns the last non-empty lines of a log, each shortened.
func logExcerpt(path string) string {
	data, err := readTail(path, excerptReadSize)
	if err != nil {
		return ""
	}
	var lines []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(stripANSI(line))
		if line == "" {
			continue
		}
		if r := []rune(line); len(r) > excerptLineMax {
			line = string(r[:excerptLineMax]) + "…"
		}
		lines = append(lines, line)
	}
	if len(lines) > excerptLines {
		lines = lines[len(lines)-excerptLines:]
	}
	return strings.Join(lines, "\n")
}

var ansiRE = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)

func stripANSI(s string) string { return ansiRE.ReplaceAllString(s, "") }

// Declared is one environment a repository declares, as the sidebar offers it.
type Declared struct {
	Repo  string `json:"repo"`
	Name  string `json:"name"`
	Label string `json:"label"`
	Start string `json:"start"`
	// RepoRoot is local; it is never reported.
	RepoRoot string `json:"-"`
}

// DiscoverDeclared finds the environments declared by the repositories in a
// task's working directory: the directory itself and up to two levels below
// it, where checkouts live.
func DiscoverDeclared(workDir string) []Declared {
	var out []Declared
	seen := make(map[string]bool)
	visit := func(dir string) {
		data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(ConfigPath)))
		if err != nil {
			return
		}
		cfg, err := ParseConfig(data)
		if err != nil {
			return
		}
		repo := filepath.Base(dir)
		for _, name := range cfg.Names() {
			if seen[repo+"\x00"+name] {
				continue
			}
			seen[repo+"\x00"+name] = true
			def := cfg.Environments[name]
			out = append(out, Declared{Repo: repo, Name: name, Label: def.Label, Start: def.Start, RepoRoot: dir})
		}
	}
	visit(workDir)
	for _, child := range subdirs(workDir) {
		visit(child)
		for _, grandchild := range subdirs(child) {
			visit(grandchild)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Repo != out[j].Repo {
			return out[i].Repo < out[j].Repo
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func subdirs(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		name := e.Name()
		if !e.IsDir() || strings.HasPrefix(name, ".") || name == "node_modules" {
			continue
		}
		out = append(out, filepath.Join(dir, name))
		if len(out) == 50 {
			break
		}
	}
	return out
}
