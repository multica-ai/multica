import type { AgentActivityMessage } from "@tutti-os/agent-activity-core";
import { describe, expect, it } from "vitest";
import { projectAgentGUIMessagesToTimelineItems } from "../agent-gui/agentGuiNode/controller/agentGuiController.promptHelpers";
import { projectAgentConversationVM } from "./agentConversation/projection/agentConversationProjection";
import { buildCanonicalWorkspaceAgentDetailView } from "./workspaceAgentTimelineCanonical";

const session = {
  agentSessionId: "s",
  workspaceId: "w",
  provider: "claude",
  title: "c",
  status: "active",
  cwd: "",
  createdAtUnixMs: 1,
  updatedAtUnixMs: 2
} as never;
const activity = { sessionId: "s" } as never;
let version = 0;

function message(
  id: string,
  turnId: string,
  at: number,
  fields: Partial<AgentActivityMessage> = {}
): AgentActivityMessage {
  return {
    agentSessionId: "s",
    workspaceId: "w",
    messageId: id,
    turnId,
    role: "assistant",
    kind: "text",
    version: ++version,
    sequence: at,
    occurredAtUnixMs: at,
    payload: { content: `body ${id}` },
    ...fields
  } as AgentActivityMessage;
}
const prompt = (
  id: string,
  turnId: string,
  at: number,
  text = `prompt ${id}`
) =>
  message(id, turnId, at, { role: "user", payload: { text, content: text } });
const tool = (
  id: string,
  turnId: string,
  at: number,
  payload: Record<string, unknown> = {}
) =>
  message(id, turnId, at, {
    kind: "tool_call",
    status: "completed",
    payload: {
      callId: id,
      toolName: "Bash",
      input: { command: "ls" },
      output: "x",
      ...payload
    }
  });

function detailOf(
  messages: AgentActivityMessage[],
  sessionTurns: never[] = []
) {
  return buildCanonicalWorkspaceAgentDetailView({
    activity,
    session,
    sessionTurns,
    timelineItems: projectAgentGUIMessagesToTimelineItems(messages)
  });
}

/** Everything but identity, against a build sharing nothing with earlier ones. */
function plain(value: unknown) {
  return JSON.parse(JSON.stringify(value));
}
const uncached = (
  messages: AgentActivityMessage[],
  sessionTurns: never[] = []
) =>
  detailOf(
    messages.map((row) => ({ ...row })),
    sessionTurns
  );

function deepFreeze<T>(value: T, seen = new Set<unknown>()): T {
  if (value && typeof value === "object" && !seen.has(value)) {
    seen.add(value);
    Object.values(value).forEach((entry) => deepFreeze(entry, seen));
    Object.freeze(value);
  }
  return value;
}

const conversation = () => [
  prompt("q0", "t0", 1),
  message("a0", "t0", 2),
  tool("c0", "t0", 3),
  message("b0", "t0", 4),
  prompt("q1", "t1", 5),
  tool("c1", "t1", 6),
  message("a1", "t1", 7)
];

describe("pure canonical turn projection", () => {
  it("preserves an unchanged turn's value", () => {
    const messages = conversation();
    const first = detailOf(messages);
    const next = detailOf([
      ...messages.slice(0, -1),
      {
        ...messages.at(-1)!,
        version: ++version,
        payload: { content: "longer" }
      }
    ]);
    expect(next.turns[0]).toEqual(first.turns[0]);
    expect(next.turns[1]).not.toBe(first.turns[1]);
  });

  it("gives what building everything gives, through a stream of changes", () => {
    let messages = conversation();
    detailOf(messages);
    const steps: ((rows: AgentActivityMessage[]) => AgentActivityMessage[])[] =
      [
        (rows) =>
          rows.map((row) =>
            row.messageId === "a1"
              ? { ...row, version: ++version, payload: { content: "more" } }
              : row
          ),
        (rows) => [...rows, tool("c2", "t1", 8)],
        (rows) => [...rows, prompt("q2", "t2", 9), message("a2", "t2", 10)],
        (rows) => [prompt("older", "tm1", 0), ...rows],
        (rows) => rows.filter((row) => row.messageId !== "c1")
      ];
    for (const step of steps) {
      messages = step(messages);
      const incremental = detailOf(messages);
      const whole = uncached(messages);
      expect(plain(incremental.turns)).toEqual(plain(whole.turns));
      expect(plain(projectAgentConversationVM(incremental).rows)).toEqual(
        plain(projectAgentConversationVM(whole).rows)
      );
    }
  });

  it("does not change a kept turn or its rows while projecting again", () => {
    const messages = conversation();
    deepFreeze(detailOf(messages).turns);
    const next = detailOf(messages);
    expect(() => projectAgentConversationVM(next)).not.toThrow();
    const rows = projectAgentConversationVM(next).rows;
    deepFreeze(rows);
    expect(plain(projectAgentConversationVM(detailOf(messages)).rows)).toEqual(
      plain(rows)
    );
  });

  it("merges adjacent replies the same every time", () => {
    const messages = [
      prompt("q", "t", 1),
      message("x", "t", 2),
      message("y", "t", 3)
    ];
    const detail = detailOf(messages);
    const once = plain(projectAgentConversationVM(detail).rows);
    expect(plain(projectAgentConversationVM(detail).rows)).toEqual(once);
    expect(plain(projectAgentConversationVM(detailOf(messages)).rows)).toEqual(
      once
    );
  });

  it("builds a turn again when its canonical error changes", () => {
    const messages = conversation();
    const failed = (message: string) =>
      [
        {
          agentSessionId: "s",
          turnId: "t1",
          origin: "legacy_unknown",
          phase: "settled",
          outcome: "failed",
          startedAtUnixMs: 1,
          updatedAtUnixMs: 2,
          error: { message, code: "x" }
        }
      ] as never[];
    const first = detailOf(messages, failed("one"));
    const second = detailOf(messages, failed("two"));
    expect(second.turns[1]).not.toBe(first.turns[1]);
    expect(plain(second.turns)).toEqual(
      plain(uncached(messages, failed("two")).turns)
    );
  });

  it("does not reuse turns when a call nests under a task in another turn", () => {
    const parent = tool("task", "t0", 1, { toolName: "Task", callId: "task" });
    const child = tool("child", "t1", 3, {
      parentToolUseId: "task",
      metadata: { parentToolUseId: "task" }
    });
    const base = [prompt("q0", "t0", 0), parent, prompt("q1", "t1", 2)];
    detailOf(base);
    const nested = [...base, child];
    expect(
      detailOf(nested).turns.find((turn) => turn.id === "t1")!.toolCalls
    ).toHaveLength(0);
    expect(plain(detailOf(nested).turns)).toEqual(
      plain(uncached(nested).turns)
    );
    expect(plain(detailOf(nested).turns)).toEqual(
      plain(uncached(nested).turns)
    );
  });
});
