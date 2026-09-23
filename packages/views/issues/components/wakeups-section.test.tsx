import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import type { IssueWakeup } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { WakeupsSection } from "./wakeups-section";
const mutate = vi.fn();
const enable = vi.fn();
let pending = false;
let wakeup: IssueWakeup;
let status = "queued";
let viewTZ = "UTC";
vi.mock("@multica/core/paths", () => ({
  useCurrentWorkspace: () => ({ id: "ws" }),
}));
vi.mock("@multica/core/issues", () => ({
  issueWakeupsOptions: () => ({ queryKey: ["wakeups"] }),
  issueTasksOptions: () => ({ queryKey: ["tasks"] }),
  useDisableIssueWakeup: () => ({ mutate, isPending: false }),
  useEnableIssueWakeup: () => ({ mutateAsync: enable, isPending: pending }),
}));
vi.mock("@tanstack/react-query", () => ({
  useQuery: ({ queryKey }: { queryKey: string[] }) => ({
    data: queryKey[0] === "wakeups" ? [wakeup] : [{ id: "task", status }],
  }),
}));
vi.mock("../../common/use-viewing-timezone", () => ({
  useViewingTimezone: () => viewTZ,
}));
vi.mock("../../common/task-transcript", () => ({
  TranscriptButton: () => <button>Transcript</button>,
}));
vi.mock("./wakeup-instruction-editor", () => ({
  WakeupInstructionEditor: () => <button>Edit prompt</button>,
}));
beforeEach(() => {
  mutate.mockReset();
  enable.mockReset().mockResolvedValue(undefined);
  pending = false;
  status = "queued";
  viewTZ = "UTC";
  wakeup = {
    id: "wake",
    revision: 2,
    issue_id: "issue",
    agent_id: "agent",
    agent_name: "Emacs",
    instruction: "Check CI",
    kind: "every",
    mode: "continuous",
    event_types: [],
    interval_seconds: 3600,
    enabled: true,
    disabled_at: null,
    last_task_id: "task",
    last_error: null,
    timezone: "UTC",
    next_fire_at: null,
    cron_expression: null,
    filter_agent_id: null,
    filter_task_id: null,
  };
});
describe("Wakeups sidebar", () => {
  it("describes hourly schedules without exposing interval seconds", () => {
    renderWithI18n(<WakeupsSection issueId="issue" />, { locale: "zh-Hans" });
    expect(screen.getByRole("button", { name: /唤醒 Emacs/ })).toHaveTextContent("每小时唤醒");
    expect(screen.queryByText(/3600/)).toBeNull();
  });
  it("describes a daily cron and preserves its expression in details", async () => {
    wakeup.kind = "cron";
    wakeup.cron_expression = "0 9 * * *";
    renderWithI18n(<WakeupsSection issueId="issue" />);
    const row = screen.getByRole("button", { name: /Wake Emacs/ });
    expect(row).toHaveTextContent("09:00");
    expect(row).not.toHaveTextContent("0 9 * * *");
    fireEvent.click(row);
    await waitFor(() => expect(screen.getByText("0 9 * * * · UTC")).toBeVisible());
  });
  describe("one-time wakeup stored in UTC", () => {
    beforeEach(() => {
      vi.useFakeTimers({ toFake: ["Date"] });
      vi.setSystemTime(new Date("2026-09-23T10:00:00Z"));
      viewTZ = "Asia/Shanghai";
      wakeup.kind = "at";
      wakeup.mode = "once";
      wakeup.interval_seconds = null;
    });
    afterEach(() => vi.useRealTimers());

    it("shows the fire time in the viewer's timezone", async () => {
      wakeup.next_fire_at = "2026-09-23T14:05:00Z";
      renderWithI18n(<WakeupsSection issueId="issue" />);
      const row = screen.getByRole("button", { name: /Wake Emacs/ });
      expect(row).toHaveTextContent("Wake at Today 10:05 PM");
      fireEvent.click(row);
      await waitFor(() =>
        expect(screen.getByText(/· Asia\/Shanghai$/)).toBeVisible(),
      );
    });

    it("moves the fire time to the viewer's next day", () => {
      wakeup.next_fire_at = "2026-09-23T17:00:00Z";
      renderWithI18n(<WakeupsSection issueId="issue" />);
      const row = screen.getByRole("button", { name: /Wake Emacs/ });
      expect(row).toHaveTextContent("Wake at Sep 24, 01:00 AM");
      expect(row).not.toHaveTextContent("Today");
    });
  });
  it("exposes the toggle and reveals the full prompt only on opening details", async () => {
    renderWithI18n(<WakeupsSection issueId="issue" />);
    expect(screen.queryByText("Check CI")).not.toBeVisible();
    fireEvent.click(screen.getByRole("switch", { name: "Wakeup for Emacs" }));
    expect(mutate).toHaveBeenCalledWith("wake", expect.any(Object));
    fireEvent.click(screen.getByRole("button", { name: /Wake Emacs/ }));
    await waitFor(() => expect(screen.getByText("Check CI")).toBeVisible());
    expect(screen.getByRole("button", { name: "Transcript" })).toBeVisible();
    expect(screen.getByRole("button", { name: "Edit prompt" })).toBeVisible();
  });
  it("keeps a consumed one-shot queued run in the current list with withdrawal", () => {
    wakeup.enabled = false;
    wakeup.kind = "event";
    wakeup.mode = "once";
    wakeup.event_types = ["task.completed"];
    renderWithI18n(<WakeupsSection issueId="issue" />);
    expect(screen.queryByText(/Wakeup history/)).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Cancel this execution" }));
    expect(mutate).toHaveBeenCalled();
  });
  it("keeps an already claimed single run visible without offering withdrawal", () => {
    wakeup.enabled = false;
    status = "running";
    renderWithI18n(<WakeupsSection issueId="issue" />);
    expect(screen.queryByText(/Wakeup history/)).toBeNull();
    expect(
      screen.queryByRole("button", { name: /Turn off wakeup/ }),
    ).toBeNull();
    expect(screen.getByRole("button", { name: /Wake Emacs/ })).toBeVisible();
  });
  it("folds ended configurations into history with an off toggle", () => {
    wakeup.enabled = false;
    status = "completed";
    renderWithI18n(<WakeupsSection issueId="issue" />);
    expect(screen.queryByRole("button", { name: /Wake Emacs/ })).toBeNull();
    expect(screen.queryByRole("switch")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Wakeup history 1" }));
    expect(screen.getByRole("button", { name: /Wake Emacs/ })).toBeVisible();
  });
  it("uses server run status while the run detail query is still incomplete", () => {
    wakeup.enabled = false;
    wakeup.last_task_id = "missing";
    wakeup.last_task_status = "dispatched";
    renderWithI18n(<WakeupsSection issueId="issue" />);
    expect(screen.queryByText(/Wakeup history/)).toBeNull();
    expect(
      screen.queryByRole("button", { name: /Turn off wakeup/ }),
    ).toBeNull();
  });
});

it("re-enables a manually disabled recurring configuration with its revision", async () => {
  wakeup.enabled = false;
  wakeup.disabled_at = "2026-09-16T00:00:00Z";
  status = "completed";
  renderWithI18n(<WakeupsSection issueId="issue" />);
  fireEvent.click(screen.getByRole("button", { name: "Wakeup history 1" }));
  const toggle = screen.getByRole("switch", { name: "Wakeup for Emacs" });
  expect(toggle).not.toBeChecked();
  fireEvent.click(toggle);
  await waitFor(() =>
    expect(enable).toHaveBeenCalledWith({ id: "wake", revision: 2 }),
  );
  expect(toggle).not.toBeChecked(); // Wait for server state; no optimistic success.
});
it("blocks re-enabling on terminal issues", () => {
  wakeup.enabled = false;
  status = "completed";
  renderWithI18n(<WakeupsSection issueId="issue" closed />);
  fireEvent.click(screen.getByRole("button", { name: "Wakeup history 1" }));
  expect(screen.getByRole("switch")).toHaveAttribute("aria-disabled", "true");
  fireEvent.click(screen.getByRole("switch"));
  expect(enable).not.toHaveBeenCalled();
  expect(mutate).not.toHaveBeenCalled();
  expect(screen.getByText(/cannot be enabled/)).toBeVisible();
});
it("requires a new future time for an expired one-shot", async () => {
  wakeup.enabled = false;
  wakeup.disabled_at = "2020-01-01T00:00:00Z";
  wakeup.kind = "at";
  wakeup.mode = "once";
  wakeup.next_fire_at = "2020-01-01T00:00:00Z";
  status = "completed";
  renderWithI18n(<WakeupsSection issueId="issue" />);
  fireEvent.click(screen.getByRole("button", { name: "Wakeup history 1" }));
  expect(screen.queryByRole("switch")).toBeNull();
  fireEvent.click(screen.getByRole("button", { name: "Set a new time" }));
  const input = await screen.findByLabelText(/Time \(/);
  fireEvent.change(input, { target: { value: "2020-01-01T12:00" } });
  fireEvent.submit(input.closest("form")!);
  expect(await screen.findByRole("alert")).toHaveTextContent(
    "Choose a future time",
  );
  expect(enable).not.toHaveBeenCalled();
  fireEvent.change(input, { target: { value: "2099-01-01T12:00" } });
  fireEvent.submit(input.closest("form")!);
  await waitFor(() =>
    expect(enable).toHaveBeenCalledWith({
      id: "wake",
      revision: 2,
      at: new Date("2099-01-01T12:00").toISOString(),
      rearm: true,
    }),
  );
});
it("uses explicit resubscribe for a completed one-shot event", async () => {
  wakeup.enabled = false;
  wakeup.kind = "event";
  wakeup.mode = "once";
  status = "completed";
  renderWithI18n(<WakeupsSection issueId="issue" />);
  fireEvent.click(screen.getByRole("button", { name: "Wakeup history 1" }));
  expect(screen.queryByRole("switch")).toBeNull();
  fireEvent.click(screen.getByRole("button", { name: "Enable again" }));
  await waitFor(() =>
    expect(enable).toHaveBeenCalledWith({
      id: "wake",
      revision: 2,
      rearm: true,
    }),
  );
});
it("disables controls during a pending mutation", () => {
  pending = true;
  renderWithI18n(<WakeupsSection issueId="issue" />);
  expect(screen.getByRole("switch")).toHaveAttribute("aria-disabled", "true");
  fireEvent.click(screen.getByRole("switch"));
  expect(enable).not.toHaveBeenCalled();
  expect(mutate).not.toHaveBeenCalled();
});

it("describes the source condition in Chinese, keeping instructions in details", async () => {
  wakeup.kind = "event";
  wakeup.mode = "once";
  wakeup.event_types = ["task.completed", "task.failed"];
  wakeup.filter_agent_name = "Grok";
  wakeup.filter_agent_id = "grok";
  renderWithI18n(<WakeupsSection issueId="issue" />, { locale: "zh-Hans" });
  const trigger = screen.getByRole("button", { name: /当 Grok 的运行成功结束时/ });
  expect(trigger).toHaveTextContent("唤醒 Emacs · 仅触发一次");
  expect(screen.queryByText("Check CI")).not.toBeVisible();
  fireEvent.click(trigger);
  await waitFor(() => expect(screen.getByText("唤醒后要做什么")).toBeVisible());
  expect(screen.getByText(/以下任一事件发生时/)).toHaveTextContent("当 Grok 的运行失败时");
  expect(screen.getByText(/监听范围/)).toHaveTextContent("当前任务");
});

it("shows a disabled rule independently from its last successful execution", () => {
  wakeup.enabled = false;
  wakeup.disabled_at = "2026-09-16T00:00:00Z";
  status = "completed";
  renderWithI18n(<WakeupsSection issueId="issue" />);
  fireEvent.click(screen.getByRole("button", { name: "Wakeup history 1" }));
  const row = screen.getByRole("button", { name: /Wake Emacs/ });
  expect(row).toHaveTextContent("Turned off");
  expect(row).toHaveTextContent("Last execution: Run succeeded");
});

it("keeps a failed one-shot triggered without calling the rule completed", () => {
  wakeup.enabled = false;
  wakeup.kind = "event";
  wakeup.mode = "once";
  wakeup.event_types = ["comment.created"];
  status = "failed";
  renderWithI18n(<WakeupsSection issueId="issue" />);
  fireEvent.click(screen.getByRole("button", { name: "Wakeup history 1" }));
  const row = screen.getByRole("button", { name: /Wake Emacs/ });
  expect(row).toHaveTextContent("Triggered");
  expect(row).toHaveTextContent("This execution: Run failed");
  expect(row).not.toHaveTextContent("Completed");
});

it("names the monitored member in the condition and details", async () => {
  wakeup.kind = "event";
  wakeup.event_types = ["comment.created", "comment.updated"];
  wakeup.filter_actor_type = "member";
  wakeup.filter_actor_id = "member-id";
  wakeup.filter_actor_name = "Jiayuan";
  renderWithI18n(<WakeupsSection issueId="issue" />, { locale: "zh-Hans" });
  const trigger = screen.getByRole("button", { name: /由 Jiayuan 触发/ });
  expect(trigger).toHaveTextContent("Jiayuan");
  fireEvent.click(trigger);
  await waitFor(() => expect(screen.getByText(/监听的成员或智能体/)).toBeVisible());
  expect(screen.getByText(/以下任一事件发生时/)).toHaveTextContent("由 Jiayuan 触发");
});

it("keeps a redacted actor restriction visible without exposing an ID", () => {
  wakeup.kind = "event";
  wakeup.event_types = ["comment.created"];
  wakeup.filter_actor_type = "agent";
  wakeup.filter_actor_id = null;
  wakeup.filter_actor_name = null;
  renderWithI18n(<WakeupsSection issueId="issue" />);
  expect(screen.getByRole("button", { name: /triggered by Selected agent/ })).toBeInTheDocument();
});
