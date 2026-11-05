// @vitest-environment node
import { normalizeAgentActivitySession, type AgentActivityTurn } from "@tutti-os/agent-activity-core";
import { autorun, observable, runInAction } from "mobx";
import { describe, expect, it, vi } from "vitest";
import type { AgentConversationVM } from "../shared/agentConversation/contracts/agentConversationVM";
import {
  attachLeadingToolRowsToFollowingMessages, buildAgentParticipantTurnProjection,
  buildAgentTranscriptTurnGroups, transcriptRowKey
} from "../shared/agentConversation/components/agentTranscriptModel";
import { buildAgentTurnWorkSectionModel, findParticipantHeaderRenderKeys } from "../shared/agentConversation/components/agentTurnWorkSectionModel";
import { TranscriptPresentationModel } from "./TranscriptPresentationModel";

vi.mock("../shared/agentConversation/components/agentTurnWorkSectionModel", async (importOriginal) => {
  const actual = await importOriginal<typeof import("../shared/agentConversation/components/agentTurnWorkSectionModel")>();
  return { ...actual, buildAgentTurnWorkSectionModel: vi.fn(actual.buildAgentTurnWorkSectionModel) };
});

type Row = AgentConversationVM["rows"][number];
const message = (id: string, turnId: string | null, speaker: "user" | "assistant" = "assistant"): Row => ({
  kind: "message", id, turnId, speaker, occurredAtUnixMs: 1,
  messages: [{ kind: "message-content", id: `${id}:message`, turnId, body: id,
    presentationKind: "content", occurredAtUnixMs: 1, isTurnFinalText: speaker === "assistant" }],
  thinking: []
});
const tool = (id: string, turnId: string): Row => ({
  kind: "tool-group", id, turnId, grouped: true, calls: [], entries: [], occurredAtUnixMs: 1
});
const turn = (turnId: string, running = false): AgentActivityTurn => ({
  agentSessionId: "s", turnId, origin: "user_prompt", phase: running ? "running" : "settled",
  outcome: running ? undefined : "completed", startedAtUnixMs: 0,
  settledAtUnixMs: running ? undefined : 1000, updatedAtUnixMs: 1000
});
function conversation(rows: Row[], turns: AgentActivityTurn[], activeTurnId: string | null = null): AgentConversationVM {
  const activity = { sessionId: "s", agentProvider: "codex" } as AgentConversationVM["activity"];
  return {
    activity, workspaceRoot: null, rows,
    sourceDetail: {
      activity,
      session: normalizeAgentActivitySession({ agentSessionId: "s", workspaceId: "w", provider: "codex", cwd: "", title: "Test", activeTurnId,
        latestTurnInteractions: [], pendingInteractions: [] }),
      cwd: "", workspaceRoot: "", sessionTurns: turns, showProcessingIndicator: false,
      turns: turns.map((value) => ({ id: value.turnId, userMessage: null, userMessages: [], agentMessages: [], toolCalls: [], toolCallCount: 0, hasFailedToolCall: false, agentItems: [] }))
    }
  };
}

describe("TranscriptPresentationModel", () => {
  it.each([false, true])("preserves turnless boundaries and cross-turn participant presentation (participants=%s)", (participantMode) => {
    const rows = [message("q1", "t1", "user"), tool("tool1", "t1"), message("a1", "t1"),
      message("within", null), message("a1-later", "t1"), message("between", null),
      message("q2", "t2", "user"), tool("cross-boundary-tool", "t2"), message("a3", "t3")];
    const turns = [turn("t1"), turn("t2"), turn("t3")];
    const input = conversation(rows, turns);
    const model = TranscriptPresentationModel.fromConversation(input, participantMode);
    const display = participantMode ? attachLeadingToolRowsToFollowingMessages(rows) : rows;
    const expected = buildAgentTranscriptTurnGroups(display, display.map(transcriptRowKey));
    const actual = model.viewportGroups.map(({ key }) => {
      const group = model.group(key)!;
      return { ...group, rows: group.rows.map((entry) => ({ ...entry, rowIndex: model.rowOffset(key) + entry.rowIndex })) };
    });
    expect(actual).toEqual(expected);
    expect(model.group("t1")!.rows.some(({ row }) => row.id === "within")).toBe(true);
    expect(model.viewportGroups.some(({ key }) => key === "orphan:between")).toBe(true);
    if (participantMode) {
      const sections = new Map(expected.map((group) => [group.key, buildAgentTurnWorkSectionModel(group, turns.find((value) => value.turnId === group.turnId), false, { collapseIntermediateAssistantReplies: true })]));
      const headers = findParticipantHeaderRenderKeys(expected, display.map(transcriptRowKey), sections, buildAgentParticipantTurnProjection(display).turnIndexByRowIndex);
      for (const group of expected) {
        const section = sections.get(group.key);
        const entries = section ? [...section.leadingRows, ...section.sections.flatMap((segment) => segment.rows)] : group.rows;
        for (const entry of entries) {
          const key = "renderKey" in entry && typeof entry.renderKey === "string" ? entry.renderKey : transcriptRowKey(entry.row);
          expect(model.headerShown(group.key, key)).toBe(headers.has(key));
        }
      }
    }
  });

  it("settles the previous turn and appends a new one without rebuilding 119 historical sections", () => {
    const canonical = Array.from({ length: 120 }, (_, index) => turn(`t${index}`, index === 119));
    const records = observable.map(canonical.map((value) => [value.turnId, value]), { deep: false });
    const parts = observable.box(canonical.map((value) => conversation([message(`q-${value.turnId}`, value.turnId, "user"), message(`a-${value.turnId}`, value.turnId)], [value])), { deep: false });
    const base = observable.box(conversation([], canonical, "t119"), { deep: false });
    const model = new TranscriptPresentationModel({ parts: () => parts.get(), base: () => base.get(), turn: (id) => records.get(id) }, "s", false);
    const stop = canonical.map((value) => autorun(() => model.workSection(value.turnId)));
    const before = model.workSection("t0");
    vi.mocked(buildAgentTurnWorkSectionModel).mockClear();
    const next = turn("t120", true);
    runInAction(() => {
      records.set("t119", turn("t119"));
      records.set("t120", next);
      parts.set([...parts.get(), conversation([message("q-t120", "t120", "user"), message("a-t120", "t120")], [next])]);
      base.set(conversation([], [...canonical.slice(0, -1), turn("t119"), next], "t120"));
    });
    const stopNext = autorun(() => model.workSection("t120"));
    expect(vi.mocked(buildAgentTurnWorkSectionModel).mock.calls.map(([group]) => group.key).sort()).toEqual(["t119", "t120"]);
    expect(model.workSection("t0")).toBe(before);
    stop.forEach((release) => release());
    stopNext();
    vi.mocked(buildAgentTurnWorkSectionModel).mockClear();
    runInAction(() => records.set("t120", turn("t120")));
    expect(buildAgentTurnWorkSectionModel).not.toHaveBeenCalled();
  });

  it("keeps disjoint segments of the same canonical turn in their original order", () => {
    const rows = [message("first", "t1"), message("middle", "t2"), message("continuation", "t1")];
    const model = TranscriptPresentationModel.fromConversation(conversation(rows, [turn("t1"), turn("t2")]), false);
    expect(model.viewportGroups.map((group) => group.turnId)).toEqual(["t1", "t2", "t1"]);
    expect(new Set(model.viewportGroups.map((group) => group.key)).size).toBe(3);
    expect(model.viewportGroups.flatMap(({ key }) => model.group(key)!.rows.map(({ row }) => row.id))).toEqual(["first", "middle", "continuation"]);
  });

  it("releases window-derived groups while preserving conversation tool disclosure", () => {
    const record = turn("t1");
    const parts = observable.box([conversation([message("q", "t1", "user"), tool("tools", "t1"), message("a", "t1")], [record])], { deep: false });
    const base = conversation([], [record]);
    const model = new TranscriptPresentationModel({ parts: () => parts.get(), base: () => base, turn: () => record }, "s", false);
    const stopOrder = autorun(() => model.viewportGroups);
    const stopSection = autorun(() => model.workSection("t1"));
    const previous = model.workSection("t1");
    model.setToolExpanded("tools", true);
    expect(model.toolExpanded("tools")).toBe(true);
    runInAction(() => parts.set([]));
    expect(model.group("t1")).toBeUndefined();
    runInAction(() => parts.set([conversation([message("q-new", "t1", "user"), tool("tools", "t1"), message("a-new", "t1")], [record])]));
    expect(model.workSection("t1")).not.toBe(previous);
    expect(model.group("t1")!.rows[0]?.row.id).toBe("q-new");
    expect(model.toolExpanded("tools")).toBe(true);
    stopOrder();
    stopSection();
  });
});
