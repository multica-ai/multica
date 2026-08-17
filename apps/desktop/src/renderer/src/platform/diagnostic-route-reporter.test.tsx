/**
 * MUL-5345 — the main window must publish which page it is showing.
 *
 * This reporting existed once and was deleted with the PostHog $pageview
 * cleanup (MUL-4127), which left the main process reading a route it was never
 * sent: every field hang report came back with only the asar `index.html` URL.
 * Nothing failed loudly, so these tests pin both consumers — the IPC push to
 * main (the only party alive during a true hang) and the in-renderer
 * diagnostic context the freeze watchdog reads.
 */
import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import { render } from "@testing-library/react";

const authState = { user: null as { id: string } | null };
const overlayState = { overlay: null as { type: string } | null };
const tabState = {
  slug: null as string | null,
  tabId: null as string | null,
  url: null as string | null,
};

vi.mock("@multica/core/auth", () => ({
  useAuthStore: (selector: (s: typeof authState) => unknown) => selector(authState),
}));

vi.mock("@/stores/window-overlay-store", () => ({
  useWindowOverlayStore: (selector: (s: typeof overlayState) => unknown) =>
    selector(overlayState),
}));

vi.mock("@/stores/tab-store", () => ({
  useActiveTabIdentity: () => ({ slug: tabState.slug, tabId: tabState.tabId }),
  useActiveTabUrl: () => tabState.url,
}));

import { DiagnosticRouteReporter } from "./diagnostic-route-reporter";
import { getDiagnosticRoute, resetDiagnosticContext } from "@multica/core/diagnostics";

const setRendererRouteContext = vi.fn();

beforeEach(() => {
  setRendererRouteContext.mockClear();
  authState.user = { id: "u-1" };
  overlayState.overlay = null;
  tabState.slug = "acme";
  tabState.tabId = "tab-1";
  tabState.url = "/acme/issues";
  Object.defineProperty(window, "desktopAPI", {
    configurable: true,
    value: { setRendererRouteContext },
  });
});

afterEach(() => {
  resetDiagnosticContext();
});

describe("DiagnosticRouteReporter", () => {
  it("pushes the bucketed tab route to main", () => {
    render(<DiagnosticRouteReporter />);

    expect(setRendererRouteContext).toHaveBeenCalledWith({
      surface: "tab",
      path: "/:slug/issues",
    });
  });

  it("never sends the workspace slug or tab id — this payload reaches telemetry", () => {
    render(<DiagnosticRouteReporter />);

    const sent = JSON.stringify(setRendererRouteContext.mock.calls);
    expect(sent).not.toContain("acme");
    expect(sent).not.toContain("tab-1");
  });

  it("publishes the same route to the in-renderer freeze watchdog", () => {
    render(<DiagnosticRouteReporter />);

    expect(getDiagnosticRoute()).toBe("/:slug/issues");
  });

  it("keeps the issue id out of telemetry", () => {
    tabState.url = "/acme/issues/8db920bc-f982-4d38-95f3-56ec43cbefa8";

    render(<DiagnosticRouteReporter />);

    expect(getDiagnosticRoute()).toBe("/:slug/issues/:id");
  });

  it("reports the login surface before a user exists", () => {
    authState.user = null;

    render(<DiagnosticRouteReporter />);

    expect(setRendererRouteContext).toHaveBeenCalledWith({
      surface: "login",
      path: "/login",
    });
  });

  it("reports an overlay, which is window state rather than a tab route", () => {
    overlayState.overlay = { type: "onboarding" };

    render(<DiagnosticRouteReporter />);

    expect(setRendererRouteContext).toHaveBeenCalledWith({
      surface: "overlay",
      path: "/onboarding",
    });
  });

  it("does not re-send an unchanged context", () => {
    const { rerender } = render(<DiagnosticRouteReporter />);
    expect(setRendererRouteContext).toHaveBeenCalledTimes(1);

    rerender(<DiagnosticRouteReporter />);

    expect(setRendererRouteContext).toHaveBeenCalledTimes(1);
  });

  it("re-reports when the tab navigates to another page", () => {
    const { rerender } = render(<DiagnosticRouteReporter />);
    tabState.url = "/acme/inbox";

    rerender(<DiagnosticRouteReporter />);

    expect(setRendererRouteContext).toHaveBeenLastCalledWith({
      surface: "tab",
      path: "/:slug/inbox",
    });
    expect(getDiagnosticRoute()).toBe("/:slug/inbox");
  });
});
