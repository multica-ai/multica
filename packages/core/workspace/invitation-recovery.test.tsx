// @vitest-environment jsdom
import { createElement, type ReactNode } from "react";
import { act, cleanup, renderHook, waitFor } from "@testing-library/react";
import { QueryClientProvider, useQuery } from "@tanstack/react-query";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { setApiInstance, type ApiClient } from "../api";
import { createQueryClient } from "../query-client";
import type { Invitation } from "../types";
import { myInvitationListOptions, workspaceListOptions } from "./queries";

const clients: ReturnType<typeof createQueryClient>[] = [];
let now: number;

beforeEach(() => {
  now = Date.UTC(2026, 10, 8);
  vi.spyOn(Date, "now").mockImplementation(() => now);
  vi.spyOn(document, "visibilityState", "get").mockReturnValue("visible");
});

afterEach(() => {
  cleanup();
  for (const client of clients.splice(0)) client.clear();
  window.dispatchEvent(new Event("online"));
  vi.restoreAllMocks();
});

function invitation(id: string): Invitation {
  return {
    id,
    workspace_id: `workspace-${id}`,
    inviter_id: "inviter",
    invitee_email: "invitee@example.test",
    invitee_user_id: "current-user",
    role: "member",
    status: "pending",
    created_at: "",
    updated_at: "",
    expires_at: "",
  };
}

async function mountList() {
  let rows = [invitation("old")];
  const listMyInvitations = vi.fn(async () => rows);
  const listWorkspaces = vi.fn(async () => []);
  setApiInstance({ listMyInvitations, listWorkspaces } as unknown as ApiClient);
  const client = createQueryClient();
  clients.push(client);
  const hook = renderHook(
    () => ({
      invitations: useQuery(myInvitationListOptions()),
      workspaces: useQuery(workspaceListOptions()),
    }),
    {
      wrapper: ({ children }: { children: ReactNode }) =>
        createElement(QueryClientProvider, { client }, children),
    },
  );
  await waitFor(() => {
    expect(hook.result.current.invitations.data).toEqual(rows);
    expect(hook.result.current.workspaces.isSuccess).toBe(true);
  });
  return {
    hook,
    listMyInvitations,
    listWorkspaces,
    changeRemote: () => { rows = [invitation("new")]; },
  };
}

async function recover(event: "visibilitychange" | "online") {
  await act(async () => {
    if (event === "online") window.dispatchEvent(new Event("offline"));
    window.dispatchEvent(new Event(event));
    // QueryClient forwards the browser event asynchronously through its managers.
    await new Promise((resolve) => setTimeout(resolve, 20));
  });
}

describe("account invitation recovery", () => {
  it("keeps fresh reads across repeated visibility and network recovery", async () => {
    const list = await mountList();
    list.changeRemote();
    now += 29_000;
    for (let i = 0; i < 3; i++) {
      await recover("visibilitychange");
      await recover("online");
    }
    expect(list.listMyInvitations).toHaveBeenCalledTimes(1);
    expect(list.hook.result.current.invitations.data?.[0]?.id).toBe("old");
  });

  it.each(["visibilitychange", "online"] as const)(
    "reconciles a successfully cached list on an eligible %s without refreshing workspace data",
    async (event) => {
      const list = await mountList();
      list.changeRemote();
      now += 31_000;
      expect(list.listMyInvitations).toHaveBeenCalledTimes(1);
      await recover(event);
      await waitFor(() =>
        expect(list.hook.result.current.invitations.data?.[0]?.id).toBe("new"),
      );
      expect(list.listMyInvitations).toHaveBeenCalledTimes(2);
      expect(list.listWorkspaces).toHaveBeenCalledTimes(1);
      await recover("visibilitychange");
      await recover("online");
      expect(list.listMyInvitations).toHaveBeenCalledTimes(2);
    },
  );

  it("retries a transient recovery read, then reconciles on the next online event", async () => {
    const list = await mountList();
    list.changeRemote();
    now += 31_000;
    list.listMyInvitations
      .mockRejectedValueOnce(new Error("temporary read failure"))
      .mockRejectedValueOnce(new Error("temporary read failure"));
    await recover("visibilitychange");
    await waitFor(() => expect(list.hook.result.current.invitations.isError).toBe(true), {
      timeout: 3_000,
    });
    expect(list.listMyInvitations).toHaveBeenCalledTimes(3);
    await recover("online");
    await waitFor(() =>
      expect(list.hook.result.current.invitations.data?.[0]?.id).toBe("new"),
    );
    expect(list.hook.result.current.invitations.isSuccess).toBe(true);
    expect(list.listMyInvitations).toHaveBeenCalledTimes(4);
  });
});
