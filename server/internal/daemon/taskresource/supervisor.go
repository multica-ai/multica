package taskresource

import "sync"

const (
	IsolationSystemd      = "systemd_cgroup_v2"
	IsolationProcessGroup = "process_group"
)

// Options controls the resource boundary around one task. Zero high/max values
// mean unlimited. SwapMaxBytes is applied when non-negative; -1 leaves the host
// default unchanged.
type Options struct {
	TaskID          string
	MemoryHighBytes int64
	MemoryMaxBytes  int64
	SwapMaxBytes    int64
}

// Usage is the stable daemon/server wire contract for one task's resource
// accounting. Fields remain useful for successful runs too; Kind and MemoryOOM
// are set only when the cgroup positively reports an OOM kill.
type Usage struct {
	IsolationMode    string  `json:"isolation_mode"`
	FallbackReason   string  `json:"fallback_reason,omitempty"`
	Kind             string  `json:"kind,omitempty"`
	MemoryOOM        bool    `json:"memory_oom,omitempty"`
	MemoryPeakBytes  int64   `json:"memory_peak_bytes"`
	SwapPeakBytes    int64   `json:"swap_peak_bytes"`
	MemoryHighBytes  int64   `json:"memory_high_bytes,omitempty"`
	MemoryMaxBytes   int64   `json:"memory_max_bytes,omitempty"`
	SwapMaxBytes     int64   `json:"swap_max_bytes,omitempty"`
	PSISomeAvg10Peak float64 `json:"psi_some_avg10_peak"`
	PSIFullAvg10Peak float64 `json:"psi_full_avg10_peak"`
	OOMKills         uint64  `json:"oom_kills,omitempty"`
	OOMGroupKills    uint64  `json:"oom_group_kills,omitempty"`
	VictimCgroup     string  `json:"victim_cgroup,omitempty"`
	LastCommand      string  `json:"last_command,omitempty"`
}

type resourceSnapshot struct {
	MemoryPeakBytes int64
	SwapPeakBytes   int64
	PSISomeAvg10    float64
	PSIFullAvg10    float64
	OOMKills        uint64
	OOMGroupKills   uint64
	VictimCgroup    string
}

// Supervisor owns the task slice and accumulates diagnostics until Close.
type Supervisor struct {
	mode           string
	fallbackReason string
	sliceUnit      string
	memoryHigh     int64
	memoryMax      int64
	swapMax        int64

	mu          sync.Mutex
	samples     resourceSnapshot
	lastCommand string
	stopMonitor func()
	waitMonitor func()
}

func (s *Supervisor) IsolationMode() string  { return s.mode }
func (s *Supervisor) FallbackReason() string { return s.fallbackReason }

func (s *Supervisor) SetLastCommand(command string) {
	s.mu.Lock()
	s.lastCommand = command
	s.mu.Unlock()
}

func (s *Supervisor) resourceUsage() Usage {
	s.mu.Lock()
	defer s.mu.Unlock()
	oom := s.samples.OOMKills > 0 || s.samples.OOMGroupKills > 0
	kind := ""
	if oom {
		kind = "memory"
	}
	return Usage{
		IsolationMode:    s.mode,
		FallbackReason:   s.fallbackReason,
		Kind:             kind,
		MemoryOOM:        oom,
		MemoryPeakBytes:  s.samples.MemoryPeakBytes,
		SwapPeakBytes:    s.samples.SwapPeakBytes,
		MemoryHighBytes:  s.memoryHigh,
		MemoryMaxBytes:   s.memoryMax,
		SwapMaxBytes:     s.swapMax,
		PSISomeAvg10Peak: s.samples.PSISomeAvg10,
		PSIFullAvg10Peak: s.samples.PSIFullAvg10,
		OOMKills:         s.samples.OOMKills,
		OOMGroupKills:    s.samples.OOMGroupKills,
		VictimCgroup:     s.samples.VictimCgroup,
		LastCommand:      s.lastCommand,
	}
}

func (s *Supervisor) merge(sample resourceSnapshot) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sample.MemoryPeakBytes > s.samples.MemoryPeakBytes {
		s.samples.MemoryPeakBytes = sample.MemoryPeakBytes
	}
	if sample.SwapPeakBytes > s.samples.SwapPeakBytes {
		s.samples.SwapPeakBytes = sample.SwapPeakBytes
	}
	if sample.PSISomeAvg10 > s.samples.PSISomeAvg10 {
		s.samples.PSISomeAvg10 = sample.PSISomeAvg10
	}
	if sample.PSIFullAvg10 > s.samples.PSIFullAvg10 {
		s.samples.PSIFullAvg10 = sample.PSIFullAvg10
	}
	if sample.OOMKills > s.samples.OOMKills {
		s.samples.OOMKills = sample.OOMKills
	}
	if sample.OOMGroupKills > s.samples.OOMGroupKills {
		s.samples.OOMGroupKills = sample.OOMGroupKills
	}
	if sample.VictimCgroup != "" {
		s.samples.VictimCgroup = sample.VictimCgroup
	}
}
