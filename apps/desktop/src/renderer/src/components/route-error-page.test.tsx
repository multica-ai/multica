import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { createMemoryRouter, RouterProvider } from "react-router-dom";

const openModal = vi.fn();
const reloadActiveTab = vi.fn();
const closeActiveTab = vi.fn();
const navigateActiveSession = vi.fn();

let activeWorkspaceSlug: string | null = "acme";

vi.mock("@multica/core/modals", () => ({
  useModalStore: {
    getState: () => ({ open: openModal }),
  },
}));

vi.mock("@/stores/tab-store", () => {
  const state = () => ({
    reloadActiveTab,
    closeActiveTab,
    navigateActiveSession,
    activeWorkspaceSlug,
  });
  // Callable-store shape: selector call for the hook, getState for imperatives.
  const useTabStore = (selector: (s: ReturnType<typeof state>) => unknown) =>
    selector(state());
  useTabStore.getState = state;
  return { useTabStore };
});

import { DesktopRouteErrorPage, formatRouteErrorReport } from "./route-error-page";

function Boom(): null {
  throw new Error("route render exploded");
  return null;
}

/**
 * Render a router whose only route does NOT match `path`, which is how React
 * Router produces a real 404 ErrorResponse — the same object shape the desktop
 * shell sees when a tab opens an unroutable URL.
 */
function renderUnmatchedRoute(path: string) {
  const router = createMemoryRouter(
    [
      {
        path: "/:workspaceSlug/issues",
        element: <div>issues</div>,
        errorElement: <DesktopRouteErrorPage />,
      },
    ],
    { initialEntries: [path] },
  );
  return render(<RouterProvider router={router} />);
}

describe("DesktopRouteErrorPage", () => {
  beforeEach(() => {
    openModal.mockReset();
    reloadActiveTab.mockReset();
    closeActiveTab.mockReset();
    navigateActiveSession.mockReset();
    activeWorkspaceSlug = "acme";
    vi.spyOn(console, "error").mockImplementation(() => {});
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("brands React Router route errors and offers tab reload", async () => {
    const router = createMemoryRouter(
      [{ path: "/", element: <Boom />, errorElement: <DesktopRouteErrorPage /> }],
      { initialEntries: ["/"] },
    );

    render(<RouterProvider router={router} />);

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Something went wrong in this tab",
    );
    fireEvent.click(screen.getByRole("button", { name: /reload tab/i }));
    expect(reloadActiveTab).toHaveBeenCalledTimes(1);
  });

  it("offers Close tab as the always-safe escape from a crashing route", async () => {
    const router = createMemoryRouter(
      [{ path: "/acme/issues/1", element: <Boom />, errorElement: <DesktopRouteErrorPage /> }],
      { initialEntries: ["/acme/issues/1"] },
    );

    render(<RouterProvider router={router} />);

    fireEvent.click(await screen.findByRole("button", { name: /close tab/i }));
    expect(closeActiveTab).toHaveBeenCalledTimes(1);
  });

  it("opens the existing feedback modal with a structured markdown report only after click", async () => {
    const router = createMemoryRouter(
      [{ path: "/acme/issues", element: <Boom />, errorElement: <DesktopRouteErrorPage /> }],
      { initialEntries: ["/acme/issues"] },
    );

    render(<RouterProvider router={router} />);

    expect(openModal).not.toHaveBeenCalled();
    fireEvent.click(await screen.findByRole("button", { name: /report error/i }));

    expect(openModal).toHaveBeenCalledWith(
      "feedback",
      expect.objectContaining({
        initialMessage: expect.stringContaining("kind: desktop_route_error"),
        kind: "bug",
      }),
    );
  });

  it("documents the structured context follow-up debt in the report template", () => {
    const report = formatRouteErrorReport({
      error: new Error("bad route"),
      url: "app://desktop/acme/issues",
      appInfo: { version: "1.2.3", os: "macos" },
      trigger: "route-errorElement",
    });

    expect(report).toContain("kind: desktop_route_error");
    expect(report).toContain("trigger: route-errorElement");
    expect(report).toContain("TODO: promote error context to structured feedback fields");
  });

  // --- 404 as a first-class product state (MUL-4899) -----------------------

  describe("404 Not Found", () => {
    it("renders a Not Found page, not a crash report, for an unroutable path", async () => {
      renderUnmatchedRoute("/Users/whoever/Desktop/shot.png");

      const alert = await screen.findByRole("alert");
      expect(alert).toHaveTextContent("This page doesn't exist");
      // The reported defect: a 404 was presented as an app crash and the user
      // was invited to file a bug for a link that was simply wrong.
      expect(alert).not.toHaveTextContent("Something went wrong");
      expect(alert).not.toHaveTextContent("Unknown route error");
      expect(
        screen.queryByRole("button", { name: /report error/i }),
      ).not.toBeInTheDocument();
      expect(
        screen.queryByRole("button", { name: /reload tab/i }),
      ).not.toBeInTheDocument();
    });

    it("shows the unroutable path so the user can see what was wrong with the link", async () => {
      renderUnmatchedRoute("/Users/whoever/Desktop/shot.png");
      expect(await screen.findByRole("alert")).toHaveTextContent(
        "/Users/whoever/Desktop/shot.png",
      );
    });

    it("always offers Close tab", async () => {
      renderUnmatchedRoute("/Users/whoever/Desktop/shot.png");
      fireEvent.click(await screen.findByRole("button", { name: /close tab/i }));
      expect(closeActiveTab).toHaveBeenCalledTimes(1);
    });

    it("takes the recovery entry from the active workspace, NEVER from the bad pathname", async () => {
      // The whole point: deriving a slug from this pathname yields "Users" and a
      // "recovery" button pointing at /Users/issues — a second 404.
      renderUnmatchedRoute("/Users/whoever/Desktop/shot.png");

      fireEvent.click(await screen.findByRole("button", { name: /go to issues/i }));
      expect(navigateActiveSession).toHaveBeenCalledWith("/acme/issues", {
        replace: true,
      });
      expect(navigateActiveSession).not.toHaveBeenCalledWith(
        "/Users/issues",
        expect.anything(),
      );
    });

    it("offers no recovery entry at all when there is no active workspace", async () => {
      // Rather than guess one from the failed URL.
      activeWorkspaceSlug = null;
      renderUnmatchedRoute("/Users/whoever/Desktop/shot.png");

      await screen.findByRole("alert");
      expect(
        screen.queryByRole("button", { name: /go to issues/i }),
      ).not.toBeInTheDocument();
      expect(
        screen.getByRole("button", { name: /close tab/i }),
      ).toBeInTheDocument();
    });

    it("keeps the crash page for a non-404 route error", async () => {
      const router = createMemoryRouter(
        [{ path: "/acme/issues", element: <Boom />, errorElement: <DesktopRouteErrorPage /> }],
        { initialEntries: ["/acme/issues"] },
      );
      render(<RouterProvider router={router} />);

      const alert = await screen.findByRole("alert");
      expect(alert).toHaveTextContent("Something went wrong in this tab");
      expect(alert).not.toHaveTextContent("This page doesn't exist");
    });
  });
});
