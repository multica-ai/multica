// @vitest-environment jsdom
import "@testing-library/jest-dom/vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { createHookDraft } from "../../core/hook-types";
import { HooksPage } from "./hooks-page";

afterEach(cleanup);

describe("HooksPage", () => {
  it("shows the rule and how to satisfy it on each Hook", () => {
    const hook = Object.assign(createHookDraft(), {
      id: "contract",
      name: "Guard completion",
      contract_rule: "An unfinished issue needs a continuation.",
      contract_satisfy: "Create a wakeup or mark the issue blocked.",
    });
    render(<HooksPage onOpenHook={vi.fn()} hooks={[hook]} />);

    expect(screen.getByText("An unfinished issue needs a continuation.")).toBeInTheDocument();
    expect(screen.getByText("Create a wakeup or mark the issue blocked.")).toBeInTheDocument();
  });

  it("shows seven-day pass and block counts on each Hook", () => {
	const hook = Object.assign(createHookDraft(), {
		id: "metrics",
		name: "Guard completion",
		pass_count_7d: 18,
		block_count_7d: 3,
	});
	render(<HooksPage onOpenHook={vi.fn()} hooks={[hook]} />);

	expect(screen.getByText("18 passed")).toBeInTheDocument();
	expect(screen.getByText("3 blocked")).toBeInTheDocument();
  });

  it("shows one-line chains, last run, and all four states", () => {
    const base = createHookDraft();
    render(<HooksPage onOpenHook={vi.fn()} hooks={[
      { ...base, id: "off", name: "Off policy", mode: "off" },
      { ...base, id: "dry", name: "Dry policy", mode: "dry_run" },
      { ...base, id: "on", name: "Live policy", mode: "enforce" },
      { ...base, id: "managed", name: "Managed policy", mode: "managed" },
    ]} />);
    expect(screen.getAllByText("When Choose a trigger for Choose what this applies to, if no conditions, Guide (let it continue), then No follow-up action.")).toHaveLength(4);
    // Scoped to the rows: the state filter above the list uses the same words.
    expect(screen.getByRole("button", { name: /Off policy/ })).toHaveTextContent("Off");
    expect(screen.getByRole("button", { name: /Dry policy/ })).toHaveTextContent("Dry run");
    expect(screen.getByRole("button", { name: /Live policy/ })).toHaveTextContent("Enforced");
    expect(screen.getByRole("button", { name: /Managed policy/ })).toHaveTextContent("Managed");
    expect(screen.getAllByText("Never")).toHaveLength(4);
  });

  it("uses the shared page header and a responsive list without a wide table", () => {
    const { container } = render(<HooksPage onOpenHook={vi.fn()} hooks={[]} />);
    expect(screen.getByRole("heading", { name: "Hooks" })).toBeInTheDocument();
    expect(container.querySelector("table")).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "New hook" })).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Hooks" }).parentElement?.parentElement).not.toHaveClass("flex-wrap");
  });

  it("opens the stable family route and distinguishes Live with Draft changes", async () => {
    const onOpenHook = vi.fn();
    const base = createHookDraft();
    render(<HooksPage onOpenHook={onOpenHook} hooks={[{
      ...base,
      id: "draft-r3",
      family_id: "family-1",
      name: "Guard completion",
      lifecycle: {
        state: "live_with_draft",
        live_policy_id: "live-v4",
        live_version: 4,
        draft_id: "draft-r3",
        draft_revision: 3,
        live_unchanged_by_draft: true,
      },
    }]} />);

    expect(screen.getByText("Enforced · Draft changes")).toBeInTheDocument();
    screen.getByRole("button", { name: /Guard completion/ }).click();
    expect(onOpenHook).toHaveBeenCalledWith("family-1");
  });

  it("reports malformed list records without hiding valid Hooks", () => {
    const base = createHookDraft();
    render(<HooksPage
      onOpenHook={vi.fn()}
      hooks={[{ ...base, id: "valid", name: "Valid Hook" }]}
      partialErrors={[{ record_id: "broken", code: "hook_record_malformed" }]}
    />);

    expect(screen.getByText("Valid Hook")).toBeInTheDocument();
    expect(screen.getByRole("alert")).toHaveTextContent("1 Hook could not be displayed");
  });

  it("searches hooks by name, rule and generated summary", async () => {
    const base = createHookDraft();
    render(<HooksPage onOpenHook={vi.fn()} hooks={[
      { ...base, id: "a", name: "Guard completion", contract_rule: "An unfinished issue needs a continuation." },
      { ...base, id: "b", name: "Notify on failure", contract_rule: "A failed run must reach a person." },
    ]} />);

    expect(screen.getByRole("button", { name: /Guard completion/ })).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText("Search hooks"), { target: { value: "failed run" } });

    expect(screen.queryByRole("button", { name: /Guard completion/ })).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Notify on failure/ })).toBeInTheDocument();
    expect(screen.getByText("1 of 2")).toBeInTheDocument();
  });

  it("filters by what a hook does to a run right now", () => {
    const base = createHookDraft();
    render(<HooksPage onOpenHook={vi.fn()} hooks={[
      { ...base, id: "live", name: "Live policy", mode: "enforce" },
      { ...base, id: "dry", name: "Dry policy", mode: "dry_run" },
    ]} />);

    fireEvent.click(screen.getByRole("button", { name: "Dry run" }));

    expect(screen.getByRole("button", { name: /Dry policy/ })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /Live policy/ })).not.toBeInTheDocument();
  });

  it("says so when the search matches nothing instead of showing an empty page", () => {
    const base = createHookDraft();
    render(<HooksPage onOpenHook={vi.fn()} hooks={[{ ...base, id: "a", name: "Guard completion" }]} />);

    fireEvent.change(screen.getByLabelText("Search hooks"), { target: { value: "zzz" } });

    expect(screen.getByText("No hook matches this search or filter.")).toBeInTheDocument();
    expect(screen.queryByRole("heading", { name: "Turn workflow moments into safe automation" })).not.toBeInTheDocument();
  });

  it("shows on every row whether the hook can stop an action", () => {
    const base = createHookDraft();
    render(<HooksPage onOpenHook={vi.fn()} hooks={[
      { ...base, id: "stop", name: "Blocking policy", decision: "block" },
      { ...base, id: "guide", name: "Guiding policy", decision: "allow" },
    ]} />);

    expect(screen.getByRole("button", { name: /Blocking policy/ })).toHaveTextContent("Stop the action");
    expect(screen.getByRole("button", { name: /Guiding policy/ })).toHaveTextContent("Guide (let it continue)");
  });

  it("renders a truncated safe summary with resolved labels on each card", () => {
    const base = createHookDraft();
    const { container } = render(<HooksPage
      onOpenHook={vi.fn()}
      directory={{ issue: [{ value: "private-issue-id", label: "Returns launch" }] }}
      hooks={[{
        ...base,
        id: "safe",
        name: "Safe Hook",
        events: ["before.task.complete"],
        bindings: [{ kind: "issue", value: "private-issue-id" }],
        actions: [{ type: "issue.comment", label: "Comment on issue", config: { body: "Private message" } }],
      }]}
    />);

    const summary = container.querySelector("span[title]");
    expect(summary).toHaveClass("truncate");
    expect(summary).toHaveTextContent("Returns launch");
    expect(summary).toHaveTextContent("<redacted>");
    expect(summary).not.toHaveTextContent("private-issue-id");
    expect(summary).not.toHaveTextContent("Private message");
  });
});

describe("HooksPage — reading the list without opening every hook (FIR-4797)", () => {
  const live = Object.assign(createHookDraft(), {
    id: "draft-rev", family_id: "family-1", name: "Chain approval",
    contract_rule: "Approve the final step first.", contract_satisfy: "Approve it.",
    mode: "dry_run" as const, decision: "allow" as const, actions: [],
    lifecycle: { state: "live_with_draft" as const, live_policy_id: "live-1", live_version: 1, draft_id: "draft-rev", live_unchanged_by_draft: true },
    live: { policy_id: "live-1", version: 1, name: "Chain approval", decision: "block" as const, requirement: "Approve it.", fail_mode: "closed" as const },
  });

  it("explains every state at the top of the page instead of leaving the words unexplained", () => {
    render(<HooksPage onOpenHook={vi.fn()} hooks={[Object.assign(createHookDraft(), { id: "a", name: "A", mode: "enforce" as const })]} />);

    const legend = screen.getByLabelText("What the states mean");
    expect(legend).toHaveTextContent("It runs on every matching event and can stop, change, or start work.");
    expect(legend).toHaveTextContent("it never stops, changes, or starts anything");
    expect(legend).toHaveTextContent("The edit is saved as a Draft that changes nothing until someone presses");
  });

  it("shows the decision that is actually enforcing, not the unpublished draft's", () => {
    render(<HooksPage onOpenHook={vi.fn()} hooks={[live]} />);

    const row = screen.getByRole("button", { name: /Chain approval/ });
    expect(row).toHaveTextContent("Stop the action");
    expect(row).toHaveTextContent("Draft (not live):");
  });

  it("flags a draft that cannot be published without calling the live hook broken", () => {
    render(<HooksPage onOpenHook={vi.fn()} hooks={[live]} />);

    const row = screen.getByRole("button", { name: /Chain approval/ });
    expect(row).toHaveTextContent("Enforced · Draft changes");
    expect(row).toHaveTextContent("Draft cannot be published");
  });

  it("filters on trigger, decision, scope, and action, not only on state", () => {
    const complete = Object.assign(createHookDraft(), { id: "one", family_id: "f1", name: "Completion guard", events: ["before.task.complete" as const], bindings: [{ kind: "workspace" as const, value: "" }], decision: "block" as const, actions: [{ type: "audit.record", label: "Record audit event", config: {} }] });
    const message = Object.assign(createHookDraft(), { id: "two", family_id: "f2", name: "Message guard", events: ["before.message.send" as const], bindings: [{ kind: "agent" as const, value: "agent-1" }], decision: "allow" as const, actions: [{ type: "issue.comment", label: "Comment on issue", config: {} }] });
    render(<HooksPage onOpenHook={vi.fn()} hooks={[complete, message]} />);

    fireEvent.change(screen.getByLabelText("Trigger"), { target: { value: "before.message.send" } });
    expect(screen.getByLabelText("Trigger")).toBeInTheDocument();
    expect(screen.getByLabelText("Decision")).toBeInTheDocument();
    expect(screen.getByLabelText("Applies to")).toBeInTheDocument();
    expect(screen.getByLabelText("Action")).toBeInTheDocument();
  });

  it("offers a way to reach the history of every hook at once", () => {
    const onOpenHistory = vi.fn();
    render(<HooksPage onOpenHook={vi.fn()} onOpenHistory={onOpenHistory} hooks={[]} />);

    fireEvent.click(screen.getByRole("button", { name: "History" }));
    expect(onOpenHistory).toHaveBeenCalled();
  });
});
