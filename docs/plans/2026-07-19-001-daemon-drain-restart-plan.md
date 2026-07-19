---
title: Drain-Aware Daemon Restart - Plan
type: feat
date: 2026-07-19
topic: daemon-drain-restart
artifact_contract: ce-unified-plan/v1
artifact_readiness: implementation-ready
execution: code
---

# Drain-Aware Daemon Restart - Plan

## Goal Capsule

- **Objective:** let an explicit daemon restart wait for active agent work to finish instead of cancelling it.
- **User surface:** `multica daemon restart --drain`.
- **Compatibility:** plain `multica daemon restart` keeps its current immediate behavior.
- **Stop conditions:** the daemon stops claiming before the idle check, in-flight claims and active tasks finish naturally, cancellation resumes claims, and the replacement starts through the invoking CLI binary.

## Problem

`daemon restart` currently calls the local `/shutdown` endpoint. That cancels the daemon root context, which is also passed to active agent tasks. The subsequent 30-second wait only waits for cancelled task goroutines to clean up; it does not let agent work finish.

The server can recover the interrupted row with `runtime_recovery`, but that still creates a failed attempt, performs another dispatch, and may spend more model tokens resuming the task. Recovery is a crash safety net, not a safe restart protocol.

## Design

1. Add an opt-in `--drain` flag to `daemon restart`.
2. Extend the local shutdown request with a drain mode. The request remains open while draining.
3. Under the existing claim mutex, set a manual drain barrier before checking liveness. New claims are rejected; a claim already in flight must finish handing tasks to `activeTasks` before the barrier can observe idle.
4. Wait until both `claimsInFlight == 0` and `activeTasks == 0`.
5. Respond to the caller, then asynchronously cancel the daemon root context so the response can flush.
6. The CLI waits for the old health endpoint to disappear and calls the existing start path. This preserves profile, foreground mode, overrides, and the invoking binary.

The handler releases the manual barrier if its request context is cancelled before the daemon reaches idle. A second drain request, or a drain request while another claim barrier owns the pause, returns a conflict instead of stealing state.

## State and Race Invariants

- `pauseClaims` and `claimsInFlight` remain guarded by `claimMu`.
- A dispatcher increments `activeTasks` before it decrements `claimsInFlight`; therefore observing both counters at zero while the barrier is held proves that no later task can appear.
- Active work receives the original live root context throughout the drain.
- Only after the counters reach zero does normal shutdown cancel the root context.
- Client cancellation clears only the barrier owned by that drain request.

## Scope

Expected implementation files:

- `server/cmd/multica/cmd_daemon.go`
- `server/cmd/multica/cmd_daemon_test.go`
- `server/internal/daemon/daemon.go`
- `server/internal/daemon/health.go`
- `server/internal/daemon/health_test.go`
- `apps/docs/content/docs/daemon-runtimes.mdx`

Out of scope:

- changing the default semantics of `daemon restart`;
- changing server-side `runtime_recovery`;
- fixing the unconfirmed one-off `signal: killed` startup failure;
- accepting a caller-supplied executable path through the unauthenticated localhost endpoint;
- changing the cross-profile ownership work in PR #5494.

## Test Plan

- CLI: `--drain` selects drain shutdown and still starts through the existing start path.
- Daemon: active tasks keep the request blocked and new claims are paused.
- Daemon: the claim-to-active handoff cannot create a false idle window.
- Daemon: cancelling the client request releases the barrier and claims resume.
- Daemon: a concurrent drain request is rejected.
- Regression: immediate shutdown and plain restart retain their current behavior.
- Verification: focused package tests, race tests for the daemon package, `go test ./server/cmd/multica ./server/internal/daemon`, `go vet` on touched packages, and `gofmt -l`.
