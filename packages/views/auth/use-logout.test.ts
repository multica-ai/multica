// @vitest-environment jsdom
import { describe, expect, it, vi, beforeEach } from "vitest";
import { renderHook } from "@testing-library/react";
import { useLogout } from "./use-logout";

// Order of the destructive calls is the contract under test: each in-memory
// reset is a Zustand setState, and persist middleware writes the reset state
// straight back to storage under the still-active workspace slug. Resetting
// AFTER the per-slug key removal therefore resurrects the just-deleted keys
// (for the issue draft store: with the previous user's lastAssignee inside).
const calls = vi.hoisted(() => [] as string[]);
const mockReset = vi.hoisted(() => vi.fn());
const mockClearWorkspaceStorage = vi.hoisted(() => vi.fn());
const mockAuthLogout = vi.hoisted(() => vi.fn());
const mockPush = vi.hoisted(() => vi.fn());
const mockQueryClientClear = vi.hoisted(() => vi.fn());

vi.mock("@tanstack/react-query", () => ({
  useQueryClient: () => ({
    getQueryData: () => [
      { slug: "acme" },
      { slug: "beta" },
    ],
    clear: mockQueryClientClear,
  }),
}));

vi.mock("@multica/core/auth", () => ({
  useAuthStore: Object.assign(
    (selector?: (s: unknown) => unknown) => {
      const state = { logout: mockAuthLogout };
      return selector ? selector(state) : state;
    },
    { getState: () => ({ logout: mockAuthLogout }) },
  ),
}));

vi.mock("@multica/core/workspace/queries", () => ({
  workspaceKeys: { list: () => ["workspaces", "list"] },
}));

vi.mock("@multica/core/platform", () => ({
  clearWorkspaceStorage: mockClearWorkspaceStorage,
  defaultStorage: { getItem: () => null, setItem: () => {}, removeItem: () => {} },
}));

vi.mock("@multica/core/drafts/cleanup-registry", () => ({
  resetAllRegisteredDrafts: mockReset,
}));

vi.mock("@multica/core/paths", () => ({
  paths: { login: () => "/login" },
}));

vi.mock("../navigation", () => ({
  useNavigation: () => ({ push: mockPush }),
}));

describe("useLogout", () => {
  beforeEach(() => {
    calls.length = 0;
    vi.clearAllMocks();
    mockReset.mockImplementation(() => calls.push("reset"));
    mockClearWorkspaceStorage.mockImplementation((_a: unknown, slug: string) =>
      calls.push(`clear:${slug}`),
    );
  });

  it("resets in-memory drafts BEFORE removing their persisted keys", () => {
    const { result } = renderHook(() => useLogout());
    result.current();

    expect(calls).toEqual(["reset", "clear:acme", "clear:beta"]);
  });

  it("still ends by clearing the query cache, auth, and navigating to /login", () => {
    const { result } = renderHook(() => useLogout());
    result.current();

    expect(mockQueryClientClear).toHaveBeenCalledTimes(1);
    expect(mockAuthLogout).toHaveBeenCalledTimes(1);
    expect(mockPush).toHaveBeenCalledWith("/login");
  });
});
