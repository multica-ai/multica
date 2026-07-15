// @vitest-environment jsdom
import "@testing-library/jest-dom/vitest";
import { cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { createHookDraft } from "../../core/hook-types";
import { HookEditor } from "./hook-editor";

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
    render(<HookEditor initialHook={createHookDraft()} onSave={vi.fn()} />);
    fireEvent.click(screen.getByRole("button", { name: "Configure Filter" }));
    expect(screen.getByLabelText("Filter field 1")).toHaveValue("issue.status");
    expect(screen.getByLabelText("Filter operator 1")).toHaveValue("not_in");
    expect(screen.getByLabelText("Filter value 1")).toHaveValue("done, cancelled");
    expect(screen.queryByText(/\{\s*"field"/)).not.toBeInTheDocument();
  });

  it("uses the same scope step for agent, issue, and session", () => {
    render(<HookEditor initialHook={createHookDraft()} onSave={vi.fn()} />);
    fireEvent.click(screen.getByRole("button", { name: "Configure Applies to" }));
    expect(screen.getByRole("option", { name: "Agent or model" })).toBeInTheDocument();
    expect(screen.getByRole("option", { name: "Issue" })).toBeInTheDocument();
    expect(screen.getByRole("option", { name: "Chat or session" })).toBeInTheDocument();
  });

  it("keeps Publish disabled until dry-run evidence exists", () => {
    const { rerender } = render(<HookEditor initialHook={createHookDraft()} onSave={vi.fn()} canPublish={false} />);
    expect(screen.getByRole("button", { name: "Publish" })).toBeDisabled();
    rerender(<HookEditor initialHook={{ ...createHookDraft(), baseline_run_count: 4 }} onSave={vi.fn()} canPublish />);
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
    render(<HookEditor initialHook={createHookDraft()} onSave={vi.fn()} />);
    fireEvent.click(screen.getByRole("button", { name: "Configure Action" }));
    fireEvent.change(screen.getByLabelText("Action 1 type"), { target: { value: "session.handoff" } });
    expect(screen.getByLabelText("Handoff target")).toBeInTheDocument();
    expect(screen.getByLabelText("Handoff plan reference")).toBeInTheDocument();
    expect(screen.getByLabelText("Start new session now")).toBeChecked();
    expect(screen.getByLabelText("Handoff summary")).toBeInTheDocument();
    expect(screen.getByLabelText("Handoff done")).toBeInTheDocument();
    expect(screen.getByLabelText("Handoff remaining")).toBeInTheDocument();
    expect(screen.queryByLabelText(/command|shell/i)).not.toBeInTheDocument();
  });
});
