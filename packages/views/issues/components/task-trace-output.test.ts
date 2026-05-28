import { describe, expect, it } from "vitest";
import type { TaskTraceLine } from "@multica/core/types";
import { __taskTraceOutputTestUtils } from "./task-trace-output";

function makeDisplayLine(seq: number, event: Record<string, unknown>): TaskTraceLine {
  return {
    seq,
    task_id: "task-1",
    run_id: "run-1",
    channel: "display_event",
    content: JSON.stringify(event),
    timestamp: "2026-05-19T00:00:00Z",
  };
}

function makeTraceLine(seq: number, channel: string, content: string): TaskTraceLine {
  return {
    seq,
    task_id: "task-1",
    run_id: "run-1",
    channel,
    content,
    timestamp: "2026-05-19T00:00:00Z",
  };
}

describe("task trace output helpers", () => {
  it("pairs tool results by call_id even when many events are in between", () => {
    const lines: TaskTraceLine[] = [
      makeDisplayLine(1, {
        type: "tool_call",
        title: "Bash",
        metadata: {
          call_id: "call-1",
          input: { command: "docker ps" },
        },
      }),
      ...Array.from({ length: 12 }, (_, index) => makeDisplayLine(index + 2, {
        type: "assistant_text",
        title: "Claude",
        content: `note-${index + 1}`,
      })),
      makeDisplayLine(14, {
        type: "tool_result",
        title: "Tool result",
        content: "CONTAINER ID   IMAGE",
        metadata: { call_id: "call-1" },
      }),
    ];

    const items = __taskTraceOutputTestUtils.buildWorkItems(lines, false);
    const toolItem = items.find((item) => item.event?.type === "tool_call");
    expect(toolItem?.pairedEvent?.type).toBe("tool_result");
    expect(toolItem?.pairedEvent?.content).toContain("CONTAINER ID");
  });

  it("extracts command text from nested stringified input payloads", () => {
    const command = __taskTraceOutputTestUtils.commandFromInput({
      input: "{\"command\":\"multica issue comment add TES-1 --content-file /tmp/reply.md\"}",
    });

    expect(command).toBe("multica issue comment add TES-1 --content-file /tmp/reply.md");
  });

  it("falls back to serialized input instead of bare Command", () => {
    const label = __taskTraceOutputTestUtils.commandLabel("", {
      script: "docker ps 2>&1",
    });

    expect(label).toContain("docker ps");
    expect(label).not.toBe("Command");
  });

  it("identifies approval trace channels for compact card rendering", () => {
    expect(__taskTraceOutputTestUtils.isApprovalTraceChannel("approval_request")).toBe(true);
    expect(__taskTraceOutputTestUtils.isApprovalTraceChannel("approval_response")).toBe(true);
    expect(__taskTraceOutputTestUtils.isApprovalTraceChannel("display_event")).toBe(false);
  });

  it("attaches adjacent approval lines to the preceding command item", () => {
    const lines: TaskTraceLine[] = [
      makeDisplayLine(1, {
        type: "tool_call",
        title: "Bash",
        metadata: {
          call_id: "call-1",
          input: { command: "multica issue comment add TES-1 --content hi" },
        },
      }),
      makeTraceLine(2, "approval_request", "Tool: Bash"),
      makeTraceLine(3, "approval_response", "allow (approved=true)"),
      makeDisplayLine(4, {
        type: "tool_result",
        title: "Tool result",
        content: "ok",
        metadata: { call_id: "call-1" },
      }),
    ];

    const items = __taskTraceOutputTestUtils.buildWorkItems(lines, false);
    expect(items).toHaveLength(1);
    expect(items[0]?.approvalRequestLine?.content).toBe("Tool: Bash");
    expect(items[0]?.approvalResponseLine?.content).toBe("allow (approved=true)");
  });
});
