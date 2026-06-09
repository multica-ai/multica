import { describe, it, expect, vi, beforeEach } from "vitest";
import { render } from "@testing-library/react";

// Mutable pathname + a spy for the shared capture helper. The tracker reads
// usePathname() and forwards it to capturePageview; section-normalization and
// dedup live in @multica/core/analytics and are unit-tested there, so here we
// only assert the wiring (which path is forwarded, and that the query string
// never re-triggers the effect).
const { state, capturePageview } = vi.hoisted(() => ({
  state: { pathname: "/" as string | null },
  capturePageview: vi.fn<(path?: string) => void>(),
}));

vi.mock("next/navigation", () => ({
  usePathname: () => state.pathname,
}));

vi.mock("@multica/core/analytics", () => ({
  capturePageview,
}));

import { PageviewTracker } from "./pageview-tracker";

beforeEach(() => {
  state.pathname = "/";
  capturePageview.mockClear();
});

describe("web PageviewTracker", () => {
  it("captures the pathname on mount and on each pathname change", () => {
    const { rerender } = render(<PageviewTracker />);
    expect(capturePageview).toHaveBeenCalledTimes(1);
    expect(capturePageview).toHaveBeenLastCalledWith("/");

    state.pathname = "/acme/issues";
    rerender(<PageviewTracker />);
    expect(capturePageview).toHaveBeenCalledTimes(2);
    expect(capturePageview).toHaveBeenLastCalledWith("/acme/issues");
  });

  it("does not re-capture on a query-string-only navigation", () => {
    state.pathname = "/acme/issues";
    const { rerender } = render(<PageviewTracker />);
    expect(capturePageview).toHaveBeenCalledTimes(1);

    // A filter/sort/search change alters only the query string, which the
    // tracker no longer reads — usePathname() is unchanged so the effect's
    // dependency does not change and no new pageview fires.
    rerender(<PageviewTracker />);
    expect(capturePageview).toHaveBeenCalledTimes(1);
  });
});
