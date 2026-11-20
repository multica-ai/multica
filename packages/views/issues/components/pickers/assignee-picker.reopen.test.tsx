// The Recent group's frequency counts revalidate when a picker that stays
// mounted (issue detail sidebar) is closed and opened again. Uses a real
// QueryClient: the regression lives in when the query refetches.
import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import enIssues from "../../../locales/en/issues.json";
import { AssigneePicker } from "./assignee-picker";

const getFrequency = vi.fn();

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "workspace-1" }));
vi.mock("@multica/core/auth", () => ({ useAuthStore: () => ({ id: "user-1" }) }));
vi.mock("@multica/core/agents", () => ({ isAgentRuntimeBound: () => true }));
vi.mock("@multica/core/permissions", () => ({
  canAssignAgentToIssue: () => ({ allowed: true }),
}));
vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({ getActorName: () => "Ada Lovelace" }),
}));
vi.mock("@multica/core/workspace/queries", () => ({
  memberListOptions: () => ({
    queryKey: ["members"],
    queryFn: () => [{ user_id: "user-1", name: "Ada Lovelace", role: "member" }],
  }),
  agentListOptions: () => ({ queryKey: ["agents"], queryFn: () => [] }),
  squadListOptions: () => ({ queryKey: ["squads"], queryFn: () => [] }),
  // Already stale, standing in for the real 60s staleTime having elapsed
  // (covered in core's queries.test.ts).
  assigneeFrequencyOptions: () => ({
    queryKey: ["frequency"],
    queryFn: () => getFrequency(),
    staleTime: 0,
  }),
}));
vi.mock("../../../common/actor-avatar", () => ({
  ActorAvatar: () => <span data-testid="actor-avatar" />,
}));

describe("AssigneePicker frequency refresh", () => {
  it("refetches stale counts when the same picker opens again", async () => {
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const picker = (open: boolean) => (
      <QueryClientProvider client={qc}>
        <I18nProvider locale="en" resources={{ en: { issues: enIssues } }}>
          <AssigneePicker
            assigneeType={null}
            assigneeId={null}
            onUpdate={() => {}}
            open={open}
            onOpenChange={() => {}}
          />
        </I18nProvider>
      </QueryClientProvider>
    );
    // Warmed empty, then an assignment elsewhere counts toward the user.
    getFrequency
      .mockResolvedValueOnce([])
      .mockResolvedValue([{ assignee_type: "member", assignee_id: "user-1", frequency: 1 }]);

    const { rerender } = render(picker(true));
    await screen.findByText("Members");
    expect(screen.queryByText("Recent")).toBeNull();

    rerender(picker(false));
    rerender(picker(true));

    expect(await screen.findByText("Recent")).toBeTruthy();
    expect(getFrequency).toHaveBeenCalledTimes(2);
  });
});
