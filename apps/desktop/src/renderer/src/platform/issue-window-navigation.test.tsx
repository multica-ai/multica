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
