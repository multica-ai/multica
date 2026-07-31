import { describe, it, expect } from "vitest";
import { paths } from "./paths";
import {
  WORKSPACE_PAGES,
  DEFAULT_ROUTE_ICON_NAME,
  resolveRouteIconName,
  pageForSegment,
  type WorkspacePageKey,
} from "./route-icons";

// Guards the class of bug where a workspace nav route exists but has no
// explicit page entry, so it silently falls back to the default (ListTodo) and
// visually diverges from the rest of the UI. Every parameterless workspace
// route that shows up in the sidebar/tab bar must map to a WORKSPACE_PAGES
// entry.
describe("workspace page coverage", () => {
  // `root` aliases `issues` (same segment) and is never rendered as its own
  // nav item; the parameterized detail routes are resources, not pages.
  const EXCLUDED_METHODS = new Set(["root"]);
  const KNOWN_SEGMENTS = new Set(
    (Object.keys(WORKSPACE_PAGES) as WorkspacePageKey[]).map(
      (k) => WORKSPACE_PAGES[k].segment,
    ),
  );

  it("every parameterless workspace route segment maps to a page", () => {
    const ws = paths.workspace("acme") as unknown as Record<string, () => string>;
    const missing: string[] = [];

    for (const [method, fn] of Object.entries(ws)) {
      if (typeof fn !== "function" || fn.length !== 0) continue;
      if (EXCLUDED_METHODS.has(method)) continue;
      const segment = fn().split("/").filter(Boolean)[1] ?? "";
      if (!KNOWN_SEGMENTS.has(segment)) missing.push(`${method} → "${segment}"`);
    }

    expect(
      missing,
      `these nav routes have no page entry (would fall back to ${DEFAULT_ROUTE_ICON_NAME}): ${missing.join(", ")}`,
    ).toEqual([]);
  });
});

describe("pageForSegment", () => {
  it("maps a known segment to its page key", () => {
    expect(pageForSegment("projects")).toBe("projects");
    expect(pageForSegment("my-issues")).toBe("myIssues");
    expect(pageForSegment("settings")).toBe("settings");
  });

  it("returns null for an unknown segment", () => {
    expect(pageForSegment("not-a-page")).toBeNull();
    expect(pageForSegment("")).toBeNull();
  });
});

describe("resolveRouteIconName", () => {
  it("resolves a page path to its page icon", () => {
    expect(resolveRouteIconName("/acme/projects")).toBe("FolderKanban");
    expect(resolveRouteIconName("/acme/autopilots")).toBe("Zap");
    expect(resolveRouteIconName("/acme/chat")).toBe("MessageSquare");
    expect(resolveRouteIconName("/acme/squads")).toBe("Users");
    expect(resolveRouteIconName("/acme/usage")).toBe("BarChart3");
    expect(resolveRouteIconName("/acme/my-issues")).toBe("CircleUser");
  });

  it("gives sub-routes their parent page icon (sidebar semantics)", () => {
    expect(resolveRouteIconName("/acme/projects/proj-123")).toBe("FolderKanban");
    expect(resolveRouteIconName("/acme/issues/bug-42")).toBe("ListTodo");
  });

  it("ignores the workspace slug and any query/hash", () => {
    expect(resolveRouteIconName("/other-team/projects?x=1#y")).toBe("FolderKanban");
  });

  it("falls back to the default for unknown or too-short paths", () => {
    expect(resolveRouteIconName("/acme/unknown-route")).toBe(DEFAULT_ROUTE_ICON_NAME);
    expect(resolveRouteIconName("/acme")).toBe(DEFAULT_ROUTE_ICON_NAME);
    expect(resolveRouteIconName("/")).toBe(DEFAULT_ROUTE_ICON_NAME);
    expect(resolveRouteIconName("")).toBe(DEFAULT_ROUTE_ICON_NAME);
  });
});
