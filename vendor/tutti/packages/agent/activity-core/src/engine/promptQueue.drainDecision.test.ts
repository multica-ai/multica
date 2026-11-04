import assert from "node:assert/strict";
import test from "node:test";
import type {
  EngineQueuedPrompt,
  PromptQueueRecord
} from "./promptQueue.types.ts";
import {
  resolveQueueDrainDecision,
  type QueueDrainDecision
} from "./promptQueue.drainDecision.ts";
import type { CanonicalSubmitAvailability } from "./sessionLifecycle.availability.ts";

type HeadKind = "none" | "plain" | "guidance";
type AvailabilityKind =
  | "available"
  | "blocked_active_turn"
  | "blocked_waiting"
  | "missing";
type BlockerKind =
  | "none"
  | "in_flight"
  | "uncertain_delivery"
  | "suspended"
  | "failed_head";

const HEAD_KINDS: readonly HeadKind[] = ["none", "plain", "guidance"];
const AVAILABILITY_KINDS: readonly AvailabilityKind[] = [
  "available",
  "blocked_active_turn",
  "blocked_waiting",
  "missing"
];
const BARRIER_PENDING_VALUES: readonly boolean[] = [true, false];
const BLOCKER_KINDS: readonly BlockerKind[] = [
  "none",
  "in_flight",
  "uncertain_delivery",
  "suspended",
  "failed_head"
];

test("contract table: every head/availability/barrier/blocker combination resolves per priority order", () => {
  let assertions = 0;
  for (const head of HEAD_KINDS) {
    for (const availabilityKind of AVAILABILITY_KINDS) {
      for (const barrierPending of BARRIER_PENDING_VALUES) {
        for (const blocker of BLOCKER_KINDS) {
          const label = `head=${head} availability=${availabilityKind} barrierPending=${barrierPending} blocker=${blocker}`;
          const actual = resolveQueueDrainDecision(
            buildRecord(head, blocker),
            availabilityFor(availabilityKind),
            barrierPending
          );
          const expected = expectedDecision(
            head,
            availabilityKind,
            barrierPending,
            blocker
          );
          assert.deepEqual(actual, expected, label);
          assertions += 1;
        }
      }
    }
  }
  // 3 head kinds x 4 availability kinds x 2 barrier states x 6 blocker
  // overlays: every combination in the table was exercised above.
  assert.equal(
    assertions,
    HEAD_KINDS.length *
      AVAILABILITY_KINDS.length *
      BARRIER_PENDING_VALUES.length *
      BLOCKER_KINDS.length
  );
});

function expectedDecision(
  head: HeadKind,
  availabilityKind: AvailabilityKind,
  barrierPending: boolean,
  blocker: BlockerKind
): QueueDrainDecision {
  if (head === "none") return { kind: "blocked", reason: "no_head" };
  if (blocker === "in_flight") return { kind: "blocked", reason: "in_flight" };
  if (blocker === "uncertain_delivery") {
    return { kind: "blocked", reason: "uncertain_delivery" };
  }
  if (blocker === "suspended") return { kind: "blocked", reason: "suspended" };
  if (blocker === "failed_head") {
    return { kind: "blocked", reason: "failed_head" };
  }
  if (head === "guidance" && availabilityKind === "blocked_active_turn") {
    return { kind: "send", guidance: true };
  }
  if (barrierPending) return { kind: "blocked", reason: "barrier_pending" };
  if (availabilityKind !== "available") {
    return { kind: "blocked", reason: "unavailable" };
  }
  return { kind: "send", guidance: false };
}

function availabilityFor(kind: AvailabilityKind): CanonicalSubmitAvailability {
  switch (kind) {
    case "available":
      return { state: "available" };
    case "blocked_active_turn":
      return { state: "blocked", reason: "active_turn" };
    case "blocked_waiting":
      return { state: "blocked", reason: "waiting" };
    case "missing":
      return { state: "missing" };
  }
}

function buildRecord(head: HeadKind, blocker: BlockerKind): PromptQueueRecord {
  const headPrompt: EngineQueuedPrompt | null =
    head === "none" ? null : prompt("head-1", head === "guidance");
  const base = emptyRecord(headPrompt ? [headPrompt] : []);
  switch (blocker) {
    case "none":
      return base;
    case "in_flight":
      return {
        ...base,
        inFlight: { commandId: "command-1", kind: "send", promptId: "head-1" }
      };
    case "uncertain_delivery":
      return {
        ...base,
        uncertainDelivery: {
          commandId: "command-1",
          kind: "send",
          promptId: "head-1"
        }
      };
    case "suspended":
      return { ...base, suspendReason: "user_stop" };
    case "failed_head":
      // Not meaningful without a head to fail; falls back to no overlay so
      // the "no_head" reason still wins, exactly as the oracle expects.
      return headPrompt ? { ...base, failedPromptId: "head-1" } : base;
  }
}

function prompt(id: string, guidance: boolean): EngineQueuedPrompt {
  return {
    content: [{ type: "text", text: id }],
    createdAtUnixMs: 1,
    id,
    ...(guidance ? { guidance: true } : {})
  };
}

function emptyRecord(
  prompts: readonly EngineQueuedPrompt[]
): PromptQueueRecord {
  return {
    agentSessionId: "session-1",
    deliveryBarrierTurnId: null,
    failedPromptId: null,
    failureMessage: null,
    inFlight: null,
    pendingSendNowByPromptId: undefined,
    prompts,
    sendNextPromptId: null,
    suspendReason: null,
    uncertainDelivery: null,
    workspaceId: "workspace-1"
  };
}
