// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, renderHook } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import type { Issue } from "../types";
import { workspaceKeys } from "../workspace/queries";
import { useIssueDeletion } from "./use-issue-deletion";

vi.mock("../auth", () => ({
  useAuthStore: (select: (state: { user: { id: string } }) => unknown) =>
    select({ user: { id: "me" } }),
}));
vi.mock("../api", () => ({
  api: {
    listWorkspaces: vi.fn(),
    listMembers: vi.fn(),
    listAgents: vi.fn(),
  },
}));
afterEach(cleanup);

function setup(
  restricted: boolean | undefined,
  role: string,
  agents: unknown[] = [],
) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  });
  client.setQueryData(workspaceKeys.list(), [{ id: "ws", restrict_issue_deletion: restricted, settings: { restrict_issue_deletion: !restricted } }]);
  client.setQueryData(workspaceKeys.members("ws"), [{ user_id: "me", role }]);
  client.setQueryData(workspaceKeys.agents("ws"), agents);
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );
  return renderHook(() => useIssueDeletion("ws"), { wrapper });
}
const issue = (type: "member" | "agent", id: string) => ({ creator_type: type, creator_id: id } as Issue);

describe("useIssueDeletion", () => {
  it("keeps the official behavior for a workspace without the setting", () => {
    const { result } = setup(undefined, "member");
    expect(result.current.restricted).toBe(false);
    expect(result.current.canDelete(issue("member", "someone"))).toBe(true);
  });
  it("reads current ownership including archived agents from the shared query", () => {
    const { result } = setup(true, "member", [
      { id: "agent", owner_id: "me", archived_at: "2026-01-01" },
    ]);
    expect(result.current.canDelete(issue("agent", "agent"))).toBe(true);
    expect(result.current.canDelete(issue("member", "someone"))).toBe(false);
    expect(result.current.canDelete(issue("agent", "missing"))).toBe(false);
  });
  it("allows admins to manage orphaned agent issues", () => {
    const { result } = setup(true, "admin");
    expect(result.current.canDelete(issue("agent", "missing"))).toBe(true);
  });
});
