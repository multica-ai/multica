// @vitest-environment node
import type {
  AgentActivityInteraction,
  AgentActivitySessionInput,
  AgentActivityTurn
} from "@tutti-os/agent-activity-core";
import { autorun } from "mobx";
import { describe, expect, it } from "vitest";
import { TestAgentSessionsModel as AgentSessionsModel } from "../shared/testing/TestAgentSessionsModel";

const session = (
  id: string,
  patch: Partial<AgentActivitySessionInput> = {}
): AgentActivitySessionInput => ({
  agentSessionId: id,
  activeTurnId: null,
  cwd: "/workspace",
  latestTurnInteractions: [],
  pendingInteractions: [],
  provider: "codex",
  title: id,
  workspaceId: "workspace",
  ...patch
});
const turn = (
  id: string,
  patch: Partial<AgentActivityTurn> = {}
): AgentActivityTurn => ({
  agentSessionId: "a",
  turnId: id,
  origin: "user_prompt",
  phase: "settled",
  startedAtUnixMs: 1,
  updatedAtUnixMs: 1,
  ...patch
});
const interaction = (
  status: "pending" | "answered"
): AgentActivityInteraction => ({
  agentSessionId: "a",
  turnId: "t",
  requestId: "question",
  kind: "question",
  status,
  createdAtUnixMs: 1,
  updatedAtUnixMs: 1
});

describe("AgentSessionsModel", () => {
  it("keeps unchanged entities and does not notify observers of an identical write", () => {
    const model = new AgentSessionsModel();
    model.replaceHistory(session("a", { latestTurn: turn("t") }), [turn("t")]);
    const first = model.session("a"),
      firstTurn = model.turn("a", "t"),
      firstTurns = model.turns("a");
    let renders = 0;
    const stop = autorun(() => {
      model.session("a");
      model.turns("a");
      renders++;
    });
    model.replaceHistory(session("a", { latestTurn: turn("t") }), [turn("t")]);
    expect(model.session("a")).toBe(first);
    expect(model.turn("a", "t")).toBe(firstTurn);
    expect(model.turns("a")).toBe(firstTurns);
    expect(renders).toBe(1);
    stop();
  });

  it("takes the last report and replaces history including removed interactions", () => {
    const model = new AgentSessionsModel();
    model.replaceHistory(
      session("a", {
        activeTurnId: "t",
        activeTurn: turn("t", { phase: "running" }),
        pendingInteractions: [interaction("pending")]
      }),
      [turn("t", { phase: "running" })]
    );
    expect(model.hasPendingInteractions("a")).toBe(true);
    model.replaceHistory(session("a", { updatedAtUnixMs: 0 }), []);
    expect(model.activeTurn("a")).toBeNull();
    expect(model.turns("a")).toEqual([]);
    expect(model.interactions("a")).toEqual([]);
  });

  it("retains omitted conversations and refuses deleted ones until explicitly restored", () => {
    const model = new AgentSessionsModel();
    model.upsertSessions([session("a"), session("b")]);
    model.upsertSessions([session("a")]);
    expect(model.session("b")).not.toBeNull();
    model.removeSession("b");
    model.upsertSession(session("b"));
    expect(model.session("b")).toBeNull();
    model.restoreSession("b");
    model.upsertSession(session("b"));
    expect(model.session("b")).not.toBeNull();
    model.upsertSession(session("   "));
    expect(model.session(" ")).toBeNull();
  });

  it("drops unreported pending interactions and retains answered interactions", () => {
    const model = new AgentSessionsModel();
    model.upsertSession(
      session("a", {
        latestTurn: turn("t"),
        pendingInteractions: [interaction("pending")]
      })
    );
    model.upsertSession(
      session("a", { latestTurnInteractions: [interaction("answered")] })
    );
    model.upsertSession(session("a"));
    expect(model.interaction("a", "t", "question")?.status).toBe("answered");
    model.upsertSession(
      session("a", { pendingInteractions: [interaction("pending")] })
    );
    model.upsertSession(session("a"));
    expect(model.hasPendingInteractions("a")).toBe(false);
    expect(model.interactions("a")).toEqual([]);
  });

  it("only invalidates readers of the conversation that changed", () => {
    const model = new AgentSessionsModel();
    model.upsertSessions([session("a"), session("b")]);
    let changes = 0;
    const stop = autorun(() => {
      model.consumerSession("b");
      changes++;
    });
    model.upsertSession(session("a", { title: "changed" }));
    expect(changes).toBe(1);
    model.setRuntimeAvailability("missing", { state: "available" });
    expect(model.runtimeAvailability("missing")).toBeNull();
    stop();
  });
});

it.each([
  ["running", undefined, "working", "active_turn"],
  ["waiting", undefined, "waiting", "active_turn"],
  ["settled", "completed", "completed", null],
  ["settled", "failed", "failed", null],
  ["settled", "interrupted", "canceled", null],
  ["settled", "canceled", "canceled", null]
] as const)(
  "derives display and admission from phase %s and outcome %s",
  (phase, outcome, displayStatus, blockedReason) => {
    const model = new AgentSessionsModel();
    const current = turn("t", { phase, ...(outcome ? { outcome } : {}) });
    model.upsertSession(
      session("a", {
        activeTurnId: phase === "settled" ? null : "t",
        activeTurn: phase === "settled" ? null : current,
        latestTurn: current
      })
    );
    expect(model.consumerSession("a")?.displayStatus).toBe(displayStatus);
    expect(model.submitAvailability("a")).toEqual(
      blockedReason
        ? { state: "blocked", reason: blockedReason }
        : { state: "available" }
    );
  }
);

it("keeps pending interactions blocking admission after their turn settles and prioritizes runtime availability", () => {
  const model = new AgentSessionsModel();
  model.upsertSession(
    session("a", {
      latestTurn: turn("t", { outcome: "completed" }),
      pendingInteractions: [interaction("pending")]
    })
  );
  expect(model.consumerSession("a")?.displayStatus).toBe("waiting");
  expect(model.submitAvailability("a")).toEqual({
    state: "blocked",
    reason: "waiting"
  });
  model.setRuntimeAvailability("a", {
    state: "blocked",
    reason: "agent_capability_unavailable"
  });
  expect(model.submitAvailability("a")).toEqual({
    state: "blocked",
    reason: "agent_capability_unavailable"
  });
});

it("orders turns deterministically and groups hidden child interactions under their root", () => {
  const model = new AgentSessionsModel();
  model.replaceHistory(session("a"), [
    turn("z", { startedAtUnixMs: 2 }),
    turn("b"),
    turn("a")
  ]);
  expect(model.turns("a").map((entry) => entry.turnId)).toEqual([
    "a",
    "b",
    "z"
  ]);
  const childTurn = turn("child-turn", { agentSessionId: "child" });
  const childInteraction = {
    ...interaction("pending"),
    agentSessionId: "child",
    turnId: "child-turn"
  };
  model.upsertSession(
    session("child", {
      kind: "child",
      rootAgentSessionId: "a",
      visible: false,
      latestTurn: childTurn,
      pendingInteractions: [childInteraction]
    })
  );
  expect(
    model.consumerSessions.map((entry) => entry.session.agentSessionId)
  ).toEqual(["a"]);
  expect(model.rootSessionIdsWithPendingInteractions).toEqual(["a"]);
  expect(model.family("a").pendingInteractions).toEqual([childInteraction]);
  model.removeSession("child");
  expect(model.family("a").childSessions).toEqual([]);
  expect(model.rootSessionIdsWithPendingInteractions).toEqual([]);
});
