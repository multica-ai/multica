import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import "@testing-library/jest-dom/vitest";

import type { ProjectListFilters } from "@multica/core/projects";
import type { ProjectTreeItem } from "@multica/core/types";
import type { WorkspaceSprint } from "@multica/cerebro-sprints/core";

import { useProjectsTreeStore } from "../core/store";
import { CerebroProjectCards } from "./project-card-groups";

function project(
  id: string,
  title: string,
  children: ProjectTreeItem[] = [],
  parentProjectId: string | null = null,
): ProjectTreeItem {
  return {
    id,
    workspace_id: "ws-1",
    title,
    description: null,
    icon: null,
    color: null,
    repo_url: null,
    status: "planned",
    priority: "none",
    lead_type: null,
    lead_id: null,
    access: "workspace",
    created_at: "2026-07-01T00:00:00Z",
    updated_at: "2026-07-01T00:00:00Z",
    issue_count: 0,
    done_count: 0,
    resource_count: 0,
    parent_project_id: parentProjectId,
    show_descendants: true,
    depth: 0,
    children,
  };
}

function sprint(id: string, projectId: string): WorkspaceSprint {
  return {
    id,
    workspace_id: "ws-1",
    project_id: projectId,
    project_title: "Project",
    name: id,
    sequence_no: 1,
    status: "active",
    start_date: "2026-07-06",
    end_date: "2026-07-19",
    issue_count: 4,
    done_count: 2,
    created_at: "2026-07-01T00:00:00Z",
    updated_at: "2026-07-01T00:00:00Z",
  };
}

const EMPTY_FILTERS: ProjectListFilters = {
  statuses: [],
  priorities: [],
  leads: [],
};

function renderCards() {
  const tree = [
    project("rocks", "Rocks", [
      project("multica", "Multica", [
        project("bugs", "Multica Bugs", [], "multica"),
      ], "rocks"),
    ]),
    project("standalone", "Standalone"),
  ];
  return render(
    <CerebroProjectCards
      workspaceId="ws-1"
      tree={tree}
      sprints={[sprint("Sprint 24", "rocks"), sprint("Sprint #3", "bugs")]}
      search=""
      filters={EMPTY_FILTERS}
      sortField="name"
      sortDirection="asc"
      renderProjectCard={({ project, details }) => (
        <article data-testid={`card-${project.id}`}>
          <span>{project.title}</span>
          {details}
        </article>
      )}
      renderSprintLink={(activeSprint, children) => (
        <a href={`/sprints/${activeSprint.id}`}>{children}</a>
      )}
    />,
  );
}

beforeEach(() => {
  useProjectsTreeStore.setState({
    expandedProjectsByWorkspace: {},
    showCompletedSprintsByWorkspace: {},
  });
});

afterEach(cleanup);

describe("CerebroProjectCards", () => {
  it("collapses and expands a top-level project section", () => {
    renderCards();

    expect(screen.getByTestId("card-multica")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Collapse Rocks" }));
    expect(screen.queryByTestId("card-multica")).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Expand Rocks" }));
    expect(screen.getByTestId("card-multica")).toBeInTheDocument();
  });

  it("renders intermediate paths, sprint links, and the Other projects group", () => {
    renderCards();

    expect(screen.getByTestId("card-bugs")).toHaveTextContent("Multica");
    expect(screen.getByRole("link", { name: /Sprint #3/ })).toHaveAttribute(
      "href",
      "/sprints/Sprint #3",
    );
    expect(screen.getByRole("link", { name: /Sprint 24/ })).toHaveAttribute(
      "href",
      "/sprints/Sprint 24",
    );
    expect(screen.getByRole("heading", { name: /Other projects/ })).toBeInTheDocument();
    expect(screen.getByTestId("card-standalone")).toBeInTheDocument();
  });
});
