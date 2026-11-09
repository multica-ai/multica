package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/multica-ai/multica/server/internal/cli"
)

// Rolling-upgrade safety for the runtime-owner claim (GH #8280).
//
// The owner claim is an OS lock, so it only coordinates binaries that know to
// take it. Before activation a coordinating daemon therefore stands by for any
// live peer that may address the same machine-scoped runtime: a same-backend
// peer that does not advertise the coordination capability, or a live profile
// whose backend cannot be read. It keeps probing and takes over once that peer
// is gone, which is the same path an owner crash uses.
//
// The protection is one-directional by nature: a legacy binary that starts AFTER
// activation does not participate in this protocol, so it cannot be controlled
// from here. Detecting one then makes this daemon yield its local serving
// authority - conflict mitigation, not coordination with the process that
// caused it (yieldRuntimesToLegacyPeer).

// RuntimeCoordinationVersion is the capability a daemon advertises when it
// participates in the runtime-owner claim. Additive: a peer that does not
// advertise it is treated as a legacy daemon that cannot be coordinated with.
const RuntimeCoordinationVersion = 1

// runtimeCoordinationKey is the health-payload field carrying the capability.
const runtimeCoordinationKey = "runtime_coordination_version"

// peerProbeTimeout bounds one peer health probe. Short: a probe that hangs must
// not delay a daemon startup, and a peer that cannot answer within this budget
// proves nothing - the probe reports "unknown", never absence, and the caller
// resolves that with the local liveness signal below.
const peerProbeTimeout = 700 * time.Millisecond

// runtimeCoordinationPeer is one sibling daemon found on this machine.
type runtimeCoordinationPeer struct {
	Profile string
	URL     string
	// Port is the peer profile's health port, kept beside the URL because the
	// local liveness check below has to dial it as a plain TCP port.
	Port int
}

// runtimeCoordinationPeers discovers sibling profile candidates. Their saved
// config cannot identify a running daemon: CLI flags and environment may override
// its server URL. Only the live health response establishes backend identity.
//
// The profile directories are the machine-local inventory of daemon processes;
// no service discovery or registry is involved.
func runtimeCoordinationPeers(ownProfile string) ([]runtimeCoordinationPeer, error) {
	root, err := cli.ProfileDir("")
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(filepath.Join(root, "profiles"))
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)

	peers := make([]runtimeCoordinationPeer, 0, len(names)+1)
	// The default profile is not a directory under profiles/, so it is named
	// explicitly - the same reason workStateOwners() names it explicitly.
	for _, name := range append([]string{""}, names...) {
		if name == ownProfile {
			continue
		}
		port := healthPortForProfile(name)
		peer := runtimeCoordinationPeer{
			Profile: name,
			URL:     fmt.Sprintf("http://127.0.0.1:%d/health", port),
			Port:    port,
		}
		peers = append(peers, peer)
	}
	return peers, nil
}

// healthPortForProfile mirrors cmd/multica healthPortForProfile. The cmd/multica
// test TestWorkStateSharedWhileProfilesStayIsolated and this package's
// coordination test pin both derivations to the same values.
func healthPortForProfile(profile string) int {
	if profile == "" {
		return DefaultHealthPort
	}
	var h int
	for _, b := range []byte(profile) {
		h += int(b)
	}
	return DefaultHealthPort + 1 + (h % 1000)
}

// peerCoordinationStatus is what one probe concluded about a peer.
//
// Alive is "answered at all", not "answered usefully": a health endpoint that
// returns a non-200 or a body that is not the expected JSON still proves a
// process is there. The caller treats "did not answer" as unknown rather than as
// absence, which is the distinction the mixed-version rule needs.
type peerCoordinationStatus struct {
	// Alive is true when the peer answered its health endpoint.
	Alive bool
	// Coordinates is true when the live peer advertises the capability, i.e. it
	// participates in the runtime-owner claim.
	Coordinates bool
	// Backend is the actual server_url advertised by this running daemon.
	Backend string
}

// peerProbeFunc performs one health probe. Indirected so tests can model a legacy
// peer without standing up a second process.
var peerProbeFunc = probeRuntimeCoordinationPeer

// probeRuntimeCoordinationPeer asks one peer what it is.
//
// Only an answer is evidence. A peer that answers without the capability is a
// live legacy daemon; a peer that does not answer at all is unknown, and the
// caller resolves that with the local liveness signal rather than assuming the
// peer is stopped (GH #8280).
func probeRuntimeCoordinationPeer(ctx context.Context, url string) peerCoordinationStatus {
	probeCtx, cancel := context.WithTimeout(ctx, peerProbeTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(probeCtx, http.MethodGet, url, nil)
	if err != nil {
		return peerCoordinationStatus{}
	}
	resp, err := (&http.Client{Timeout: peerProbeTimeout}).Do(req)
	if err != nil {
		return peerCoordinationStatus{}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		// It answered, so something is serving that port, but not as a
		// coordinating daemon. Reading a non-200 as "absent" was the fail-open
		// half of the mixed-version check: a live peer whose health endpoint was
		// merely unhealthy looked stopped, and this process would activate the
		// runtime the peer was still serving.
		return peerCoordinationStatus{Alive: true}
	}
	var payload map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		// A health endpoint that answers but is not JSON still proves the process
		// is alive; it just cannot prove coordination.
		return peerCoordinationStatus{Alive: true}
	}
	version, ok := payload[runtimeCoordinationKey].(float64)
	backend, _ := payload["server_url"].(string)
	return peerCoordinationStatus{
		Alive:       true,
		Coordinates: ok && int(version) >= RuntimeCoordinationVersion,
		Backend:     backend,
	}
}

// legacyPeerDecision is what the caller should do about the peers it found.
type legacyPeerDecision struct {
	// Blocked is true when a live peer that cannot be coordinated with exists, so
	// this process must stay in standby rather than activate a runtime.
	Blocked bool
	// Peers lists the non-coordinating peers with the evidence for each, for the
	// actionable log line.
	Peers []string
}

// block records one peer this process must not activate a runtime against.
func (d *legacyPeerDecision) block(detail string) {
	d.Blocked = true
	d.Peers = append(d.Peers, detail)
}

// peerHealthPortOwnedFunc probes the local liveness signal for a peer that did
// not answer its health endpoint. Indirected so tests can model a stale profile
// directory and a slow-but-live peer without standing up a listener.
var peerHealthPortOwnedFunc = peerHealthPortOwned

// peerHealthPortOwned reports whether a process still holds the peer profile's
// health port - the local liveness signal, deliberately the port rather than the
// pid file: a port is exclusive, so a listener proves a daemon is alive now,
// while a pid can be recycled by an unrelated program. The daemon lifecycle
// already maintains this port (it binds it for its whole run, and a second
// daemon for the same profile fails to start on it), so no second registry.
//
// false is the only local evidence that a profile directory is stale, which is
// what keeps a stopped peer from blocking activation forever.
func peerHealthPortOwned(port int) bool {
	if port <= 0 {
		return false
	}
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), peerProbeTimeout)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// checkRuntimeCoordinationPeers reports whether any sibling profile is a daemon
// this process must not activate a runtime against.
//
// The live health response identifies the backend. A same-backend peer with
// coordination relies on the owner claim; a same-backend legacy peer blocks.
// An unknown live backend also blocks, while a free port is a stale profile.
func (d *Daemon) checkRuntimeCoordinationPeers(ctx context.Context) legacyPeerDecision {
	peers, err := runtimeCoordinationPeers(d.cfg.Profile)
	if err != nil {
		return legacyPeerDecision{Blocked: true, Peers: []string{fmt.Sprintf("cannot discover sibling profiles: %v", err)}}
	}
	if len(peers) == 0 {
		return legacyPeerDecision{}
	}
	var decision legacyPeerDecision
	for _, peer := range peers {
		label := peer.Profile
		if label == "" {
			label = "default profile"
		}
		status := peerProbeFunc(ctx, peer.URL)
		backend, err := NormalizeServerBaseURL(status.Backend)
		parsed, parseErr := url.Parse(status.Backend)
		identified := err == nil && parseErr == nil && parsed.Host != ""
		switch {
		case status.Alive && identified && backend != d.cfg.ServerBaseURL:
			continue
		case status.Alive && identified && status.Coordinates:
			continue
		case status.Alive && identified:
			decision.block(fmt.Sprintf("%s (%s) is alive but does not advertise runtime coordination", label, peer.URL))
		case status.Alive:
			decision.block(fmt.Sprintf("%s (%s) is alive but its backend could not be established", label, peer.URL))
		case peerHealthPortOwnedFunc(peer.Port):
			decision.block(fmt.Sprintf("%s (%s) did not answer, but port %d is held and its backend could not be established", label, peer.URL, peer.Port))
		}
	}
	return decision
}

// logLegacyPeerStandby records the actionable reason a daemon is standing by.
func (d *Daemon) logLegacyPeerStandby(decision legacyPeerDecision) {
	// Deliberately not "serves the same backend": an unreadable profile blocks
	// without that proof, and the per-peer details carry the actual evidence.
	d.logger.Warn("another Multica daemon on this machine may be serving this backend without runtime coordination; "+
		"standing by so this process cannot activate a runtime a peer may already be serving. "+
		"Update that daemon (or stop it) to hand the runtime over.",
		"peers", decision.Peers)
}

// yieldRuntimesToLegacyPeer handles a live uncoordinated peer that appears AFTER
// this process started serving runtimes - conflict mitigation, not coordination.
// The legacy binary takes no ownership claim and cannot see this process's
// standby state, so nothing here can stop it from registering the shared runtime
// (same machine-scoped daemon_id) or running its own startup work against it,
// orphan recovery included. This transition only stops THIS daemon from making
// the collision worse:
//
//  1. close the claim gate and let the claims that already entered finish their
//     ClaimTask -> dispatch step (enterLegacyPeerStandby);
//  2. per workspace, under its registration lock, drop local runtime tracking and
//     release the logical ownership claims, so a register response already in
//     flight cannot republish what was just given up (yieldTrackedRuntimes);
//  3. nudge the runtime-set watchers so heartbeat and poll supervisors re-derive
//     the empty set immediately;
//  4. stay in standby until the peer is gone or upgraded; resumeAfterLegacyPeer
//     reopens the gate and the normal reconcile re-registers the runtimes.
//
// It never Deregisters these runtimes: both processes share the machine-scoped
// daemon identity, so the row it would take offline is the row the legacy peer is
// serving (GH #8280's sibling-shutdown failure through another path), and the
// shared row simply stops being heartbeated here.
//
// Tasks already executing are not cancelled by this daemon. The drain covers the
// claim transition only; a task whose handleTask goroutine has not resolved its
// runtime yet keeps the runtime_offline retry documented there.
func (d *Daemon) yieldRuntimesToLegacyPeer(ctx context.Context, decision legacyPeerDecision) {
	if !d.enterLegacyPeerStandby(ctx) {
		// The daemon is shutting down: the claim gate stays closed, which is the
		// safe direction, and there is no point moving ownership around.
		return
	}

	d.mu.Lock()
	workspaceIDs := make([]string, 0, len(d.workspaces))
	for id := range d.workspaces {
		workspaceIDs = append(workspaceIDs, id)
	}
	d.mu.Unlock()
	sort.Strings(workspaceIDs)

	// One workspace at a time, each under its own register lock: a registration
	// for that workspace either completed before this section or runs after it,
	// so it cannot republish a runtime this transition has just given up.
	yielded := 0
	for _, workspaceID := range workspaceIDs {
		_ = d.withWorkspaceRegisterLock(workspaceID, func() error {
			yielded += len(d.yieldTrackedRuntimes(workspaceID))
			return nil
		})
	}
	if yielded == 0 {
		return
	}
	d.notifyRuntimeSetChanged()
	d.logger.Warn("gave up local runtime ownership to a daemon that cannot coordinate; "+
		"the shared server runtimes stay online for that peer, which is still serving them",
		"peers", decision.Peers, "runtimes", yielded)
}

// yieldTrackedRuntimes gives up this process's local serving authority for one
// workspace: the runtime rows leave local tracking, and the logical ownership
// claims are released so a coordinating peer can take them over. The server is
// not told anything.
//
// The caller must hold the workspace's register lock (workspaceRegisterLock) —
// the drop and the release are one ordered step against every registration and
// profile-drift apply for that workspace.
func (d *Daemon) yieldTrackedRuntimes(workspaceID string) []droppedRuntime {
	dropped := d.dropTrackedRuntimeRows(workspaceID)
	for _, rt := range dropped {
		d.releaseRuntimeOwnership(rt.Target)
	}
	return dropped
}

// resumeAfterLegacyPeer clears the standby gate once no uncoordinated peer is
// visible any more (it was stopped, or upgraded to a release that takes the
// ownership claim). Registration and task claiming then follow the normal path
// again.
func (d *Daemon) resumeAfterLegacyPeer() {
	if !d.legacyPeerStandby.Swap(false) {
		return
	}
	d.logger.Info("no uncoordinated peer remains; resuming runtime ownership and task claiming")
}
