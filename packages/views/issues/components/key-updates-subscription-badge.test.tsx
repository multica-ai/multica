import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import { notificationPreferenceKeys } from "@multica/core/notification-preferences/queries";
import { agentListOptions } from "@multica/core/workspace/queries";
import type { Agent } from "@multica/core/types";
import enIssues from "../../locales/en/issues.json";
import { KeyUpdatesSubscriptionBadge } from "./key-updates-subscription-badge";

const push = vi.fn();
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-a" }));
vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({ settings: () => "/acme/settings" }),
}));
vi.mock("../../navigation", () => ({ useNavigation: () => ({ push }) }));

afterEach(() => {
  cleanup();
  push.mockReset();
});

const me = "user-me";

function renderBadge({
  assignee,
  reason = "commenter",
  projectCovered = false,
  preferences = {},
  onFollowAll = vi.fn(),
}: {
  assignee: { assignee_type: "member" | "agent" | null; assignee_id: string | null };
  reason?: "commenter" | "manual" | "creator";
  projectCovered?: boolean;
  preferences?: Record<string, string>;
  onFollowAll?: () => void;
}) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity } } });
  qc.setQueryData(notificationPreferenceKeys.all("ws-a"), { workspace_id: "ws-a", preferences });
  qc.setQueryData(agentListOptions("ws-a").queryKey, [{ id: "agent-mine", owner_id: me } as Agent]);
  render(
    <QueryClientProvider client={qc}>
      <I18nProvider locale="en" resources={{ en: { issues: enIssues } }}>
        <KeyUpdatesSubscriptionBadge
          issue={assignee}
          userId={me}
          reason={reason}
          projectCovered={projectCovered}
          pending={false}
          onFollowAll={onFollowAll}
        />
      </I18nProvider>
    </QueryClientProvider>,
  );
}

describe("KeyUpdatesSubscriptionBadge", () => {
  it("offers all activity on someone else's issue", async () => {
    const onFollowAll = vi.fn();
    renderBadge({ assignee: { assignee_type: "member", assignee_id: "user-other" }, onFollowAll });
    fireEvent.click(screen.getByRole("button", { name: "Key updates only" }));
    fireEvent.click(await screen.findByRole("menuitem", { name: "Get all activity" }));
    expect(onFollowAll).toHaveBeenCalledOnce();
  });

  it("links to the default in settings", async () => {
    renderBadge({ assignee: { assignee_type: "member", assignee_id: "user-other" } });
    fireEvent.click(screen.getByRole("button", { name: "Key updates only" }));
    fireEvent.click(await screen.findByRole("menuitem", { name: "Change the default" }));
    expect(push).toHaveBeenCalledWith("/acme/settings?tab=notifications&section=others_issues");
  });

  it.each([
    { name: "own issue", assignee: { assignee_type: "member" as const, assignee_id: me } },
    { name: "own agent", assignee: { assignee_type: "agent" as const, assignee_id: "agent-mine" } },
    { name: "unassigned", assignee: { assignee_type: null, assignee_id: null } },
  ])("stays hidden on the $name", ({ assignee }) => {
    renderBadge({ assignee });
    expect(screen.queryByRole("button", { name: "Key updates only" })).toBeNull();
  });

  it("stays hidden for an explicit subscription or the all preference", () => {
    const theirs = { assignee_type: "member" as const, assignee_id: "user-other" };
    renderBadge({ assignee: theirs, reason: "manual" });
    expect(screen.queryByRole("button", { name: "Key updates only" })).toBeNull();
    cleanup();
    renderBadge({ assignee: theirs, preferences: { others_issues: "all" } });
    expect(screen.queryByRole("button", { name: "Key updates only" })).toBeNull();
  });

  // Commenting on someone else's issue in a project the member watches leaves
  // a commenter row, but the project still delivers every event (MUL-9343).
  it("stays hidden when the project already delivers everything", () => {
    renderBadge({
      assignee: { assignee_type: "member", assignee_id: "user-other" },
      projectCovered: true,
    });
    expect(screen.queryByRole("button", { name: "Key updates only" })).toBeNull();
  });

  // Clickable chips use rounded-md (packages/ui/docs/button.md).
  it("uses the clickable chip radius", () => {
    renderBadge({ assignee: { assignee_type: "member", assignee_id: "user-other" } });
    const trigger = screen.getByRole("button", { name: "Key updates only" });
    expect(trigger.className).toContain("rounded-md");
    expect(trigger.className).not.toContain("rounded-full");
  });
});
