import { describe, expect, it } from "vitest";

import type {
  ProjectListFilters,
  ProjectSortDirection,
  ProjectSortField,
} from "@multica/core/projects";
import type { ProjectTreeItem } from "@multica/core/types";
import type { WorkspaceSprint } from "@multica/cerebro-sprints/core";

import { buildProjectTreeRows } from "./tree";

function project(
  id: string,
  title: string,
  children: ProjectTreeItem[] = [],
  overrides: Partial<ProjectTreeItem> = {},
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
    parent_project_id: null,
    show_descendants: true,
    depth: 0,
    children,
    ...overrides,
  };
}

function sprint(
  id: string,
  projectId: string,
  status: WorkspaceSprint["status"],
): WorkspaceSprint {
  return {
    id,
    workspace_id: "ws-1",
    project_id: projectId,
    project_title: "Project",
    name: id,
    sequence_no: 1,
    status,
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

function rows(
  tree: ProjectTreeItem[],
  options: {
    sprints?: WorkspaceSprint[];
    expandedProjects?: Record<string, boolean>;
    showCompletedSprints?: Record<string, boolean>;
    search?: string;
    filters?: ProjectListFilters;
    sortField?: ProjectSortField;
    sortDirection?: ProjectSortDirection;
  } = {},
) {
  return buildProjectTreeRows({
    tree,
    sprints: options.sprints ?? [],
    expandedProjects: options.expandedProjects ?? {},
    showCompletedSprints: options.showCompletedSprints ?? {},
    search: options.search ?? "",
    filters: options.filters ?? EMPTY_FILTERS,
    sortField: options.sortField ?? "name",
    sortDirection: options.sortDirection ?? "asc",
  });
}

describe("buildProjectTreeRows", () => {
  it("sorts each sibling group without separating children from their parent", () => {
    const tree = [
      project("root-b", "Bravo"),
      project("root-a", "Alpha", [
        project("child-z", "Zulu", [], { parent_project_id: "root-a" }),
        project("child-a", "Able", [], { parent_project_id: "root-a" }),
      ]),
    ];

    const result = rows(tree).filter((row) => row.kind === "project");

    expect(result.map((row) => row.project.id)).toEqual([
      "root-a",
      "child-a",
      "child-z",
      "root-b",
    ]);
    expect(result.map((row) => row.depth)).toEqual([0, 1, 1, 0]);
  });

  it("hides descendants and sprint rows when a project is collapsed", () => {
    const tree = [
      project("root", "Root", [
        project("child", "Child", [], { parent_project_id: "root" }),
      ]),
    ];

    const result = rows(tree, {
      sprints: [sprint("active-sprint", "root", "active")],
      expandedProjects: { root: false },
    });

    expect(result.map((row) => row.kind === "project" ? row.project.id : row.kind)).toEqual([
      "root",
    ]);
    expect(result[0]).toMatchObject({ kind: "project", hasChildren: true, expanded: false });
  });

  it("flattens search matches and shows their parent path", () => {
    const tree = [
      project("rocks", "Rocks", [
        project("multica", "Multica", [
          project("bugs", "Multica Bugs", [], { parent_project_id: "multica" }),
        ], { parent_project_id: "rocks" }),
      ]),
    ];

    const result = rows(tree, { search: "bugs" });

    expect(result).toHaveLength(1);
    expect(result[0]).toMatchObject({
      kind: "project",
      depth: 0,
      path: ["Rocks", "Multica"],
      hasChildren: false,
    });
  });

  it("keeps a non-matching ancestor as dimmed context for a matching child", () => {
    const tree = [
      project("root", "Root", [
        project("match", "Match", [], {
          parent_project_id: "root",
          status: "in_progress",
        }),
        project("miss", "Miss", [], { parent_project_id: "root" }),
      ]),
    ];

    const result = rows(tree, {
      filters: { ...EMPTY_FILTERS, statuses: ["in_progress"] },
    }).filter((row) => row.kind === "project");

    expect(result.map((row) => [row.project.id, row.contextOnly])).toEqual([
      ["root", true],
      ["match", false],
    ]);
  });

  it("shows active and planned sprints by default and gates completed sprints per project", () => {
    const tree = [project("root", "Root")];
    const sprints = [
      sprint("active", "root", "active"),
      sprint("planned", "root", "planned"),
      sprint("done", "root", "done"),
      sprint("cancelled", "root", "cancelled"),
    ];

    const hidden = rows(tree, { sprints });
    expect(hidden.map((row) => row.kind === "sprint" ? row.sprint.id : row.kind)).toEqual([
      "project",
      "active",
      "planned",
      "completed-toggle",
    ]);
    expect(hidden.at(-1)).toMatchObject({ kind: "completed-toggle", count: 1, expanded: false });

    const shown = rows(tree, {
      sprints,
      showCompletedSprints: { root: true },
    });
    expect(shown.map((row) => row.kind === "sprint" ? row.sprint.id : row.kind)).toEqual([
      "project",
      "active",
      "planned",
      "done",
      "completed-toggle",
    ]);
  });
});
