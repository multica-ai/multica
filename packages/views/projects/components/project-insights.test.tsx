import { describe, expect, it, vi } from "vitest";
import { screen } from "@testing-library/react";
import type { Project } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { ProjectInsights } from "./project-insights";

vi.mock("./project-progress-section", () => ({
  ProjectProgressSection: () => null,
  ProjectBreakdown: () => <p>breakdown rows</p>,
}));
vi.mock("./project-activity", () => ({ ProjectActivityFeed: () => null }));

const project = {
  id: "project-1",
  title: "Mobile 2.0",
  issue_count: 3,
  done_count: 3,
  progress: { unit: "issues", scope: 0, completed: 0, started: 0, unstarted: 0, cancelled: 3, percent: 0 },
} as Project;

describe("ProjectInsights", () => {
  it("leaves the breakdown out while nothing is in scope", () => {
    renderWithI18n(<ProjectInsights project={project} onFilter={vi.fn()} onShowActivity={vi.fn()} />);
    expect(screen.getByRole("button", { name: "Progress" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Breakdown" })).not.toBeInTheDocument();
  });

  it("shows the breakdown once an issue is in scope", () => {
    const inScope = { ...project, progress: { ...project.progress!, scope: 1, cancelled: 2 } };
    renderWithI18n(<ProjectInsights project={inScope} onFilter={vi.fn()} onShowActivity={vi.fn()} />);
    expect(screen.getByRole("button", { name: "Breakdown" })).toBeInTheDocument();
    expect(screen.getByText("breakdown rows")).toBeInTheDocument();
  });
});
