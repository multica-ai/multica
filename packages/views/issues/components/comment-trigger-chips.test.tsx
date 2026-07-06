import { describe, expect, it, vi } from "vitest";
import { fireEvent, screen } from "@testing-library/react";
import type { CommentTriggerPreviewAgent } from "@multica/core/types";
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

describe("CommentTriggerChips", () => {
  it("renders nothing without agents", () => {
    const { container } = renderWithI18n(
      <CommentTriggerChips agents={[]} suppressedAgentIds={new Set()} onToggle={vi.fn()} />,
    );
    expect(container).toBeEmptyDOMElement();
  });

  it("renders a single agent as a full sentence and toggles on click", () => {
    const onToggle = vi.fn();
    renderWithI18n(
      <CommentTriggerChips agents={[walt]} suppressedAgentIds={new Set()} onToggle={onToggle} />,
    );

    const chip = screen.getByRole("button");
    expect(chip).toHaveTextContent("Will start when sent");
    expect(chip).toHaveAttribute("aria-pressed", "false");

    fireEvent.click(chip);
    expect(onToggle).toHaveBeenCalledWith("agent-1");
  });

  it("dims a suppressed single agent into the skip state", () => {
    renderWithI18n(
      <CommentTriggerChips
        agents={[walt]}
        suppressedAgentIds={new Set(["agent-1"])}
        onToggle={vi.fn()}
      />,
    );

    const chip = screen.getByRole("button");
    expect(chip).toHaveTextContent("Won't start this time");
    expect(chip).toHaveAttribute("aria-pressed", "true");
  });

  it("collapses several agents into a stack with an active count", () => {
    renderWithI18n(
      <CommentTriggerChips
        agents={[walt, bob]}
        suppressedAgentIds={new Set()}
        onToggle={vi.fn()}
      />,
    );

    expect(screen.getByRole("button")).toHaveTextContent("2 agents will start when sent");
  });

  it("counts only non-suppressed agents in the sentence", () => {
    renderWithI18n(
      <CommentTriggerChips
        agents={[walt, bob]}
        suppressedAgentIds={new Set(["agent-2"])}
        onToggle={vi.fn()}
      />,
    );

    expect(screen.getByRole("button")).toHaveTextContent("1 agent will start when sent");
  });

  it("switches to the none-will-trigger state when every agent is suppressed", () => {
    renderWithI18n(
      <CommentTriggerChips
        agents={[walt, bob]}
        suppressedAgentIds={new Set(["agent-1", "agent-2"])}
        onToggle={vi.fn()}
      />,
    );

    expect(screen.getByRole("button")).toHaveTextContent("No agents will start");
  });

  it("opens the popover on click and toggles a row", () => {
    const onToggle = vi.fn();
    renderWithI18n(
      <CommentTriggerChips
        agents={[walt, bob]}
        suppressedAgentIds={new Set()}
        onToggle={onToggle}
      />,
    );

    fireEvent.click(screen.getByRole("button"));

    const row = screen.getByRole("button", { name: /Bob/ });
    expect(row).toHaveTextContent("Bob");
    fireEvent.click(row);
    expect(onToggle).toHaveBeenCalledWith("agent-2");
  });
});
