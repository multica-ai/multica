// @vitest-environment node
import { describe, expect, it } from "vitest";
import { conversationHumanEntries, issueConversationTimeline } from "./issue-conversation-timeline";
import type { TaskInteraction } from "@multica/core/chat";

describe("issueConversationTimeline", () => {
  it("does not present agent completion intent as a human message", () => {
    const record: TaskInteraction = {owner:"process",state:{state:"working",activity:1},deadline:"later",updatedAt:"now",openingInput:"",finishing:false,commands:[
      {id:"complete",kind:"complete",activity:1,actorId:"agent",createdAt:"now",text:"",error:"",receipt:null},
      {id:"input",kind:"input",text:"hello",activity:1,actorId:"human",createdAt:"now",error:"",receipt:null},
    ]};
    const entries = conversationHumanEntries(record, "now", {human:"Human",interrupt:"Interrupt",finish:"Finish",pending:"Pending"});
    expect(entries.map((entry) => entry.id)).toEqual(["input"]);
    const timeline = issueConversationTimeline([], entries);
    expect(timeline[0]?.content).toContain("**Human**");
    expect(timeline[0]?.humanContent).toBe("hello\n\nPending");
  });
  it("does not coalesce assistant prose across a human instruction", () => {
    const result = issueConversationTimeline([
      { task_id: "run", issue_id: "issue", seq: 2, type: "text", content: "after", created_at: "2026-01-01T00:00:03Z" },
      { task_id: "run", issue_id: "issue", seq: 1, type: "text", content: "before", created_at: "2026-01-01T00:00:01Z" },
    ], [{ id: "input", text: "correction", createdAt: "2026-01-01T00:00:02Z" }]);
    expect(result.map((item) => item.content)).toEqual(["before", "correction", "after"]);
  });
  it("retains tool identity across the interruption boundary", () => {
    const result = issueConversationTimeline([
      { task_id: "run", issue_id: "issue", seq: 1, type: "tool_use", call_id: "call", tool: "read", created_at: "2026-01-01T00:00:01Z" },
      { task_id: "run", issue_id: "issue", seq: 2, type: "tool_result", call_id: "call", output: "done", created_at: "2026-01-01T00:00:03Z" },
    ], [{ id: "stop", text: "Interrupt", createdAt: "2026-01-01T00:00:02Z" }]);
    expect(result.map((item) => item.callId)).toEqual(["call", undefined, "call"]);
  });
  it("interleaves server UTC instructions with daemon offset timestamps", () => {
    const result = issueConversationTimeline([
      { task_id: "run", issue_id: "issue", seq: 1, type: "text", content: "before", created_at: "2026-01-01T07:00:01+07:00" },
      { task_id: "run", issue_id: "issue", seq: 2, type: "text", content: "after", created_at: "2026-01-01T07:00:03+07:00" },
    ], [{ id: "input", text: "hello", createdAt: "2026-01-01T00:00:02Z" }]);
    expect(result.map((item) => item.content)).toEqual(["before", "hello", "after"]);
    expect(result[1]?.humanId).toBe("input");
  });
  it("orders fractional seconds numerically instead of putting instructions last", () => {
    const result = issueConversationTimeline([
      { task_id: "run", issue_id: "issue", seq: 1, type: "text", content: "reply", created_at: "2026-01-01T00:00:02.500Z" },
    ], [{ id: "input", text: "question", createdAt: "2026-01-01T00:00:02Z" }]);
    expect(result.map((item) => item.content)).toEqual(["question", "reply"]);
  });
});
