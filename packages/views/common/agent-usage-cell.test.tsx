import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { AgentUsageCell } from "./agent-usage-cell";

// The live ActorAvatar pulls in workspace hooks, navigation, and hover cards;
// stub it down to a marker that exposes the actorId so we can assert it links
// through to the agent profile rather than the deleted placeholder.
vi.mock("./actor-avatar", () => ({
  ActorAvatar: ({ actorId }: { actorId: string }) => (
    <span data-testid="live-avatar" data-actor-id={actorId} />
  ),
}));

// The base avatar is what the deleted placeholder renders directly.
vi.mock("@multica/ui/components/common/actor-avatar", () => ({
  ActorAvatar: ({ name }: { name: string }) => (
    <span data-testid="base-avatar" data-name={name} />
  ),
}));

describe("AgentUsageCell", () => {
  it("renders the live avatar and name for a resolved agent", () => {
    render(
      <AgentUsageCell
        agentId="agent-1"
        agent={{ name: "Atlas" }}
        agentsLoaded
        deletedLabel="Deleted agent"
      />,
    );

    expect(screen.getByTestId("live-avatar")).toHaveAttribute(
      "data-actor-id",
      "agent-1",
    );
    expect(screen.getByText("Atlas")).toBeInTheDocument();
    expect(screen.queryByTestId("base-avatar")).not.toBeInTheDocument();
    expect(screen.queryByText("Deleted agent")).not.toBeInTheDocument();
  });

  it("renders the deleted placeholder once the list has loaded without the agent", () => {
    render(
      <AgentUsageCell
        agentId="agent-gone"
        agent={undefined}
        agentsLoaded
        deletedLabel="Deleted agent"
      />,
    );

    expect(screen.getByTestId("base-avatar")).toHaveAttribute(
      "data-name",
      "Deleted agent",
    );
    expect(screen.getByText("Deleted agent")).toBeInTheDocument();
    // No live avatar — a deleted agent must not link through to a 404 profile.
    expect(screen.queryByTestId("live-avatar")).not.toBeInTheDocument();
  });

  it("keeps the live avatar for an absent agent while the list is still loading", () => {
    render(
      <AgentUsageCell
        agentId="agent-2"
        agent={undefined}
        agentsLoaded={false}
        deletedLabel="Deleted agent"
      />,
    );

    // Absent + not loaded only means the agent list is still resolving, so we
    // keep the live avatar rather than flashing the deleted placeholder.
    expect(screen.getByTestId("live-avatar")).toHaveAttribute(
      "data-actor-id",
      "agent-2",
    );
    // Falls back to the id so the row stays identifiable instead of blank
    // while the agent list is still resolving.
    expect(screen.getByText("agent-2")).toBeInTheDocument();
    expect(screen.queryByTestId("base-avatar")).not.toBeInTheDocument();
    expect(screen.queryByText("Deleted agent")).not.toBeInTheDocument();
  });
});
