import assert from "node:assert/strict";
import test from "node:test";
import { createInitialAgentSessionEngineState, rootEngineReducer } from "./rootReducer.ts";
import { canonicalInteractionKey, canonicalTurnKey } from "./sessionEntityKeys.ts";

const session = (id: string, overrides: Record<string, unknown> = {}) => ({
  activeTurnId: null, agentSessionId: id, cwd: "/w", latestTurnInteractions: [], pendingInteractions: [], provider: "codex", title: id, workspaceId: "w", ...overrides
});
const turn = (id: string, phase: "running" | "settled" = "settled") => ({
  agentSessionId: "s", turnId: id, origin: "user_prompt" as const, phase, startedAtUnixMs: 1, updatedAtUnixMs: 1
});
const interaction = (status: "pending" | "answered") => ({
  agentSessionId: "s", turnId: "t1", requestId: "q", kind: "question" as const, status, createdAtUnixMs: 1, updatedAtUnixMs: 1
});
const initial = createInitialAgentSessionEngineState();
const apply = (state: typeof initial, ...intents: Parameters<typeof rootEngineReducer>[1][]) =>
  intents.reduce((current, intent) => rootEngineReducer(current, intent).state, state);

test("what did not change keeps its objects, and no state is produced at all", () => {
  const first = apply(initial, { type: "session/upserted", session: session("s") });
  assert.equal(apply(first, { type: "session/upserted", session: session("s") }), first);
  const history = { type: "session/historyAuthoritativeSnapshotReceived", agentSessionId: "s", workspaceId: "w", childSessions: [], historyRevision: 1, session: session("s"), turns: [turn("t1")] } as const;
  const withHistory = apply(first, history);
  assert.equal(apply(withHistory, { ...history, turns: [turn("t1")] }), withHistory);
});

test("a history replaces the conversation's turns and clears the active turn it no longer reports", () => {
  const running = apply(initial, { type: "session/upserted", session: session("s", { activeTurnId: "t1", activeTurn: turn("t1", "running"), latestTurn: turn("t1", "running") }) });
  assert.equal(running.sessionLifecycle.sessionsById.s?.activeTurnId, "t1");
  const settled = apply(running, { type: "session/historyAuthoritativeSnapshotReceived", agentSessionId: "s", workspaceId: "w", childSessions: [], historyRevision: 1, session: session("s", { latestTurn: turn("t1") }), turns: [turn("t1")] });
  assert.equal(settled.sessionLifecycle.sessionsById.s?.activeTurnId, null);
  assert.equal(settled.sessionLifecycle.turnsById[canonicalTurnKey("s", "t1")]?.phase, "settled");
  const retracted = apply(settled, { type: "session/historyAuthoritativeSnapshotReceived", agentSessionId: "s", workspaceId: "w", childSessions: [], historyRevision: 2, session: session("s"), turns: [] });
  assert.deepEqual(retracted.sessionLifecycle.turnsById, {});
});

test("a list does not delete what it omits, and a removed conversation stays removed", () => {
  const known = apply(initial, { type: "session/snapshotReceived", sessions: [session("a"), session("b")] });
  const listed = apply(known, { type: "session/snapshotReceived", sessions: [session("a")] });
  assert.ok(listed.sessionLifecycle.sessionsById.b);
  const removed = apply(listed, { type: "session/removed", agentSessionId: "b" }, { type: "session/snapshotReceived", sessions: [session("b")] }, { type: "session/upserted", session: session("b") });
  assert.equal(removed.sessionLifecycle.sessionsById.b, undefined);
  assert.equal(removed.sessionLifecycle.deletedSessionIds.b, true);
  const restored = apply(removed, { type: "session/restored", agentSessionId: "b" }, { type: "session/upserted", session: session("b") });
  assert.ok(restored.sessionLifecycle.sessionsById.b);
});

test("an interaction no longer reported as pending goes, and an answered one stays", () => {
  const key = canonicalInteractionKey("s", "t1", "q");
  const asked = apply(initial, { type: "session/upserted", session: session("s", { pendingInteractions: [interaction("pending")] }) });
  assert.equal(asked.sessionLifecycle.interactionsById[key]?.status, "pending");
  const answered = apply(asked, { type: "session/upserted", session: session("s", { latestTurnInteractions: [interaction("answered")] }) });
  assert.equal(answered.sessionLifecycle.interactionsById[key]?.status, "answered");
  const quiet = apply(asked, { type: "session/upserted", session: session("s") });
  assert.equal(quiet.sessionLifecycle.interactionsById[key], undefined);
});

test("runtime availability changes only what exists", () => {
  const known = apply(initial, { type: "session/upserted", session: session("s") });
  const blocked = apply(known, { type: "session/runtimeAvailabilityChanged", agentSessionId: "s", availability: { state: "blocked", reason: "transport_unavailable" } });
  assert.equal(blocked.sessionLifecycle.operationBySessionId.s?.runtimeAvailability.state, "blocked");
  assert.equal(apply(blocked, { type: "session/runtimeAvailabilityChanged", agentSessionId: "s", availability: { state: "blocked", reason: "transport_unavailable" } }), blocked);
  assert.equal(apply(known, { type: "session/runtimeAvailabilityChanged", agentSessionId: "missing", availability: { state: "available" } }), known);
});
