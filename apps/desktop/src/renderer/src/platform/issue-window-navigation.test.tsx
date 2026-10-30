/**
 * MUL-5208 — a dedicated issue window must answer content-link navigation.
 *
 * `multica:navigate` is fired by the shared link handler for every in-app
 * destination, including an absolute URL on this deployment's own origin. Only
 * the main shell listened for it, so a link clicked inside an issue window did
 * nothing at all. This window hosts exactly one issue route: another issue opens
 * in place, anything else goes to the browser — never a silent no-op.
 */
import { describe, expect, it, vi, beforeEach } from "vitest";
import { act, render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom";
import {
  currentPath,
  useNavigation,
  type NavigationAdapter,
} from "@multica/views/navigation";

const APP_URL = "https://app.example";

vi.mock("@multica/core/paths", () => ({
  useCurrentWorkspace: () => ({ slug: "acme", id: "ws-1" }),
}));

import { IssueWindowNavigationProvider } from "./issue-window-navigation";

const openExternal = vi.fn().mockResolvedValue(undefined);
const setRendererRouteContext = vi.fn();

beforeEach(() => {
  openExternal.mockClear();
  setRendererRouteContext.mockClear();
  Object.defineProperty(window, "desktopAPI", {
    configurable: true,
    value: {
      runtimeConfig: { ok: true, config: { appUrl: APP_URL } },
      openExternal,
      setRendererRouteContext,
      openIssueWindow: vi.fn(),
    },
  });
});

function CurrentPath() {
  const location = useLocation();
  return <span data-testid="path">{location.pathname}</span>;
}

function renderWindow() {
  return render(
    <MemoryRouter initialEntries={["/acme/issues/MUL-1"]}>
      <Routes>
        <Route
          path=":workspaceSlug/issues/:id"
          element={
            <IssueWindowNavigationProvider>
              <CurrentPath />
            </IssueWindowNavigationProvider>
          }
        />
      </Routes>
    </MemoryRouter>,
  );
}

function navigate(path: string) {
  // The listener runs outside React's event system; act() flushes the router
  // state update it triggers.
  act(() => {
    window.dispatchEvent(
      new CustomEvent("multica:navigate", { detail: { path } }),
    );
  });
}

describe("IssueWindowNavigationProvider content links", () => {
  it("opens another issue in place", () => {
    renderWindow();

    navigate("/acme/issues/MUL-2");

    expect(screen.getByTestId("path")).toHaveTextContent("/acme/issues/MUL-2");
    expect(openExternal).not.toHaveBeenCalled();
  });

  it("hands a page this window cannot host to the browser instead of swallowing it", () => {
    renderWindow();

    navigate("/acme/chat");

    expect(screen.getByTestId("path")).toHaveTextContent("/acme/issues/MUL-1");
    expect(openExternal).toHaveBeenCalledWith(`${APP_URL}/acme/chat`);
  });

  it("stops listening once unmounted", () => {
    const { unmount } = renderWindow();

    unmount();
    navigate("/acme/chat");

    expect(openExternal).not.toHaveBeenCalled();
  });
});

/**
 * MUL-8098 — the issue workflow card's "Manage workflows" button calls the
 * adapter's push with a settings path. This window cannot host it, so it must
 * go to the browser rather than being dropped (which only closed the card).
 */
describe("IssueWindowNavigationProvider adapter push/replace", () => {
  function renderWithAdapter(): () => NavigationAdapter {
    let adapter: NavigationAdapter | null = null;
    function Probe() {
      adapter = useNavigation();
      return <CurrentPath />;
    }
    render(
      <MemoryRouter initialEntries={["/acme/issues/MUL-1"]}>
        <Routes>
          <Route
            path=":workspaceSlug/issues/:id"
            element={
              <IssueWindowNavigationProvider>
                <Probe />
              </IssueWindowNavigationProvider>
            }
          />
        </Routes>
      </MemoryRouter>,
    );
    return () => adapter!;
  }

  it("opens a settings page in the browser on push", () => {
    const adapter = renderWithAdapter();

    act(() => adapter().push("/acme/settings?tab=workflows"));

    expect(screen.getByTestId("path")).toHaveTextContent("/acme/issues/MUL-1");
    expect(openExternal).toHaveBeenCalledTimes(1);
    expect(openExternal).toHaveBeenCalledWith(
      `${APP_URL}/acme/settings?tab=workflows`,
    );
  });

  it("opens a non-issue page in the browser on replace", () => {
    const adapter = renderWithAdapter();

    act(() => adapter().replace("/acme/chat"));

    expect(screen.getByTestId("path")).toHaveTextContent("/acme/issues/MUL-1");
    expect(openExternal).toHaveBeenCalledWith(`${APP_URL}/acme/chat`);
  });

  it("still opens another issue in place on push", () => {
    const adapter = renderWithAdapter();

    act(() => adapter().push("/acme/issues/MUL-2"));

    expect(screen.getByTestId("path")).toHaveTextContent("/acme/issues/MUL-2");
    expect(openExternal).not.toHaveBeenCalled();
  });

  it("does nothing for a non-issue page when runtime config is unavailable", () => {
    window.desktopAPI.runtimeConfig = { ok: false } as never;
    const adapter = renderWithAdapter();

    act(() => adapter().push("/acme/settings?tab=workflows"));

    expect(screen.getByTestId("path")).toHaveTextContent("/acme/issues/MUL-1");
    expect(openExternal).not.toHaveBeenCalled();
  });
});

/**
 * MUL-6784 — an issue window is a MemoryRouter over the packaged file:// page,
 * so `window.location` knows nothing about the route. A report or share link
 * built from this adapter must still name the comment the user was reading.
 */
describe("IssueWindowNavigationProvider current location", () => {
  function renderAdapter(entry: string): () => NavigationAdapter {
    let adapter: NavigationAdapter | null = null;
    function Probe() {
      adapter = useNavigation();
      return null;
    }
    render(
      <MemoryRouter initialEntries={[entry]}>
        <Routes>
          <Route
            path=":workspaceSlug/issues/:id"
            element={
              <IssueWindowNavigationProvider>
                <Probe />
              </IssueWindowNavigationProvider>
            }
          />
        </Routes>
      </MemoryRouter>,
    );
    return () => adapter!;
  }

  it("reports the route's fragment, which window.location cannot", () => {
    expect(window.location.hash).toBe("");

    expect(renderAdapter("/acme/issues/MUL-1#comment-c1")().hash).toBe(
      "#comment-c1",
    );
  });

  it('reports "" for a route without a fragment', () => {
    expect(renderAdapter("/acme/issues/MUL-1")().hash).toBe("");
  });

  it("rebuilds the current page as a web URL that keeps the fragment", () => {
    const adapter = renderAdapter("/acme/issues/MUL-1#comment-c1")();

    expect(adapter.getShareableUrl(currentPath(adapter))).toBe(
      `${APP_URL}/acme/issues/MUL-1#comment-c1`,
    );
  });
});
