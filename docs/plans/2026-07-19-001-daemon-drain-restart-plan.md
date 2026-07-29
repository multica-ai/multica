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

## Goal

Add `multica daemon restart --drain`: stop claiming, let accepted work finish, then restart through the invoking CLI. Plain restart remains immediate for compatibility.

## Problem

`daemon restart` currently calls the local `/shutdown` endpoint. That cancels the daemon root context, which is also passed to active agent tasks. The subsequent 30-second wait only waits for cancelled task goroutines to clean up; it does not let agent work finish.

`runtime_recovery` can retry the interrupted row, but still records a failure, redispatches work, and can spend more model tokens. It is crash recovery, not safe restart.

## Design

1. Add an opt-in `--drain` flag to `daemon restart`.
2. Use a dedicated local endpoint so an older daemon returns 404 rather than ignoring a query parameter and stopping immediately.
3. Under `claimMu`, represent the shared claim barrier with one explicit owner: `none`, `drain`, or `update`. Any non-`none` owner pauses new claims. This single state replaces independent `pauseClaims`, `draining`, and `updating` flags so acquisition and release cannot disagree about which lifecycle operation is active.
4. Manual drain acquires the `drain` owner even while claims or tasks are active, then waits for `claimsInFlight == 0 && activeTasks == 0` without cancelling active contexts. Preserve the existing handoff invariant: `activeTasks` increments before `claimsInFlight` decrements, so the drain cannot observe false idle.
5. Periodic auto-update acquires the `update` owner only while fully idle. Heartbeat-triggered update acquires the same `update` owner without requiring idle, preserving its existing immediate-update behavior while preventing it from bypassing an active drain.
6. A heartbeat update that cannot acquire ownership remains pending and is retried by a later heartbeat; claim-barrier contention is not reported as an update failure. A drain that finds `update` ownership already held returns `409 Conflict`.
7. Release is owner-specific. A cancelled drain releases only `drain`; a failed update releases only `update`. A successful drain or update keeps ownership through root-context cancellation so no new claim can enter during shutdown.
8. After drain reaches idle, cancel the daemon and use the existing CLI start path, preserving binary, profile, foreground mode, and overrides.

## Ownership Transitions

| Current owner | Request | Result |
| --- | --- | --- |
| `none` | new task claim | Claim enters and increments `claimsInFlight` |
| `none` | manual drain | Acquire `drain`, pause new claims, wait for accepted work |
| `none`, idle | periodic auto-update | Acquire `update` and run the upgrade |
| `none` | heartbeat update | Acquire `update` and run the existing immediate upgrade path |
| `drain` | task claim | Reject the claim attempt |
| `drain` | periodic or heartbeat update | Defer without running or reporting failure |
| `update` | manual drain | Return `409 Conflict` |
| `update` | another update | Defer without starting a second upgrade |
| `drain` | requester cancellation | Release `drain` and resume claims |
| `update` | upgrade failure | Release `update` and resume claims |
| `drain` or `update` | successful shutdown/restart | Retain ownership until process exit |

Plain `multica daemon restart` remains an intentionally immediate shutdown and does not participate in this opt-in drain protocol.

## Concurrency Verification

Add deterministic tests for both reviewer-reported orderings using channel-gated operations rather than timing-only assertions:

1. **Drain then heartbeat update:** keep one task active, start `/shutdown/drain`, wait until `drain` owns the barrier, then invoke `handleUpdate`. Assert that the update function and restart are not called, the drain retains ownership, and the update is not reported failed.
2. **Heartbeat update then drain:** start `handleUpdate` with an update function blocked after `update` ownership is acquired, then call `/shutdown/drain`. Assert `409 Conflict`; release the update and verify its existing completion/restart path.

Keep the existing auto-update, claim-handoff, drain cancellation, concurrent-drain, endpoint compatibility, and immediate-shutdown regression tests. Run the focused package tests under the race detector because the contract is specifically about cross-goroutine ownership and ordering.

## Scope and Verification

Change only daemon lifecycle/health code, focused tests, and the daemon command doc. Do not change default restart, server recovery, executable trust, or PR #5494 ownership semantics.

Cover active work, claim handoff, client cancellation, all owner transition conflicts, dedicated endpoint selection, immediate-shutdown regression, focused package tests, race tests, vet, and formatting.
