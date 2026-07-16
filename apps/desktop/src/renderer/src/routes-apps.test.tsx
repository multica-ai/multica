// @vitest-environment jsdom
import { describe, expect, it } from "vitest";
import type { RouteObject } from "react-router-dom";
import { appRoutes } from "./routes";

function routePaths(routes: RouteObject[], parent = ""): string[] {
  return routes.flatMap((route) => {
    const path = route.path ? `${parent}/${route.path}`.replace(/\/+/g, "/") : parent;
    return [path, ...routePaths(route.children ?? [], path)];
  });
}

describe("desktop Apps routes", () => {
  it("wires the catalog, builder, and full app surface inside a workspace", () => {
    expect(routePaths(appRoutes)).toEqual(expect.arrayContaining([
      "/:workspaceSlug/apps",
      "/:workspaceSlug/apps/new",
      "/:workspaceSlug/apps/:appId",
    ]));
  });
});
