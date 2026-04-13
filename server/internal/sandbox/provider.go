// Package sandbox provides the sandbox execution layer for cloud agent runtimes.
// It defines the SandboxProvider interface for managing remote sandboxes and
// implements the CloudDaemon that orchestrates task execution.
package sandbox

import (
	"context"
	"time"
)

// SandboxProvider abstracts the underlying sandbox infrastructure (E2B, Daytona, Docker, etc.).
// All sandbox interactions go through this interface, making the CloudDaemon provider-agnostic.
type SandboxProvider interface {
	// CreateOrConnect creates a new sandbox or reconnects to an existing one.
	// The call is idempotent: if sandboxID refers to an existing sandbox, it reconnects.
	// If sandboxID is empty, a new sandbox is created.
	CreateOrConnect(ctx context.Context, sandboxID string, opts CreateOpts) (*Sandbox, error)

	// Exec runs a command inside the sandbox and returns its stdout.
	// Commands are passed as an array to prevent shell injection.
	Exec(ctx context.Context, sb *Sandbox, cmd []string) (stdout string, err error)

	// ReadFile reads a file from the sandbox filesystem.
	ReadFile(ctx context.Context, sb *Sandbox, path string) ([]byte, error)

	// WriteFile writes content to a file in the sandbox filesystem.
	WriteFile(ctx context.Context, sb *Sandbox, path string, content []byte) error

	// Destroy permanently removes a sandbox and all its data.
	Destroy(ctx context.Context, sandboxID string) error
}

// Sandbox represents a running sandbox instance.
type Sandbox struct {
	ID       string
	Status   string // "running", "paused", "stopped"
	Provider string // "e2b", "daytona", etc.

	// metadata holds provider-specific data (e.g. envdAccessToken for E2B).
	// Not exported; accessed by provider implementations in the same package.
	metadata map[string]string
}

// CreateOpts configures sandbox creation.
type CreateOpts struct {
	TemplateID string
	EnvVars    map[string]string
	Timeout    time.Duration
}
