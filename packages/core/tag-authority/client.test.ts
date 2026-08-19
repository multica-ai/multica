// @vitest-environment node
import { QueryClient } from "@tanstack/react-query";
import { describe, expect, it, vi } from "vitest";
import {
  AuthorityClientError,
  completeAuthorityWorkspaceSwitch,
  createTagAuthorityClient,
  waitForAuthorityWorkspace,
} from "./client";

function json(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "content-type": "application/json" },
  });
}

const workspace = {
  id: "workspace-1",
  slug: "alpha",
  name: "Alpha",
  role: "owner",
  authorityVersion: 4,
  membershipGeneration: 1,
  capabilities: {
    collaborate: true,
    viewMembers: true,
    leaveWorkspace: true,
    manageWorkspace: true,
    manageOwners: true,
    manageAdmins: true,
    manageMembers: true,
    inviteMembers: true,
  },
} as const;

describe("VIBES Tag authority client", () => {
  it("lists only through the same-origin VIBES authority seam", async () => {
    const fetcher = vi.fn(async () =>
      json({ ok: true, workspaces: [workspace] }),
    );
    const client = createTagAuthorityClient({ fetcher });

    await expect(client.listWorkspaces()).resolves.toEqual([workspace]);
    expect(fetcher).toHaveBeenCalledWith("/api/tag-authority/workspaces", {
      credentials: "same-origin",
      headers: { accept: "application/json" },
      method: "GET",
    });
    expect(fetcher.mock.calls.flat().join(" ")).not.toMatch(
      /\/api\/(?:workspaces|invitations|share-links)(?:\/|\b)/u,
    );
  });

  it("rejects malformed authority responses instead of casting network JSON", async () => {
    const client = createTagAuthorityClient({
      fetcher: async () => json({ ok: true, workspaces: [{ id: 12 }] }),
    });

    await expect(client.listWorkspaces()).rejects.toMatchObject({
      code: "invalid_response",
      status: 200,
    });
  });

  it("normalizes both authority error envelope shapes", async () => {
    const nested = createTagAuthorityClient({
      fetcher: async () => json({ error: { code: "last_owner" } }, 409),
    });
    const flat = createTagAuthorityClient({
      fetcher: async () => json({ error: "wrong_account" }, 403),
    });

    await expect(
      nested.changeMember("workspace-1", {
        targetUserId: "user-1",
        role: "member",
        status: "removed",
        expectedAuthorityVersion: 4,
        idempotencyKey: "member-command-1",
      }),
    ).rejects.toMatchObject({ code: "last_owner", status: 409 });
    await expect(flat.acceptInvitation("opaque-token")).rejects.toMatchObject({
      code: "wrong_account",
      status: 403,
    });
  });

  it("accepts only the native 204 handoff success contract", async () => {
    const success = createTagAuthorityClient({
      fetcher: async () => new Response(null, { status: 204 }),
    });
    const malformed = createTagAuthorityClient({
      fetcher: async () => json({ ok: true }),
    });

    await expect(
      success.exchangeHandoff({ code: "handoff", workspaceSlug: "alpha" }),
    ).resolves.toBeUndefined();
    await expect(
      malformed.exchangeHandoff({ code: "handoff", workspaceSlug: "alpha" }),
    ).rejects.toMatchObject({ code: "invalid_response", status: 200 });
  });
});

describe("projection-ready wait", () => {
  it("keeps provisioning until the created workspace appears in the authority list", async () => {
    const listWorkspaces = vi
      .fn()
      .mockResolvedValueOnce([])
      .mockResolvedValueOnce([workspace]);
    const delay = vi.fn(async () => undefined);

    await expect(
      waitForAuthorityWorkspace({ listWorkspaces }, "workspace-1", {
        timeoutMs: 1000,
        intervalMs: 10,
        delay,
      }),
    ).resolves.toEqual(workspace);
    expect(delay).toHaveBeenCalledOnce();
  });

  it("reports a retryable projection timeout without inventing ready state", async () => {
    const delay = vi.fn(async () => undefined);
    const listWorkspaces = vi.fn(async () => []);

    await expect(
      waitForAuthorityWorkspace({ listWorkspaces }, "workspace-1", {
        timeoutMs: 0,
        intervalMs: 10,
        delay,
      }),
    ).rejects.toEqual(new AuthorityClientError("projection_pending", 202));
  });
});

describe("workspace switch ordering", () => {
  it("drops the old realtime, queries, and selections before exchange and navigation", async () => {
    const order: string[] = [];
    const queryClient = new QueryClient();
    queryClient.setQueryData(["workspaces", "workspace-1", "tasks"], ["old"]);
    const cancel = vi
      .spyOn(queryClient, "cancelQueries")
      .mockImplementation(async () => {
        order.push("cancel-queries");
      });
    vi.spyOn(queryClient, "clear").mockImplementation(() => {
      order.push("clear-queries");
    });

    await completeAuthorityWorkspaceSwitch({
      client: {
        switchWorkspace: async () => {
          order.push("fresh-switch");
          return {
            ok: true as const,
            workspace: { id: "workspace-2", slug: "beta" },
            handoff: { code: "handoff-code" },
          };
        },
        exchangeHandoff: async () => {
          order.push("exchange-handoff");
        },
      },
      workspaceId: "workspace-2",
      destination: "/tag/beta/chat",
      queryClient,
      disconnectRealtime: () => order.push("disconnect-realtime"),
      clearClientSelection: () => order.push("clear-selection"),
      navigate: (href) => order.push(`navigate:${href}`),
    });

    expect(cancel).toHaveBeenCalledOnce();
    expect(order).toEqual([
      "fresh-switch",
      "disconnect-realtime",
      "cancel-queries",
      "clear-queries",
      "clear-selection",
      "exchange-handoff",
      "navigate:/tag/beta/chat",
    ]);
  });
});
