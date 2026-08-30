import { QueryClient } from "@tanstack/react-query";
import { describe, expect, it, vi } from "vitest";
import { workspaceKeys } from "../workspace/queries";
import { operationalControlKeys } from "./queries";
import {
  evictProtectedOperationalControlCaches,
  handleCurrentMemberRoleUpdated,
  handleOperationalControlsChanged,
} from "./realtime";

const workspaceId = "11111111-1111-4111-8111-111111111111";
const otherWorkspaceId = "22222222-2222-4222-8222-222222222222";

function seedProtectedCaches(qc: QueryClient, wsId: string) {
  qc.setQueryData(operationalControlKeys.policy(wsId, "agent-1"), {
    configured: false,
    rules: [],
  });
  qc.setQueryData(operationalControlKeys.actions(wsId, "agent-1"), {
    items: [],
  });
  qc.setQueryData(operationalControlKeys.approvals(wsId), { items: [] });
  qc.setQueryData(operationalControlKeys.capabilities(wsId), {
    capabilities: [],
  });
  qc.setQueryData(
    operationalControlKeys.summary(wsId, { days: 1, tz: "UTC" }),
    {
    workspace_id: wsId,
    },
  );
}

describe("operational control realtime coherence", () => {
  it("validates the workspace-only event before invalidating its protected tree", async () => {
    const qc = new QueryClient();
    seedProtectedCaches(qc, workspaceId);
    seedProtectedCaches(qc, otherWorkspaceId);

    expect(
      handleOperationalControlsChanged(qc, { workspace_id: workspaceId }),
    ).toBe(true);

    expect(
      qc.getQueryState(operationalControlKeys.policy(workspaceId, "agent-1"))
        ?.isInvalidated,
    ).toBe(true);
    expect(
      qc.getQueryState(
        operationalControlKeys.policy(otherWorkspaceId, "agent-1"),
      )?.isInvalidated,
    ).toBe(false);

    const invalidate = vi.spyOn(qc, "invalidateQueries");
    expect(
      handleOperationalControlsChanged(qc, {
        workspace_id: workspaceId,
        agent_id: "forbidden-extra-detail",
      }),
    ).toBe(false);
    expect(handleOperationalControlsChanged(qc, { workspace_id: "bad" })).toBe(
      false,
    );
    expect(
      handleOperationalControlsChanged(
        qc,
        { workspace_id: otherWorkspaceId },
        workspaceId,
      ),
    ).toBe(false);
    expect(invalidate).not.toHaveBeenCalled();
  });

  it("evicts only the downgraded workspace protected cache", () => {
    const qc = new QueryClient();
    seedProtectedCaches(qc, workspaceId);
    seedProtectedCaches(qc, otherWorkspaceId);
    qc.setQueryData(workspaceKeys.members(workspaceId), [{ user_id: "me" }]);

    evictProtectedOperationalControlCaches(qc, workspaceId);

    expect(
      qc.getQueryData(operationalControlKeys.policy(workspaceId, "agent-1")),
    ).toBeUndefined();
    expect(
      qc.getQueryData(
        operationalControlKeys.policy(otherWorkspaceId, "agent-1"),
      ),
    ).toBeDefined();
    expect(qc.getQueryData(workspaceKeys.members(workspaceId))).toBeDefined();
  });

  it("evicts protected data when the current user loses owner or admin access", () => {
    const qc = new QueryClient();
    seedProtectedCaches(qc, workspaceId);

    expect(
      handleCurrentMemberRoleUpdated(qc, {
        workspaceId,
        currentUserId: "me",
        member: { user_id: "me", role: "member" },
      }),
    ).toBe(true);
    expect(
      qc.getQueryData(operationalControlKeys.policy(workspaceId, "agent-1")),
    ).toBeUndefined();
  });

  it("does not evict for another member or a still-privileged current member", () => {
    const qc = new QueryClient();
    seedProtectedCaches(qc, workspaceId);

    expect(
      handleCurrentMemberRoleUpdated(qc, {
        workspaceId,
        currentUserId: "me",
        member: { user_id: "someone-else", role: "member" },
      }),
    ).toBe(false);
    expect(
      handleCurrentMemberRoleUpdated(qc, {
        workspaceId,
        currentUserId: "me",
        member: { user_id: "me", role: "admin" },
      }),
    ).toBe(false);
    expect(
      qc.getQueryData(operationalControlKeys.policy(workspaceId, "agent-1")),
    ).toBeDefined();
  });
});
