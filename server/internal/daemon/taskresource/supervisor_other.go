//go:build !linux

package taskresource

import "context"

func Start(_ context.Context, opts Options) *Supervisor {
	return &Supervisor{
		mode:           IsolationProcessGroup,
		fallbackReason: "systemd cgroup v2 is only available on Linux",
		memoryHigh:     opts.MemoryHighBytes,
		memoryMax:      opts.MemoryMaxBytes,
		swapMax:        opts.SwapMaxBytes,
		lastCommand:    "unknown",
	}
}

func (s *Supervisor) CommandPrefix() []string { return nil }
func (s *Supervisor) Close() Usage            { return s.resourceUsage() }
