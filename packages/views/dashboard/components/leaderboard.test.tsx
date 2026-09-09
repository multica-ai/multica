import { describe, expect, it, vi } from "vitest";
import { screen, within } from "@testing-library/react";
import { renderWithI18n } from "../../test/i18n";
import type { AgentDashboardRow } from "../utils";
import { Leaderboard } from "./leaderboard";

vi.mock("../../common/actor-avatar", () => ({
  ActorAvatar: () => <span data-testid="actor-avatar" />,
}));

function row(
  agentId: string,
  cost: number,
  hasUnpricedUsage: boolean,
): AgentDashboardRow {
  return {
    agentId,
    tokens: 1_000_000,
    cost,
    hasUnpricedUsage,
    seconds: 60,
    taskCount: 1,
  };
}

describe("Leaderboard cost completeness", () => {
  it("renders unknown, partial, and explicit-zero costs as distinct states", () => {
    renderWithI18n(
      <Leaderboard
        rows={[
          row("unknown", 0, true),
          row("partial", 1.25, true),
          row("free", 0, false),
        ]}
        agents={[
          { id: "unknown", name: "Unknown price" },
          { id: "partial", name: "Partially priced" },
          { id: "free", name: "Explicitly free" },
        ]}
        deletedAgentCount={0}
        lessThanMinuteLabel="<1m"
      />,
    );

    const rows = within(screen.getByRole("list", { name: "Leaderboard" }))
      .getAllByRole("listitem");
    const findRow = (name: string) =>
      rows.find((item) => item.textContent?.includes(name));

    expect(findRow("Unknown price")).toHaveTextContent("—");
    expect(findRow("Unknown price")?.querySelector("[title]")).toHaveAttribute(
      "title",
      "Cost excludes usage from models without pricing.",
    );
    expect(findRow("Partially priced")).toHaveTextContent("$1.25+");
    expect(findRow("Explicitly free")).toHaveTextContent("$0.00");
  });
});
