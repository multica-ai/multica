import { describe, it, expect } from "vitest";
import { paths } from "./paths";
import {
  ROUTE_ICON_NAMES,
  DEFAULT_ROUTE_ICON_NAME,
  resolveRouteIconName,
} from "./route-icons";

// Guards the class of bug where a workspace nav route exists but has no
// explicit icon, so it silently falls back to the default (ListTodo) and
// visually diverges from the rest of the UI. Every parameterless workspace
// route that shows up in the sidebar/tab bar must have an explicit entry in
// ROUTE_ICON_NAMES.
describe("route icon coverage", () => {
  // `root` aliases `issues` (same segment) and is never rendered as its own
  // nav item, so it's excluded from the icon requirement.
  const EXCLUDED_METHODS = new Set(["root"]);

  it("every parameterless workspace route segment has an explicit icon", () => {
    const ws = paths.workspace("acme") as unknown as Record<string, () => string>;
    const missing: string[] = [];

    for (const [method, fn] of Object.entries(ws)) {
      if (typeof fn !== "function" || fn.length !== 0) continue;
      if (EXCLUDED_METHODS.has(method)) continue;
      const segment = fn().split("/").filter(Boolean)[1] ?? "";
      if (!(segment in ROUTE_ICON_NAMES)) missing.push(`${method} → "${segment}"`);
    }

    expect(
      missing,
      `these nav routes have no explicit icon (would fall back to ${DEFAULT_ROUTE_ICON_NAME}): ${missing.join(", ")}`,
    ).toEqual([]);
  });
});

describe("resolveRouteIconName", () => {
  it("resolves the route segment at index 1 of a workspace-scoped path", () => {
    expect(resolveRouteIconName("/acme/projects")).toBe("FolderKanban");
    expect(resolveRouteIconName("/acme/autopilots")).toBe("Zap");
    expect(resolveRouteIconName("/acme/my-issues")).toBe("CircleUser");
    // Sub-routes keep the parent route's icon.
    expect(resolveRouteIconName("/acme/projects/proj-123")).toBe("FolderKanban");
  });

  it("falls back to the default for unknown or too-short paths", () => {
    expect(resolveRouteIconName("/acme/unknown-route")).toBe(DEFAULT_ROUTE_ICON_NAME);
    expect(resolveRouteIconName("/acme")).toBe(DEFAULT_ROUTE_ICON_NAME);
    expect(resolveRouteIconName("/")).toBe(DEFAULT_ROUTE_ICON_NAME);
    expect(resolveRouteIconName("")).toBe(DEFAULT_ROUTE_ICON_NAME);
  });
});
