// @vitest-environment node
import { describe, expect, it } from "vitest";
import { backgroundTaskPresentation, type BackgroundTaskEvent } from "./background-task-presentation";

const task = { id: "shell", description: "Inspect releases", status: "completed" as const, summary: "Inspect releases" };
const events = [
  { seq: 1, type: "tool_use", call_id: "call", tool: "Bash", input: { description: task.description, command: "fixture release list" } },
  { seq: 2, type: "tool_result", call_id: "call", output: "v2.1.0\nnode v24" },
] as (BackgroundTaskEvent & { seq: number })[];

describe("background task display", () => {
  it("shows the saved command and output on an older card instead of repeating its title", () => {
    expect(backgroundTaskPresentation(task, events, "zh-CN")).toEqual({ progress: "", detail: "命令：\nfixture release list\n\n输出：\nv2.1.0\nnode v24" });
  });
  it("does not attach an ambiguous historical command", () => {
    expect(backgroundTaskPresentation(task, [...events, { ...events[0]!, call_id: "other" }], "en").detail).toBe("");
  });
  it("shows measured progress and prefers the final output file over the initial background placeholder", () => {
    const value = backgroundTaskPresentation({ ...task, toolUseId: "call", output: "Build complete", progressSummary: "Compiling", lastToolName: "Bash", usage: { totalTokens: 1200, toolUses: 3, durationMs: 4200 } }, events, "en");
    expect(value.progress).toBe("Compiling\n4s elapsed · 3 tool uses · 1,200 tokens · Last tool: Bash");
    expect(value.detail).toContain("Build complete"); expect(value.detail).not.toContain("v2.1.0");
  });
  it("bounds large output and explicitly identifies the excerpt", () => {
    const value = backgroundTaskPresentation({ ...task, output: "x".repeat(40000) }, [], "en");
    expect(value.detail.length).toBeLessThan(33000); expect(value.detail).toContain("Only the output tail is shown");
  });
  it("shows the latest shell output and observed elapsed time without fabricated percentages", () => {
    const value = backgroundTaskPresentation({ ...task, status: "running", createdAt: "2026-10-25T00:00:00Z", updatedAt: "2026-10-25T00:00:12Z", output: "Installing\nDownloading browser 50%\n" }, [], "zh-CN");
    expect(value.progress).toBe("最近输出：Downloading browser 50%\n耗时 12 秒");
  });
});
