package daemon

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
)

// Logical runtime ownership (GH #8280).
//
// One physical machine + one backend is one logical runtime identity: the server
// upserts on (workspace_id, daemon_id, provider) and daemon_id is machine-scoped,
// so two profile processes on one host address the same runtime row. Filesystem
// work state was made safe for that in the earlier revisions; runtime *lifecycle*
// was not. Registration, orphan recovery, heartbeats and deregistration all
// assumed one runtime row meant one daemon process, which produced two failures
// between siblings:
//
//   - a sibling starting up ran RecoverOrphans against the shared runtime and
//     hard-failed the tasks the first process was still running;
//   - a sibling shutting down sent Deregister for that runtime and took it
//     offline while the first process was still serving it.
//
// The fix is an explicit active-owner claim: exactly one process at a time may
// act as a runtime, and the claim is held for the whole lifecycle rather than for
// a critical section. It lives in the work-state scope lock directory, which is
// already keyed on machine + normalized backend, so profiles sharing a backend
// contend and different backends never do.

// runtimeOwnerTarget names the claim for one logical runtime.
//
// It mirrors the server UPSERT identity and nothing else:
//
//	built-in runtime   workspace + provider      (profile_id IS NULL)
//	custom runtime     workspace + profile_id    (profile_id IS NOT NULL)
//
// daemon_id is not part of it because the lock root is already machine-local, and
// the Multica profile name is not part of it because profile is exactly the
// boundary this claim has to look through: the CLI profile and the Desktop
// profile serving one backend must contend on ONE target. The server runtime UUID
// is not part of it either - it does not exist before registration, and a
// server-side delete/recreate would move the claim to a new name while the old
// process still held the old one.
func runtimeOwnerTarget(workspaceID, provider, profileID string) string {
	workspaceID = strings.TrimSpace(workspaceID)
	provider = strings.TrimSpace(provider)
	profileID = strings.TrimSpace(profileID)
	if profileID != "" {
		return "runtime-owner:custom:" + workspaceID + ":" + profileID
	}
	return "runtime-owner:builtin:" + workspaceID + ":" + provider
}

// runtimeOwnerTargetWorkspace decodes the workspace a target names, so standby
// bookkeeping can be scoped to one workspace (the reconcile retry below needs to
// ask "does THIS workspace have a sibling-owned runtime?").
//
// Both halves of the identity are colon-free — workspace IDs, provider names and
// profile IDs are UUIDs and slugs — so the third field is unambiguously the
// workspace.
func runtimeOwnerTargetWorkspace(target string) string {
	parts := strings.SplitN(target, ":", 4)
	if len(parts) < 4 || parts[0] != "runtime-owner" {
		return ""
	}
	return parts[2]
}

// runtimeOwnerClaim is one held ownership and its server-visible fencing token.
//
// Startup orphan recovery runs once for this claim. A new claim gets a new
// token and a fresh recovery decision; re-registration under the same claim
// must not recover tasks the owner is executing.
type runtimeOwnerClaim struct {
	claim     *execenv.ScopeClaim
	token     string
	recovered bool
}

// runtimeOwners is the process-local view of the claims this daemon holds.
type runtimeOwners struct {
	mu       sync.Mutex
	byTarget map[string]*runtimeOwnerClaim
	// standbyTargets records runtimes a sibling process owns, so the daemon
	// retries them later instead of concluding "nothing to host".
	standbyTargets map[string]struct{}
}

func newRuntimeOwners() *runtimeOwners {
	return &runtimeOwners{
		byTarget:       make(map[string]*runtimeOwnerClaim),
		standbyTargets: make(map[string]struct{}),
	}
}

// runtimeOwnershipOutcome distinguishes the states a runtime candidate can be in.
// Collapsing "a peer owns it" into "nothing to host" is what made a standby
// daemon deregister a sibling runtime, so they stay separate.
type runtimeOwnershipOutcome int

const (
	// runtimeOwnedByThisProcess: this process holds the claim and may register,
	// claim work, heartbeat and recover orphans.
	runtimeOwnedByThisProcess runtimeOwnershipOutcome = iota
	// runtimeOwnedByPeer: the runtime exists on this machine but a sibling
	// process in this work-state scope serves it. This process must not register,
	// claim, heartbeat, recover orphans or deregister it.
	runtimeOwnedByPeer
)

// acquireRuntimeOwnership takes the claim for target, or reports that a sibling
// holds it. Non-blocking on purpose: waiting here would stall a whole daemon
// startup behind another process lifetime, and the correct behaviour is to serve
// whatever this process does own and retry the rest on the next reconcile.
func (d *Daemon) acquireRuntimeOwnership(target string) (runtimeOwnershipOutcome, error) {
	d.ownerState().mu.Lock()
	defer d.ownerState().mu.Unlock()
	if claim, ok := d.ownerState().byTarget[target]; ok && (claim.claim != nil || !d.scopeLocks.Enabled()) {
		return runtimeOwnedByThisProcess, nil
	}
	claim, ok, err := d.scopeLocks.TryAcquireTargetDelete(target)
	if err != nil {
		return runtimeOwnedByPeer, fmt.Errorf("runtime ownership claim %s: %w", target, err)
	}
	if !ok {
		return runtimeOwnedByPeer, nil
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		claim.Release()
		return runtimeOwnedByPeer, fmt.Errorf("runtime ownership token: %w", err)
	}
	token := hex.EncodeToString(nonce[:])
	if d.scopeLocks.Enabled() {
		generation, err := nextRuntimeOwnerGeneration(d.scopeLocks.Dir(), target)
		if err != nil {
			claim.Release()
			return runtimeOwnedByPeer, fmt.Errorf("runtime ownership generation: %w", err)
		}
		token = fmt.Sprintf("g%020d:%s", generation, token)
	}
	entry := &runtimeOwnerClaim{claim: claim, token: token}
	d.ownerState().byTarget[target] = entry
	delete(d.ownerState().standbyTargets, target)
	return runtimeOwnedByThisProcess, nil
}

// nextRuntimeOwnerGeneration runs while the caller holds target's OS claim.
// Persisting before Register makes a later claimant's generation strictly newer
// even if the previous process dies with a request still in flight.
func nextRuntimeOwnerGeneration(lockDir, target string) (uint64, error) {
	sum := sha256.Sum256([]byte(filepath.Clean(target)))
	path := filepath.Join(lockDir, hex.EncodeToString(sum[:8])+".generation")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	var buf [8]byte
	n, err := f.ReadAt(buf[:], 0)
	if !(n == 0 && err == io.EOF || n == len(buf) && err == nil) {
		return 0, fmt.Errorf("invalid runtime ownership generation file %s: read %d bytes: %w", path, n, err)
	}
	generation := binary.BigEndian.Uint64(buf[:])
	if generation == math.MaxUint64 {
		return 0, fmt.Errorf("runtime ownership generation exhausted for %s", target)
	}
	generation++
	binary.BigEndian.PutUint64(buf[:], generation)
	if _, err := f.WriteAt(buf[:], 0); err != nil {
		return 0, err
	}
	if err := f.Sync(); err != nil {
		return 0, err
	}
	return generation, nil
}

// noteRuntimeStandby records that a sibling owns target, so the workspace keeps
// being reconciled instead of converging to "nothing to host".
func (d *Daemon) noteRuntimeStandby(target string) {
	d.ownerState().mu.Lock()
	defer d.ownerState().mu.Unlock()
	d.ownerState().standbyTargets[target] = struct{}{}
}

// clearRuntimeStandby forgets a standby target that is no longer a candidate.
func (d *Daemon) clearRuntimeStandby(target string) {
	d.ownerState().mu.Lock()
	defer d.ownerState().mu.Unlock()
	delete(d.ownerState().standbyTargets, target)
}

// pruneRuntimeStandby removes this workspace's standby entries that are no
// longer candidates, so the set reflects the current candidate list instead of
// accumulating historical targets (a disabled profile, an uninstalled provider,
// a profile that changed provider identity). Without it a workspace would be
// reconciled forever for a runtime nobody hosts.
//
// candidates is the target set of the round that just ran; entries for other
// workspaces are left alone.
func (d *Daemon) pruneRuntimeStandby(workspaceID string, candidates map[string]bool) {
	d.ownerState().mu.Lock()
	defer d.ownerState().mu.Unlock()
	for target := range d.ownerState().standbyTargets {
		if runtimeOwnerTargetWorkspace(target) != workspaceID {
			continue
		}
		if !candidates[target] {
			delete(d.ownerState().standbyTargets, target)
		}
	}
}

// workspaceHasStandbyRuntime reports whether any runtime target recorded as
// sibling-owned belongs to workspaceID — i.e. this process is still standing by
// for a runtime in that workspace and must keep retrying it.
func (d *Daemon) workspaceHasStandbyRuntime(workspaceID string) bool {
	d.ownerState().mu.Lock()
	defer d.ownerState().mu.Unlock()
	for target := range d.ownerState().standbyTargets {
		if runtimeOwnerTargetWorkspace(target) == workspaceID {
			return true
		}
	}
	return false
}

// ownsTarget reports whether this process currently holds target.
func (d *Daemon) ownsTarget(target string) bool {
	d.ownerState().mu.Lock()
	defer d.ownerState().mu.Unlock()
	claim, ok := d.ownerState().byTarget[target]
	return ok && claim.claim != nil
}

func (d *Daemon) runtimeOwnerToken(target string) string {
	d.ownerState().mu.Lock()
	defer d.ownerState().mu.Unlock()
	if claim := d.ownerState().byTarget[target]; claim != nil && (claim.claim != nil || !d.scopeLocks.Enabled()) {
		return claim.token
	}
	return ""
}

func (d *Daemon) ownerGenerations(runtimeIDs []string, targets map[string]string) map[string]string {
	generations := make(map[string]string, len(runtimeIDs))
	for _, id := range runtimeIDs {
		if token := d.runtimeOwnerToken(targets[id]); token != "" {
			generations[id] = token
		}
	}
	return generations
}

// markOwnershipRecovered records that startup orphan recovery has run for the
// target current ownership generation, and reports whether this call is the one
// that should run it.
//
// Exactly one caller per generation gets true. That is what keeps a periodic
// re-register, a profile refresh or a version refresh from re-running recovery
// against tasks the owner is still executing.
func (d *Daemon) markOwnershipRecovered(target string) bool {
	d.ownerState().mu.Lock()
	defer d.ownerState().mu.Unlock()
	claim, ok := d.ownerState().byTarget[target]
	if !ok || claim.claim == nil {
		return false
	}
	if claim.recovered {
		return false
	}
	claim.recovered = true
	return true
}

// releaseRuntimeOwnership drops the claim for target. The server fences any
// delayed deregistration by this claim's owner token after a sibling takes over.
func (d *Daemon) releaseRuntimeOwnership(target string) {
	// A ClaimTasks request already sent under this ownership must finish before
	// a sibling can acquire the claim. The poller rechecks its snapshot after
	// entering the gate, so an old ID cannot be sent once this release ends.
	d.claimMu.Lock()
	d.ownershipTransitions++
	if d.claimsDrained == nil {
		d.claimsDrained = sync.NewCond(&d.claimMu)
	}
	for d.claimsInFlight > 0 {
		d.claimsDrained.Wait()
	}
	d.ownerState().mu.Lock()
	claim, ok := d.ownerState().byTarget[target]
	if ok {
		delete(d.ownerState().byTarget, target)
	}
	d.ownerState().mu.Unlock()
	if ok && claim.claim != nil {
		claim.claim.Release()
	}
	d.ownershipTransitions--
	d.claimMu.Unlock()
}

// releaseAllRuntimeOwnership drops every claim this process holds.
func (d *Daemon) releaseAllRuntimeOwnership() {
	d.ownerState().mu.Lock()
	targets := make([]string, 0, len(d.ownerState().byTarget))
	for target := range d.ownerState().byTarget {
		targets = append(targets, target)
	}
	d.ownerState().mu.Unlock()
	for _, target := range targets {
		d.releaseRuntimeOwnership(target)
	}
}

// runtimeOwnerTargetForEntry derives the ownership target a registration payload
// entry will land on. The payload entry is the daemon's own description of the
// runtime, so it is the right source for provider and profile_id.
func runtimeOwnerTargetForEntry(workspaceID string, entry map[string]string) string {
	return runtimeOwnerTarget(workspaceID, entry["type"], entry["profile_id"])
}

// runtimeOwnerTargetForRuntime derives the target from a server runtime row.
func runtimeOwnerTargetForRuntime(rt Runtime, workspaceID string) string {
	return runtimeOwnerTarget(workspaceID, rt.Provider, rt.ProfileID)
}

// errAllRuntimesPeerOwned reports that every runtime candidate this round would
// have registered is currently served by a sibling process in this work-state
// scope.
//
// It is deliberately NOT ErrNoRuntimesToRegister. That error means "this process
// has nothing to host here", and the converge-to-zero path answers it by
// deregistering the workspace runtimes. A standby process has the opposite
// situation: the runtimes exist and are being served, just not by this process -
// so treating the two the same would let a standby daemon take a sibling runtime
// offline, which is one of the two failures this revision exists to fix.
var errAllRuntimesPeerOwned = errors.New("all runtime candidates are owned by a sibling daemon process")

// filterOwnedRuntimeCandidates splits a registration payload into the entries
// this process may register and the ones a sibling owns.
//
// Ordering matters: ownership is taken HERE, before the Register call, so a
// process can never tell the server about a runtime it does not own. Registering
// first and claiming afterwards would leave a window in which two processes have
// both announced the same runtime.
//
// Duplicate candidates for one target (the same provider twice, or a built-in
// repeated across a batch) collapse to a single claim.
func (d *Daemon) filterOwnedRuntimeCandidates(ctx context.Context, workspaceID string, candidates, failures []map[string]string) ([]map[string]string, []map[string]string, []string, error) {
	owned := make([]map[string]string, 0, len(candidates))
	ownedFailures := make([]map[string]string, 0, len(failures))
	var peerOwned []string
	seen := make(map[string]bool, len(candidates)+len(failures))
	for groupIndex, group := range [][]map[string]string{candidates, failures} {
		for _, entry := range group {
			failure := groupIndex == 1
			target := runtimeOwnerTargetForEntry(workspaceID, entry)
			if seen[target] {
				continue
			}
			seen[target] = true
			outcome, err := d.acquireRuntimeOwnership(target)
			if err != nil {
				return nil, ownedFailures, nil, err
			}
			if outcome == runtimeOwnedByPeer {
				d.noteRuntimeStandby(target)
				label := entry["type"]
				if failure {
					label = entry["profile_id"]
				}
				peerOwned = append(peerOwned, label)
				continue
			}
			d.clearRuntimeStandby(target)
			entry["owner_generation"] = d.runtimeOwnerToken(target)
			if entry["owner_generation"] == "" {
				return nil, ownedFailures, nil, fmt.Errorf("runtime ownership claim %s disappeared before registration", target)
			}
			if failure {
				ownedFailures = append(ownedFailures, entry)
			} else {
				owned = append(owned, entry)
			}
		}
	}
	d.pruneRuntimeStandby(workspaceID, seen)
	return owned, ownedFailures, peerOwned, nil
}

// recoverOrphansOncePerOwnership runs startup orphan recovery for a runtime this
// process has just taken ownership of, and never twice for one ownership.
//
// Recovery hard-fails every dispatched/running/waiting_local_directory task on
// the runtime, which is correct exactly once: after a process that owned the
// runtime died, those tasks are genuinely orphaned. Re-running it on a later
// registration of the same ownership would fail the tasks this process is
// itself executing, so the generation gate is the contract, not an optimisation.
func (d *Daemon) recoverOrphansOncePerOwnership(ctx context.Context, workspaceID string, rt Runtime) {
	target := runtimeOwnerTargetForRuntime(rt, workspaceID)
	if !d.markOwnershipRecovered(target) {
		return
	}
	d.logger.Info("recovering orphaned tasks for newly owned runtime",
		"workspace_id", workspaceID, "runtime_id", rt.ID, "provider", rt.Provider)
	if err := d.client.RecoverOrphans(ctx, rt.ID); err != nil {
		d.logger.Warn("recover-orphans failed", "runtime_id", rt.ID, "error", err)
	}
}

// ownerState returns the runtime-owner state, creating it on first use so a
// Daemon built as a struct literal (tests, embedded callers) does not panic on
// the ownership path. LoadConfig always seeds it; this is the backstop.
func (d *Daemon) ownerState() *runtimeOwners {
	d.runtimeOwnersInit.Lock()
	defer d.runtimeOwnersInit.Unlock()
	if d.runtimeOwners == nil {
		d.runtimeOwners = newRuntimeOwners()
	}
	return d.runtimeOwners
}
