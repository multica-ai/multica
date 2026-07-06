/**
 * @vitest-environment jsdom
 */
import { renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { Workspace } from "./types";
import { useWorkspaceId } from "./hooks";

let currentWorkspace: Workspace | null = null;
let currentWsId: string | null = null;

vi.mock("./paths/hooks", () => ({
  useCurrentWorkspace: () => currentWorkspace,
}));

vi.mock("./platform/workspace-storage", () => ({
  getCurrentWsId: () => currentWsId,
}));

describe("useWorkspaceId", () => {
  beforeEach(() => {
    currentWorkspace = null;
    currentWsId = null;
  });

  it("returns the resolved workspace id when the workspace is available", () => {
    currentWorkspace = {
      id: "ws-resolved",
      name: "Resolved",
      slug: "resolved",
    } as Workspace;
    currentWsId = "ws-mirror";

    const { result } = renderHook(() => useWorkspaceId());

    expect(result.current).toBe("ws-resolved");
  });

  it("falls back to the route-synced workspace id during query handoff", () => {
    currentWsId = "ws-mirror";

    const { result } = renderHook(() => useWorkspaceId());

    expect(result.current).toBe("ws-mirror");
  });

  it("still throws when no workspace is selected", () => {
    expect(() => renderHook(() => useWorkspaceId())).toThrow(
      "useWorkspaceId: no workspace selected",
    );
  });
});
