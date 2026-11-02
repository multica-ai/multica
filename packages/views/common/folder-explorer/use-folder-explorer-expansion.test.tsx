// @vitest-environment jsdom

import { act, renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import {
  folderExplorerStorageKey,
  useFolderExplorerExpansion,
} from "./use-folder-explorer-expansion";

afterEach(() => {
  window.localStorage.clear();
  vi.restoreAllMocks();
});

describe("folder explorer expansion persistence", () => {
  it("isolates workspaces and supports bulk actions", () => {
    const { result, rerender } = renderHook(
      ({ wsId }) => useFolderExplorerExpansion("agents", wsId),
      { initialProps: { wsId: "ws-1" } },
    );

    act(() => result.current.toggleFolder("a"));
    expect([...result.current.expandedFolderIds]).toEqual(["a"]);
    expect(window.localStorage.getItem(folderExplorerStorageKey("agents", "ws-1"))).toBe("[\"a\"]");

    rerender({ wsId: "ws-2" });
    expect(result.current.expandedFolderIds.size).toBe(0);
    act(() => result.current.expandAll(["x", "y"]));
    expect([...result.current.expandedFolderIds]).toEqual(["x", "y"]);

    rerender({ wsId: "ws-1" });
    expect([...result.current.expandedFolderIds]).toEqual(["a"]);
    act(() => result.current.collapseAll());
    expect(result.current.expandedFolderIds.size).toBe(0);
  });

  it("ignores malformed or non-string stored values", () => {
    window.localStorage.setItem(folderExplorerStorageKey("skills", "ws-json"), "{broken");
    const malformed = renderHook(() => useFolderExplorerExpansion("skills", "ws-json"));
    expect(malformed.result.current.expandedFolderIds.size).toBe(0);

    window.localStorage.setItem(folderExplorerStorageKey("skills", "ws-values"), "[\"a\",2,\"a\",null]");
    const valid = renderHook(() => useFolderExplorerExpansion("skills", "ws-values"));
    expect([...valid.result.current.expandedFolderIds]).toEqual(["a"]);
  });

  it("keeps expansion working in memory when storage writes fail", () => {
    const key = folderExplorerStorageKey("agents", "ws-denied");
    window.localStorage.setItem(key, "[\"old\"]");
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
      throw new DOMException("Denied", "SecurityError");
    });
    const { result } = renderHook(() => useFolderExplorerExpansion("agents", "ws-denied"));
    expect([...result.current.expandedFolderIds]).toEqual(["old"]);
    act(() => result.current.expandFolder("new"));
    expect([...result.current.expandedFolderIds]).toEqual(["old", "new"]);
  });
});
