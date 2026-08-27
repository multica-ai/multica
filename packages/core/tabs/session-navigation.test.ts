// @vitest-environment node
// @ts-nocheck
import { beforeEach, describe, expect, it } from "vitest";
import {
  extractWorkspaceSlug,
  navigateSessionPush,
  openSessionTab,
  routeContentLinkPath,
  tryRouteToOtherWorkspace,
} from "./session-navigation";
import { getActiveTab, useTabStore } from "./tab-store";

beforeEach(() => {
  useTabStore.getState().reset();
  useTabStore.getState().switchWorkspace("acme", "/acme/issues");
});

describe("extractWorkspaceSlug", () => {
  it("returns the first segment of a workspace-scoped path", () => {
    expect(extractWorkspaceSlug("/acme/issues")).toBe("acme");
  });

  it("rejects reserved prefixes", () => {
    expect(extractWorkspaceSlug("/login")).toBeNull();
    expect(extractWorkspaceSlug("/workspaces/new")).toBeNull();
  });
});

describe("session navigation", () => {
  it("switches workspace groups instead of leaking a foreign slug into the active group", () => {
    expect(tryRouteToOtherWorkspace("/butter/inbox")).toBe(true);
    expect(useTabStore.getState().activeWorkspaceSlug).toBe("butter");
    expect(getActiveTab(useTabStore.getState())?.url).toBe("/butter/inbox");
  });

  it("navigates in place on a same-workspace push", () => {
    navigateSessionPush("/acme/projects");
    expect(getActiveTab(useTabStore.getState())?.url).toBe("/acme/projects");
    expect(useTabStore.getState().byWorkspace.acme.tabs).toHaveLength(1);
  });

  it("opens a background tab without activating it", () => {
    openSessionTab("/acme/projects", "", { activate: false });
    const state = useTabStore.getState();
    expect(getActiveTab(state)?.url).toBe("/acme/issues");
    expect(state.byWorkspace.acme.tabs).toHaveLength(2);
  });

  it("routes a content-link foreground disposition into a focused tab", () => {
    routeContentLinkPath("/acme/projects", "foreground-tab");
    expect(getActiveTab(useTabStore.getState())?.url).toBe("/acme/projects");
  });
});
