import "@testing-library/jest-dom/vitest";
import { render, screen } from "@testing-library/react";
import { createRef } from "react";
import { describe, expect, it, vi } from "vitest";
import { DESKTOP_WINDOW_TOP_MARGIN } from "../../workspaceDesktop/constants";
import { ComposerFloatingMenuSurface } from "./ComposerFloatingMenuSurface";

function mockRect(rect: {
  bottom: number;
  height: number;
  left: number;
  right: number;
  top: number;
  width: number;
}): DOMRect {
  return { ...rect, x: rect.left, y: rect.top, toJSON: () => ({}) } as DOMRect;
}

describe("ComposerFloatingMenuSurface", () => {
  it("keeps viewport coordinates when a workbench boundary requests a body portal", () => {
    const boundary = document.createElement("div");
    boundary.setAttribute("data-slot", "viewport-menu-boundary");
    boundary.setAttribute("data-viewport-menu-portal-target", "body");
    boundary.setAttribute("data-workbench-window-id", "node-1");
    boundary.style.zIndex = "4";

    const anchorRef = createRef<HTMLDivElement>();
    const anchor = document.createElement("div");
    anchor.getBoundingClientRect = vi.fn(() =>
      mockRect({
        bottom: 560,
        height: 30,
        left: 312,
        right: 588,
        top: 530,
        width: 276
      })
    );
    boundary.appendChild(anchor);
    document.body.appendChild(boundary);
    anchorRef.current = anchor;

    render(
      <ComposerFloatingMenuSurface
        anchorRef={anchorRef}
        maxHeight={320}
        open
        placement="fixed-height"
        testId="composer-floating-menu-surface-workbench"
      >
        <div>menu content</div>
      </ComposerFloatingMenuSurface>
    );

    const surface = screen.getByTestId(
      "composer-floating-menu-surface-workbench"
    );
    expect(surface.parentElement).toHaveAttribute(
      "data-tutti-portal-root",
      "true"
    );
    expect(surface.style.left).toBe("312px");

    boundary.remove();
  });

  it("keeps a fixed-height menu clear of the workspace window's traffic-light header even when the anchor sits near the top of the viewport", () => {
    const anchorRef = createRef<HTMLDivElement>();
    const anchor = document.createElement("div");
    anchor.getBoundingClientRect = vi.fn(() =>
      mockRect({
        bottom: 90,
        height: 30,
        left: 12,
        right: 200,
        top: 60,
        width: 188
      })
    );
    document.body.appendChild(anchor);
    anchorRef.current = anchor;

    render(
      <ComposerFloatingMenuSurface
        anchorRef={anchorRef}
        maxHeight={320}
        open
        placement="fixed-height"
        testId="composer-floating-menu-surface"
      >
        <div>menu content</div>
      </ComposerFloatingMenuSurface>
    );

    const surface = screen.getByTestId("composer-floating-menu-surface");
    const top = Number.parseFloat(surface.style.top);
    expect(top).toBeGreaterThanOrEqual(DESKTOP_WINDOW_TOP_MARGIN);

    anchor.remove();
  });

  it("clamps the menu against a compact virtual node window's own header, not just the real viewport's 52px margin", () => {
    // Simulate a short WorkspaceNodeWindow (well under COMPOSER_MENU_MIN_HEIGHT_PX
    // of 280px) parked in the middle of the workspace canvas, far from the real
    // screen's top edge. Its own decorative header sits at, say, 400-436px in
    // viewport coordinates -- nowhere near the real-window 52px safe area, so the
    // 52px floor alone can't protect it.
    const nodeRoot = document.createElement("div");
    nodeRoot.setAttribute("data-workspace-node-window-root", "true");
    nodeRoot.getBoundingClientRect = vi.fn(() =>
      mockRect({
        bottom: 570,
        height: 170,
        left: 300,
        right: 600,
        top: 400,
        width: 300
      })
    );

    const header = document.createElement("header");
    header.setAttribute("data-workspace-node-window-header", "true");
    header.getBoundingClientRect = vi.fn(() =>
      mockRect({
        bottom: 436,
        height: 36,
        left: 300,
        right: 600,
        top: 400,
        width: 300
      })
    );
    nodeRoot.appendChild(header);

    const anchorRef = createRef<HTMLDivElement>();
    const anchor = document.createElement("div");
    // Composer sits directly below the header, near the bottom of the short node.
    anchor.getBoundingClientRect = vi.fn(() =>
      mockRect({
        bottom: 560,
        height: 30,
        left: 312,
        right: 588,
        top: 530,
        width: 276
      })
    );
    nodeRoot.appendChild(anchor);
    document.body.appendChild(nodeRoot);
    anchorRef.current = anchor;

    render(
      <ComposerFloatingMenuSurface
        anchorRef={anchorRef}
        maxHeight={320}
        open
        placement="fixed-height"
        testId="composer-floating-menu-surface-node"
      >
        <div>menu content</div>
      </ComposerFloatingMenuSurface>
    );

    const surface = screen.getByTestId("composer-floating-menu-surface-node");
    // The palette must render inside the node (portal target), not document.body.
    expect(surface.parentElement).toBe(nodeRoot);

    const top = Number.parseFloat(surface.style.top);
    const minHeight = Number.parseFloat(surface.style.minHeight);

    // The node's own header bottom edge (436) is a far more restrictive floor
    // here than the real-viewport 52px margin -- the fix must honor it.
    expect(top).toBeGreaterThanOrEqual(436);
    // The forced minimum height must never be so large that top + minHeight
    // pushes back above the node's own header, even though the palette's
    // "natural" minimum (280px) alone would do exactly that from this anchor.
    expect(top + minHeight).toBeLessThanOrEqual(560 - 8 + 0.5);
    expect(minHeight).toBeLessThanOrEqual(280);
  });

  it("still applies the real-viewport 52px floor when the enclosing node's own header is above it (real window case is unaffected)", () => {
    const nodeRoot = document.createElement("div");
    nodeRoot.setAttribute("data-workspace-node-window-root", "true");
    nodeRoot.getBoundingClientRect = vi.fn(() =>
      mockRect({
        bottom: 400,
        height: 400,
        left: 0,
        right: 400,
        top: 0,
        width: 400
      })
    );

    const header = document.createElement("header");
    header.setAttribute("data-workspace-node-window-header", "true");
    // Header bottom edge sits above the real-window 52px safe area (e.g. a
    // maximized node docked flush with the canvas top).
    header.getBoundingClientRect = vi.fn(() =>
      mockRect({
        bottom: 36,
        height: 36,
        left: 0,
        right: 400,
        top: 0,
        width: 400
      })
    );
    nodeRoot.appendChild(header);

    const anchorRef = createRef<HTMLDivElement>();
    const anchor = document.createElement("div");
    anchor.getBoundingClientRect = vi.fn(() =>
      mockRect({
        bottom: 90,
        height: 30,
        left: 12,
        right: 200,
        top: 60,
        width: 188
      })
    );
    nodeRoot.appendChild(anchor);
    document.body.appendChild(nodeRoot);
    anchorRef.current = anchor;

    render(
      <ComposerFloatingMenuSurface
        anchorRef={anchorRef}
        maxHeight={320}
        open
        placement="fixed-height"
        testId="composer-floating-menu-surface-real-floor"
      >
        <div>menu content</div>
      </ComposerFloatingMenuSurface>
    );

    const surface = screen.getByTestId(
      "composer-floating-menu-surface-real-floor"
    );
    const top = Number.parseFloat(surface.style.top);
    expect(top).toBeGreaterThanOrEqual(DESKTOP_WINDOW_TOP_MARGIN);
  });
});
