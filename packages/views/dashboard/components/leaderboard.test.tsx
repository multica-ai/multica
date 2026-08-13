import { describe, expect, it } from "vitest";
import { screen } from "@testing-library/react";
import { renderWithI18n } from "../../test/i18n";
import { DELETED_AGENTS_ROW_ID } from "../utils";
import { Leaderboard } from "./leaderboard";

describe("Leaderboard", () => {
  it("distinguishes recorded unpriced tokens from zero usage", () => {
    renderWithI18n(
      <Leaderboard
        rows={[
          {
            agentId: DELETED_AGENTS_ROW_ID,
            tokens: 20_243,
            cost: 0,
            costUnpriced: true,
            seconds: 0,
            taskCount: 1,
          },
        ]}
        agents={[]}
        deletedAgentCount={1}
        lessThanMinuteLabel="<1m"
      />,
    );

    expect(screen.getByText("Unpriced")).toBeInTheDocument();
    expect(
      screen.getByTitle(
        "Tokens are recorded, but some model prices are missing; the cost may be incomplete.",
      ),
    ).toBeInTheDocument();
    expect(screen.queryByText("$0.00")).not.toBeInTheDocument();
  });

  it("marks a known cost as incomplete when usage mixes priced and unpriced models", () => {
    renderWithI18n(
      <Leaderboard
        rows={[
          {
            agentId: DELETED_AGENTS_ROW_ID,
            tokens: 30_000,
            cost: 1.25,
            costUnpriced: true,
            seconds: 0,
            taskCount: 2,
          },
        ]}
        agents={[]}
        deletedAgentCount={1}
        lessThanMinuteLabel="<1m"
      />,
    );

    expect(screen.getByText("$1.25+")).toBeInTheDocument();
    expect(screen.queryByText("Unpriced")).not.toBeInTheDocument();
  });
});
