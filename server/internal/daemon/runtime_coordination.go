package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/internal/cli"
)

// Rolling-upgrade safety for the runtime-owner claim (GH #8280).
//
// The owner claim is an OS lock, and an OS lock only excludes processes that know
// to take it. A daemon from a previous release registers, claims, heartbeats and
// recovers orphans without ever looking at it, so two daemons of different
// versions on one machine would both serve the same runtime - the exact failure
// this revision exists to prevent. Nothing inside the lock can detect that,
// because the legacy process is not misbehaving by its own rules.
//
// So a new daemon refuses to ACTIVATE a runtime while it can see a live
// same-machine, same-backend peer that does not advertise the coordination
// capability. The advertisement is additive on the health surface every daemon
// already exposes per profile, so no new protocol is needed.
//
// This is a fail-closed standby rather than a startup abort: the new daemon keeps
// probing and takes over once the legacy peer is gone, which is the same takeover
// path an owner crash uses.

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

// runtimeCoordinationPeers lists the health endpoints of every OTHER Multica
// profile on this machine that points at the same normalized backend.
//
// The profile directories are the machine-local inventory of daemon processes:
// each profile owns one config (its backend) and one health port, both derived
// from names the daemon itself computed, so no service discovery or registry is
// needed. A profile whose config cannot be read is skipped rather than guessed
// at - an unreadable peer is indistinguishable from an absent one, and treating
// it as absent is the unsafe direction.
func runtimeCoordinationPeers(backend string, ownProfile string) []runtimeCoordinationPeer {
	root, err := cli.ProfileDir("")
	if err != nil {
		return nil
	}
	entries, err := os.ReadDir(filepath.Join(root, "profiles"))
	if err != nil {
		return nil
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
		cfg, err := cli.LoadCLIConfigForProfile(name)
		if err != nil {
			continue
		}
		raw := strings.TrimSpace(cfg.ServerURL)
		if raw == "" {
			continue
		}
		peerBackend, err := NormalizeServerBaseURL(raw)
		if err != nil || peerBackend != backend {
			continue
		}
		port := healthPortForProfile(name)
		peers = append(peers, runtimeCoordinationPeer{
			Profile: name,
			URL:     fmt.Sprintf("http://127.0.0.1:%d/health", port),
			Port:    port,
		})
	}
	return peers
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
	return peerCoordinationStatus{
		Alive:       true,
		Coordinates: ok && int(version) >= RuntimeCoordinationVersion,
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
// health port.
//
// This is the local half of the mixed-version rule, and it is deliberately the
// port rather than the pid file: a port is exclusive, so a listener proves a
// daemon is alive right now, while a pid can be recycled by an unrelated
// program. It is also a signal the daemon lifecycle already maintains - the
// daemon binds this port for its whole run, "daemon status" and "daemon stop"
// probe it, and a second daemon for the same profile fails to start on it - so
// no second heartbeat registry is introduced.
//
// false means "nothing is listening", which is the only local evidence that a
// same-backend profile directory is stale. That is what keeps a stopped peer
// from blocking activation forever.
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

// checkRuntimeCoordinationPeers probes every same-backend sibling and reports
// whether any of them is a live daemon that cannot be coordinated with.
//
// A coordinating peer is not a blocker: it takes the same owner claim this
// process does, so the claim - not this probe - decides which of them serves a
// runtime, and the loser reports peer-owned candidates as standby.
//
// The other two outcomes are kept apart, and that is the fail-safe part:
//
//   - a peer that answered without the capability is a live legacy daemon. It
//     does not take the claim, so nothing else can exclude it - this process has
//     to.
//   - a peer that did not answer is UNKNOWN, not absent. A known same-backend
//     profile can be slow, starting up, or unhealthy without being stopped, so
//     its health port is consulted before this process concludes anything. Only
//     a free port - proof that no daemon runs under that profile - lets
//     activation proceed.
func (d *Daemon) checkRuntimeCoordinationPeers(ctx context.Context) legacyPeerDecision {
	peers := runtimeCoordinationPeers(d.cfg.ServerBaseURL, d.cfg.Profile)
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
		switch {
		case status.Alive && status.Coordinates:
			continue
		case status.Alive:
			decision.block(fmt.Sprintf("%s (%s) is alive but does not advertise runtime coordination", label, peer.URL))
		case peerHealthPortOwnedFunc(peer.Port):
			decision.block(fmt.Sprintf("%s (%s) did not answer, but port %d is still held by a running daemon", label, peer.URL, peer.Port))
		}
	}
	return decision
}

// logLegacyPeerStandby records the actionable reason a daemon is standing by.
func (d *Daemon) logLegacyPeerStandby(decision legacyPeerDecision) {
	d.logger.Warn("another Multica daemon on this machine serves the same backend but does not support runtime coordination; "+
		"standing by so this process cannot activate a runtime that peer is already serving. "+
		"Update that daemon (or stop it) to hand the runtime over.",
		"peers", decision.Peers)
}

// legacyPeerYieldTimeout bounds the deregistration burst a standby transition
// makes. The claims are released whether or not it succeeds, with the server's
// stale-heartbeat sweep as the backstop.
const legacyPeerYieldTimeout = 10 * time.Second

// yieldRuntimesToLegacyPeer is the transition for a live uncoordinated peer that
// appears AFTER this process already started serving runtimes.
//
// Refusing to activate is only half the mixed-version guarantee. Simply
// returning from the sync leaves this process claiming tasks, heartbeating and
// re-registering runtimes that the legacy peer serves at the same time, which is
// the two-owners failure the whole revision exists to prevent (GH #8280). So the
// transition is explicit, and ordered:
//
//  1. close the claim gate first, so no NEW task can start while both processes
//     are known live;
//  2. take this process's runtimes offline on the server;
//  3. release their ownership claims, so a peer that does coordinate can take
//     them over immediately;
//  4. stay in standby until the peer is gone or upgraded - resumeAfterLegacyPeer
//     clears the gate, and the normal reconcile re-registers what this process
//     owns from the now-empty tracked set.
//
// Tasks already running are NOT cancelled: the server has routed them and this
// process is the only one that can finish them, so the boundary this revision
// fixes is "no new work starts", not "drain in flight". That boundary is
// deliberate and covered by TestRuntimeCoordination_LateLegacyPeerYieldsRuntimes.
func (d *Daemon) yieldRuntimesToLegacyPeer(ctx context.Context, decision legacyPeerDecision) {
	d.legacyPeerStandby.Store(true)

	d.mu.Lock()
	workspaceIDs := make([]string, 0, len(d.workspaces))
	for id := range d.workspaces {
		workspaceIDs = append(workspaceIDs, id)
	}
	d.mu.Unlock()
	sort.Strings(workspaceIDs)

	yieldCtx, cancel := context.WithTimeout(ctx, legacyPeerYieldTimeout)
	defer cancel()

	// One workspace at a time, each under its own register lock, like every
	// other cleanup: the rows are dropped locally, the server is told, and only
	// then are the claims released (deregisterDroppedRuntimes).
	yielded := 0
	for _, workspaceID := range workspaceIDs {
		dropped := d.dropTrackedRuntimeRows(workspaceID)
		if len(dropped) == 0 {
			continue
		}
		_ = d.withWorkspaceRegisterLock(workspaceID, func() error {
			d.deregisterDroppedRuntimes(yieldCtx, workspaceID, dropped, "uncoordinated peer", nil)
			return nil
		})
		yielded += len(dropped)
	}
	if yielded == 0 {
		return
	}
	d.notifyRuntimeSetChanged()
	d.logger.Warn("gave up owned runtimes to a daemon that cannot coordinate; standing by until it is gone or upgraded",
		"peers", decision.Peers, "runtimes", yielded)
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
