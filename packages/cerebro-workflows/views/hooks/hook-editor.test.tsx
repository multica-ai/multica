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
  // CI runners can exceed the default 5s when userEvent types two long fields
  // character-by-character under load (saw 5237ms on PR #2977 frontend).
  it("edits the rule and how to satisfy it", { timeout: 15_000 }, async () => {
    const user = userEvent.setup();
    const onSave = vi.fn();
    render(<HookEditor initialHook={createHookDraft()} onSave={onSave} />);

    await user.type(screen.getByRole("textbox", { name: "Rule" }), "An unfinished issue needs a continuation.");
    await user.type(screen.getByRole("textbox", { name: "How to satisfy it" }), "Create a wakeup or mark the issue blocked.");
    await user.click(screen.getByRole("button", { name: "Save draft" }));

    expect(onSave).toHaveBeenCalledWith(expect.objectContaining({
      contract_rule: "An unfinished issue needs a continuation.",
      contract_satisfy: "Create a wakeup or mark the issue blocked.",
    }));
  });

  it("renders the three grouped steps in order", () => {
    render(<HookEditor initialHook={createHookDraft()} onSave={vi.fn()} />);
    const chain = screen.getByLabelText("Hook chain");
    expect(within(chain).getAllByRole("button", { name: /Configure/ }).map((button) => button.textContent)).toEqual([
      expect.stringContaining("When"),
      expect.stringContaining("Guide or enforce"),
      expect.stringContaining("Actions"),
    ]);
  });

  it("groups trigger, applies-to, and only-when under a single When step", () => {
    render(<HookEditor initialHook={{ ...createHookDraft(), events: ["before.task.complete"], bindings: [{ kind: "workspace", value: "" }] }} onSave={vi.fn()} />);
    fireEvent.click(screen.getByRole("button", { name: "Configure When" }));
    const region = screen.getByRole("region", { name: "when configuration" });
    expect(within(region).getByLabelText("Trigger")).toBeInTheDocument();
    expect(within(region).getByLabelText("Applies to")).toBeInTheDocument();
    expect(within(region).getByLabelText("Only when")).toBeInTheDocument();
  });

  it("edits filters as field, operator, and value rows without raw JSON", () => {
    render(<HookEditor initialHook={{ ...createHookDraft(), events: ["before.task.complete"], conditions: [{ field: "issue.status", operator: "not_in", value: "done, cancelled" }] }} onSave={vi.fn()} />);
    fireEvent.click(screen.getByRole("button", { name: "Configure When" }));
    expect(screen.getByLabelText("Filter field 1")).toHaveTextContent("Issue status");
    expect(screen.getByLabelText("Filter operator 1")).toHaveTextContent("is not one of");
    expect(screen.getByLabelText("Filter value 1")).toHaveValue("done, cancelled");
    expect(screen.queryByText(/\{\s*"field"/)).not.toBeInTheDocument();
  });

  it("treats an empty filter as always and stays valid without conditions", () => {
    const valid = {
      ...createHookDraft(), name: "Guide comments", events: ["before.message.send" as const],
      contract_rule: "Comments must follow the workspace guidance.",
      contract_satisfy: "Write a compliant comment.",
      bindings: [{ kind: "workspace" as const, value: "" }],
      conditions: [],
      decision: "allow" as const,
      actions: [{ type: "audit.record", label: "Record audit event", config: { event: "message_reviewed" } }],
      baseline_run_count: 2,
    };
    render(<HookEditor initialHook={valid} onSave={vi.fn()} canPublish />);
    fireEvent.click(screen.getByRole("button", { name: "Configure When" }));
    expect(screen.getByRole("region", { name: "when configuration" })).toHaveTextContent("Leave empty to run every time");
    expect(screen.getByRole("button", { name: "Configure When" })).toHaveTextContent("Every time");
    expect(screen.getByRole("button", { name: "Configure When" })).toHaveAttribute("data-state", "complete");
    expect(screen.getByRole("button", { name: "Publish" })).toBeEnabled();
  });

  it("lets the user choose whether all or any condition must match", async () => {
    const user = userEvent.setup();
    render(<HookEditor initialHook={{
      ...createHookDraft(),
      events: ["before.task.complete"],
      conditions: [
        { field: "issue.status", operator: "eq", value: "todo", conjunction: "AND" },
        { field: "issue.priority", operator: "eq", value: "urgent", conjunction: "AND" },
      ],
    }} onSave={vi.fn()} />);
    fireEvent.click(screen.getByRole("button", { name: "Configure When" }));
    const all = screen.getByRole("radio", { name: "All conditions" });
    const any = screen.getByRole("radio", { name: "Any condition" });
    expect(all).toBeChecked();
    await user.click(any);
    expect(any).toBeChecked();
  });

  it("uses the same scope control for agent, issue, and session", () => {
    render(<HookEditor initialHook={createHookDraft()} onSave={vi.fn()} />);
    fireEvent.click(screen.getByRole("button", { name: "Configure When" }));
    fireEvent.click(screen.getByRole("button", { name: "Add scope" }));
    fireEvent.click(screen.getByLabelText("Scope 1"));
    expect(screen.getByRole("option", { name: "Agent" })).toBeInTheDocument();
    expect(screen.getByRole("option", { name: "Model" })).toBeInTheDocument();
    expect(screen.getByRole("option", { name: "Issue" })).toBeInTheDocument();
    expect(screen.getByRole("option", { name: "Chat or session" })).toBeInTheDocument();
  });

  it("offers Guide, Require an outcome, and Stop under Guide or enforce", () => {
    render(<HookEditor initialHook={{ ...createHookDraft(), events: ["before.message.send"] }} onSave={vi.fn()} />);
    fireEvent.click(screen.getByRole("button", { name: "Configure Guide or enforce" }));
    expect(screen.getByRole("radio", { name: "Guide" })).toBeInTheDocument();
    expect(screen.getByRole("radio", { name: "Require an outcome" })).toBeInTheDocument();
    expect(screen.getByRole("radio", { name: "Stop" })).toBeInTheDocument();
    fireEvent.click(screen.getByRole("radio", { name: "Require an outcome" }));
    expect(screen.getByLabelText("Required outcome")).toBeInTheDocument();
  });

  it("moves failure behavior to plain-language Advanced choices", () => {
    render(<HookEditor initialHook={{ ...createHookDraft(), actions: [{ type: "audit.record", label: "Record audit event", config: { event: "x" } }] }} onSave={vi.fn()} />);
    fireEvent.click(screen.getByRole("button", { name: "Configure Actions" }));
    const advanced = screen.getByRole("group", { name: "Advanced" });
    expect(within(advanced).getByRole("radio", { name: "Continue and log" })).toBeInTheDocument();
    expect(within(advanced).getByRole("radio", { name: "Stop" })).toBeInTheDocument();
    expect(screen.queryByRole("radio", { name: /Fail open|Fail closed|Fail warn/ })).not.toBeInTheDocument();
  });

  it("keeps Publish disabled until dry-run evidence exists", () => {
    const valid = {
      ...createHookDraft(), name: "Guard completion", events: ["before.task.complete" as const],
      contract_rule: "Tasks need a continuing outcome before completion.",
      contract_satisfy: "Continue the issue before completing the task.",
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

  it("opens the retained event picker and tests the exact saved revision without saving again", async () => {
    const user = userEvent.setup();
    const onTest = vi.fn();
    const onSave = vi.fn();
    render(<HookEditor initialHook={{
      ...createHookDraft(), id: "hook-1", revision: 2,
      compatible_events: [{
        id: "event-journal-1", event_id: "observed-1", event_type: "before.task.complete",
        schema_version: 1, occurred_at: "2026-07-30T10:00:00Z", expires_at: "2026-08-06T10:00:00Z",
      }],
    }} onSave={onSave} onTest={onTest} />);
    await user.click(screen.getByRole("button", { name: "Test" }));
    expect(onTest).not.toHaveBeenCalled();
    expect(screen.getByRole("region", { name: "Test and history" })).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Run test with selected event" }));
    expect(onTest).toHaveBeenCalledWith(expect.objectContaining({ id: "hook-1", revision: 2 }), "event-journal-1");
    expect(onSave).not.toHaveBeenCalled();
  });

  it("configures Start Handoff as a typed action without commands", () => {
    render(<HookEditor initialHook={{ ...createHookDraft(), actions: [{ type: "session.handoff", label: "Start Handoff", config: {} }] }} onSave={vi.fn()} />);
    fireEvent.click(screen.getByRole("button", { name: "Configure Actions" }));
    expect(screen.getByLabelText("Handoff target")).toBeInTheDocument();
    expect(screen.getByLabelText("Handoff plan reference")).toBeInTheDocument();
    expect(screen.getByRole("checkbox")).toBeChecked();
    expect(screen.getByLabelText("Handoff summary")).toBeInTheDocument();
    expect(screen.getByLabelText("Handoff done")).toBeInTheDocument();
    expect(screen.getByLabelText("Handoff remaining")).toBeInTheDocument();
    expect(screen.queryByLabelText(/command|shell/i)).not.toBeInTheDocument();
  });

  it("adds and removes filters and actions", () => {
    render(<HookEditor initialHook={{ ...createHookDraft(), events: ["before.task.complete"] }} onSave={vi.fn()} />);
    fireEvent.click(screen.getByRole("button", { name: "Configure When" }));
    fireEvent.click(screen.getByRole("button", { name: "Add filter" }));
    expect(screen.getAllByRole("button", { name: /Remove filter/ })).toHaveLength(1);
    fireEvent.click(screen.getByRole("button", { name: "Remove filter 1" }));
    expect(screen.queryByRole("button", { name: /Remove filter/ })).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Configure Actions" }));
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
    expect(screen.getByLabelText("What this hook does")).toHaveTextContent("Before task completes");
  });

  it("uses resolved labels and redaction in the editor and Publish summary", async () => {
    const user = userEvent.setup();
    const hook = {
      ...createHookDraft(),
      name: "Notify owner",
      contract_rule: "The issue owner must be notified before completion.",
      contract_satisfy: "Notify the issue owner.",
      events: ["before.task.complete" as const],
      bindings: [{ kind: "issue" as const, value: "private-issue-id" }],
      conditions: [{ field: "issue.status", operator: "eq", value: "in_review" }],
      actions: [{ type: "member.notify", label: "Notify member", config: { member_id: "private-member-id", title: "Private title", message: "Private message" } }],
      baseline_run_count: 1,
    };
    render(<HookEditor
      initialHook={hook}
      onSave={vi.fn()}
      canPublish
      directory={{
        issue: [{ value: "private-issue-id", label: "Returns launch" }],
        member: [{ value: "private-member-id", label: "Jesper" }],
      }}
    />);

    const summary = screen.getByLabelText("What this hook does");
    expect(summary).toHaveTextContent("Returns launch");
    expect(summary).toHaveTextContent("Jesper");
    expect(summary).toHaveTextContent("<redacted>");
    expect(summary).not.toHaveTextContent("private-issue-id");
    expect(summary).not.toHaveTextContent("Private title");

    await user.click(screen.getByRole("button", { name: "Publish" }));
    const publish = screen.getByRole("dialog", { name: "Publish hook" });
    expect(publish).toHaveTextContent("Returns launch");
    expect(publish).toHaveTextContent("Jesper");
    expect(publish).toHaveTextContent("<redacted>");
    expect(publish).not.toHaveTextContent("private-member-id");
    expect(publish).not.toHaveTextContent("Private message");
  });

  it("labels an enforced version honestly", () => {
    render(<HookEditor initialHook={{ ...createHookDraft(), mode: "enforce", version: 4 }} onSave={vi.fn()} />);
    expect(screen.getByText(/Enforced v4/)).toBeInTheDocument();
    expect(screen.queryByText(/Draft v4/)).not.toBeInTheDocument();
  });

  it("explains every selected trigger", () => {
    render(<HookEditor initialHook={{ ...createHookDraft(), events: ["before.task.complete", "on.task.failure"] }} onSave={vi.fn()} />);
    fireEvent.click(screen.getByRole("button", { name: "Configure When" }));
    expect(screen.getByText("Trigger 1:")).toBeInTheDocument();
    expect(screen.getByText("Trigger 2:")).toBeInTheDocument();
  });

  it("requires dirty Draft changes to be saved before Test or Publish", async () => {
    const user = userEvent.setup();
    const valid = {
      ...createHookDraft(), id: "hook-1", name: "Guard completion", events: ["before.task.complete" as const],
      contract_rule: "Tasks need a continuing outcome before completion.",
      contract_satisfy: "Continue the issue before completing the task.",
      family_id: "family-1",
      bindings: [{ kind: "workspace" as const, value: "" }],
      conditions: [{ field: "issue.status", operator: "eq", value: "todo" }],
      decision: "block" as const, requirement: "Continue the issue",
      actions: [{ type: "audit.record", label: "Record audit event", config: { event: "completion_blocked" } }],
      baseline_run_count: 4,
      version: 5,
      revision: 2,
      lifecycle: {
        state: "live_with_draft" as const,
        live_policy_id: "live-v4",
        live_version: 4,
        draft_id: "hook-1",
        draft_series_id: "draft-series-1",
        draft_revision: 2,
        live_unchanged_by_draft: true,
      },
    };
    render(<HookEditor initialHook={valid} onSave={vi.fn()} onPublish={vi.fn()} onTest={vi.fn()} canPublish />);
    await user.clear(screen.getByLabelText("Hook name"));
    await user.type(screen.getByLabelText("Hook name"), "Changed name");
    expect(screen.getByRole("button", { name: "Test" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Publish" })).toBeDisabled();
    expect(screen.getByLabelText("Draft save requirement")).toHaveTextContent("Save draft before testing or publishing.");
  });

  it("publishes the already tested saved Draft revision without saving again", async () => {
    const user = userEvent.setup();
    const onSave = vi.fn();
    const onPublish = vi.fn();
    const valid = {
      ...createHookDraft(), id: "hook-1", name: "Guard completion", events: ["before.task.complete" as const],
      contract_rule: "Tasks need a continuing outcome before completion.",
      contract_satisfy: "Continue the issue before completing the task.",
      family_id: "family-1", bindings: [{ kind: "workspace" as const, value: "" }],
      conditions: [{ field: "issue.status", operator: "eq", value: "todo" }],
      decision: "block" as const, requirement: "Continue the issue",
      actions: [{ type: "audit.record", label: "Record audit event", config: { event: "completion_blocked" } }],
      baseline_run_count: 1, can_publish: true, version: 5, revision: 2,
      lifecycle: {
        state: "live_with_draft" as const, live_policy_id: "live-v4", live_version: 4,
        draft_id: "hook-1", draft_series_id: "draft-series-1", draft_revision: 2,
        live_unchanged_by_draft: true,
      },
    };
    render(<HookEditor initialHook={valid} onSave={onSave} onPublish={onPublish} canPublish />);
    await user.click(screen.getByRole("button", { name: "Publish" }));
    expect(screen.getByRole("dialog", { name: "Publish hook" })).toBeInTheDocument();
    expect(screen.getByText("Publishing replaces Live v4 with Draft v5. Live v4 stays active until this finishes.")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Confirm publish" }));
    expect(onSave).not.toHaveBeenCalled();
    expect(onPublish).toHaveBeenCalled();
  });

  it("saves Draft changes without replacing the Live policy", async () => {
    const user = userEvent.setup();
    const onSave = vi.fn();
    render(<HookEditor initialHook={{
      ...createHookDraft(),
      id: "live-v4",
      family_id: "family-1",
      mode: "enforce",
      version: 4,
      lifecycle: {
        state: "live",
        live_policy_id: "live-v4",
        live_version: 4,
        live_unchanged_by_draft: false,
      },
    }} onSave={onSave} />);

    await user.click(screen.getByRole("button", { name: "Save draft" }));
    expect(onSave).toHaveBeenCalledWith(expect.objectContaining({ mode: "dry_run" }));
    expect(screen.queryByRole("dialog", { name: "Save as draft?" })).not.toBeInTheDocument();
    expect(screen.getByLabelText("What is enforcing")).toHaveTextContent("Version 4 runs on every matching event right now.");
  });

  it("offers Discard draft only when a separate Draft exists", async () => {
    const user = userEvent.setup();
    const onDiscard = vi.fn();
    render(<HookEditor initialHook={{
      ...createHookDraft(),
      id: "draft-r3",
      family_id: "family-1",
      revision: 3,
      version: 5,
      lifecycle: {
        state: "live_with_draft",
        live_policy_id: "live-v4",
        live_version: 4,
        draft_id: "draft-r3",
        draft_revision: 3,
        live_unchanged_by_draft: true,
      },
    }} onSave={vi.fn()} onDiscard={onDiscard} />);

    await user.click(screen.getByRole("button", { name: "More hook actions" }));
    await user.click(await screen.findByRole("menuitem", { name: "Discard draft" }));
    expect(screen.getByRole("dialog", { name: "Discard draft?" })).toHaveTextContent("Live v4 remains active.");
    await user.click(screen.getByRole("button", { name: "Confirm discard" }));
    expect(onDiscard).toHaveBeenCalledWith(expect.objectContaining({ id: "draft-r3" }));
  });

  it("confirms Disable and names the version that stops running", async () => {
    const user = userEvent.setup();
    const onDisable = vi.fn();
    render(<HookEditor initialHook={{
      ...createHookDraft(),
      id: "draft-r3",
      family_id: "family-1",
      revision: 3,
      version: 5,
      lifecycle: {
        state: "live_with_draft",
        live_policy_id: "live-v4",
        live_version: 4,
        draft_id: "draft-r3",
        draft_revision: 3,
        live_unchanged_by_draft: true,
      },
    }} onSave={vi.fn()} onDisable={onDisable} />);

    await user.click(screen.getByRole("button", { name: "More hook actions" }));
    await user.click(await screen.findByRole("menuitem", { name: "Disable hook" }));
    expect(onDisable).not.toHaveBeenCalled();
    const dialog = screen.getByRole("dialog", { name: "Disable hook?" });
    expect(dialog).toHaveTextContent("Live v4 stops running on matching events.");
    expect(dialog).toHaveTextContent("Draft v5 is kept and can still be published later.");
    await user.click(screen.getByRole("button", { name: "Confirm disable" }));
    expect(onDisable).toHaveBeenCalledWith(expect.objectContaining({ id: "draft-r3" }));
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
    expect(screen.getByRole("button", { name: "Configure When" })).toHaveAttribute("data-state", "incomplete");
    fireEvent.click(screen.getByRole("button", { name: "Configure Actions" }));
    fireEvent.click(screen.getByRole("button", { name: "Add action" }));
    fireEvent.click(screen.getByLabelText("Action 1 type"));
    expect(screen.getByRole("option", { name: "Run skill" })).toBeInTheDocument();
    expect(screen.getByRole("option", { name: "Judge gate" })).toBeInTheDocument();
  });

  it("uses a stacked layout on mobile-sized screens", () => {
    const { container } = render(<HookEditor initialHook={createHookDraft()} onSave={vi.fn()} />);
    expect(container.querySelector("main")).toHaveClass("grid-cols-1", "md:grid-cols-[minmax(18rem,430px)_minmax(0,1fr)]");
  });

  it("offers a compact three-step wizard with Back and Next controls on mobile", async () => {
    const user = userEvent.setup();
    render(<HookEditor initialHook={createHookDraft()} onSave={vi.fn()} />);

    const steps = screen.getByRole("list", { name: "Hook steps" });
    expect(within(steps).getAllByRole("button")).toHaveLength(3);
    expect(screen.getByRole("button", { name: "Back one step" })).toBeDisabled();
    await user.click(screen.getByRole("button", { name: "Next step" }));
    expect(screen.getByRole("region", { name: "guide configuration" })).toBeInTheDocument();
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
