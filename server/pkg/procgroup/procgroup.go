// Package procgroup binds spawned subprocesses (and their descendants)
// to a kernel-managed process group, so the entire tree can be reaped
// atomically when the daemon is done with it — even if the daemon
// itself crashes.
//
// Two-tier API:
//   - Low-level Job{New, PrepareCmd, Attach, Close} for callers that
//     need to manage Job lifetime explicitly (tests, multi-process
//     callers).
//   - High-level Start(cmd) for the common case: New + PrepareCmd +
//     cmd.Start + Attach in one call. Caller defers the returned
//     cleanup. This is what every spawn site in the daemon uses.
//
// Implementation:
//   - Windows: Job Object with JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE.
//     When the last handle to the Job closes, the kernel terminates
//     every process in the Job. This guarantees reap even if the
//     daemon crashes (the kernel closes all the daemon's open
//     handles on process termination).
//   - Unix: process group leader via SysProcAttr.Setpgid. Close
//     sends SIGKILL to the negative PID; ESRCH is silently ignored.
//
// Failure handling: Job creation / attach failures never block
// cmd.Start. The Start helper logs nothing (callers can inspect
// Counters() for observability), increments the matching counter,
// and returns a no-op cleanup. The caller does not need fallback
// code.
package procgroup

import (
	"os"
	"os/exec"
	"sync/atomic"
)

// Stats is a snapshot of procgroup counters since process start.
// Counters are monotonic and reset only when the daemon restarts.
type Stats struct {
	JobsCreated    uint64
	JobsClosed     uint64
	CreateFailures uint64
	AttachFailures uint64
}

// Package-level counters. Exported via Counters().
var (
	jobsCreated    atomic.Uint64
	jobsClosed     atomic.Uint64
	createFailures atomic.Uint64
	attachFailures atomic.Uint64
)

// Counters returns a snapshot of the current counters.
func Counters() Stats {
	return Stats{
		JobsCreated:    jobsCreated.Load(),
		JobsClosed:     jobsClosed.Load(),
		CreateFailures: createFailures.Load(),
		AttachFailures: attachFailures.Load(),
	}
}

// Job represents a process group for a tree of subprocesses. The
// concrete fields are platform-specific and live in
// procgroup_unix.go / procgroup_windows.go.
type Job struct {
	plat   platJob
	closed atomic.Bool
}

// New allocates a Job. On Windows this opens a Job Object handle
// configured with KILL_ON_JOB_CLOSE.
func New() (*Job, error) {
	plat, err := newPlatJob()
	if err != nil {
		createFailures.Add(1)
		return nil, err
	}
	jobsCreated.Add(1)
	return &Job{plat: plat}, nil
}

// PrepareCmd configures cmd.SysProcAttr so that cmd.Start spawns the
// child as the leader of a fresh process group (Unix). On Windows
// this is a no-op; the Job Object is attached after Start.
// MUST be called before cmd.Start.
func (j *Job) PrepareCmd(cmd *exec.Cmd) {
	if j == nil {
		return
	}
	j.plat.prepareCmd(cmd)
}

// Attach binds an already-started process to this Job.
//   - Unix: no-op (Setpgid happened at fork time).
//   - Windows: AssignProcessToJobObject.
func (j *Job) Attach(p *os.Process) error {
	if j == nil {
		return nil
	}
	if err := j.plat.attach(p); err != nil {
		attachFailures.Add(1)
		return err
	}
	return nil
}

// Close terminates every process in the Job and releases the
// underlying handle. Idempotent: second and later calls are no-ops.
func (j *Job) Close() error {
	if j == nil {
		return nil
	}
	if !j.closed.CompareAndSwap(false, true) {
		return nil
	}
	err := j.plat.close()
	jobsClosed.Add(1)
	return err
}

// Start is the high-level helper for the common case: New +
// PrepareCmd + cmd.Start + Attach. Returns a cleanup the caller
// MUST defer; the cleanup closes the Job (reaping descendants).
//
// Errors during Job creation are NOT fatal: cmd is started anyway
// and cleanup is a no-op. The only error this returns is from
// cmd.Start itself.
func Start(cmd *exec.Cmd) (cleanup func(), err error) {
	job, jobErr := New()
	if jobErr != nil {
		// Degrade: still start the cmd, no-op cleanup.
		if err := cmd.Start(); err != nil {
			return func() {}, err
		}
		return func() {}, nil
	}

	job.PrepareCmd(cmd)

	if err := cmd.Start(); err != nil {
		_ = job.Close() // release Job handle on failed start
		return func() {}, err
	}

	if attachErr := job.Attach(cmd.Process); attachErr != nil {
		// Degrade: cmd is running, but unattached. Still close the Job
		// so we don't leak a handle. cleanup is now a no-op for the
		// running process tree.
		_ = job.Close()
		return func() {}, nil
	}

	return func() { _ = job.Close() }, nil
}
