import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { createMemoryRouter, RouterProvider } from "react-router-dom";

const openModal = vi.fn();
const reloadActiveTab = vi.fn();
const closeActiveTab = vi.fn();

vi.mock("@multica/core/modals", () => ({
  useModalStore: {
    getState: () => ({ open: openModal }),
  },
}));

vi.mock("@/stores/tab-store", () => ({
  useTabStore: {
    getState: () => ({ reloadActiveTab, closeActiveTab }),
  },
}));

import { DesktopRouteErrorPage, formatRouteErrorReport } from "./route-error-page";

function Boom(): null {
  throw new Error("route render exploded");
  return null;
}

describe("DesktopRouteErrorPage", () => {
  beforeEach(() => {
    openModal.mockReset();
    reloadActiveTab.mockReset();
    closeActiveTab.mockReset();
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
});