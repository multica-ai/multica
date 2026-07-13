import { afterEach, describe, expect, it } from "vitest";
import { render, cleanup, waitFor } from "@testing-library/react";
import {
  createMemoryRouter,
  RouterProvider,
  Navigate,
  Outlet,
  type DataRouter,
  type RouteObject,
} from "react-router-dom";
import {
  HistoryMirror,
  navigateByDelta,
  type HistorySnapshot,
} from "./history-mirror";

// Loader-free routes, matching the app (routes.tsx has no loaders): every
// navigation commits directly to idle with its final (location, historyAction).
const routes: RouteObject[] = [
  { path: "/a" },
  { path: "/b" },
  { path: "/c" },
  { path: "/d" },
  { path: "/s" },
];

function bareRouter(entries: string[], index?: number): DataRouter {
  const router = createMemoryRouter(routes, {
    initialEntries: entries,
    initialIndex: index,
  });
  router.initialize();
  return router;
}

afterEach(cleanup);

describe("HistoryMirror", () => {
  it("projects a PUSH as append + index advance", async () => {
    const router = bareRouter(["/a"], 0);
    const mirror = new HistoryMirror(router, { entries: ["/a"], index: 0 });

    await router.navigate("/b");
    expect(mirror.snapshot()).toEqual({ entries: ["/a", "/b"], index: 1 });

    await router.navigate("/c");
    expect(mirror.snapshot()).toEqual({ entries: ["/a", "/b", "/c"], index: 2 });

    mirror.dispose();
  });

  it("projects REPLACE as an in-place overwrite of the current entry", async () => {
    const router = bareRouter(["/a"], 0);
    const mirror = new HistoryMirror(router, { entries: ["/a"], index: 0 });

    await router.navigate("/b", { replace: true });
    expect(mirror.snapshot()).toEqual({ entries: ["/b"], index: 0 });

    mirror.dispose();
  });

  it("projects POP via the known delta and truncates the forward tail on a diverging PUSH", async () => {
    const router = bareRouter(["/a"], 0);
    const mirror = new HistoryMirror(router, { entries: ["/a"], index: 0 });

    await router.navigate("/b");
    await router.navigate("/c"); // [/a,/b,/c] @2
    await navigateByDelta(router, -1); // back to @1
    expect(mirror.snapshot()).toEqual({ entries: ["/a", "/b", "/c"], index: 1 });

    await navigateByDelta(router, -1); // back to @0
    expect(mirror.index).toBe(0);

    await router.navigate("/d"); // PUSH truncates the abandoned /b,/c branch
    expect(mirror.snapshot()).toEqual({ entries: ["/a", "/d"], index: 1 });

    mirror.dispose();
  });

  it("preserves search and hash in stored entries", async () => {
    const router = bareRouter(["/a"], 0);
    const mirror = new HistoryMirror(router, { entries: ["/a"], index: 0 });

    await router.navigate("/s?x=1&y=2#frag");
    expect(mirror.snapshot()).toEqual({
      entries: ["/a", "/s?x=1&y=2#frag"],
      index: 1,
    });

    mirror.dispose();
  });

  it("round-trips a seeded multi-entry session and navigates within it", async () => {
    const seed: HistorySnapshot = { entries: ["/a", "/b", "/c"], index: 1 };
    const router = bareRouter(seed.entries, seed.index); // seeded at /b
    const mirror = new HistoryMirror(router, seed);

    expect(mirror.snapshot()).toEqual(seed);
    expect(router.state.location.pathname).toBe("/b");

    await navigateByDelta(router, -1); // /a
    expect(mirror.index).toBe(0);
    expect(router.state.location.pathname).toBe("/a");

    await navigateByDelta(router, 1); // forward /b
    expect(mirror.index).toBe(1);
    expect(router.state.location.pathname).toBe("/b");

    mirror.dispose();
  });

  it("clamps a forward POP past the end to a no-op", async () => {
    const router = bareRouter(["/a"], 0);
    const mirror = new HistoryMirror(router, { entries: ["/a"], index: 0 });

    await router.navigate("/b"); // [/a,/b] @1 (at the end)
    await navigateByDelta(router, 1); // nothing forward -> no-op
    expect(mirror.snapshot()).toEqual({ entries: ["/a", "/b"], index: 1 });
    expect(mirror.canGoForward).toBe(false);
    expect(mirror.canGoBack).toBe(true);

    mirror.dispose();
  });
});

describe("HistoryMirror — render-driven <Navigate replace> collapse", () => {
  const collapseRoutes: RouteObject[] = [
    {
      path: "/",
      element: <Outlet />,
      children: [
        { index: true, element: <Navigate to="/a" replace /> },
        { path: "a", element: <div>A</div> },
      ],
    },
  ];

  it("folds a bare seeded entry into the default child on mount (post-mount REPLACE)", async () => {
    const router = createMemoryRouter(collapseRoutes, { initialEntries: ["/"] });
    const mirror = new HistoryMirror(router, { entries: ["/"], index: 0 });

    render(<RouterProvider router={router} />);

    await waitFor(() => expect(router.state.location.pathname).toBe("/a"));
    expect(mirror.snapshot()).toEqual({ entries: ["/a"], index: 0 });

    mirror.dispose();
  });
});
