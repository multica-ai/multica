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
3. Under `claimMu`, pause new claims, then wait for `claimsInFlight == 0 && activeTasks == 0` without cancelling active contexts.
4. Preserve the existing handoff invariant: `activeTasks` increments before `claimsInFlight` decrements, so the drain cannot observe false idle.
5. If the requester disconnects, release only its barrier and resume claims. Conflicting drain/auto-update requests return 409.
6. After idle, cancel the daemon and use the existing CLI start path, preserving binary, profile, foreground mode, and overrides.

## Scope and Verification

Change only daemon lifecycle/health code, focused tests, and the daemon command doc. Do not change default restart, server recovery, executable trust, or PR #5494 ownership semantics.

Cover active work, claim handoff, client cancellation, barrier conflict, dedicated endpoint selection, immediate-shutdown regression, focused package tests, race tests, vet, and formatting.
