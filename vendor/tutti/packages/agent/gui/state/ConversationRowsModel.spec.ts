// @vitest-environment node
import { TranscriptModel } from "@multica/conversation-client/models";
import type {
  AgentActivityMessage,
  AgentActivityTurn
} from "@tutti-os/agent-activity-core";
import { autorun, observable, runInAction } from "mobx";
import { describe, expect, it } from "vitest";
import type { ConversationServices } from "../conversationServices";
import { createTestRuntimeState } from "../shared/testing/createTestRuntimeState";
import { ConversationRowsModel } from "./ConversationRowsModel";

function fixture(totalTurnCount?: () => number) {
  const services = createTestRuntimeState("w");
  const transcript = new TranscriptModel<AgentActivityMessage>();
  const turns: AgentActivityTurn[] = ["t1", "t2"].map((turnId, index) => ({
    agentSessionId: "s",
    turnId,
    origin: "user_prompt",
    phase: "settled",
    outcome: "completed",
    startedAtUnixMs: index + 1,
    updatedAtUnixMs: index + 2,
    settledAtUnixMs: index + 2
  }));
  services.sessions.replaceHistory(
    {
      agentSessionId: "s",
      workspaceId: "w",
      provider: "codex",
      cwd: "/workspace",
      title: "Test",
      latestTurn: turns[1],
      latestTurnInteractions: [],
      pendingInteractions: []
    },
    turns
  );
  const runtime = {
    ...services,
    transcript
  } as unknown as ConversationServices;
  const rows = new ConversationRowsModel(runtime, "s", {
    workspaceId: "w",
    userId: "u",
    totalTurnCount
  });
  const message = (
    id: string,
    turnId: string,
    body: string,
    sequence: number,
    role = "agent"
  ): AgentActivityMessage => ({
    agentSessionId: "s",
    messageId: id,
    turnId,
    role,
    kind: "message",
    version: sequence,
    sequence,
    occurredAtUnixMs: sequence,
    payload: { content: body }
  });
  const messages = [
    message("q1", "t1", "First", 1, "user"),
    message("a1", "t1", "One", 2),
    message("q2", "t2", "Second", 3, "user"),
    message("a2", "t2", "Two", 4)
  ];
  transcript.commit("s", messages);
  return { services, transcript, rows, message, messages };
}

describe("ConversationRowsModel", () => {
  it("uses the real turn count when the initial page has only five turns", () => {
    const total = observable.box(7);
    const { services, rows, transcript, messages, message } = fixture(() => total.get());
    const page = [
      ...messages,
      message("q3", "t3", "Third", 5, "user"),
      message("q4", "t4", "Fourth", 6, "user"),
      message("q5", "t5", "Fifth", 7, "user")
    ];
    transcript.commit("s", page.map((value) => value.messageId === "a1"
      ? { ...value, payload: { content: "Large reply ".repeat(10_000) } } : value));
    let virtualized = false;
    const stop = autorun(() => { virtualized = rows.shouldVirtualize; });
    try {
      expect(rows.turnIds).toHaveLength(5);
      expect(virtualized).toBe(false);
      runInAction(() => total.set(8));
      expect(virtualized).toBe(true);
      transcript.commit("s", page);
      expect(virtualized).toBe(false);
      runInAction(() => total.set(100));
      expect(virtualized).toBe(true);
      runInAction(() => total.set(7));
      expect(virtualized).toBe(false);
    } finally {
      stop();
      services.dispose();
    }
  });

  it("recomputes only the changed turn and preserves unrelated row identities", () => {
    const { rows, transcript, messages, message } = fixture();
    let firstReads = 0,
      secondReads = 0,
      layoutReads = 0;
    const stop1 = autorun(() => {
      rows.rowKeys("t1").forEach((key) => rows.row(key));
      firstReads++;
    });
    const stop2 = autorun(() => {
      rows.rowKeys("t2").forEach((key) => rows.row(key));
      secondReads++;
    });
    const stopLayout = autorun(() => {
      rows.structure;
      layoutReads++;
    });
    const first = rows.row(rows.rowKeys("t1")[0]!);
    transcript.commit("s", [
      ...messages.slice(0, 3),
      message("a2", "t2", "Two streamed tokens", 4)
    ]);
    expect(firstReads).toBe(1);
    expect(secondReads).toBe(2);
    expect(layoutReads).toBe(1);
    expect(rows.row(rows.rowKeys("t1")[0]!)).toBe(first);
    expect(JSON.stringify(rows.conversation?.rows)).toContain(
      "Two streamed tokens"
    );
    stop1();
    stop2();
    stopLayout();
  });

  it("prepends history without changing existing turn keys or locator targets", () => {
    const { rows, transcript, messages, message } = fixture();
    const stop = autorun(() => {
      rows.structure;
      rows.locatorItems;
    });
    const oldKeys = rows.rowKeys("t2");
    transcript.commit("s", [
      message("old", "t0", "Earlier", 0, "user"),
      ...messages
    ]);
    expect(rows.turnIds).toEqual(["t0", "t1", "t2"]);
    expect(rows.rowKeys("t2")).toEqual(oldKeys);
    expect(rows.locatorItems.map((item) => item.summary)).toEqual([
      "Earlier",
      "First",
      "Second"
    ]);
    stop();
  });

  it("updates layout when row semantics or the editable prompt changes", () => {
    const { rows, transcript, messages, message } = fixture();
    let changes = 0;
    const stop = autorun(() => {
      rows.structure;
      changes++;
    });
    transcript.commit("s", [
      message("q1", "t1", "Edited prompt", 1, "user"),
      ...messages.slice(1)
    ]);
    expect(changes).toBe(2);
    expect(rows.locatorItems[0]?.summary).toBe("Edited prompt");
    transcript.commit("s", messages.slice(0, 2));
    expect(rows.turnIds).toEqual(["t1"]);
    expect(rows.row("missing")).toBeNull();
    stop();
  });
});

it("updates native fork controls when only canonical lifecycle capabilities change", () => {
  const { services, rows } = fixture();
  let changes = 0;
  const stop = autorun(() => {
    rows.structure;
    changes++;
  });
  const current = services.sessions.presentedSession("s")!;
  services.sessions.upsertSession({
    ...current,
    lifecycleCapabilities: {
      ...current.lifecycleCapabilities,
      forkThroughTurn: true
    }
  });
  expect(
    rows.structure?.sourceDetail.session.lifecycleCapabilities.forkThroughTurn
  ).toBe(true);
  expect(changes).toBe(2);
  stop();
  services.dispose();
});
