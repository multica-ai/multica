/**
 * @vitest-environment jsdom
 */
import { describe, expect, it } from "vitest";
import { act, renderHook } from "@testing-library/react";
import { useCreateIssueSurfaceSelection } from "./selection-context";

describe("useCreateIssueSurfaceSelection", () => {
  it("keeps selection local to a surface key and clears on surface change", () => {
    const { result, rerender } = renderHook(
      ({ surfaceKey }) => useCreateIssueSurfaceSelection(surfaceKey),
      { initialProps: { surfaceKey: "project:p1" } },
    );

    act(() => {
      result.current.select(["i-1", "i-2"]);
      result.current.toggle("i-3");
    });
    expect(result.current.selectedIds).toEqual(new Set(["i-1", "i-2", "i-3"]));

    act(() => {
      result.current.deselect(["i-2"]);
    });
    expect(result.current.selectedIds).toEqual(new Set(["i-1", "i-3"]));

    rerender({ surfaceKey: "project:p2" });
    expect(result.current.selectedIds).toEqual(new Set());
  });

  it("clears selection on reset key change even when the surface key is stable", () => {
    const { result, rerender } = renderHook(
      ({ resetKey }) => useCreateIssueSurfaceSelection("my:user-1:assigned", resetKey),
      { initialProps: { resetKey: "my:user-1:assigned:list" } },
    );

    act(() => {
      result.current.select(["i-1"]);
    });
    expect(result.current.selectedIds).toEqual(new Set(["i-1"]));

    rerender({ resetKey: "my:user-1:assigned:board" });
    expect(result.current.selectedIds).toEqual(new Set());
  });
});
