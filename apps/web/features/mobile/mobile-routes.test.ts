import { describe, expect, it } from "vitest";
import {
  MOBILE_BOTTOM_NAV_ITEMS,
  MOBILE_DRAWER_NAV_ITEMS,
  mobileRoutes,
} from "./mobile-routes";

describe("mobileRoutes", () => {
  it("builds workspace-scoped /m routes without colliding with desktop routes", () => {
    expect(mobileRoutes("acme")).toEqual({
      root: "/acme/m/issues",
      kanban: "/acme/m/kanban",
      issues: "/acme/m/issues",
      projects: "/acme/m/projects",
      inbox: "/acme/m/inbox",
      runtime: "/acme/m/runtime",
      chat: "/acme/m/chat",
      settings: "/acme/m/settings",
    });
  });

  it("keeps the bottom navigation to five primary tabs", () => {
    expect(MOBILE_BOTTOM_NAV_ITEMS).toHaveLength(5);
    expect(MOBILE_BOTTOM_NAV_ITEMS.map((item) => item.routeKey)).toEqual([
      "kanban",
      "issues",
      "projects",
      "inbox",
      "settings",
    ]);
  });

  it("keeps secondary mobile routes reachable from the drawer", () => {
    expect(MOBILE_DRAWER_NAV_ITEMS.map((item) => item.routeKey)).toEqual([
      "runtime",
      "chat",
    ]);
  });
});
