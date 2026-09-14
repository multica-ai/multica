//go:build linux

package taskresource

import (
	"bufio"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const sampleInterval = 100 * time.Millisecond

func Start(ctx context.Context, opts Options) *Supervisor {
	s := &Supervisor{
		mode:        IsolationProcessGroup,
		memoryHigh:  opts.MemoryHighBytes,
		memoryMax:   opts.MemoryMaxBytes,
		swapMax:     opts.SwapMaxBytes,
		lastCommand: "unknown",
	}
	if _, err := os.Stat("/sys/fs/cgroup/cgroup.controllers"); err != nil {
		s.fallbackReason = "cgroup v2 unavailable"
		return s
	}
	if _, err := exec.LookPath("systemd-run"); err != nil {
		s.fallbackReason = "systemd-run unavailable"
		return s
	}
	if _, err := exec.LookPath("systemctl"); err != nil {
		s.fallbackReason = "systemctl unavailable"
		return s
	}

	s.sliceUnit = taskSliceUnit(opts.TaskID)
	args := []string{"--user", "set-property", "--runtime", s.sliceUnit, "MemoryAccounting=yes"}
	if opts.MemoryHighBytes > 0 {
		args = append(args, "MemoryHigh="+strconv.FormatInt(opts.MemoryHighBytes, 10))
	}
	if opts.MemoryMaxBytes > 0 {
		args = append(args, "MemoryMax="+strconv.FormatInt(opts.MemoryMaxBytes, 10))
	}
	if opts.SwapMaxBytes >= 0 {
		args = append(args, "MemorySwapMax="+strconv.FormatInt(opts.SwapMaxBytes, 10))
	}
	setupCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if output, err := exec.CommandContext(setupCtx, "systemctl", args...).CombinedOutput(); err != nil {
		detail := strings.TrimSpace(string(output))
		if detail == "" {
			detail = err.Error()
		}
		s.fallbackReason = "systemd user scope unavailable: " + detail
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 2*time.Second)
		_ = exec.CommandContext(cleanupCtx, "systemctl", "--user", "stop", s.sliceUnit).Run()
		cleanupCancel()
		return s
	}

	s.mode = IsolationSystemd
	monitorCtx, stop := context.WithCancel(context.Background())
	done := make(chan struct{})
	s.stopMonitor = stop
	s.waitMonitor = func() { <-done }
	go func() {
		defer close(done)
		ticker := time.NewTicker(sampleInterval)
		defer ticker.Stop()
		for {
			s.sample()
			select {
			case <-monitorCtx.Done():
				s.sample()
				return
			case <-ticker.C:
			}
		}
	}()
	return s
}

func (s *Supervisor) CommandPrefix() []string {
	if s == nil || s.mode != IsolationSystemd {
		return nil
	}
	return []string{
		"systemd-run", "--user", "--scope", "--quiet",
		"--slice=" + s.sliceUnit,
		// Kill only this task scope when any member is selected by the kernel
		// OOM killer. The daemon and sibling tasks live in different units.
		"--property=OOMPolicy=kill",
		"--property=KillMode=control-group",
		"--",
	}
}

func (s *Supervisor) Close() Usage {
	if s == nil {
		return Usage{IsolationMode: IsolationProcessGroup, FallbackReason: "supervisor unavailable"}
	}
	if s.stopMonitor != nil {
		s.stopMonitor()
		s.waitMonitor()
	}
	// One final systemd read catches the kernel-maintained peaks even when a
	// very short process started and exited between monitor ticks.
	s.sampleSystemd()
	usage := s.resourceUsage()
	if s.mode == IsolationSystemd {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = exec.CommandContext(cleanupCtx, "systemctl", "--user", "stop", s.sliceUnit).Run()
	}
	return usage
}

func (s *Supervisor) sample() {
	if s == nil || s.mode != IsolationSystemd {
		return
	}
	s.mu.Lock()
	cgroup := s.samples.VictimCgroup
	s.mu.Unlock()
	if cgroup == "" {
		// The slice has no ControlGroup until its first scope starts. Poll
		// systemd only for discovery; once found, sample cgroupfs directly so a
		// long-running fleet does not spawn a systemctl process every 100 ms.
		s.sampleSystemd()
		return
	}
	s.merge(snapshotFromCgroup(cgroup, nil))
}

func (s *Supervisor) sampleSystemd() {
	if s == nil || s.mode != IsolationSystemd {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "systemctl", "--user", "show", s.sliceUnit,
		"--property=ControlGroup", "--property=MemoryPeak", "--property=MemorySwapPeak", "--no-pager").Output()
	if err != nil {
		return
	}
	cgroup := propertyValue(out, "ControlGroup")
	s.merge(snapshotFromCgroup(cgroup, out))
}

func snapshotFromCgroup(cgroup string, systemdProperties []byte) resourceSnapshot {
	snapshot := resourceSnapshot{
		MemoryPeakBytes: propertyInt64(systemdProperties, "MemoryPeak"),
		SwapPeakBytes:   propertyInt64(systemdProperties, "MemorySwapPeak"),
		VictimCgroup:    cgroup,
	}
	if cgroup == "" {
		return snapshot
	}
	cleanCgroup := strings.TrimPrefix(filepath.Clean("/"+cgroup), "/")
	base := filepath.Join("/sys/fs/cgroup", cleanCgroup)
	if peak := readInt64File(filepath.Join(base, "memory.peak")); peak > snapshot.MemoryPeakBytes {
		snapshot.MemoryPeakBytes = peak
	}
	if peak := readInt64File(filepath.Join(base, "memory.swap.peak")); peak > snapshot.SwapPeakBytes {
		snapshot.SwapPeakBytes = peak
	}
	events, _ := os.ReadFile(filepath.Join(base, "memory.events"))
	pressure, _ := os.ReadFile(filepath.Join(base, "memory.pressure"))
	snapshot.OOMKills = eventUint(events, "oom_kill")
	snapshot.OOMGroupKills = eventUint(events, "oom_group_kill")
	snapshot.PSISomeAvg10 = psiAvg10(pressure, "some")
	snapshot.PSIFullAvg10 = psiAvg10(pressure, "full")
	return snapshot
}

func readInt64File(path string) int64 {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	n, _ := strconv.ParseInt(strings.TrimSpace(string(raw)), 10, 64)
	return n
}

func parseResourceSnapshot(cgroup string, properties, events, pressure []byte) resourceSnapshot {
	return resourceSnapshot{
		MemoryPeakBytes: propertyInt64(properties, "MemoryPeak"),
		SwapPeakBytes:   propertyInt64(properties, "MemorySwapPeak"),
		OOMKills:        eventUint(events, "oom_kill"),
		OOMGroupKills:   eventUint(events, "oom_group_kill"),
		PSISomeAvg10:    psiAvg10(pressure, "some"),
		PSIFullAvg10:    psiAvg10(pressure, "full"),
		VictimCgroup:    cgroup,
	}
}

func propertyValue(data []byte, name string) string {
	prefix := name + "="
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(line, prefix))
		}
	}
	return ""
}

func propertyInt64(data []byte, name string) int64 {
	value := propertyValue(data, name)
	if value == "" || strings.HasPrefix(value, "[") {
		return 0
	}
	n, _ := strconv.ParseInt(value, 10, 64)
	return n
}

func eventUint(data []byte, name string) uint64 {
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 2 && fields[0] == name {
			n, _ := strconv.ParseUint(fields[1], 10, 64)
			return n
		}
	}
	return 0
}

func psiAvg10(data []byte, class string) float64 {
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 0 || fields[0] != class {
			continue
		}
		for _, field := range fields[1:] {
			if strings.HasPrefix(field, "avg10=") {
				value, _ := strconv.ParseFloat(strings.TrimPrefix(field, "avg10="), 64)
				return value
			}
		}
	}
	return 0
}

func taskSliceUnit(taskID string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(taskID) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' {
			b.WriteRune(r)
		}
	}
	key := strings.Trim(b.String(), "-")
	if key == "" {
		key = "unknown"
	}
	return "multica-task-" + key + ".slice"
}
