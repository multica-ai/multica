import { afterEach, describe, expect, it } from "vitest";
import { render, cleanup, act, waitFor } from "@testing-library/react";
import {
  createMemoryRouter,
  redirect,
  Navigate,
  Outlet,
  RouterProvider,
  type DataRouter,
  type RouteObject,
} from "react-router-dom";

/**
 * Characterization test for React Router 7.14.0's `createMemoryRouter`.
 *
 * Howard's mandated Step 1 (MUL-4475): before writing the per-tab history
 * *mirror*, lock the observable contract of the memory router the mirror is a
 * pure projection of. The Session-model tab runtime seeds a router from a
 * persisted `{ entries, index }` and then mirrors `(location, historyAction)`
 * transitions back out. Every assertion here is a fact the mirror's
 * PUSH/REPLACE/POP arithmetic and its `initialIndex` seeding rely on.
 *
 * Uses a minimal, self-contained route table (no app pages, no data fetching)
 * so it documents the *library primitive*, not the app's route config. If a
 * future react-router bump changes any of these, the mirror design must be
 * revisited, not silently patched.
 */

// Bare element-less routes for the pure history-semantics cases: we only
// observe history state there, never render, so route `element`s are
// irrelevant. `/redirect-me` covers a loader `redirect()` reached via an
// explicit navigate.
const routes: RouteObject[] = [
  { path: "/", loader: () => redirect("/a") },
  { path: "/a" },
  { path: "/b" },
  { path: "/c" },
  { path: "/search" },
  { path: "/redirect-me", loader: () => redirect("/c") },
];

function makeRouter(initialEntries: string[], initialIndex?: number): DataRouter {
  const router = createMemoryRouter(routes, { initialEntries, initialIndex });
  // Match production: RouterProvider calls initialize() on mount.
  router.initialize();
  return router;
}

/** Full serialized location, the exact string shape the mirror stores. */
function fullPath(router: DataRouter): string {
  const { pathname, search, hash } = router.state.location;
  return `${pathname}${search}${hash}`;
}

/** Wait until the router settles back to an idle navigation state. */
async function settle(router: DataRouter): Promise<void> {
  if (router.state.navigation.state === "idle") return;
  await new Promise<void>((resolve) => {
    const unsub = router.subscribe((s) => {
      if (s.navigation.state === "idle") {
        unsub();
        resolve();
      }
    });
  });
}

afterEach(cleanup);

describe("createMemoryRouter — history contract (RR 7.14.0)", () => {
  it("honors initialIndex: seeds location at entries[initialIndex]", async () => {
    const router = makeRouter(["/a", "/b", "/c"], 1);
    await settle(router);
    expect(router.state.location.pathname).toBe("/b");
  });

  it("defaults index to the last entry when initialIndex is omitted", async () => {
    const router = makeRouter(["/a", "/b", "/c"]);
    await settle(router);
    expect(router.state.location.pathname).toBe("/c");
  });

  it("reports PUSH for a plain navigate()", async () => {
    const router = makeRouter(["/a"]);
    await settle(router);
    await router.navigate("/b");
    await settle(router);
    expect(router.state.location.pathname).toBe("/b");
    expect(router.state.historyAction).toBe("PUSH");
  });

  it("reports REPLACE for navigate(path, { replace: true })", async () => {
    const router = makeRouter(["/a"]);
    await settle(router);
    await router.navigate("/b", { replace: true });
    await settle(router);
    expect(router.state.location.pathname).toBe("/b");
    expect(router.state.historyAction).toBe("REPLACE");
  });

  it("reports POP for navigate(-1) and returns to the previous entry", async () => {
    const router = makeRouter(["/a"]);
    await settle(router);
    await router.navigate("/b");
    await settle(router);
    await router.navigate(-1);
    await settle(router);
    expect(router.state.location.pathname).toBe("/a");
    expect(router.state.historyAction).toBe("POP");
  });

  it("reports POP for a forward navigate(1) after a back", async () => {
    const router = makeRouter(["/a"]);
    await settle(router);
    await router.navigate("/b");
    await settle(router);
    await router.navigate(-1); // at /a
    await settle(router);
    await router.navigate(1); // forward to /b
    await settle(router);
    expect(router.state.location.pathname).toBe("/b");
    expect(router.state.historyAction).toBe("POP");
  });

  it("preserves search and hash across navigations", async () => {
    const router = makeRouter(["/a"]);
    await settle(router);
    await router.navigate("/search?x=1&y=2#frag");
    await settle(router);
    expect(router.state.location.search).toBe("?x=1&y=2");
    expect(router.state.location.hash).toBe("#frag");
    expect(fullPath(router)).toBe("/search?x=1&y=2#frag");
  });

  it("preserves search/hash when seeded via initialEntries (round-trip)", async () => {
    const router = makeRouter(["/a", "/search?x=1#h"], 1);
    await settle(router);
    expect(fullPath(router)).toBe("/search?x=1#h");
  });

  it("loader redirect() during a navigate lands on target and REPLACES the visited entry", async () => {
    // Guard-style redirect: navigate to a route whose loader redirect()s. The
    // in-flight entry is replaced by the target, so it leaves no history stop.
    const router = makeRouter(["/a"]);
    await settle(router);
    await router.navigate("/redirect-me");
    await settle(router);
    expect(router.state.location.pathname).toBe("/c");
    // Back must return to /a, proving /redirect-me was not stacked.
    await router.navigate(-1);
    await settle(router);
    expect(router.state.location.pathname).toBe("/a");
  });

  it("navigate(1) past the end of the stack is a no-op (basis for the mirror index clamp)", async () => {
    const router = makeRouter(["/a"]);
    await settle(router);
    await router.navigate("/b"); // a -> b
    await settle(router);
    await router.navigate(-1); // back to a
    await settle(router);
    await router.navigate("/search"); // push truncates the forward (b) branch
    await settle(router);
    expect(router.state.location.pathname).toBe("/search");
    // No forward entry remains; a forward pop stays put.
    await router.navigate(1);
    await settle(router);
    expect(router.state.location.pathname).toBe("/search");
  });
});

// The app's index route collapses a bare workspace path to its default child
// via a *rendered* `<Navigate replace>` element (routes.tsx:124), NOT a loader
// redirect. A pure createMemoryRouter().initialize() does NOT auto-follow an
// initial-location redirect — the collapse only happens once RouterProvider
// renders. This is the mechanism the tab runtime seeds a bare `/{slug}` with,
// so characterize it under render, exactly as production drives it.
describe("index-route <Navigate replace> collapse (rendered, RR 7.14.0)", () => {
  const collapseRoutes: RouteObject[] = [
    {
      path: "/",
      element: <Outlet />,
      children: [
        { index: true, element: <Navigate to="/a" replace /> },
        { path: "a", element: <div>A</div> },
        { path: "b", element: <div>B</div> },
      ],
    },
  ];

  it("replaces the bare initial entry with the default child on mount", async () => {
    const router = createMemoryRouter(collapseRoutes, { initialEntries: ["/"] });
    render(<RouterProvider router={router} />);

    await waitFor(() => expect(router.state.location.pathname).toBe("/a"));
    expect(router.state.historyAction).toBe("REPLACE");

    // Back must NOT return to "/": the bare entry was replaced, not stacked.
    await act(async () => {
      await router.navigate(-1);
    });
    expect(router.state.location.pathname).toBe("/a");
  });
});
