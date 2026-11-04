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
    listMembers: vi.fn(),
    listAgents: vi.fn(),
  },
}));
afterEach(cleanup);

function setup(role: string | null, agents: unknown[] = []) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  });
  if (role) client.setQueryData(workspaceKeys.members("ws"), [{ user_id: "me", role }]);
  client.setQueryData(workspaceKeys.agents("ws"), agents);
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );
  return renderHook(() => useIssueDeletion("ws"), { wrapper });
}
const issue = (type: "member" | "agent", id: string) => ({ creator_type: type, creator_id: id } as Issue);

describe("useIssueDeletion", () => {
  it("lets members delete only their own and their agents' issues", () => {
    const { result } = setup("member", [
      { id: "agent", owner_id: "me", archived_at: "2026-01-01" },
    ]);
    expect(result.current.canDelete(issue("member", "me"))).toBe(true);
    expect(result.current.canDelete(issue("agent", "agent"))).toBe(true);
    expect(result.current.canDelete(issue("member", "someone"))).toBe(false);
    expect(result.current.canDelete(issue("agent", "missing"))).toBe(false);
  });
  it("allows admins to manage any issue, including orphaned agent issues", () => {
    const { result } = setup("admin");
    expect(result.current.canDelete(issue("member", "someone"))).toBe(true);
    expect(result.current.canDelete(issue("agent", "missing"))).toBe(true);
  });
  it("offers no deletion until membership is known", () => {
    const { result } = setup(null);
    expect(result.current.canDelete(issue("member", "me"))).toBe(false);
  });
});
