// @vitest-environment jsdom
import { beforeEach, describe, expect, it } from "vitest";
import { useIssuesScopeStore } from "./issues-scope-store";

describe("issues scope store", () => {
  beforeEach(() => {
    useIssuesScopeStore.setState({ scopes: {} });
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
