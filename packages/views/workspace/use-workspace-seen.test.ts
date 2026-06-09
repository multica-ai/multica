import { describe, expect, it } from "vitest";
import { renderHook } from "@testing-library/react";
import { useWorkspaceSeen } from "./use-workspace-seen";

describe("useWorkspaceSeen", () => {
  it("returns false when slug has never resolved", () => {
    const { result } = renderHook(() => useWorkspaceSeen("acme", false));
    expect(result.current).toBe(false);
  });

  it("returns true after slug resolved at least once", () => {
    const { result, rerender } = renderHook(
      ({ slug, resolved }) => useWorkspaceSeen(slug, resolved),
      { initialProps: { slug: "acme", resolved: true } },
    );
    expect(result.current).toBe(true);

    // Workspace disappears (e.g. just deleted) — hook still reports "seen"
    rerender({ slug: "acme", resolved: false });
    expect(result.current).toBe(true);
  });

  it("remembers multiple slugs independently", () => {
    const { result, rerender } = renderHook(
      ({ slug, resolved }) => useWorkspaceSeen(slug, resolved),
      { initialProps: { slug: "acme", resolved: true } },
    );
    // Switch to a different resolved slug
    rerender({ slug: "beta", resolved: true });
    expect(result.current).toBe(true);

    // Now check a never-seen slug — should not leak positive
    rerender({ slug: "gamma", resolved: false });
    expect(result.current).toBe(false);

    // Back to "acme" (which we saw first) — still seen
    rerender({ slug: "acme", resolved: false });
    expect(result.current).toBe(true);
  });

  it("returns false for undefined slug", () => {
    const { result } = renderHook(() => useWorkspaceSeen(undefined, true));
    expect(result.current).toBe(false);
  });
});
