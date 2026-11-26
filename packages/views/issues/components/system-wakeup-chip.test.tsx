import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import type { SystemWakeup } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { SystemWakeupChips } from "./system-wakeup-chip";

const updateSystem = vi.fn();
let rules: SystemWakeup[] = [];
vi.mock("./wakeup-condition-names", () => ({
  useConditionNames: () => ({ status: (key: string) => key, label: () => undefined, property: () => undefined, actor: (_type: string, id: string) => id }),
}));
vi.mock("../../navigation", () => ({
  AppLink: ({ href, children, ...props }: { href: string; children: ReactNode }) => (
    <a href={href} {...props}>
      {children}
    </a>
  ),
}));
vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({ autopilots: () => "/acme/autopilots" }),
}));
vi.mock("@multica/core/issues", () => ({
  issueSystemWakeupsOptions: () => ({ queryKey: ["system"] }),
  useUpdateIssueSystemWakeup: () => ({ mutateAsync: updateSystem, isPending: false }),
}));
vi.mock("@tanstack/react-query", () => ({
  useQuery: () => ({ data: rules }),
}));
vi.mock("../../common/use-viewing-timezone", () => ({
  useViewingTimezone: () => "UTC",
}));

const childDone = (patch: Partial<SystemWakeup> = {}): SystemWakeup => ({
  id: "rule", revision: 1, default_instruction: "Advance the next stage.", customized: false, paused_reason: null,
  rule: "child_done", workspace_default: true, enabled: true, instruction: "", staged: false, stage: null, total: 3, remaining: 1,
  waiting: ["MUL-7704"], target: { type: "agent", id: "a", name: "Emacs" }, blocked: "",
  ...patch,
});

beforeEach(() => {
  updateSystem.mockReset().mockResolvedValue(undefined);
  rules = [];
});

describe("SystemWakeupChips", () => {
  it("renders nothing without a system rule", () => {
    const { container } = renderWithI18n(<SystemWakeupChips workspaceId="ws" issueId="issue" />);
    expect(container).toBeEmptyDOMElement();
  });

  it("names who is woken and keeps the rule's detail in its popover", async () => {
    rules = [childDone({ staged: true, stage: 1 })];
    renderWithI18n(<SystemWakeupChips workspaceId="ws" issueId="issue" />, { locale: "zh-Hans" });
    fireEvent.click(screen.getByRole("button", { name: "结束后唤醒 Emacs" }));
    expect(await screen.findByText("当第 1 阶段的子任务全部结束时")).toBeVisible();
    expect(screen.getByText("系统")).toBeVisible();
    expect(screen.getByText(/2\/3 已结束/)).toHaveTextContent("还差 MUL-7704");
  });

  it("turns the rule off for this issue from the popover", async () => {
    rules = [childDone()];
    renderWithI18n(<SystemWakeupChips workspaceId="ws" issueId="issue" />);
    fireEvent.click(screen.getByRole("button", { name: "Wakes Emacs when all finish" }));
    fireEvent.click(await screen.findByRole("switch", { name: "Wake the assignee when sub-issues finish" }));
    await waitFor(() => expect(updateSystem).toHaveBeenCalledWith({ rule: "child_done", enabled: false, instruction: "" }));
  });

  it("keeps a quiet chip for a turned-off rule so it can be turned back on", async () => {
    rules = [childDone({ enabled: false, customized: true })];
    renderWithI18n(<SystemWakeupChips workspaceId="ws" issueId="issue" />);
    fireEvent.click(screen.getByRole("button", { name: "Sub-issue wakeup off" }));
    fireEvent.click(await screen.findByRole("switch", { name: "Wake the assignee when sub-issues finish" }));
    await waitFor(() => expect(updateSystem).toHaveBeenCalledWith({ rule: "child_done", enabled: true, instruction: "" }));
  });

  it("explains in the popover why a backlog issue wakes nobody yet", async () => {
    rules = [childDone({ blocked: "backlog" })];
    renderWithI18n(<SystemWakeupChips workspaceId="ws" issueId="issue" />);
    fireEvent.click(screen.getByRole("button", { name: "Wakes Emacs when all finish" }));
    expect(await screen.findByText("Issue is in backlog; no wakeup for now")).toBeVisible();
  });

  it("says a member assignee is notified instead of woken", () => {
    rules = [childDone({ target: { type: "member", id: "u", name: "Jiayuan" }, blocked: "member_assignee" })];
    renderWithI18n(<SystemWakeupChips workspaceId="ws" issueId="issue" />);
    expect(screen.getByRole("button", { name: "Notifies Jiayuan when all finish" })).toBeVisible();
  });

  it("shows the default instruction and saves one for this issue", async () => {
    rules = [childDone()];
    renderWithI18n(<SystemWakeupChips workspaceId="ws" issueId="issue" />);
    fireEvent.click(screen.getByRole("button", { name: "Wakes Emacs when all finish" }));
    expect(await screen.findByText("Advance the next stage.")).toBeVisible();
    expect(screen.getByRole("link", { name: "Change the default" })).toHaveAttribute("href", "/acme/autopilots?tab=wakeups");
    fireEvent.click(screen.getByRole("button", { name: "Edit instruction" }));
    const input = screen.getByLabelText("What to do when woken");
    expect(input).toHaveAttribute("placeholder", "Advance the next stage.");
    fireEvent.change(input, { target: { value: "Ask Jiayuan first" } });
    fireEvent.submit(input.closest("form")!);
    await waitFor(() => expect(updateSystem).toHaveBeenCalledWith({ rule: "child_done", enabled: true, instruction: "Ask Jiayuan first" }));
  });
});
