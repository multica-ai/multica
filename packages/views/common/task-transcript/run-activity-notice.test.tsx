import { act, cleanup, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { api } from "@multica/core/api";
import type { AgentTask } from "@multica/core/types/agent";
import { renderWithI18n } from "../../test/i18n";
import type { SupportedLocale } from "@multica/core/i18n";
import type { TimelineItem } from "./build-timeline";
import { buildSteps } from "./build-steps";
import { RunActivityNotice } from "./run-activity-notice";

vi.mock("@multica/core/api", () => ({ api: { cancelTask: vi.fn() } }));

const startedAt = "2026-10-29T10:00:00Z";
const task: AgentTask = {
  id: "running-hermes", agent_id: "agent", runtime_id: "runtime", issue_id: "issue",
  status: "running", priority: 0, created_at: startedAt, started_at: startedAt,
  dispatched_at: null, completed_at: null, result: null, error: null,
};
const call: TimelineItem = {
  seq: 1, type: "tool_use", callId: "wait-1", tool: "process_manage",
  input: { action: "wait", session_id: "proc_1234" }, created_at: startedAt,
};

function setup(items: TimelineItem[] = [call], currentTask = task, locale: SupportedLocale = "en") {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  const view = (events: TimelineItem[], run: AgentTask) => <QueryClientProvider client={client}>
    <RunActivityNotice task={run} items={events} steps={buildSteps(events)} />
  </QueryClientProvider>;
  const rendered = renderWithI18n(view(items, currentTask), { locale });
  return { rerender: (events: TimelineItem[], run = currentTask) => rendered.rerender(view(events, run)) };
}

beforeEach(() => {
  vi.useFakeTimers({ toFake: ["Date", "setInterval", "clearInterval"] });
  vi.setSystemTime(new Date("2026-10-29T10:06:00Z"));
  vi.mocked(api.cancelTask).mockReset();
  vi.mocked(api.cancelTask).mockResolvedValue({ ...task, status: "cancelled" });
});
afterEach(() => { cleanup(); vi.useRealTimers(); });

describe("RunActivityNotice", () => {
  it("marks a long silent wait without automatically cancelling a healthy tool", () => {
    vi.setSystemTime(new Date(startedAt));
    setup();
    expect(screen.queryByText(/No new events/)).not.toBeInTheDocument();
    act(() => vi.advanceTimersByTime(5 * 60_000));
    expect(screen.getByText("No new events for 5 minutes")).toBeInTheDocument();
    expect(screen.getByText("Waiting for process_manage.")).toBeInTheDocument();
    act(() => vi.advanceTimersByTime(2 * 60 * 60_000));
    expect(api.cancelTask).not.toHaveBeenCalled();
  });

  it("clears on a tool result, then stops naming the completed tool", () => {
    const { rerender } = setup();
    expect(screen.getByText("Waiting for process_manage.")).toBeInTheDocument();
    const result: TimelineItem = { seq: 2, type: "tool_result", callId: "wait-1", tool: "process_manage",
      output: "not_found", created_at: new Date().toISOString() };
    rerender([call, result]);
    expect(screen.queryByText(/No new events/)).not.toBeInTheDocument();
    act(() => vi.advanceTimersByTime(5 * 60_000));
    expect(screen.getByText("The run may still be working.")).toBeInTheDocument();
    expect(screen.queryByText("Waiting for process_manage.")).not.toBeInTheDocument();
  });

  it("uses call IDs for overlapping waits with the same tool name", () => {
    setup([call, { ...call, seq: 2, callId: "wait-2", tool: "terminal" },
      { seq: 3, type: "tool_result", callId: "wait-1", tool: "process_manage", created_at: startedAt }]);
    expect(screen.getByText("Waiting for terminal.")).toBeInTheDocument();
  });

  it.each([undefined, "bad timestamp"])("does not guess freshness when the newest event timestamp is %s", (created_at) => {
    setup([call, { seq: 2, type: "text", content: "Still working", created_at }]);
    expect(document.querySelector("[data-run-activity-notice]")).not.toBeInTheDocument();
  });

  it("does not infer silence from total runtime or a future event", () => {
    const { rerender } = setup([]);
    expect(document.querySelector("[data-run-activity-notice]")).not.toBeInTheDocument();
    rerender([{ ...call, created_at: "2026-10-29T11:00:00Z" }]);
    expect(document.querySelector("[data-run-activity-notice]")).not.toBeInTheDocument();
  });

  it("removes the notice when the task becomes terminal", () => {
    const { rerender } = setup();
    rerender([call], { ...task, status: "cancelled" });
    expect(document.querySelector("[data-run-activity-notice]")).not.toBeInTheDocument();
  });

  it("requires confirmation, cancels only the selected run once, and waits for success", async () => {
    let finish!: () => void;
    vi.mocked(api.cancelTask).mockReturnValue(new Promise((resolve) => {
      finish = () => resolve({ ...task, status: "cancelled" });
    }));
    setup();
    fireEvent.click(screen.getByRole("button", { name: "Stop run" }));
    const dialog = screen.getByRole("alertdialog");
    expect(dialog).toHaveTextContent("Queued comments can continue in a new run.");
    expect(api.cancelTask).not.toHaveBeenCalled();
    const confirm = within(dialog).getByRole("button", { name: "Stop run" });
    await act(async () => { fireEvent.click(confirm); });
    fireEvent.click(confirm);
    await waitFor(() => expect(confirm).toBeDisabled());
    expect(api.cancelTask).toHaveBeenCalledExactlyOnceWith("issue", "running-hermes");
    expect(dialog).toBeInTheDocument();
    await act(async () => { finish(); });
    expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument();
    await waitFor(() => expect(screen.getByRole("button", { name: "Stopping..." })).toBeDisabled());
  });

  it("keeps a failed cancellation open and allows a retry", async () => {
    vi.mocked(api.cancelTask).mockRejectedValueOnce(new Error("offline"));
    setup();
    fireEvent.click(screen.getByRole("button", { name: "Stop run" }));
    const dialog = screen.getByRole("alertdialog");
    await act(async () => { fireEvent.click(within(dialog).getByRole("button", { name: "Stop run" })); });
    expect(within(dialog).getByRole("alert")).toBeInTheDocument();
    await act(async () => { fireEvent.click(within(dialog).getByRole("button", { name: "Stop run" })); });
    expect(api.cancelTask).toHaveBeenCalledTimes(2);
    expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument();
  });

  it("does not expose issue cancellation for a chat or autopilot run", () => {
    setup([call], { ...task, issue_id: "" });
    expect(screen.getByText("No new events for 6 minutes")).toBeInTheDocument();
    expect(screen.queryByRole("button")).not.toBeInTheDocument();
  });

  it("redacts a credential in a runtime-supplied tool name", () => {
    const secret = `ghp_${"x".repeat(36)}`;
    setup([{ ...call, tool: secret }]);
    expect(document.body.textContent).not.toContain(secret);
    expect(screen.getByText(/REDACTED/)).toBeInTheDocument();
  });

  it.each(["en", "zh-Hans", "ja", "ko", "fr"] as const)("renders localized copy in %s", (locale) => {
    setup([call], task, locale);
    const notice = document.querySelector("[data-run-activity-notice]")!;
    expect(notice.textContent).toContain("6");
    expect(notice.textContent).not.toContain("quiet_minutes");
    expect(notice.textContent).not.toContain("quiet_tool");
    if (locale === "zh-Hans") expect(notice).toHaveTextContent("已 6 分钟没有新动态");
  });
});
