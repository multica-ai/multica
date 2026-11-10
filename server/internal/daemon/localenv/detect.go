package localenv

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
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
	ignore  map[string]bool
	list    func(ctx context.Context) ([]Listener, error)
	inspect func(ctx context.Context, pids []int) map[int]Process
	isCDP   func(ctx context.Context, port int) bool
	now     func() time.Time

	mu   sync.Mutex
	seen map[string]*sighting
	// procs and cdp remember what a process is and which of its ports speak
	// DevTools, so each is looked up once rather than every scan.
	procs map[int]Process
	cdp   map[[2]int]bool
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
	return &Detector{
		ignore: ignore, list: listListeners, inspect: inspectProcesses, isCDP: probeCDP, now: time.Now,
		seen: make(map[string]*sighting), procs: make(map[int]Process), cdp: make(map[[2]int]bool),
	}
}

// Detect lists the servers whose working directory belongs to a task.
// resolve maps a working directory to its task; owned reports process groups
// the manager already supervises. A server is reported from its second
// sighting on, so test servers that live for a moment never show up.
//
// A desktop app is reported once, as an app with no URL, however many ports
// it listens on. A DevTools port is never reported as a URL: opening it shows
// a debugger page, not the app.
func (d *Detector) Detect(ctx context.Context, resolve func(cwd string) (Task, bool), owned func(pgid int) bool) ([]Environment, error) {
	listeners, err := d.list(ctx)
	if err != nil {
		return nil, err
	}
	type candidate struct {
		Listener
		task Task
	}
	var candidates []candidate
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
		candidates = append(candidates, candidate{l, task})
	}
	pids := make([]int, 0, len(candidates))
	for _, c := range candidates {
		pids = append(pids, c.PID)
	}
	procs := d.processes(ctx, pids)

	now := d.now()
	found := make(map[string]Environment)
	debugPorts := make(map[string][]int)
	for _, c := range candidates {
		proc := procs[c.PID]
		debug := proc.debugs(c.Port) || (proc.App != "" && d.speaksCDP(ctx, c.PID, c.Port))
		if proc.App != "" {
			key := detectedAppKey(c.task.RootDir, proc)
			if _, dup := found[key]; !dup {
				found[key] = Environment{
					Key: key, TaskID: c.task.TaskID, IssueID: c.task.IssueID, Source: SourceDetected, Kind: KindApp,
					Name: truncateName(proc.App), State: StateRunning, Dir: c.task.RootDir,
					pid: c.PID, bundle: proc.Bundle,
				}
			}
			if debug {
				debugPorts[key] = append(debugPorts[key], c.Port)
			}
		}
		if debug {
			continue
		}
		key := detectedKey(c.task.RootDir, c.Port)
		if _, dup := found[key]; dup {
			continue
		}
		found[key] = Environment{
			Key: key, TaskID: c.task.TaskID, IssueID: c.task.IssueID, Source: SourceDetected, Kind: KindServer,
			Name: truncateName(c.Command), URL: fmt.Sprintf("http://localhost:%d", c.Port), Port: c.Port, State: StateRunning,
			Dir: c.task.RootDir,
		}
	}
	for key, ports := range debugPorts {
		sort.Ints(ports)
		hosts := make([]string, len(ports))
		for i, port := range ports {
			hosts[i] = fmt.Sprintf("localhost:%d", port)
		}
		env := found[key]
		env.Detail = "Remote debugging on " + strings.Join(hosts, ", ")
		found[key] = env
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

// processes returns what each pid is, asking only about pids not seen in an
// earlier scan, and forgets pids that stopped listening.
func (d *Detector) processes(ctx context.Context, pids []int) map[int]Process {
	d.mu.Lock()
	var unknown []int
	live := make(map[int]bool, len(pids))
	for _, pid := range pids {
		if _, ok := d.procs[pid]; !ok && !live[pid] {
			unknown = append(unknown, pid)
		}
		live[pid] = true
	}
	d.mu.Unlock()
	var learned map[int]Process
	if len(unknown) > 0 {
		learned = d.inspect(ctx, unknown)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	for pid, p := range learned {
		d.procs[pid] = p
	}
	for pid := range d.procs {
		if !live[pid] {
			delete(d.procs, pid)
		}
	}
	for k := range d.cdp {
		if !live[k[0]] {
			delete(d.cdp, k)
		}
	}
	out := make(map[int]Process, len(pids))
	for _, pid := range pids {
		out[pid] = d.procs[pid]
	}
	return out
}

func (d *Detector) speaksCDP(ctx context.Context, pid, port int) bool {
	k := [2]int{pid, port}
	d.mu.Lock()
	known, ok := d.cdp[k]
	d.mu.Unlock()
	if ok {
		return known
	}
	known = d.isCDP(ctx, port)
	d.mu.Lock()
	d.cdp[k] = known
	d.mu.Unlock()
	return known
}

func truncateName(name string) string {
	if r := []rune(name); len(r) > 64 {
		return string(r[:64])
	}
	return name
}

// detectedKey is stable across restarts of the same server, and carries no
// local path to the server.
func detectedKey(root string, port int) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%d", root, port)))
	return "detected:" + hex.EncodeToString(sum[:8])
}

// detectedAppKey is stable across restarts of the same app, whatever ports
// it listens on this time.
func detectedAppKey(root string, p Process) string {
	id := p.Bundle
	if id == "" {
		id = p.App
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s\x00app\x00%s", root, id)))
	return "detected:" + hex.EncodeToString(sum[:8])
}

// parsePsArgs reads `ps -o pid=,args=` output into pid → process.
func parsePsArgs(out string) map[int]Process {
	procs := make(map[int]Process)
	scanner := bufio.NewScanner(strings.NewReader(out))
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		pidField, args, _ := strings.Cut(strings.TrimSpace(scanner.Text()), " ")
		pid, err := strconv.Atoi(pidField)
		if err != nil || pid <= 0 {
			continue
		}
		procs[pid] = Process{Args: strings.TrimSpace(args)}
	}
	return procs
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
