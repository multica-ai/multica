// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { LocalV2Event } from "@multica/core/local-conversation/v2";
import { ItemModel } from "./item-model";
import { TurnModel } from "./turn-model";

const at = "2026-11-07T06:00:00Z";

function message(payload: Record<string, unknown>) {
  const event: LocalV2Event = {
    seq: 1, turn_id: "turn", turn_seq: 1, item_id: "call",
    type: "item.completed", final: true, created_at: at, payload,
  };
  const item = new ItemModel(event, "call", "turn");
  item.applyEvent(event);
  const turn = new TurnModel({ turnItems: () => [item], turnMessages: () => [], turnInteractions: () => [] }, {
    id: "turn", index: 0, requestId: "request", status: "completed", startedAt: at, completedAt: at, error: null,
  });
  return turn.messages("en")[0]!;
}

describe("tool message payloads", () => {
  it("preserves command output and execution metadata in the structured message", () => {
    const output = '  {"text":"this is command output, not a payload"}\n[output truncated]\n';
    const row = message({ kind: "command", status: "completed", command: "fixture", cwd: "/repo", output, exit_code: 0, duration_ms: 0 });
    expect(row).toMatchObject({ messageId: "call", turnId: "turn", kind: "tool_call", status: "completed", payload: {
      callId: "call", toolName: "Bash", input: { command: "fixture", cwd: "/repo" }, output: { text: output, exitCode: 0, durationMs: 0 },
    } });
  });

  it.each(["", '{"stdout":"literal JSON"}', "[1,2]", " \n"])("keeps MCP output as text: %j", (output) => {
    expect(message({ kind: "tool_call", status: "completed", server: "fixture", tool: "search", input: { query: "test" }, output }).payload)
      .toMatchObject({ toolName: "mcp__fixture__search", input: { query: "test" }, output: { text: output } });
  });

  it("keeps file changes structured instead of serializing them as tool text", () => {
    const changes = [{ path: "a.ts", kind: "update", diff: "@@ -1 +1 @@\n-a\n+b\n" }];
    expect(message({ kind: "file_change", status: "completed", changes }).payload)
      .toMatchObject({ toolName: "Edit", input: { changes }, output: { changes } });
  });

  it("retains tool failure diagnostics as well as output", () => {
    const error = { kind: "tool_error", message: "timeout" };
    expect(message({ kind: "tool_call", status: "failed", tool: "Bash", input: { command: "fixture" }, output: "partial\n", error }))
      .toMatchObject({ status: "failed", payload: { output: { text: "partial\n" }, error: { ...error, text: "timeout" } } });
  });

  it("preserves cancellation instead of reporting a tool failure", () => {
    expect(message({ kind: "command", status: "cancelled", command: "fixture", output: "partial" }))
      .toMatchObject({ status: "canceled", payload: { output: { text: "partial" } } });
  });

  it("decodes only the image generation tool's structured output", () => {
    const output = { savedPath: "/repo/image.png", mimeType: "image/png", text: "A fixture image" };
    expect(message({ kind: "tool_call", status: "completed", tool: "ImageGeneration", input: { prompt: "fixture" }, output: JSON.stringify(output) }).payload.output)
      .toEqual(output);
  });

  it.each(["not JSON", "[]", "null"])("retains image generation diagnostics: %s", (output) => {
    expect(message({ kind: "tool_call", status: "failed", tool: "ImageGeneration", output }).payload.output).toEqual({ text: output });
  });
});
