// @vitest-environment jsdom
import { cleanup, fireEvent, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, describe, expect, it, vi } from "vitest";
import { api } from "@multica/core/api";
import type { AgentTask } from "@multica/core/types";
import type { TaskMessagePayload } from "@multica/core/types/events";
import { renderWithI18n } from "../../test/i18n";
import { InlineCommentRun } from "../../issues/components/inline-comment-run";
import { StepBody } from "./agent-transcript-dialog";
import { buildTimelineStructure } from "./build-timeline";

/**
 * Redaction must run over a whole body before anything cuts it down.
 *
 * These bodies reach the screen through several clips — `StepBody`'s display
 * clip, the 200-character step summary — and a pattern only matches while both
 * of its ends are present. Cutting first and redacting second therefore hides
 * nothing and renders the head of the secret. The inline run deriving its
 * timeline unredacted (MUL-7227) is what put raw bodies in front of those
 * clips, so each case here pins the order rather than the redaction.
 *
 * Deliberately renders the real `StepBody` and the real detail surfaces: the
 * inline suite mocks them, which is exactly why these boundaries were missed.
 */
vi.mock("@multica/core/api", () => ({ api: {
  getIssue: vi.fn(), listTaskMessages: vi.fn(), cancelTask: vi.fn(), rerunIssue: vi.fn(),
}, dispatchReasonCode: () => undefined }));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "workspace" }));
vi.mock("@multica/core/workspace/hooks", () => ({ useActorName: () => ({ getActorName: () => "Reviewer" }) }));
vi.mock("../actor-avatar", () => ({ ActorAvatar: () => <span /> }));
vi.mock("./use-trace-issue-labels", () => ({ useTraceIssueLabels: () => (text: string) => text }));

afterEach(() => { cleanup(); vi.clearAllMocks(); });

const id = "4a2e8d1c-7f9b-4e2a-9c1d-123456789abc";
const fakeKey = "AKIA1234567890ABCDEF";
const task: AgentTask = {
  id, agent_id: "agent", runtime_id: "runtime", issue_id: "issue", status: "running", priority: 0,
  created_at: "2026-09-07T00:00:00Z", started_at: "2026-09-07T00:00:00Z", dispatched_at: null,
  completed_at: null, result: null, error: null,
};
const item = (msgs: TaskMessagePayload[]) => buildTimelineStructure(msgs)[0]!;

describe("redaction runs before every clip", () => {
  it.each(["text", "thinking"] as const)("hides a %s secret straddling the display clip", (type) => {
    // Split across two frames so it only exists after coalescing, and placed so
    // the key spans the 8000-character clip.
    const view = renderWithI18n(<StepBody item={item([
      { task_id: id, issue_id: "issue", seq: 1, type, content: "x".repeat(7989) + " " + fakeKey.slice(0, 10) },
      { task_id: id, issue_id: "issue", seq: 2, type, content: fakeKey.slice(10) },
    ])} />);

    expect(view.container.textContent).not.toContain("AKIA123456");
  });

  it("hides a PEM body whose closing marker falls past the display clip", () => {
    const material = "QUJDREVGR0hJSktMTU5PUFFSU1RVVldYWVo=";
    const keyStart = `-----BEGIN PRIVATE KEY-----\n${material}\n`;
    const view = renderWithI18n(<StepBody item={item([
      { task_id: id, issue_id: "issue", seq: 1, type: "text", content: "x".repeat(7999 - keyStart.length) + "\n" + keyStart },
      { task_id: id, issue_id: "issue", seq: 2, type: "text", content: "-----END PRIVATE KEY-----" },
    ])} />);

    expect(view.container.textContent).not.toContain(material);
  });

  it("hides a secret past the 200-character cut in the inline step summary", async () => {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    vi.mocked(api.listTaskMessages).mockResolvedValue([
      { task_id: id, issue_id: "issue", seq: 1, type: "tool_result", tool: "exec_command", output: "x".repeat(189) + " " + fakeKey },
    ]);
    const view = renderWithI18n(<QueryClientProvider client={client}>
      <InlineCommentRun run={{ task, commentId: "comment", hasReply: false }} />
    </QueryClientProvider>);

    await screen.findByText("exec_command");
    fireEvent.click(screen.getByRole("button", { name: /View activity/ }));

    const summary = view.container.querySelector("details summary");
    expect(summary?.textContent).not.toContain("AKIA123456");
    // The same string is also the hover title, which no clip protects.
    expect(summary?.querySelector("[title]")?.getAttribute("title")).not.toContain("AKIA123456");
    client.clear();
  });

  it("hides a secret in a syntax-highlighted diff", () => {
    // The highlighter returns markup rendered with dangerouslySetInnerHTML, so
    // redaction that only runs on the un-highlighted fallback never applies to
    // a file whose language resolves. Built from tool input, which carries no
    // upstream redaction at all.
    const view = renderWithI18n(<StepBody item={item([
      { task_id: id, issue_id: "issue", seq: 1, type: "tool_use", tool: "Edit",
        input: { file_path: "config.ts", old_string: "const value = 0;", new_string: `const value = '${fakeKey}';` } },
    ])} />);

    expect(view.container.querySelector(".hljs")).not.toBeNull();
    expect(view.container.textContent).not.toContain(fakeKey);
  });
});
