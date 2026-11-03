// @vitest-environment jsdom
import { act } from "react";
import { createRoot } from "react-dom/client";
import { afterEach, expect, it, vi } from "vitest";
import { SubagentTranscript } from "./subagent-transcript";

vi.mock("../../../vendor/tutti/packages/agent/gui/shared/AgentMessageMarkdown", () => ({ AgentMessageMarkdown: ({ content }: { content: string }) => <p>{content}</p> }));
vi.mock("../../../vendor/tutti/packages/agent/gui/shared/agentConversation/components/AgentMessageImages", () => ({ AgentUserImageGrid: () => <div>Image preview</div> }));
vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
const container = document.createElement("div");
const root = createRoot(container);
afterEach(async () => { await act(async () => root.render(null)); });
const source = { id: 1, workspaceId: "ws", agentSessionId: "gui", eventId: "bg", actorType: "agent", actorId: "claude", itemType: "message" };

it("keeps the transcript expanded while live tool results replace running calls", async () => {
  const message = (status: string, output = "") => ({ sourceTimelineItems: [{ ...source, payload: { subagentTranscript: { locale: "en", prompt: "Review changes", items: [
    { id: "text", kind: "assistant_text", text: "Reading source" },
    { id: "tool", kind: "command", tool: "Bash", input: { command: "git diff" }, status, output },
  ] } } }] });
  await act(async () => root.render(<SubagentTranscript message={message("in_progress")} />));
  const disclosure = container.querySelector("details")!;
  expect(disclosure.open).toBe(false);
  disclosure.open = true;
  container.querySelectorAll("details")[1]!.open = true;
  await act(async () => root.render(<SubagentTranscript message={message("failed", "Permission denied")} />));
  expect(container.querySelector("details")).toBe(disclosure);
  expect(disclosure.open).toBe(true);
  expect(container.textContent).toContain("Reading source");
  expect(container.textContent).toContain("git diff");
  expect(container.textContent).toContain("Failed");
  expect(container.textContent).toContain("Permission denied");
});

it("shows unavailable details for old tasks and ignores unrelated notices", async () => {
  await act(async () => root.render(<SubagentTranscript message={{ sourceTimelineItems: [{ ...source, payload: { subagentTranscript: { items: [], locale: "zh-CN" } } }] }} />));
  expect(container.textContent).toContain("旧任务未保存的详情无法回看");
  await act(async () => root.render(<SubagentTranscript message={{}} />));
  expect(container.textContent).toBe("");
});
