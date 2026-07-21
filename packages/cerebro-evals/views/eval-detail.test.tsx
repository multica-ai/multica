// @vitest-environment jsdom

import { describe, expect, it, vi } from "vitest";
import { render, screen, fireEvent, within } from "@testing-library/react";
import { EvalDetail } from "./eval-detail";
import type { CerebroEval, EvalBinding } from "../types";

function makeEval(overrides: Partial<CerebroEval> = {}): CerebroEval {
  return {
    id: "e1", workspace_id: "w1", key: "customer-service-quality", version: "1.0.0",
    title: "Customer-service reply quality", description: "", status: "draft",
    owner: { team: "Firtal AI" }, objective: "Answer accurately and on policy.",
    target: { kind: "agent", locator: "mention://agent/x", ref: "main" },
    datasets: [{ id: "t1", situation: "Angry refund request", expected: "offer refund", critical: true }],
    graders: [{ id: "grader-1", type: "ai_judge", config: { model: "gpt-judge" } }],
    thresholds: [{ metric: "pass_rate", operator: "gte", value: 0.8 }, { metric: "all_critical_pass", operator: "eq", value: true }],
    runner: {}, source: { repository: "https://github.com/firtal-group/firtal-evals", commit: "abc", path: "evals/x/eval.json" },
    created_by_id: "u1", created_by_type: "member",
    created_at: "2026-07-18T06:00:00Z", updated_at: "2026-07-18T06:00:00Z",
    ...overrides,
  };
}

const baseProps = {
  runs: [], runsLoading: false, runsError: false,
  selectedRunId: "", selectedRun: null, onSelectRun: () => {},
  bindings: [] as EvalBinding[], workflows: [],
  onEdit: () => {}, onDuplicate: () => {}, onStatusChange: () => {},
  statusPending: false, statusError: false, onDelete: () => {},
  onRunNow: () => {}, runPending: false, runError: false,
  schedule: null, scheduleLoading: false, schedulePending: false, scheduleError: false,
  onSaveSchedule: () => {}, onOpenHooks: () => {},
  onClose: () => {}, onOpenVersion: () => {},
};

describe("EvalDetail", () => {
  it("shows the eval header, status, version and overview facts", () => {
    const item = makeEval();
    render(<EvalDetail {...baseProps} item={item} versions={[item]} />);
    expect(screen.getByText("Customer-service reply quality")).toBeInTheDocument();
    expect(screen.getAllByText("draft").length).toBeGreaterThan(0);
    expect(screen.getAllByText("1.0.0").length).toBeGreaterThan(0);
    // Overview surfaces objective, grader and pass rule as plain lines.
    expect(screen.getByText("Answer accurately and on policy.")).toBeInTheDocument();
    expect(screen.getByText("AI judge · gpt-judge")).toBeInTheDocument();
    expect(screen.getByText("Pass rate ≥ 80% · every critical task must pass")).toBeInTheDocument();
  });

  // Lifecycle moves live only on the Settings tab — they used to be duplicated
  // in the detail header, which left two controls firing the same transition.
  it("activates a draft from the Settings tab", () => {
    const onStatusChange = vi.fn();
    const item = makeEval({ status: "draft" });
    render(<EvalDetail {...baseProps} item={item} versions={[item]} initialTab="settings" onStatusChange={onStatusChange} />);
    fireEvent.click(screen.getByRole("button", { name: "Activate" }));
    expect(onStatusChange).toHaveBeenCalledWith("active");
  });

  it("keeps the lifecycle controls out of the detail header", () => {
    const item = makeEval({ status: "draft" });
    render(<EvalDetail {...baseProps} item={item} versions={[item]} />);
    // Overview is the default tab; Activate is reachable from Settings only.
    expect(screen.queryByRole("button", { name: "Activate" })).not.toBeInTheDocument();
  });

  it("runs an active eval from the detail header", () => {
    const onRunNow = vi.fn();
    const item = makeEval({ status: "active" });
    render(<EvalDetail {...baseProps} item={item} versions={[item]} onRunNow={onRunNow} />);
    fireEvent.click(screen.getByRole("button", { name: "Run now" }));
    expect(onRunNow).toHaveBeenCalledOnce();
  });

  // Retiring ends a version, so it asks first — through the themed AlertDialog
  // the rest of the workspace uses, not the browser's native confirm().
  it("asks in a dialog before retiring an active eval", async () => {
    const onStatusChange = vi.fn();
    const item = makeEval({ status: "active" });
    render(<EvalDetail {...baseProps} item={item} versions={[item]} initialTab="settings" onStatusChange={onStatusChange} />);

    fireEvent.click(screen.getByRole("button", { name: "Retire" }));
    // Opening the dialog must not retire anything on its own.
    expect(onStatusChange).not.toHaveBeenCalled();

    const dialog = await screen.findByRole("alertdialog");
    expect(within(dialog).getByText("Retire 1.0.0?")).toBeInTheDocument();

    fireEvent.click(within(dialog).getByRole("button", { name: "Retire" }));
    expect(onStatusChange).toHaveBeenCalledWith("retired");
  });

  it("lists version history and opens another version", () => {
    const onOpenVersion = vi.fn();
    const v1 = makeEval({ id: "e1", version: "1.0.0", status: "retired" });
    const v2 = makeEval({ id: "e2", version: "2.0.0", status: "active", created_at: "2026-07-19T06:00:00Z" });
    render(<EvalDetail {...baseProps} item={v1} versions={[v1, v2]} onOpenVersion={onOpenVersion} />);
    expect(screen.getByText("2.0.0")).toBeInTheDocument();
    fireEvent.click(screen.getByText("Open"));
    expect(onOpenVersion).toHaveBeenCalledWith("e2");
  });

  it("shows the eval's tasks on the Tasks tab", () => {
    const item = makeEval();
    render(<EvalDetail {...baseProps} item={item} versions={[item]} initialTab="tasks" />);
    expect(screen.getByText("Angry refund request")).toBeInTheDocument();
  });

  it("shows bound workflows on the Connections tab", () => {
    const item = makeEval();
    const binding: EvalBinding = {
      id: "b1", workspace_id: "w1", workflow_id: "wf1", eval_id: "e1", phase: "delivery", blocking: true,
      eval_key: "customer-service-quality", eval_version: "1.0.0", eval_title: item.title, created_at: "2026-07-18T06:00:00Z",
    };
    render(<EvalDetail {...baseProps} item={item} versions={[item]} initialTab="connections" bindings={[binding]} workflows={[{ id: "wf1", name: "Support loop" }]} />);
    expect(screen.getByText("Support loop")).toBeInTheDocument();
  });

  it("closes back to the catalog", () => {
    const onClose = vi.fn();
    const item = makeEval();
    render(<EvalDetail {...baseProps} item={item} versions={[item]} onClose={onClose} />);
    fireEvent.click(screen.getByLabelText("Back to catalog"));
    expect(onClose).toHaveBeenCalled();
  });
});
