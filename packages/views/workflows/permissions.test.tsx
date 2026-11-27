import { renderHook } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { useWorkflowPermissions } from "./permissions";

const state = vi.hoisted(() => ({ role: "member", userId: "user-1" as string | null }));

vi.mock("@tanstack/react-query", () => ({
  useQuery: () => ({ data: state.role ? [{ user_id: "user-1", role: state.role }] : [] }),
}));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "workspace-1" }));
vi.mock("@multica/core/workspace/queries", () => ({ memberListOptions: () => ({}) }));
vi.mock("@multica/core/auth", () => ({
  useAuthStore: (select: (s: { user: { id: string } | null }) => unknown) =>
    select({ user: state.userId ? { id: state.userId } : null }),
}));

describe("useWorkflowPermissions", () => {
  it.each([
    { name: "project creator", role: "member", creator_type: "member", creator_id: "user-1", allowed: true },
    { name: "another creator", role: "member", creator_type: "member", creator_id: "user-2", allowed: false },
    { name: "agent creator with the same id", role: "member", creator_type: "agent", creator_id: "user-1", allowed: false },
    { name: "unknown creator", role: "member", allowed: false },
    { name: "member lead", role: "member", lead_type: "member", lead_id: "user-1", allowed: true },
    { name: "agent lead with the same id", role: "member", lead_type: "agent", lead_id: "user-1", allowed: false },
    { name: "workspace owner", role: "owner", allowed: true },
    { name: "workspace admin", role: "admin", allowed: true },
  ] as const)("checks project management for $name", ({ role, allowed, ...project }) => {
    state.role = role;
    state.userId = "user-1";
    const { result } = renderHook(() => useWorkflowPermissions({ lead_type: null, lead_id: null, ...project }));

    expect(result.current.canManageProject).toBe(allowed);
    expect(result.current.canCreateWorkflow).toBe(true);
    expect(result.current.canEditWorkflow).toBe(role === "owner" || role === "admin");
  });

  it("lets the project creator edit their own workflow", () => {
    state.role = "member";
    state.userId = "user-1";
    const { result } = renderHook(() => useWorkflowPermissions(
      { creator_type: "member", creator_id: "user-1", lead_type: null, lead_id: null },
      { owner_id: "user-1" },
    ));

    expect(result.current.canManageProject).toBe(true);
    expect(result.current.canEditWorkflow).toBe(true);
  });

  it("grants no permissions without a signed-in member", () => {
    state.role = "";
    state.userId = null;
    const { result } = renderHook(() => useWorkflowPermissions());

    expect(result.current).toEqual({
      isAdmin: false,
      canCreateWorkflow: false,
      canEditWorkflow: false,
      canManageProject: false,
    });
  });
});
