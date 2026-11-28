package daemon

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/internal/filelock"
)

// acquireRuntimeOwnership keeps two profiles with the same runtime identity
// from registering or deregistering the same rows. The account ID comes from
// the server because independently issued tokens can belong to one account.
func (d *Daemon) acquireRuntimeOwnership(ctx context.Context) (*os.File, error) {
	reqCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	var account struct {
		ID string `json:"id"`
	}
	if err := d.client.getJSON(reqCtx, "/api/me", &account); err != nil {
		return nil, fmt.Errorf("identify daemon account: %w", err)
	}
	if account.ID == "" {
		return nil, fmt.Errorf("identify daemon account: server returned no user ID")
	}

	root, err := cli.ProfileDir("")
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(root, "runtime-locks")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	daemonID := strings.TrimSpace(d.cfg.DaemonID)
	key := sha256.Sum256([]byte(d.cfg.ServerBaseURL + "\x00" + daemonID + "\x00" + account.ID))
	base := filepath.Join(dir, fmt.Sprintf("%x", key))

	// Keep the owner name and lock acquisition consistent when profiles race
	// to start. The guard is held only for this local file operation.
	lockCtx, cancelLock := context.WithTimeout(ctx, 5*time.Second)
	defer cancelLock()
	guard, err := filelock.Acquire(lockCtx, base+".guard")
	if err != nil {
		return nil, fmt.Errorf("check daemon runtime ownership: %w", err)
	}
	defer guard.Close()

	lease, acquired, err := filelock.Try(base + ".lock")
	if err != nil {
		return nil, fmt.Errorf("lock daemon runtimes: %w", err)
	}
	ownerPath := base + ".owner"
	if !acquired {
		owner, err := os.ReadFile(ownerPath)
		if err != nil {
			return nil, fmt.Errorf("daemon runtimes are already in use: read owner profile: %w", err)
		}
		return nil, fmt.Errorf("daemon runtimes are already served by %s", owner)
	}

	owner := "the default profile"
	if d.cfg.Profile != "" {
		owner = fmt.Sprintf("profile %q", d.cfg.Profile)
	}
	if err := os.WriteFile(ownerPath, []byte(owner), 0o600); err != nil {
		lease.Close()
		return nil, fmt.Errorf("record daemon runtime owner: %w", err)
	}
	return lease, nil
}
