import type { RouteObject } from "react-router-dom";
import { describe, expect, it } from "vitest";
import { parseTabSubject } from "@multica/core/paths";
import { appRoutes } from "./routes";

function collect(routes: RouteObject[]): RouteObject[] {
  return routes.flatMap((route) => [route, ...collect(route.children ?? [])]);
}

const workspaceRoute = collect(appRoutes).find(
  (route) => route.path === ":workspaceSlug",
);
const workspacePaths = (workspaceRoute?.children ?? [])
  .map((route) => route.path)
  .filter((path): path is string => !!path);

describe("appRoutes titles", () => {
  it("carries no route-level titles", () => {
    for (const route of collect(appRoutes)) {
      expect(route.handle, route.path ?? "(index)").toBeUndefined();
    }
  });

  it.each(workspacePaths)("resolves /:slug/%s to a localized tab subject", (path) => {
    const url = `/acme/${path.replace(/:[A-Za-z]+/g, "x1")}`;
    expect(parseTabSubject(url).kind).not.toBe("unknown");
  });

  it("finds the workspace routes", () => {
    expect(workspacePaths.length).toBeGreaterThan(20);
  });
});
