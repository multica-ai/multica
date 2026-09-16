// Package daemon: per-task in-memory request-secret broker for WS-11 P1
// per-visitor identity passthrough.
//
// Why a broker instead of the child env: the redeemed visitor secret is a raw
// bearer credential for the accountable human. It must live only on the
// server↔daemon trusted boundary and be handed to exactly ONE whitelisted
// query child (Accel / Titan via identity_shim.py), never to the general Agent
// runtime. Putting it in agentEnv would seed the whole runtime environment and
// the Codex shell-env allowlist, so any shell command or tool the agent runs
// could read it. Instead the daemon:
//
//  1. redeems the handle over the daemon-authenticated server endpoint at
//     launch time and stores the plaintext HERE, keyed by task id;
//  2. serves it exactly once (or until swept) to the whitelisted child over the
//     daemon's 127.0.0.1 loopback control server, gated by the task's own token;
//  3. never writes it into agentEnv, the Codex allowlist, the prompt, task /
//     comment bodies, normal logs, or any file.
//
// Entries are TTL-bound and one-shot: a Take removes the entry so a leaked
// broker port cannot be replayed, and a periodic sweep drops anything a task
// left behind (e.g. a child that never launched). The broker holds nothing for
// tasks without a visitor credential.
package daemon

import (
	"crypto/subtle"
	"sync"
	"time"
)

// requestSecretBrokerTTL bounds how long a stashed secret is fetchable. It is
// generous relative to child-launch latency but short enough that a secret for
// a task that never spawned its query child is swept quickly. The server-side
// requestsecret.Store TTL (DefaultTTL) already bounds the handle; this is a
// second, independent bound on the plaintext once it crosses into the daemon.
const requestSecretBrokerTTL = 5 * time.Minute

// brokerEntry is one stashed secret plus its expiry and the task token a
// fetcher must present to take it.
type brokerEntry struct {
	secret    string
	token     string
	expiresAt time.Time
}

// requestSecretBroker holds redeemed visitor secrets keyed by task id. All
// access is guarded by mu; the map never contains an empty secret.
type requestSecretBroker struct {
	mu    sync.Mutex
	ttl   time.Duration
	now   func() time.Time
	items map[string]brokerEntry
}

// newRequestSecretBroker builds an empty broker with the given TTL (0 → default).
func newRequestSecretBroker(ttl time.Duration) *requestSecretBroker {
	if ttl <= 0 {
		ttl = requestSecretBrokerTTL
	}
	return &requestSecretBroker{
		ttl:   ttl,
		now:   time.Now,
		items: make(map[string]brokerEntry),
	}
}

// Put stashes secret for taskID, fetchable only by a caller presenting token,
// with a fresh TTL. An empty taskID, secret, or token is a no-op (fail-closed:
// an entry with no token could be taken by any local caller). Overwrites any
// prior entry so a re-launched task always gets the freshest credential.
func (b *requestSecretBroker) Put(taskID, token, secret string) {
	if b == nil || taskID == "" || token == "" || secret == "" {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.items[taskID] = brokerEntry{secret: secret, token: token, expiresAt: b.now().Add(b.ttl)}
}

// Take returns the secret for taskID and removes it (one-shot). It returns
// ("", false) when there is no live entry — missing, expired (which it also
// sweeps), OR the presented token does not match the entry's bound token. A
// token mismatch does NOT burn the entry so the legitimate child can still
// fetch. Callers must treat !ok as fail-closed.
func (b *requestSecretBroker) Take(taskID, token string) (string, bool) {
	if b == nil || taskID == "" || token == "" {
		return "", false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	e, ok := b.items[taskID]
	if !ok {
		return "", false
	}
	// Expired entries are removed and reported as absent.
	if !e.expiresAt.After(b.now()) {
		delete(b.items, taskID)
		return "", false
	}
	// Wrong token: reject WITHOUT burning the entry (constant-time compare).
	if subtle.ConstantTimeCompare([]byte(e.token), []byte(token)) != 1 {
		return "", false
	}
	delete(b.items, taskID)
	return e.secret, true
}

// Discard drops any entry for taskID without returning it. Called when a task
// ends (or fails to launch its child) so a secret never lingers past its task.
func (b *requestSecretBroker) Discard(taskID string) {
	if b == nil || taskID == "" {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.items, taskID)
}

// sweep drops every expired entry. Cheap enough to call opportunistically.
func (b *requestSecretBroker) sweep() {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.now()
	for k, e := range b.items {
		if !e.expiresAt.After(now) {
			delete(b.items, k)
		}
	}
}

// len returns the number of live-or-not entries; test-only visibility.
func (b *requestSecretBroker) len() int {
	if b == nil {
		return 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.items)
}
