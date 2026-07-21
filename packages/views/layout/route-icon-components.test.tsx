import { describe, it, expect } from "vitest";
import { paths, WORKSPACE_PAGES, DEFAULT_ROUTE_ICON_NAME } from "@multica/core/paths";
import { ROUTE_ICON_COMPONENTS, routeIconForPath } from "./route-icon-components";

describe("ROUTE_ICON_COMPONENTS", () => {
  it("has a component for every page icon", () => {
    const missing = Object.values(WORKSPACE_PAGES)
      .map((page) => page.icon)
      .filter((name) => !ROUTE_ICON_COMPONENTS[name]);
    expect(missing).toEqual([]);
  });
});

describe("routeIconForPath", () => {
  // The invariant this whole contract exists for (MUL-4370): the sidebar and
  // the desktop tab bar both resolve from a path, so the same route can never
  // render two different icons.
  it("gives a route the same component wherever it is rendered", () => {
    const p = paths.workspace("acme");
    for (const href of [p.projects(), p.autopilots(), p.chat(), p.squads(), p.usage()]) {
      // Sidebar passes the bare nav href; a tab passes its own url, which for
      // a sub-route carries extra segments.
      expect(routeIconForPath(`${href}/some-id`)).toBe(routeIconForPath(href));
    }
  });

  it("resolves distinct components for distinct routes", () => {
    const p = paths.workspace("acme");
    expect(routeIconForPath(p.projects())).not.toBe(routeIconForPath(p.issues()));
    expect(routeIconForPath(p.autopilots())).not.toBe(routeIconForPath(p.issues()));
  });

  it("returns the default component instead of undefined for an unknown route", () => {
    expect(routeIconForPath("/acme/not-a-route")).toBe(
      ROUTE_ICON_COMPONENTS[DEFAULT_ROUTE_ICON_NAME],
    );
    expect(routeIconForPath("")).toBe(ROUTE_ICON_COMPONENTS[DEFAULT_ROUTE_ICON_NAME]);
  });
});
