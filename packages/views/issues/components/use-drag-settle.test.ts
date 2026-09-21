/**
 * @vitest-environment jsdom
 */
import { act, renderHook } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { useDragSettle } from "./use-drag-settle";

describe("useDragSettle — settle lock generations", () => {
  it("releases the lock and bumps settleVersion on settle", () => {
    const { result } = renderHook(() => useDragSettle(() => ({})));

    const release = result.current.beginSettle();
    expect(result.current.isSettlingRef.current).toBe(true);

    act(() => release());
    expect(result.current.isSettlingRef.current).toBe(false);
    expect(result.current.settleVersion).toBe(1);
  });

  it("a superseded release cannot unlock a newer settle", () => {
    // The release runs after the move's table refetch lands, so a second
    // drop can engage the lock while the first release is still in flight.
    // The stale release must neither unlock the newer settle nor bump the
    // version (which would resync the newer optimistic move away).
    const { result } = renderHook(() => useDragSettle(() => ({})));

    const firstRelease = result.current.beginSettle();
    const secondRelease = result.current.beginSettle();

    act(() => firstRelease());
    expect(result.current.isSettlingRef.current).toBe(true);
    expect(result.current.settleVersion).toBe(0);

    act(() => secondRelease());
    expect(result.current.isSettlingRef.current).toBe(false);
    expect(result.current.settleVersion).toBe(1);
  });

  it("a late release from a fully settled generation is a no-op", () => {
    const { result } = renderHook(() => useDragSettle(() => ({})));

    const firstRelease = result.current.beginSettle();
    act(() => firstRelease());
    const secondRelease = result.current.beginSettle();
    act(() => secondRelease());
    expect(result.current.settleVersion).toBe(2);

    // Calling a consumed release again (double onSettled) changes nothing.
    act(() => firstRelease());
    expect(result.current.settleVersion).toBe(2);
    expect(result.current.isSettlingRef.current).toBe(false);
  });
});
