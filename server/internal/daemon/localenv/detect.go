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
	// DevTools, so a settled answer is looked up once rather than every scan.
	// An answer that can still change -- a process not yet showing as an app,
	// a port not yet answering as DevTools -- is asked again, per the recheck
	// kept for it.
	procs       map[int]Process
	cdp         map[[2]int]bool
	procChecks  map[int]*recheck
	probeChecks map[[2]int]*recheck
}

// recheck paces repeated lookups of an answer that may still change: every
// scan for the first few, when a starting app settles, then every tenth, so a
// process that never becomes an app costs next to nothing.
type recheck struct{ tries, wait int }

func (r *recheck) due() bool {
	if r.wait > 0 {
		r.wait--
		return false
	}
	r.tries++
	if r.tries > 5 {
		r.wait = 9
	}
	return true
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
		procChecks: make(map[int]*recheck), probeChecks: make(map[[2]int]*recheck),
	}
}

// Detect lists the servers whose working directory belongs to a task.
// resolve maps a working directory to its task; owner names the declared
// environment a process belongs to, if any. A server is reported from its
// second sighting on, so test servers that live for a moment never show up.
//
// A desktop app is reported once, as an app with no URL, however many ports
// it listens on. A DevTools port is never reported as a URL: opening it shows
// a debugger page, not the app.
//
// The servers of a declared environment are already listed under it and are
// left out. A desktop app among them is returned under the environment's own
// key, with the config source, so the caller can show that environment as the
// app and bring its window forward.
func (d *Detector) Detect(ctx context.Context, resolve func(cwd string) (Task, bool), owner func(pid int) string) ([]Environment, error) {
	listeners, err := d.list(ctx)
	if err != nil {
		return nil, err
	}
	type candidate struct {
		Listener
		task  Task
		owner string
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
		candidates = append(candidates, candidate{l, task, owner(l.PID)})
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
		if c.owner != "" {
			if _, dup := found[c.owner]; proc.App != "" && !dup {
				found[c.owner] = Environment{
					Key: c.owner, TaskID: c.task.TaskID, IssueID: c.task.IssueID, Source: SourceConfig, Kind: KindApp,
					Name: truncateName(proc.App), State: StateRunning, Dir: c.task.RootDir,
					pid: c.PID, bundle: proc.Bundle,
				}
			}
			continue
		}
		// A process that may yet be an app is probed like one, so its debug
		// port is not offered as a link while it starts.
		debug := proc.debugs(c.Port) || ((proc.App != "" || proc.Unsettled) && d.speaksCDP(ctx, c.PID, c.Port))
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

// processes returns what each pid is, asking about pids not seen in an
// earlier scan and those whose answer may still change, and forgets pids that
// stopped listening.
func (d *Detector) processes(ctx context.Context, pids []int) map[int]Process {
	d.mu.Lock()
	var ask []int
	live := make(map[int]bool, len(pids))
	for _, pid := range pids {
		if live[pid] {
			continue
		}
		live[pid] = true
		known, ok := d.procs[pid]
		if !ok {
			ask = append(ask, pid)
		} else if known.Unsettled {
			check := d.procChecks[pid]
			if check == nil {
				check = &recheck{tries: 1}
				d.procChecks[pid] = check
			}
			if check.due() {
				ask = append(ask, pid)
			}
		}
	}
	d.mu.Unlock()
	var learned map[int]Process
	if len(ask) > 0 {
		learned = d.inspect(ctx, ask)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	for pid, p := range learned {
		d.procs[pid] = p
		if !p.Unsettled {
			delete(d.procChecks, pid)
		}
	}
	for pid := range d.procs {
		if !live[pid] {
			delete(d.procs, pid)
			delete(d.procChecks, pid)
		}
	}
	for k := range d.cdp {
		if !live[k[0]] {
			delete(d.cdp, k)
		}
	}
	for k := range d.probeChecks {
		if !live[k[0]] {
			delete(d.probeChecks, k)
		}
	}
	out := make(map[int]Process, len(pids))
	for _, pid := range pids {
		out[pid] = d.procs[pid]
	}
	return out
}

// speaksCDP remembers a port that answered as DevTools. One that did not may
// simply not be serving yet, so it is probed again rather than written off.
func (d *Detector) speaksCDP(ctx context.Context, pid, port int) bool {
	k := [2]int{pid, port}
	d.mu.Lock()
	if d.cdp[k] {
		d.mu.Unlock()
		return true
	}
	check := d.probeChecks[k]
	if check == nil {
		check = &recheck{}
		d.probeChecks[k] = check
	}
	due := check.due()
	d.mu.Unlock()
	if !due || !d.isCDP(ctx, port) {
		return false
	}
	d.mu.Lock()
	d.cdp[k] = true
	delete(d.probeChecks, k)
	d.mu.Unlock()
	return true
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
