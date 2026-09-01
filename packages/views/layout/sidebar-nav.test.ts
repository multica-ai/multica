import { describe, expect, it } from "vitest";
import { configureNav, personalNav, workspaceNav } from "./sidebar-nav";

describe("sidebar nav groups", () => {
  it("keeps the workspace group to issues and projects", () => {
    expect(workspaceNav.map((item) => item.key)).toEqual(["issues", "projects"]);
  });

  it("moves autopilots, squads, and usage into configure", () => {
    expect(configureNav.map((item) => item.key)).toEqual([
      "runtimes",
      "autopilots",
      "squads",
      "usage",
      "skills",
      "settings",
    ]);
  });

  it("does not give agents its own sidebar row", () => {
    const keys = [...personalNav, ...workspaceNav, ...configureNav].map(
      (item) => item.key,
    );
    expect(keys).not.toContain("agents");
  });
});
