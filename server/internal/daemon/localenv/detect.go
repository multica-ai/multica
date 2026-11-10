package localenv

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Listener is a process listening on a TCP port.
type Listener struct {
	PID     int
	Port    int
	Command string
	Cwd     string
}

// Detector finds servers running inside task directories that nobody
// declared, so a repository with no environments file still shows what its
// agent started.
type Detector struct {
	// ignore holds command names that are never an environment: the agent
	// CLIs themselves and the multica binary.
	ignore map[string]bool
	list   func(ctx context.Context) ([]Listener, error)
	now    func() time.Time

	mu   sync.Mutex
	seen map[string]*sighting
}

type sighting struct {
	first time.Time
	scans int
}

func NewDetector(ignoreCommands []string) *Detector {
	ignore := map[string]bool{"multica": true}
	for _, c := range ignoreCommands {
		if c = strings.ToLower(strings.TrimSpace(c)); c != "" {
			ignore[c] = true
		}
	}
	return &Detector{ignore: ignore, list: listListeners, now: time.Now, seen: make(map[string]*sighting)}
}

// Detect lists the servers whose working directory belongs to a task.
// resolve maps a working directory to its task; owned reports process groups
// the manager already supervises. A server is reported from its second
// sighting on, so test servers that live for a moment never show up.
func (d *Detector) Detect(ctx context.Context, resolve func(cwd string) (Task, bool), owned func(pgid int) bool) ([]Environment, error) {
	listeners, err := d.list(ctx)
	if err != nil {
		return nil, err
	}
	now := d.now()
	found := make(map[string]Environment)
	for _, l := range listeners {
		if l.Cwd == "" || d.ignore[strings.ToLower(l.Command)] {
			continue
		}
		task, ok := resolve(l.Cwd)
		if !ok {
			continue
		}
		if pgid := processGroup(l.PID); pgid > 0 && owned(pgid) {
			continue
		}
		key := detectedKey(task.RootDir, l.Port)
		if _, dup := found[key]; dup {
			continue
		}
		name := l.Command
		if r := []rune(name); len(r) > 64 {
			name = string(r[:64])
		}
		found[key] = Environment{
			Key: key, TaskID: task.TaskID, IssueID: task.IssueID, Source: SourceDetected,
			Name: name, URL: fmt.Sprintf("http://localhost:%d", l.Port), Port: l.Port, State: StateRunning,
			Dir: task.RootDir,
		}
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]Environment, 0, len(found))
	for key, env := range found {
		s := d.seen[key]
		if s == nil {
			s = &sighting{first: now}
			d.seen[key] = s
		}
		s.scans++
		if s.scans >= 2 {
			env.StartedAt = s.first
			out = append(out, env)
		}
	}
	for key := range d.seen {
		if _, ok := found[key]; !ok {
			delete(d.seen, key)
		}
	}
	sortEnvironments(out)
	return out, nil
}

// detectedKey is stable across restarts of the same server, and carries no
// local path to the server.
func detectedKey(root string, port int) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%d", root, port)))
	return "detected:" + hex.EncodeToString(sum[:8])
}

// parseLsofListeners reads `lsof -nP -iTCP -sTCP:LISTEN -Fpcn` output.
func parseLsofListeners(out string) []Listener {
	var listeners []Listener
	pid, command := 0, ""
	seen := make(map[[2]int]bool)
	scanner := bufio.NewScanner(strings.NewReader(out))
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		switch line[0] {
		case 'p':
			pid, _ = strconv.Atoi(line[1:])
			command = ""
		case 'c':
			command = line[1:]
		case 'n':
			port := portFromAddress(line[1:])
			if pid > 0 && port > 0 && !seen[[2]int{pid, port}] {
				seen[[2]int{pid, port}] = true
				listeners = append(listeners, Listener{PID: pid, Port: port, Command: command})
			}
		}
	}
	return listeners
}

// parseLsofCwd reads `lsof -a -d cwd -Fpn -p ...` output into pid → cwd.
func parseLsofCwd(out string) map[int]string {
	cwds := make(map[int]string)
	pid := 0
	scanner := bufio.NewScanner(strings.NewReader(out))
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		switch line[0] {
		case 'p':
			pid, _ = strconv.Atoi(line[1:])
		case 'n':
			if pid > 0 {
				cwds[pid] = line[1:]
			}
		}
	}
	return cwds
}

func portFromAddress(addr string) int {
	addr = strings.TrimSpace(addr)
	if i := strings.Index(addr, "->"); i >= 0 {
		addr = addr[:i]
	}
	i := strings.LastIndex(addr, ":")
	if i < 0 {
		return 0
	}
	port, err := strconv.Atoi(addr[i+1:])
	if err != nil || port <= 0 || port > 65535 {
		return 0
	}
	return port
}

// parseProcNetTCP reads /proc/net/tcp{,6} into socket inode → listening port.
func parseProcNetTCP(out string, into map[string]int) {
	scanner := bufio.NewScanner(strings.NewReader(out))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		// sl local_address rem_address st tx:rx tr:when retrnsmt uid timeout inode
		if len(fields) < 10 || fields[3] != "0A" {
			continue
		}
		local := fields[1]
		i := strings.LastIndex(local, ":")
		if i < 0 {
			continue
		}
		port, err := strconv.ParseInt(local[i+1:], 16, 32)
		if err != nil || port <= 0 {
			continue
		}
		if fields[9] != "0" {
			into[fields[9]] = int(port)
		}
	}
}
