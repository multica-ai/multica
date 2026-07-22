// @vitest-environment jsdom
import "@testing-library/jest-dom/vitest";
import { useState } from "react";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

// This package runs vitest without globals, so RTL's auto-cleanup never fires.
afterEach(cleanup);

import { NavigationProvider } from "@multica/views/navigation";
import type { NavigationAdapter } from "@multica/views/navigation";

import type { LoopChainBlock, LoopChainSpec, WorkflowEvalBinding } from "../core/types";
import {
  ChainRail,
  AgentCandidates,
  ApprovalTemplateField,
  EvalGateFields,
  StepStatusFields,
  applyGateSelection,
  gateOptionValue,
} from "./workflow-issue-loop-form";

vi.mock("@multica/views/autopilots/components/pickers/agent-picker", () => ({
  AgentPicker: () => <button type="button">Select agent</button>,
}));

const navigation: NavigationAdapter = {
  push: vi.fn(),
  replace: vi.fn(),
  back: vi.fn(),
  pathname: "/acme/workflows/wf-1",
  searchParams: new URLSearchParams(),
  getShareableUrl: (path: string) => `https://example.test${path}`,
};

function withNavigation(node: React.ReactNode) {
  return <NavigationProvider value={navigation}>{node}</NavigationProvider>;
}

const chain: LoopChainSpec = {
  version: 2,
  done_status: "done",
  phases: [
    {
      id: "build",
      name: "Build and verify",
      limits: { max_steps: 8, max_rounds: 3, no_progress_stalls: 2, max_wait_seconds: 600 },
      // A command step keeps the render free of pickers that would need a
      // workspace + query shell, so this seam stays a pure UI assertion.
      blocks: [{ id: "check", type: "command", name: "Run the checks", check: ["make", "test"] }],
    },
  ],
};

function renderRail(openId: string | null = null) {
  return render(
    withNavigation(
      <ChainRail
        chain={chain}
        openId={openId}
        onToggle={vi.fn()}
        onReorder={vi.fn()}
        onNudge={vi.fn()}
        onReorderPhase={vi.fn()}
        onNudgePhase={vi.fn()}
        onPhaseChange={vi.fn()}
        onRemovePhase={vi.fn()}
        onAddPhase={vi.fn()}
        onAddBlock={vi.fn()}
        onRemoveBlock={vi.fn()}
        doneStatus="done"
        onDoneStatusChange={vi.fn()}
      />,
    ),
  );
}

describe("Issue workflow editor — phase row", () => {
  it("renders the phase name as a real, editable, placeholder-carrying field", () => {
    const onPhaseChange = vi.fn();
    render(
      withNavigation(
        <ChainRail
          chain={chain}
          openId={null}
          onToggle={vi.fn()}
          onReorder={vi.fn()}
          onNudge={vi.fn()}
          onReorderPhase={vi.fn()}
          onNudgePhase={vi.fn()}
          onPhaseChange={onPhaseChange}
          onRemovePhase={vi.fn()}
          onAddPhase={vi.fn()}
          onAddBlock={vi.fn()}
          onRemoveBlock={vi.fn()}
          doneStatus="done"
          onDoneStatusChange={vi.fn()}
        />,
      ),
    );

    const field = screen.getByLabelText("Phase name");
    expect(field).toHaveValue("Build and verify");
    expect(field).not.toBeDisabled();
    expect(field).toHaveAttribute("placeholder", "Name this phase");

    fireEvent.change(field, { target: { value: "Ship it" } });
    expect(onPhaseChange).toHaveBeenCalledWith("build", expect.any(Function));
  });

  it("pairs every Input font size with its md: twin, because Input bakes in md:text-sm", () => {
    renderRail();
    // Without the md: twin these render 14px on desktop no matter what the
    // base class says — class merging does not drop a responsive variant.
    const field = screen.getByLabelText("Phase name");
    expect(field.className).toContain("text-xs");
    expect(field.className).toContain("md:text-xs");
  });

  it("picks the finish status from the board's statuses instead of taking typed text", () => {
    renderRail();
    const control = screen.getByLabelText("Done status");
    // A typed status used to save fine and then park the issue in a status the
    // board does not have. The control is now a picker, so that cannot happen.
    expect(control.tagName).not.toBe("INPUT");
    expect(control).toHaveTextContent("Done");
  });

  it("offers a drag handle on the phase row, not only on steps", () => {
    renderRail();
    expect(screen.getByLabelText("Drag to reorder phase")).toBeVisible();
    expect(screen.getByLabelText("Drag to reorder")).toBeVisible();
  });

  it("calls nudgePhase from the keyboard so reordering never needs a pointer", () => {
    const onNudgePhase = vi.fn();
    render(
      withNavigation(
        <ChainRail
          chain={chain}
          openId={null}
          onToggle={vi.fn()}
          onReorder={vi.fn()}
          onNudge={vi.fn()}
          onReorderPhase={vi.fn()}
          onNudgePhase={onNudgePhase}
          onPhaseChange={vi.fn()}
          onRemovePhase={vi.fn()}
          onAddPhase={vi.fn()}
          onAddBlock={vi.fn()}
          onRemoveBlock={vi.fn()}
          doneStatus="done"
          onDoneStatusChange={vi.fn()}
        />,
      ),
    );

    fireEvent.keyDown(screen.getByLabelText("Drag to reorder phase"), { key: "ArrowDown" });
    expect(onNudgePhase).toHaveBeenCalledWith("build", 1);
  });

  it("names the limits panel Settings and explains each number", () => {
    renderRail();
    expect(screen.queryByText("limits")).not.toBeInTheDocument();

    fireEvent.click(screen.getByText("Settings"));
    expect(screen.getByText("How many steps this phase may open in total.")).toBeVisible();
    expect(screen.getByText("How many times this phase may start over.")).toBeVisible();
    expect(screen.getByText("How many rounds without progress before the phase stops.")).toBeVisible();
    expect(screen.getByText("How long to wait for a free agent before giving up.")).toBeVisible();
  });
});

describe("Issue workflow editor — step card", () => {
  it("no longer exposes the engine's internal block ID for editing", () => {
    renderRail("check");

    // The step is open — its own fields are on screen…
    expect(screen.getByPlaceholderText("Name this step")).toHaveValue("Run the checks");
    // …but the engine's step identity is not editable any more. Changing it on
    // a running workflow made the engine lose track of the step.
    expect(screen.queryByText("Stable block ID")).not.toBeInTheDocument();
    expect(screen.queryByText(/Advanced/)).not.toBeInTheDocument();
  });

  it("places a newly added agent selector where the clicked button was", () => {
    function AgentCandidateHarness() {
      const [block, setBlock] = useState<LoopChainBlock>({
        id: "session",
        type: "session",
        name: "Session",
        agents: [],
      });

      return <AgentCandidates block={block} onChange={setBlock} />;
    }

    render(<AgentCandidateHarness />);
    const addButton = screen.getByRole("button", { name: "Add agent" });
    fireEvent.click(addButton);

    const selector = screen.getByRole("button", { name: "Select agent" });
    expect(selector.compareDocumentPosition(addButton) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  });
});

describe("Human approval template", () => {
  it("explains that the preceding agent fills the delivered work and evidence", () => {
    render(<ApprovalTemplateField block={{ id: "approve", type: "human", prompt: "{{previous.output}}" }} onChange={vi.fn()} />);
    expect(screen.getByText(/fills \{\{previous.output\}\} and \{\{previous.evidence\}\}/i)).toBeVisible();
    expect(screen.getByDisplayValue("{{previous.output}}")).toBeVisible();
  });
});

const binding = (over: Partial<WorkflowEvalBinding>): WorkflowEvalBinding => ({
  id: "b1",
  workspace_id: "ws",
  workflow_id: "wf-1",
  eval_id: "e1",
  phase: "delivery",
  blocking: true,
  eval_key: "delivery-quality",
  eval_version: "1",
  eval_title: "Delivery quality",
  created_at: "2026-07-22T00:00:00Z",
  ...over,
});

describe("Eval step — quality gate", () => {
  it("refuses free text when no gate is bound, and points at Workflow gates", () => {
    render(
      withNavigation(
        <EvalGateFields
          block={{ id: "e", type: "eval", eval_phase: "delivery" }}
          bindings={[]}
          gatesHref="/acme/workflows/evals"
          onChange={vi.fn()}
        />,
      ),
    );

    expect(screen.getByText("No quality gate is bound to this workflow yet.")).toBeVisible();
    expect(screen.getByRole("link", { name: "Open Workflow gates" })).toHaveAttribute(
      "href",
      "/acme/workflows/evals",
    );
    // The old free-text key field is what let an unresolvable key be saved.
    expect(screen.queryByRole("textbox")).not.toBeInTheDocument();
  });

  it("picks from the workflow's bound gates instead of a text field", () => {
    render(
      withNavigation(
        <EvalGateFields
          block={{ id: "e", type: "eval", eval_key: "delivery-quality", eval_phase: "delivery" }}
          bindings={[binding({}), binding({ id: "b2", phase: "monitor", eval_key: "drift-watch", eval_title: "Drift watch" })]}
          gatesHref="/acme/workflows/evals"
          onChange={vi.fn()}
        />,
      ),
    );

    expect(screen.queryByRole("textbox")).not.toBeInTheDocument();
    expect(screen.getByLabelText("Quality gate")).toBeVisible();
  });

  it("shows the phase as a consequence of the gate, and says monitor can never block", () => {
    render(
      withNavigation(
        <EvalGateFields
          block={{ id: "e", type: "eval", eval_key: "drift-watch", eval_phase: "monitor" }}
          bindings={[binding({ phase: "monitor", eval_key: "drift-watch" })]}
          gatesHref="/acme/workflows/evals"
          onChange={vi.fn()}
        />,
      ),
    );

    expect(screen.getByText(/advisory only, it warns but never blocks the run/i)).toBeVisible();
    // The phase is reported, not offered as a second, contradictable choice.
    expect(screen.queryByLabelText("Eval phase")).not.toBeInTheDocument();
  });
});

describe("gate option encoding", () => {
  it("carries key and phase together, because the engine resolves on the pair", () => {
    expect(gateOptionValue("delivery-quality", "monitor")).toBe("delivery-quality::monitor");
  });

  it("moves key and phase together when a gate is picked", () => {
    const block = { id: "e", type: "eval" as const, eval_key: "old", eval_phase: "delivery" as const };
    expect(applyGateSelection(block, gateOptionValue("drift-watch", "monitor"))).toEqual({
      id: "e",
      type: "eval",
      eval_key: "drift-watch",
      eval_phase: "monitor",
    });
  });

  it("ignores an empty selection rather than clearing the gate", () => {
    const block = { id: "e", type: "eval" as const, eval_key: "keep", eval_phase: "plan" as const };
    expect(applyGateSelection(block, "")).toBe(block);
  });
});

describe("per-step status control", () => {
  it("offers a status before and after the step, both optional", () => {
    render(
      withNavigation(
        <StepStatusFields
          block={{ id: "build", type: "session", status_on_start: "in_progress" }}
          onChange={vi.fn()}
        />,
      ),
    );

    // Before this change the only status a chain could set was the phase's and
    // the chain's final one, so a run could not show "In review" while a review
    // step waited.
    expect(screen.getByLabelText("Status before this step")).toHaveTextContent("In progress");
    expect(screen.getByLabelText("Status after this step")).toHaveTextContent("Leave unchanged");
  });

  it("keeps a status a newer board added rather than rewriting it on open", () => {
    render(
      withNavigation(
        <StepStatusFields
          block={{ id: "build", type: "session", status_on_done: "waiting_on_customer" }}
          onChange={vi.fn()}
        />,
      ),
    );

    expect(screen.getByLabelText("Status after this step")).toHaveTextContent("waiting_on_customer");
  });
});
