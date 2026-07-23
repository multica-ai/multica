import { describe, expect, it } from "vitest";

import type {
  ProjectListFilters,
  ProjectSortDirection,
  ProjectSortField,
} from "@multica/core/projects";
import type { ProjectTreeItem } from "@multica/core/types";
import type { WorkspaceSprint } from "@multica/cerebro-sprints/core";

import { buildProjectCardGroups } from "./cards";

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
  overrides: Partial<WorkspaceSprint> = {},
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
    ...overrides,
  };
}

const EMPTY_FILTERS: ProjectListFilters = {
  statuses: [],
  priorities: [],
  leads: [],
};

function groups(
  tree: ProjectTreeItem[],
  options: {
    sprints?: WorkspaceSprint[];
    visibleProjectIds?: readonly string[];
    search?: string;
    filters?: ProjectListFilters;
    sortField?: ProjectSortField;
    sortDirection?: ProjectSortDirection;
  } = {},
) {
  return buildProjectCardGroups({
    tree,
    sprints: options.sprints ?? [],
    visibleProjectIds: options.visibleProjectIds,
    search: options.search ?? "",
    filters: options.filters ?? EMPTY_FILTERS,
    sortField: options.sortField ?? "name",
    sortDirection: options.sortDirection ?? "asc",
  });
}

describe("buildProjectCardGroups", () => {
  it("creates one section per top-level folder and keeps nested projects in that section", () => {
    const tree = [
      project("rocks", "Rocks", [
        project("multica", "Multica", [
          project("bugs", "Multica Bugs", [], { parent_project_id: "multica" }),
        ], { parent_project_id: "rocks" }),
        project("support", "Support", [], { parent_project_id: "rocks" }),
      ]),
    ];

    const result = groups(tree);
    const section = result.sections[0]!;

    expect(result.sections).toHaveLength(1);
    expect(section.project.id).toBe("rocks");
    expect(section.cards.map((card) => card.project.id)).toEqual([
      "multica",
      "bugs",
      "support",
    ]);
  });

  it("exposes an active sprint for the section header and for each project card", () => {
    const tree = [
      project("root", "Root", [
        project("child", "Child", [], { parent_project_id: "root" }),
      ]),
    ];
    const result = groups(tree, {
      sprints: [
        sprint("root-active", "root", "active"),
        sprint("child-planned", "child", "planned"),
        sprint("child-active", "child", "active", { end_date: "2026-07-12" }),
      ],
    });
    const section = result.sections[0]!;

    expect(section.activeSprint?.id).toBe("root-active");
    expect(section.cards[0]!.activeSprint?.id).toBe("child-active");
  });

  it("adds only the intermediate path to cards deeper than one level", () => {
    const tree = [
      project("rocks", "Rocks", [
        project("multica", "Multica", [
          project("bugs", "Multica Bugs", [], { parent_project_id: "multica" }),
        ], { parent_project_id: "rocks" }),
      ]),
    ];

    const cards = groups(tree).sections[0]!.cards;

    expect(cards.find((card) => card.project.id === "multica")?.path).toEqual([]);
    expect(cards.find((card) => card.project.id === "bugs")?.path).toEqual(["Multica"]);
  });

  it("puts top-level projects without children in Other projects without duplicating nested leaves", () => {
    const tree = [
      project("folder", "Folder", [
        project("nested-leaf", "Nested leaf", [], { parent_project_id: "folder" }),
      ]),
      project("standalone", "Standalone"),
    ];

    const result = groups(tree);

    expect(result.otherProjects.map((card) => card.project.id)).toEqual(["standalone"]);
    expect(result.sections[0]!.cards.map((card) => card.project.id)).toEqual(["nested-leaf"]);
  });

  it("keeps a matching descendant under its top-level section when searching", () => {
    const tree = [
      project("rocks", "Rocks", [
        project("multica", "Multica", [
          project("bugs", "Multica Bugs", [], { parent_project_id: "multica" }),
        ], { parent_project_id: "rocks" }),
      ]),
      project("other", "Other"),
    ];

    const result = groups(tree, { search: "bugs" });
    const section = result.sections[0]!;

    expect(result.sections).toHaveLength(1);
    expect(section.cards.map((card) => card.project.id)).toEqual(["bugs"]);
    expect(section.cards[0]!.path).toEqual(["Multica"]);
    expect(result.otherProjects).toEqual([]);
  });

  it("accepts the upstream visible project set so existing search semantics stay intact", () => {
    const tree = [
      project("folder", "Folder", [
        project("matched-elsewhere", "非拉丁项目", [], { parent_project_id: "folder" }),
        project("hidden", "Hidden", [], { parent_project_id: "folder" }),
      ]),
    ];

    const result = groups(tree, {
      search: "external matcher",
      visibleProjectIds: ["matched-elsewhere"],
    });

    expect(result.sections[0]!.cards.map((card) => card.project.id)).toEqual([
      "matched-elsewhere",
    ]);
  });
});
