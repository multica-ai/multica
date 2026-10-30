import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import { render, screen } from "@testing-library/react";

const captureException = vi.hoisted(() => vi.fn());
vi.mock("@multica/core/analytics", () => ({ captureException }));

// Marker stand-in so the structural assertion below tests the contract ("the
// drag strip is the first flex child") rather than DragStrip's own markup.
vi.mock("@multica/views/platform", () => ({
  DragStrip: () => <div data-testid="drag-strip" />,
}));

import { AppCrashBoundary } from "./app-crash-boundary";

function Boom(): never {
  throw new Error("useWorkspaceId: no workspace selected");
}

beforeEach(() => {
  captureException.mockClear();
  // The boundary logs the captured error on purpose; keep the suite output
  // readable without hiding a genuinely unexpected console.error elsewhere.
  vi.spyOn(console, "error").mockImplementation(() => {});
});

afterEach(() => {
  vi.restoreAllMocks();
});

/**
 * MUL-6231 / #7021. The desktop renderer mounted <App /> with no boundary
 * above it, so one throw in the shell emptied the window and left force-quit
 * as the only way out.
 */
describe("AppCrashBoundary", () => {
  it("renders children when nothing throws", () => {
    render(
      <AppCrashBoundary>
        <div data-testid="app" />
      </AppCrashBoundary>,
    );

    expect(screen.queryByTestId("app")).not.toBeNull();
  });

  it("shows a recoverable fallback instead of blanking the window", () => {
    render(
      <AppCrashBoundary>
        <Boom />
      </AppCrashBoundary>,
    );

    const alert = screen.getByRole("alert");
    expect(alert).not.toBeNull();
    expect(alert.textContent).toContain("useWorkspaceId: no workspace selected");
    expect(screen.getByRole("button", { name: /reload/i })).not.toBeNull();
  });

  it("keeps the window draggable by mounting the drag strip as the first child", () => {
    // A full-window view outside the dashboard shell owns its own window
    // chrome. Without this the user loses the draggable top edge precisely
    // when the app is least usable — see CLAUDE.md Desktop Rules.
    const { container } = render(
      <AppCrashBoundary>
        <Boom />
      </AppCrashBoundary>,
    );

    const shell = container.firstElementChild;
    expect(shell).not.toBeNull();
    expect(shell).toHaveClass("flex", "flex-col");
    expect(shell?.firstElementChild).toHaveAttribute("data-testid", "drag-strip");
  });

  it("reports the crash through the exception pipeline, not as a plain event", () => {
    // captureException routes into posthog's `$exception` path, the only one
    // initAnalytics' before_send hook redacts and de-dupes. A plain
    // captureEvent would ship the raw message and stack unredacted.
    render(
      <AppCrashBoundary>
        <Boom />
      </AppCrashBoundary>,
    );

    expect(captureException).toHaveBeenCalledTimes(1);
    expect(captureException).toHaveBeenCalledWith(
      expect.objectContaining({
        message: "useWorkspaceId: no workspace selected",
      }),
      { source: "desktop-renderer-boundary" },
    );
  });
});
