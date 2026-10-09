import { describe, expect, it, vi } from "vitest";
import { fireEvent, screen, within } from "@testing-library/react";
import type { AgentTask, CommentTriggerPreviewAgent } from "@multica/core/types";
import {
  recipientActions,
  resolveRecipientAction,
  type AgentRunState,
  type RecipientAction,
} from "@multica/core/issues/run-steering";
import type { RecipientEntry } from "../hooks/use-recipient-actions";
import { renderWithI18n } from "../../test/i18n";
import { CommentTriggerChips } from "./comment-trigger-chips";

vi.mock("@multica/core/agents", () => ({
  useAgentPresenceDetail: () => ({ availability: "online", workload: "idle" }),
}));

vi.mock("@multica/core/paths", () => ({
  useCurrentWorkspace: () => ({ id: "ws-1" }),
}));

vi.mock("../../common/actor-avatar", () => ({
  AgentStatusDot: () => <span data-testid="status-dot" />,
}));

const walt: CommentTriggerPreviewAgent = {
  id: "agent-1",
  name: "Walt",
  source: "issue_assignee",
  reason: "",
};

const bob: CommentTriggerPreviewAgent = {
  id: "agent-2",
  name: "Bob",
  source: "mention_agent",
  reason: "",
};


function turn(status: AgentTask["status"]): AgentTask {
  return { id: `turn-${status}`, agent_id: "agent-1", issue_id: "issue", status, priority: 0,
    created_at: "2026-09-23T00:00:00Z", dispatched_at: null, started_at: null, completed_at: null,
    result: null, error: null } as AgentTask;
}
const running: AgentRunState = { kind: "running", task: turn("running"), steerable: true };
const idle: AgentRunState = { kind: "idle" };

function entry(agent: CommentTriggerPreviewAgent, state: AgentRunState, chosen?: RecipientAction): RecipientEntry {
  const opts = { canSteer: true, canRestart: true, steerByDefault: true };
  return { agent, state, action: resolveRecipientAction(state, chosen, opts), actions: recipientActions(state, opts) };
}

describe("CommentTriggerChips", () => {
  it("renders nothing without recipients", () => {
    const { container } = renderWithI18n(<CommentTriggerChips recipients={[]} onActionChange={vi.fn()} />);
    expect(container).toBeEmptyDOMElement();
  });

  it("describes @all semantics without promising any recipients", () => {
    renderWithI18n(<CommentTriggerChips recipients={[]} hasAllMembersMention onActionChange={vi.fn()} />);
    expect(screen.getByText("Member broadcast · @all does not start agents")).toBeInTheDocument();
    expect(screen.queryByText(/notif/i)).not.toBeInTheDocument();
  });

  it("keeps explicit agent triggers visible alongside @all semantics", () => {
    renderWithI18n(<CommentTriggerChips recipients={[entry(bob, idle)]} hasAllMembersMention onActionChange={vi.fn()} />);
    expect(screen.getByText("Member broadcast · @all does not start agents")).toBeInTheDocument();
    expect(screen.getByText("Will start when sent")).toBeInTheDocument();
  });

  it("offers an idle recipient only to start or skip — never to add to a run", async () => {
    const onActionChange = vi.fn();
    renderWithI18n(<CommentTriggerChips recipients={[entry(walt, idle)]} onActionChange={onActionChange} />);
    fireEvent.click(screen.getByRole("button", { name: "Walt trigger: Will start when sent" }));
    const menu = await screen.findByRole("menu");
    expect(within(menu).queryByText("Add to current run")).not.toBeInTheDocument();
    expect(within(menu).getAllByRole("menuitemradio").map((item) => item.textContent)).toEqual([
      expect.stringContaining("Will start when sent"),
      expect.stringContaining("Won't start this time"),
    ]);
    fireEvent.click(within(menu).getByRole("menuitemradio", { name: /Won't start this time/ }));
    expect(onActionChange).toHaveBeenCalledWith("agent-1", "skip");
  });

  it("defaults a running recipient to its turn and explains each choice", async () => {
    const onActionChange = vi.fn();
    renderWithI18n(<CommentTriggerChips recipients={[entry(walt, running)]} onActionChange={onActionChange} />);
    fireEvent.click(screen.getByRole("button", { name: "Walt trigger: Add to current run" }));
    const menu = await screen.findByRole("menu");
    expect(within(menu).getByText("Running")).toBeInTheDocument();
    expect(within(menu).getByText("Walt reads it after the current step and keeps working on the original task. Text only.")).toBeInTheDocument();
    expect(within(menu).getByText("Stops the current run now and starts over from this message.")).toBeInTheDocument();
    fireEvent.click(within(menu).getByRole("menuitemradio", { name: /Start after this run/ }));
    expect(onActionChange).toHaveBeenCalledWith("agent-1", "after_run");
  });

  it("folds a message into a queued run instead of starting another", () => {
    const queued: AgentRunState = { kind: "queued", task: turn("queued") };
    renderWithI18n(<CommentTriggerChips recipients={[entry(walt, queued)]} onActionChange={vi.fn()} />);
    expect(screen.getByText("Include when it starts")).toBeInTheDocument();
  });

  it("dims a skipped recipient", () => {
    renderWithI18n(<CommentTriggerChips recipients={[entry(walt, idle, "skip")]} onActionChange={vi.fn()} />);
    expect(screen.getByText("Won't start this time")).toBeInTheDocument();
  });

  it("stacks several recipients, counting only those that will receive the message", () => {
    renderWithI18n(<CommentTriggerChips recipients={[entry(walt, running), entry(bob, idle, "skip")]} onActionChange={vi.fn()} />);
    expect(screen.getByText("1 agent will receive this")).toBeInTheDocument();
  });

  it("switches to the none-will-trigger state when every recipient is skipped", () => {
    renderWithI18n(<CommentTriggerChips recipients={[entry(walt, idle, "skip"), entry(bob, idle, "skip")]} onActionChange={vi.fn()} />);
    expect(screen.getByText("No agents will start")).toBeInTheDocument();
  });

  it("gives each stacked recipient its own state and choice", async () => {
    const onActionChange = vi.fn();
    renderWithI18n(<CommentTriggerChips recipients={[entry(walt, running), entry(bob, idle)]} onActionChange={onActionChange} />);
    fireEvent.click(screen.getByText("2 agents will receive this"));
    expect(await screen.findByText(/· Running/)).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Bob trigger: Will start when sent" }));
    const menu = await screen.findByRole("menu");
    fireEvent.click(within(menu).getByRole("menuitemradio", { name: /Won't start this time/ }));
    expect(onActionChange).toHaveBeenCalledWith("agent-2", "skip");
  });

  it("names a blocked mention with an error reason instead of a count", () => {
    renderWithI18n(
      <CommentTriggerChips
        recipients={[]}
        blocked={[
          {
            target_type: "agent",
            target_id: "deadbeef-0001",
            status: "blocked",
            reason_code: "invocation_not_allowed",
          },
        ]}
        draftContent="[@Go](mention://agent/deadbeef-0001) hi"
        onActionChange={vi.fn()}
      />,
    );

    // The name the user typed (not a "1 mention won't trigger" count) plus the
    // short reason, which must not assert a permission cause the server never
    // gave: invocation_not_allowed also covers an unresolved id (MUL-5548).
    expect(screen.getByText("Go")).toBeInTheDocument();
    expect(screen.getByText("Not found or no permission")).toBeInTheDocument();
    expect(screen.queryByText(/won't trigger/i)).not.toBeInTheDocument();
  });

  it("falls back to the reason alone when the label can't be correlated", () => {
    renderWithI18n(
      <CommentTriggerChips
        recipients={[]}
        blocked={[
          {
            target_type: "agent",
            target_id: "deadbeef-0001",
            status: "blocked",
            reason_code: "invocation_not_allowed",
          },
        ]}
        // No matching mention markup for the blocked target → no label available.
        draftContent="plain text"
        onActionChange={vi.fn()}
      />,
    );

    expect(screen.getByText("Not found or no permission")).toBeInTheDocument();
    expect(screen.queryByText("Go")).not.toBeInTheDocument();
  });

  it("renders one named chip per blocked mention", () => {
    renderWithI18n(
      <CommentTriggerChips
        recipients={[]}
        blocked={[
          { target_type: "agent", target_id: "deadbeef-0001", status: "blocked", reason_code: "invocation_not_allowed" },
          { target_type: "squad", target_id: "cafef00d-0002", status: "blocked", reason_code: "runtime_offline" },
        ]}
        draftContent="[@Go](mention://agent/deadbeef-0001) [@Ops](mention://squad/cafef00d-0002)"
        onActionChange={vi.fn()}
      />,
    );

    expect(screen.getByText("Go")).toBeInTheDocument();
    expect(screen.getByText("Not found or no permission")).toBeInTheDocument();
    expect(screen.getByText("Ops")).toBeInTheDocument();
    expect(screen.getByText("Runtime offline")).toBeInTheDocument();
  });
});
