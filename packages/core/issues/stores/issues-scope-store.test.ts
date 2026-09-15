// @vitest-environment jsdom
import { beforeEach, describe, expect, it } from "vitest";
import { useIssuesScopeStore } from "./issues-scope-store";

describe("issues scope store", () => {
  beforeEach(() => {
    useIssuesScopeStore.setState({ scopes: {}, projects: {} });
  });

  it("keeps each page's tab independent", () => {
    const { setScope } = useIssuesScopeStore.getState();
    setScope("project:p1", "agents");
    setScope("issues", "members");

    const { scopes } = useIssuesScopeStore.getState();
    expect(scopes["project:p1"]).toBe("agents");
    expect(scopes["issues"]).toBe("members");
    expect(scopes["project:p2"]).toBeUndefined();
  });

  it("migrates the v0 global tab onto the Issues page only", () => {
    const migrate = useIssuesScopeStore.persist.getOptions().migrate!;
    expect(migrate({ scope: "agents" }, 0)).toEqual({
      scopes: { issues: "agents" },
    });
    // v0 "all" (or garbage) starts every page fresh.
    expect(migrate({ scope: "all" }, 0)).toEqual({ scopes: {} });
    expect(migrate({ bogus: true }, 0)).toEqual({ scopes: {} });
    expect(migrate(undefined, 0)).toEqual({ scopes: {} });
  });
});


it("remembers project selection independently for Issues and My Issues", () => {
  const { setProject } = useIssuesScopeStore.getState();
  setProject("issues", "p1");
  setProject("my-issues", "p2");
  expect(useIssuesScopeStore.getState().projects).toEqual({ issues: "p1", "my-issues": "p2" });
  setProject("issues", null);
  expect(useIssuesScopeStore.getState().projects).toEqual({ issues: null, "my-issues": "p2" });
});


it("resets selection when rehydrating a workspace with no saved preferences", () => {
  const state = useIssuesScopeStore.getState();
  const merge = useIssuesScopeStore.persist.getOptions().merge!;
  expect(merge(undefined, { ...state, projects: { issues: "p1" } }).projects).toEqual({});
  expect(merge({ scopes: { issues: "agents" } }, { ...state, projects: { issues: "p1" } }).projects).toEqual({});
});
