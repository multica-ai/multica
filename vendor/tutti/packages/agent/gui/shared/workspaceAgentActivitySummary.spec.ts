import type { AgentActivityMessage } from "@tutti-os/agent-activity-core";
import { describe, expect, it } from "vitest";
import { resolveLatestActivity } from "./workspaceAgentActivitySummary";

function message(id: string, at: number, text: string, kind = "text"): AgentActivityMessage {
  return { agentSessionId: "s", workspaceId: "w", messageId: id, role: "assistant", kind, version: 1, sequence: at, occurredAtUnixMs: at, payload: { text } } as AgentActivityMessage;
}
const latestText = (messages: AgentActivityMessage[]) => resolveLatestActivity(messages, "idle" as never, { agentName: "agent", userName: "user" }).summary;

describe("the latest activity of a conversation", () => {
  it("does not depend on the order of the messages", () => {
    const forward = latestText([message("a", 1, "first"), message("b", 2, "second")]);
    const backward = latestText([message("b", 2, "second"), message("a", 1, "first")]);
    expect(backward).toEqual(forward);
    expect(forward).toBe("second");
  });

  it("skips tool calls and messages with no text", () => {
    expect(latestText([message("a", 1, "kept"), message("b", 2, "tool", "tool_call"), message("c", 3, "   ")])).toBe("kept");
  });

  it("breaks a tie in time by message id", () => {
    expect(latestText([message("b", 1, "bee"), message("a", 1, "ay")])).toBe("bee");
    expect(latestText([message("a", 1, "ay"), message("b", 1, "bee")])).toBe("bee");
  });

  it("reads the text of only the messages that could be the latest", () => {
    let reads = 0;
    const counted = (id: string, at: number) => {
      const row = message(id, at, "x");
      return { ...row, payload: { get text() { reads += 1; return "x"; } } } as unknown as AgentActivityMessage;
    };
    latestText(Array.from({ length: 500 }, (_, index) => counted(`m${index}`, index)));
    expect(reads).toBeLessThan(5);
  });
});
