// @vitest-environment jsdom

import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { SkillChangesNavBadge } from "./skill-changes-nav-badge";

const { mine } = vi.hoisted(() => ({
  mine: { current: [] as Array<{ id: string }> },
}));

vi.mock("@tanstack/react-query", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-query")>()),
  useQuery: () => ({ data: [{ id: "skill-1" }] }),
}));

vi.mock("../use-skill-changes", () => ({
  useSkillChanges: () => ({
    all: mine.current,
    mine: mine.current,
    isLoading: false,
  }),
}));

afterEach(() => {
  cleanup();
  mine.current = [];
});

describe("SkillChangesNavBadge", () => {
  it("renders nothing when there are no skill changes to review", () => {
    render(<SkillChangesNavBadge workspaceId="workspace-1" />);

    expect(
      screen.queryByTestId("skills-sidebar-change-count"),
    ).toBeNull();
  });

  it("renders the pending skill change count", () => {
    mine.current = Array.from({ length: 4 }, (_, index) => ({
      id: `change-${index}`,
    }));

    render(<SkillChangesNavBadge workspaceId="workspace-1" />);

    expect(
      screen.getByTestId("skills-sidebar-change-count").textContent,
    ).toBe("4");
    expect(
      screen.getByLabelText("4 skill changes to review"),
    ).toBeTruthy();
  });

  it("caps large counts at 99+", () => {
    mine.current = Array.from({ length: 100 }, (_, index) => ({
      id: `change-${index}`,
    }));

    render(<SkillChangesNavBadge workspaceId="workspace-1" />);

    expect(
      screen.getByTestId("skills-sidebar-change-count").textContent,
    ).toBe("99+");
  });
});
