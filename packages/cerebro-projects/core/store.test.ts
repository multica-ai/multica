import { beforeEach, describe, expect, it } from "vitest";

import { useProjectsTreeStore } from "./store";

describe("useProjectsTreeStore", () => {
  beforeEach(() => {
    useProjectsTreeStore.setState({
      expandedProjectsByWorkspace: {},
      showCompletedSprintsByWorkspace: {},
    });
  });

  it("tracks expansion and completed-sprint visibility per project", () => {
    useProjectsTreeStore.getState().toggleProject("workspace-1", "project-1");
    useProjectsTreeStore.getState().toggleCompletedSprints("workspace-1", "project-1");

    expect(useProjectsTreeStore.getState().expandedProjectsByWorkspace).toEqual({
      "workspace-1": { "project-1": false },
    });
    expect(useProjectsTreeStore.getState().showCompletedSprintsByWorkspace).toEqual({
      "workspace-1": { "project-1": true },
    });

    useProjectsTreeStore.getState().toggleProject("workspace-1", "project-1");
    expect(useProjectsTreeStore.getState().expandedProjectsByWorkspace).toEqual({
      "workspace-1": { "project-1": true },
    });
  });

  it("isolates tree preferences between workspaces", () => {
    useProjectsTreeStore.getState().toggleProject("workspace-1", "project-1");
    useProjectsTreeStore.getState().toggleCompletedSprints("workspace-2", "project-1");

    expect(useProjectsTreeStore.getState().expandedProjectsByWorkspace["workspace-2"]).toBeUndefined();
    expect(
      useProjectsTreeStore.getState().showCompletedSprintsByWorkspace["workspace-1"],
    ).toBeUndefined();
  });
});
