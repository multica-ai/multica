// @vitest-environment jsdom
import "@testing-library/jest-dom/vitest";
import { cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createHookDraft } from "../../core/hook-types";
import { HookEditor } from "./hook-editor";
import { HookTargetPicker } from "./hook-target-picker";

beforeEach(() => {
  Object.defineProperty(window, "innerWidth", { configurable: true, value: 1024 });
  Object.defineProperty(window, "matchMedia", {
    configurable: true,
    value: vi.fn().mockReturnValue({ matches: false, addEventListener: vi.fn(), removeEventListener: vi.fn() }),
  });
});
afterEach(cleanup);

describe("HookEditor", () => {
  it("renders the approved six-step Zapier chain in order", () => {
    render(<HookEditor initialHook={createHookDraft()} onSave={vi.fn()} />);
    const chain = screen.getByLabelText("Hook chain");
    expect(within(chain).getAllByRole("button", { name: /Configure/ }).map((button) => button.textContent)).toEqual([
      expect.stringContaining("Trigger"),
      expect.stringContaining("Applies to"),
      expect.stringContaining("Filter"),
      expect.stringContaining("Decision"),
      expect.stringContaining("Action"),
      expect.stringContaining("On hook failure"),
    ]);
  });

  it("edits filters as field, operator, and value rows without raw JSON", () => {
    render(<HookEditor initialHook={{ ...createHookDraft(), events: ["before.task.complete"], conditions: [{ field: "issue.status", operator: "not_in", value: "done, cancelled" }] }} onSave={vi.fn()} />);
    fireEvent.click(screen.getByRole("button", { name: "Configure Filter" }));
    expect(screen.getByLabelText("Filter field 1")).toHaveTextContent("Issue status");
    expect(screen.getByLabelText("Filter operator 1")).toHaveTextContent("is not one of");
    expect(screen.getByLabelText("Filter value 1")).toHaveValue("done, cancelled");
    expect(screen.queryByText(/\{\s*"field"/)).not.toBeInTheDocument();
  });

  it("states the real all-conditions-must-match behavior without an OR control", () => {
    render(<HookEditor initialHook={{
      ...createHookDraft(),
      events: ["before.task.complete"],
      conditions: [
        { field: "issue.status", operator: "eq", value: "todo", conjunction: "AND" },
        { field: "issue.priority", operator: "eq", value: "urgent", conjunction: "AND" },
      ],
    }} onSave={vi.fn()} />);
    fireEvent.click(screen.getByRole("button", { name: "Configure Filter" }));
    expect(screen.getByText("All of the following must match")).toBeInTheDocument();
    expect(screen.queryByLabelText("Filter conjunction 2")).not.toBeInTheDocument();
  });

  it("uses the same scope step for agent, issue, and session", () => {
    render(<HookEditor initialHook={createHookDraft()} onSave={vi.fn()} />);
    fireEvent.click(screen.getByRole("button", { name: "Configure Applies to" }));
    fireEvent.click(screen.getByRole("button", { name: "Add scope" }));
    fireEvent.click(screen.getByLabelText("Scope 1"));
    expect(screen.getByRole("option", { name: "Agent" })).toBeInTheDocument();
    expect(screen.getByRole("option", { name: "Model" })).toBeInTheDocument();
    expect(screen.getByRole("option", { name: "Issue" })).toBeInTheDocument();
    expect(screen.getByRole("option", { name: "Chat or session" })).toBeInTheDocument();
  });

  it("keeps Publish disabled until dry-run evidence exists", () => {
    const valid = {
      ...createHookDraft(), name: "Guard completion", events: ["before.task.complete" as const],
      bindings: [{ kind: "workspace" as const, value: "" }],
      conditions: [{ field: "issue.status", operator: "eq", value: "todo" }],
      decision: "block" as const, requirement: "Continue the issue",
      actions: [{ type: "audit.record", label: "Record audit event", config: { event: "completion_guarded" } }],
    };
    const { rerender } = render(<HookEditor initialHook={valid} onSave={vi.fn()} canPublish={false} />);
    expect(screen.getByRole("button", { name: "Publish" })).toBeDisabled();
    expect(screen.getByLabelText("Publish requirements")).toHaveTextContent("Run at least one dry-run test before publishing.");
    rerender(<HookEditor initialHook={{ ...valid, baseline_run_count: 4 }} onSave={vi.fn()} canPublish />);
    expect(screen.getByRole("button", { name: "Publish" })).toBeEnabled();
  });

  it("runs Test through the persisted hook API callback before showing history", () => {
    const onTest = vi.fn();
    render(<HookEditor initialHook={{ ...createHookDraft(), id: "hook-1" }} onSave={vi.fn()} onTest={onTest} />);
    fireEvent.click(screen.getByRole("button", { name: "Test" }));
    expect(onTest).toHaveBeenCalledWith(expect.objectContaining({ id: "hook-1" }));
    expect(screen.getByRole("region", { name: "Test and history" })).toBeInTheDocument();
  });

  it("configures Start Handoff as a typed action without commands", () => {
    render(<HookEditor initialHook={{ ...createHookDraft(), actions: [{ type: "session.handoff", label: "Start Handoff", config: {} }] }} onSave={vi.fn()} />);
    fireEvent.click(screen.getByRole("button", { name: "Configure Action" }));
    expect(screen.getByLabelText("Handoff target")).toBeInTheDocument();
    expect(screen.getByLabelText("Handoff plan reference")).toBeInTheDocument();
    expect(screen.getByRole("checkbox")).toBeChecked();
    expect(screen.getByLabelText("Handoff summary")).toBeInTheDocument();
    expect(screen.getByLabelText("Handoff done")).toBeInTheDocument();
    expect(screen.getByLabelText("Handoff remaining")).toBeInTheDocument();
    expect(screen.queryByLabelText(/command|shell/i)).not.toBeInTheDocument();
  });

  it("adds and removes conditions and actions", () => {
    render(<HookEditor initialHook={{ ...createHookDraft(), events: ["before.task.complete"] }} onSave={vi.fn()} />);
    fireEvent.click(screen.getByRole("button", { name: "Configure Filter" }));
    fireEvent.click(screen.getByRole("button", { name: "Add condition" }));
    expect(screen.getAllByRole("button", { name: /Remove condition/ })).toHaveLength(1);
    fireEvent.click(screen.getByRole("button", { name: "Remove condition 1" }));
    expect(screen.queryByRole("button", { name: /Remove condition/ })).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Configure Action" }));
    fireEvent.click(screen.getByRole("button", { name: "Add action" }));
    expect(screen.getByRole("button", { name: "Remove action 1" })).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Remove action 1" }));
    expect(screen.queryByRole("button", { name: /Remove action/ })).not.toBeInTheDocument();
  });

  it("does not show insertion controls that cannot change the chain shape", () => {
    render(<HookEditor initialHook={createHookDraft()} onSave={vi.fn()} />);
    expect(screen.queryByRole("button", { name: /Quick add/i })).not.toBeInTheDocument();
  });

  it("shows a live plain-language explanation and editable description", () => {
    render(<HookEditor initialHook={{ ...createHookDraft(), events: ["before.task.complete"] }} onSave={vi.fn()} />);
    expect(screen.getByLabelText("Hook description")).toBeInTheDocument();
    expect(screen.getByLabelText("What this hook does")).toHaveTextContent("before a task completes");
  });

  it("labels an enforced version honestly", () => {
    render(<HookEditor initialHook={{ ...createHookDraft(), mode: "enforce", version: 4 }} onSave={vi.fn()} />);
    expect(screen.getByText(/Enforced v4/)).toBeInTheDocument();
    expect(screen.queryByText(/Draft v4/)).not.toBeInTheDocument();
  });

  it("explains every selected trigger", () => {
    render(<HookEditor initialHook={{ ...createHookDraft(), events: ["before.task.complete", "on.task.failure"] }} onSave={vi.fn()} />);
    fireEvent.click(screen.getByRole("button", { name: "Configure Trigger" }));
    expect(screen.getByText("Trigger 1:")).toBeInTheDocument();
    expect(screen.getByText("Trigger 2:")).toBeInTheDocument();
  });

  it("saves current edits before publishing and asks for confirmation", async () => {
    const user = userEvent.setup();
    const onSave = vi.fn().mockResolvedValue({ id: "hook-1" });
    const onPublish = vi.fn();
    const valid = {
      ...createHookDraft(), id: "hook-1", name: "Guard completion", events: ["before.task.complete" as const],
      bindings: [{ kind: "workspace" as const, value: "" }],
      conditions: [{ field: "issue.status", operator: "eq", value: "todo" }],
      decision: "block" as const, requirement: "Continue the issue",
      actions: [{ type: "audit.record", label: "Record audit event", config: { event: "completion_blocked" } }],
      baseline_run_count: 4,
    };
    render(<HookEditor initialHook={valid} onSave={onSave} onPublish={onPublish} canPublish />);
    await user.clear(screen.getByLabelText("Hook name"));
    await user.type(screen.getByLabelText("Hook name"), "Changed name");
    await user.click(screen.getByRole("button", { name: "Publish" }));
    expect(screen.getByRole("dialog", { name: "Publish hook" })).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Confirm publish" }));
    expect(onSave).toHaveBeenCalledWith(expect.objectContaining({ name: "Changed name" }));
    expect(onPublish).toHaveBeenCalled();
  });

  it("warns before saving an enforced hook as a dry-run draft", async () => {
    const user = userEvent.setup();
    const onSave = vi.fn();
    render(<HookEditor initialHook={{ ...createHookDraft(), id: "hook-1", mode: "enforce" }} onSave={onSave} />);

    await user.click(screen.getByRole("button", { name: "Save draft" }));
    expect(onSave).not.toHaveBeenCalled();
    expect(screen.getByRole("dialog", { name: "Save as draft?" })).toHaveTextContent("This hook is live. Saving a draft switches it to Dry run until you publish again.");

    await user.click(screen.getByRole("button", { name: "Keep enforced" }));
    expect(onSave).not.toHaveBeenCalled();

    await user.click(screen.getByRole("button", { name: "Save draft" }));
    await user.click(screen.getByRole("button", { name: "Save as draft" }));
    expect(onSave).toHaveBeenCalledWith(expect.objectContaining({ mode: "dry_run" }));
  });

  it("offers lifecycle controls for an existing editable hook", async () => {
    const user = userEvent.setup();
    render(<HookEditor initialHook={{ ...createHookDraft(), id: "hook-1" }} onSave={vi.fn()} onDisable={vi.fn()} onDelete={vi.fn()} onDuplicate={vi.fn()} />);
    await user.click(screen.getByRole("button", { name: "More hook actions" }));
    expect(await screen.findByRole("menuitem", { name: "Disable hook" })).toBeInTheDocument();
    expect(screen.getByRole("menuitem", { name: "Duplicate hook" })).toBeInTheDocument();
    expect(screen.getByRole("menuitem", { name: "Delete hook" })).toBeInTheDocument();
  });

  it("shows honest completion state and the new skill and judge actions", () => {
    render(<HookEditor initialHook={createHookDraft()} onSave={vi.fn()} />);
    expect(screen.getByRole("button", { name: "Configure Trigger" })).toHaveAttribute("data-state", "incomplete");
    fireEvent.click(screen.getByRole("button", { name: "Configure Action" }));
    fireEvent.click(screen.getByRole("button", { name: "Add action" }));
    fireEvent.click(screen.getByLabelText("Action 1 type"));
    expect(screen.getByRole("option", { name: "Run skill" })).toBeInTheDocument();
    expect(screen.getByRole("option", { name: "Judge gate" })).toBeInTheDocument();
  });

  it("uses a stacked layout on mobile-sized screens", () => {
    const { container } = render(<HookEditor initialHook={createHookDraft()} onSave={vi.fn()} />);
    expect(container.querySelector("main")).toHaveClass("grid-cols-1", "md:grid-cols-[minmax(18rem,430px)_minmax(0,1fr)]");
  });

  it("offers a compact six-step wizard with Back and Next controls on mobile", async () => {
    const user = userEvent.setup();
    render(<HookEditor initialHook={createHookDraft()} onSave={vi.fn()} />);

    const steps = screen.getByRole("list", { name: "Hook steps" });
    expect(within(steps).getAllByRole("button")).toHaveLength(6);
    expect(screen.getByRole("button", { name: "Back one step" })).toBeDisabled();
    await user.click(screen.getByRole("button", { name: "Next step" }));
    expect(screen.getByRole("region", { name: "scope configuration" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Back one step" })).toBeEnabled();
  });

  it("uses a full-width bottom sheet for target selection on mobile", async () => {
    Object.defineProperty(window, "innerWidth", { configurable: true, value: 390 });
    Object.defineProperty(window, "matchMedia", {
      configurable: true,
      value: vi.fn().mockReturnValue({ matches: true, addEventListener: vi.fn(), removeEventListener: vi.fn() }),
    });
    const user = userEvent.setup();
    render(<HookTargetPicker label="Agent" value="" options={[{ value: "lone", label: "Lone" }]} onChange={vi.fn()} />);
    await user.click(screen.getByRole("button", { name: "Agent" }));
    expect(await screen.findByRole("dialog")).toHaveAttribute("data-slot", "drawer-content");
    expect(screen.getByRole("dialog")).toHaveClass("w-full");
  });
});
