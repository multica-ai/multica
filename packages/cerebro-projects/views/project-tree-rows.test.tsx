import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import "@testing-library/jest-dom/vitest";

import type { ProjectColumnKey, ProjectListFilters } from "@multica/core/projects";
import type { ProjectTreeItem } from "@multica/core/types";

import { useProjectsTreeStore } from "../core/store";
import { CerebroProjectTreeRows } from "./project-tree-rows";

function project(id: string, title: string, children: ProjectTreeItem[] = []): ProjectTreeItem {
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
    parent_project_id: null,
    show_descendants: true,
    depth: 0,
    children,
  };
}

const EMPTY_FILTERS: ProjectListFilters = {
  statuses: [],
  priorities: [],
  leads: [],
};

beforeEach(() => {
  useProjectsTreeStore.setState({
    expandedProjectsByWorkspace: {},
    showCompletedSprintsByWorkspace: {},
  });
});

afterEach(cleanup);

describe("CerebroProjectTreeRows", () => {
  it("renders for a workspace with no persisted tree preferences", () => {
    render(
      <CerebroProjectTreeRows
        workspaceId="ws-1"
        tree={[project("root", "Root project", [project("child", "Child project")])]}
        sprints={[]}
        search=""
        filters={EMPTY_FILTERS}
        sortField="name"
        sortDirection="asc"
        isColVisible={(_key: ProjectColumnKey) => true}
        renderProjectRow={({ project: rowProject, namePrefix }) => (
          <div key={rowProject.id}>
            {namePrefix}
            <span>{rowProject.title}</span>
          </div>
        )}
        renderSprintLink={(_sprint, children) => <a href="/sprints/test">{children}</a>}
      />,
    );

    expect(screen.getByText("Root project")).toBeInTheDocument();
    expect(screen.getByText("Child project")).toBeInTheDocument();
  });
});
