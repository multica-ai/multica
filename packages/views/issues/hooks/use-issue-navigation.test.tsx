import { renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";
import { useIssueNavigationStore } from "@multica/core/issues/stores/issue-navigation-store";
import {
  useRegisterIssueNavigation,
  useClearIssueNavigation,
  useIssueSiblings,
} from "./use-issue-navigation";

const WS = "ws-1";

beforeEach(() => {
  useIssueNavigationStore.setState({ byWorkspace: {} });
});

describe("issue navigation hooks", () => {
  it("publishes columns so the detail can read a sibling pair back", () => {
    renderHook(() => useRegisterIssueNavigation(WS, { "status:todo": ["a", "b", "c"] }));
    const { result } = renderHook(() => useIssueSiblings(WS, "b"));
    expect(result.current).toEqual({ hasContext: true, prevId: "a", nextId: "c" });
  });

  it("keeps the published snapshot after the list view unmounts", () => {
    // The core guarantee: navigating into an issue unmounts the board/list, but
    // the detail still needs the columns the user just left. A future "clear on
    // unmount" refactor would silently break previous/next — this pins it.
    const list = renderHook(() => useRegisterIssueNavigation(WS, { "status:todo": ["a", "b"] }));
    list.unmount();
    const { result } = renderHook(() => useIssueSiblings(WS, "a"));
    expect(result.current).toEqual({ hasContext: true, prevId: null, nextId: "b" });
  });

  it("clears published columns so navigation is hidden", () => {
    useIssueNavigationStore.getState().setColumns(WS, { "status:todo": ["a", "b"] });
    renderHook(() => useClearIssueNavigation(WS));
    const { result } = renderHook(() => useIssueSiblings(WS, "a"));
    expect(result.current.hasContext).toBe(false);
  });

  it("only reads the current workspace's columns", () => {
    renderHook(() => useRegisterIssueNavigation("other-ws", { c: ["a", "b"] }));
    const { result } = renderHook(() => useIssueSiblings(WS, "a"));
    expect(result.current.hasContext).toBe(false);
  });

  it("lets the most recent list win when columns are republished", () => {
    const { rerender } = renderHook(
      ({ cols }) => useRegisterIssueNavigation(WS, cols),
      { initialProps: { cols: { c1: ["a", "b"] } as Record<string, string[]> } },
    );
    rerender({ cols: { c1: ["x", "y", "z"] } });
    const { result } = renderHook(() => useIssueSiblings(WS, "y"));
    expect(result.current).toEqual({ hasContext: true, prevId: "x", nextId: "z" });
  });
});
