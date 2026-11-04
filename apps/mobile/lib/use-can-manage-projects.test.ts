// @vitest-environment node
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useCanManageProjects } from "./use-can-manage-projects";

const state = vi.hoisted(() => ({
  wsId: "workspace-1",
  userId: "user-1" as string | undefined,
  members: [] as Array<{ user_id: string; role: string }> | undefined,
}));
vi.mock("@tanstack/react-query", () => ({
  useQuery: (options: { queryKey: string[] }) => {
    expect(options.queryKey).toEqual(["members", state.wsId]);
    return { data: state.members };
  },
  queryOptions: (options: unknown) => options,
}));
vi.mock("@/data/api", () => ({ api: {} }));
vi.mock("@/data/auth-store", () => ({
  useAuthStore: (selector: (s: { user: { id: string } | undefined }) => unknown) =>
    selector({ user: state.userId ? { id: state.userId } : undefined }),
}));
vi.mock("@/data/workspace-store", () => ({
  useWorkspaceStore: (selector: (s: { currentWorkspaceId: string }) => unknown) =>
    selector({ currentWorkspaceId: state.wsId }),
}));

// Check mobile-owned query/store wiring; the role matrix lives in core.
describe("useCanManageProjects", () => {
  beforeEach(() => {
    state.wsId = "workspace-1";
    state.userId = "user-1";
    state.members = [{ user_id: "other-user", role: "owner" }, { user_id: "user-1", role: "member" }];
  });

  it("uses the signed-in member, not another member's role", () => {
    expect(useCanManageProjects()).toBe(false);
    state.userId = "other-user";
    expect(useCanManageProjects()).toBe(true);
  });

  it("fails closed while membership or authentication is unavailable", () => {
    state.members = undefined;
    expect(useCanManageProjects()).toBe(false);
    state.members = [{ user_id: "user-1", role: "owner" }];
    state.userId = undefined;
    expect(useCanManageProjects()).toBe(false);
  });

  it("queries the active workspace after a switch", () => {
    state.wsId = "workspace-2";
    state.members = [];
    expect(useCanManageProjects()).toBe(false);
  });
});
