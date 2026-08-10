// @vitest-environment jsdom
import "@testing-library/jest-dom/vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { HookRunSummary } from "../../core/hook-types";
import { HookHistoryPage } from "./hook-history-page";

afterEach(cleanup);

const stopped: HookRunSummary = {
  id: "run-1", family_id: "family-1", hook_name: "Comment quality gate", policy_version: 2,
  event_type: "before.message.send", agent_id: "agent-1", issue_id: "issue-1",
  decision: "block", enforced: true, requirements: ["Mention a recipient."],
  timed_out: false, latency_ms: 12, created_at: "2026-08-10T10:00:00Z",
};
const observed: HookRunSummary = {
  ...stopped, id: "run-2", hook_name: "Continuation gate", agent_id: "agent-2",
  decision: "allow", would_decision: "block", enforced: false, requirements: [],
};
const directory = { agent: [{ value: "agent-1", label: "Lone" }, { value: "agent-2", label: "Maja" }], issue: [{ value: "issue-1", label: "Hooks UX" }] };

describe("HookHistoryPage", () => {
  it("names the hook, the agent it judged, and what it actually did", () => {
    render(<HookHistoryPage runs={[stopped]} directory={directory} />);

    expect(screen.getByText("Comment quality gate")).toBeInTheDocument();
    expect(screen.getByText("Before message is sent")).toBeInTheDocument();
    expect(screen.getByText("Lone")).toBeInTheDocument();
    expect(screen.getByText("Hooks UX")).toBeInTheDocument();
    expect(screen.getByText("Stopped the action")).toBeInTheDocument();
    expect(screen.getByText("Mention a recipient.")).toBeInTheDocument();
  });

  it("marks a Dry run as having changed nothing, so it is not read as a stop", () => {
    render(<HookHistoryPage runs={[observed]} directory={directory} />);

    expect(screen.getByText("Dry run · changed nothing")).toBeInTheDocument();
  });

  it("searches across hook, trigger, and agent", () => {
    render(<HookHistoryPage runs={[stopped, observed]} directory={directory} />);

    fireEvent.change(screen.getByLabelText("Search history"), { target: { value: "maja" } });
    expect(screen.getByText("Continuation gate")).toBeInTheDocument();
    expect(screen.queryByText("Comment quality gate")).not.toBeInTheDocument();
  });

  it("opens the hook behind a run", () => {
    const onOpenHook = vi.fn();
    render(<HookHistoryPage runs={[stopped]} directory={directory} onOpenHook={onOpenHook} />);

    fireEvent.click(screen.getByRole("button", { name: /Comment quality gate/ }));
    expect(onOpenHook).toHaveBeenCalledWith("family-1");
  });
});
