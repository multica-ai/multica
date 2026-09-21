/** @vitest-environment jsdom */
import { QueryClientProvider, useQuery } from "@tanstack/react-query";
import { act, cleanup, renderHook, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, expect, it, vi } from "vitest";
import { setApiInstance } from "../api";
import type { ApiClient } from "../api/client";
import { WSClient } from "../api/ws-client";
import { createQueryClient } from "../query-client";
import type { Invitation } from "../types";
import { myInvitationListOptions, workspaceKeys } from "../workspace/queries";
import { useRealtimeSync, type RealtimeSyncStores } from "./use-realtime-sync";

vi.mock("../platform/workspace-storage", () => ({
  getCurrentWsId: () => "ws-1",
  getCurrentSlug: () => "test-ws",
  createWorkspaceAwareStorage: (adapter: unknown) => adapter,
  registerForWorkspaceRehydration: () => {},
}));
vi.mock("../paths", () => ({
  useHasOnboarded: () => true,
  resolvePostAuthDestination: () => "/",
}));

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

// Global QueryClient defaults are staleTime: Infinity + no focus refetch, so
// the pending-invitations list only moves when something invalidates it. The
// invitation:accepted / invitation:declined handlers must include it: the
// accepting client may have concluded the invite from another surface, and
// its own stale sidebar row otherwise survives until restart.
it.each(["invitation:accepted", "invitation:declined"] as const)(
  "refreshes the pending-invitations list on %s when the actor is the current user",
  async (event) => {
    const qc = createQueryClient();
    const invitation: Invitation = {
      id: "inv-1",
      workspace_id: "ws-2",
      inviter_id: "u2",
      invitee_email: "me@example.com",
      invitee_user_id: "u1",
      role: "member",
      status: "pending",
      created_at: "2026-09-14T00:00:00Z",
      updated_at: "2026-09-14T00:00:00Z",
      expires_at: "2026-09-21T00:00:00Z",
      workspace_name: "Second Workspace",
      inviter_name: "Inviter",
      inviter_email: "inviter@example.com",
    };
    let rows = [invitation];
    const listMyInvitations = vi.fn(async () => rows);
    setApiInstance({
      listMyInvitations,
    } as unknown as ApiClient);

    const handlers: Record<string, (payload: unknown, actorId?: string, actorType?: string) => void> = {};
    const ws = {
      on: (e: string, handler: (payload: unknown) => void) => {
        handlers[e] = handler;
        return () => {
          delete handlers[e];
        };
      },
      onAny: () => () => {},
      onReconnect: () => () => {},
    } as unknown as WSClient;
    const stores = {
      authStore: { getState: () => ({ user: { id: "u1" } }) },
    } as unknown as RealtimeSyncStores;
    function Wrapper({ children }: { children: ReactNode }) {
      return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
    }
    vi.spyOn(document, "hasFocus").mockReturnValue(true);

    const page = renderHook(() => useQuery(myInvitationListOptions()), {
      wrapper: Wrapper,
    });
    renderHook(() => useRealtimeSync(ws, stores), { wrapper: Wrapper });
    try {
      await waitFor(() => expect(page.result.current.data).toEqual(rows));
      expect(listMyInvitations).toHaveBeenCalledTimes(1);

      rows = [];
      await act(async () => {
        // ws-client unpacks a frame into (payload, actorId, actorType) — the
        // actor rides the frame envelope, not the payload.
        handlers[event]!({ invitation_id: "inv-1" }, "u1", "member");
      });
      await waitFor(() => expect(listMyInvitations).toHaveBeenCalledTimes(2));
      await waitFor(() => expect(page.result.current.data).toEqual([]));
      expect(qc.getQueryState(workspaceKeys.myInvitations())?.data).toEqual([]);
    } finally {
      page.unmount();
    }
  },
);

// The workspace broadcast fans the event out to every online member; the
// account-level pending list must only move for the actor, or every
// accept/decline refetches the list once per member. The workspace-scoped
// admin list still refreshes unconditionally — admins rely on the broadcast.
it.each(["invitation:accepted", "invitation:declined"] as const)(
  "does not refresh the pending-invitations list on %s for a non-actor member",
  async (event) => {
    const qc = createQueryClient();
    const listMyInvitations = vi.fn(async () => []);
    setApiInstance({
      listMyInvitations,
    } as unknown as ApiClient);

    const handlers: Record<string, (payload: unknown, actorId?: string, actorType?: string) => void> = {};
    const ws = {
      on: (e: string, handler: (payload: unknown) => void) => {
        handlers[e] = handler;
        return () => {
          delete handlers[e];
        };
      },
      onAny: () => () => {},
      onReconnect: () => () => {},
    } as unknown as WSClient;
    const stores = {
      authStore: { getState: () => ({ user: { id: "u1" } }) },
    } as unknown as RealtimeSyncStores;
    function Wrapper({ children }: { children: ReactNode }) {
      return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
    }

    qc.setQueryData(workspaceKeys.invitations("ws-1"), []);
    qc.setQueryData(workspaceKeys.myInvitations(), []);
    const page = renderHook(() => useQuery(myInvitationListOptions()), {
      wrapper: Wrapper,
    });
    renderHook(() => useRealtimeSync(ws, stores), { wrapper: Wrapper });
    try {
      // Cache hit + staleTime: Infinity: the list mounts fresh and never
      // fetches, so any call after the event means a (wrong) invalidation.
      await waitFor(() => expect(page.result.current.data).toEqual([]));
      expect(listMyInvitations).not.toHaveBeenCalled();

      await act(async () => {
        handlers[event]!({ invitation_id: "inv-1" }, "u2", "member");
      });
      // Give any (wrong) invalidation a chance to trigger a refetch.
      await act(async () => {
        await new Promise((resolve) => setTimeout(resolve, 20));
      });
      expect(listMyInvitations).not.toHaveBeenCalled();
      expect(qc.getQueryState(workspaceKeys.myInvitations())?.isInvalidated).toBe(
        false,
      );
      expect(
        qc.getQueryState(workspaceKeys.invitations("ws-1"))?.isInvalidated,
      ).toBe(true);
    } finally {
      page.unmount();
    }
  },
);

// --- Wire contract ---
//
// The tests above feed (payload, actorId, actorType) directly — the shape
// ws-client hands to handlers — but they cannot catch drift in that shape
// itself. These drive a real WSClient through a fake WebSocket and feed the
// exact frames server/cmd/server/listeners.go marshals for these events
// ({type, payload, actor_id, actor_type}), pinning frame → onmessage unpack
// → handler → gate end to end.
class FakeWebSocket {
  static lastInstance: FakeWebSocket | null = null;
  onopen: (() => void) | null = null;
  onmessage: ((ev: { data: string }) => void) | null = null;
  onclose: (() => void) | null = null;
  onerror: (() => void) | null = null;
  readyState = 1;
  constructor() {
    FakeWebSocket.lastInstance = this;
  }
  close() {}
  send() {}
}

function wireFrame(
  event: "invitation:accepted" | "invitation:declined",
  payload: Record<string, unknown>,
  actorId: string,
): string {
  return JSON.stringify({
    type: event,
    payload,
    actor_id: actorId,
    actor_type: "member",
  });
}

it.each(["invitation:accepted", "invitation:declined"] as const)(
  "unpacks a real %s frame: refreshes the actor's list, holds a non-actor's",
  async (event) => {
    vi.stubGlobal("WebSocket", FakeWebSocket as unknown as typeof WebSocket);
    const qc = createQueryClient();
    const invitation: Invitation = {
      id: "inv-1",
      workspace_id: "ws-2",
      inviter_id: "u2",
      invitee_email: "me@example.com",
      invitee_user_id: "u1",
      role: "member",
      status: "pending",
      created_at: "2026-09-14T00:00:00Z",
      updated_at: "2026-09-14T00:00:00Z",
      expires_at: "2026-09-21T00:00:00Z",
      workspace_name: "Second Workspace",
      inviter_name: "Inviter",
      inviter_email: "inviter@example.com",
    };
    let rows = [invitation];
    const listMyInvitations = vi.fn(async () => rows);
    setApiInstance({
      listMyInvitations,
    } as unknown as ApiClient);

    const client = new WSClient("ws://example.test/ws");
    client.setAuth("tok", "test-ws");
    client.connect();
    const stores = {
      authStore: { getState: () => ({ user: { id: "u1" } }) },
    } as unknown as RealtimeSyncStores;
    function Wrapper({ children }: { children: ReactNode }) {
      return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
    }
    vi.spyOn(document, "hasFocus").mockReturnValue(true);

    const page = renderHook(() => useQuery(myInvitationListOptions()), {
      wrapper: Wrapper,
    });
    renderHook(() => useRealtimeSync(client, stores), { wrapper: Wrapper });
    try {
      await waitFor(() => expect(page.result.current.data).toEqual(rows));
      expect(listMyInvitations).toHaveBeenCalledTimes(1);

      rows = [];
      // Minimal stand-ins for the payloads the server publishes on each
      // producer path (invitation.go): member on accept, email on decline.
      const payload =
        event === "invitation:accepted"
          ? { invitation_id: "inv-1", member: { user_id: "u1", role: "member" } }
          : { invitation_id: "inv-1", invitee_email: "me@example.com" };

      // The acting invitee's device hears the conclusion (direct send or
      // broadcast) — its stale row must drop within a WS tick.
      await act(async () => {
        FakeWebSocket.lastInstance!.onmessage!({
          data: wireFrame(event, payload, "u1"),
        });
      });
      await waitFor(() => expect(listMyInvitations).toHaveBeenCalledTimes(2));
      await waitFor(() => expect(page.result.current.data).toEqual([]));

      // A non-actor member receiving the same broadcast must not refresh.
      await act(async () => {
        FakeWebSocket.lastInstance!.onmessage!({
          data: wireFrame(event, payload, "u2"),
        });
      });
      await act(async () => {
        await new Promise((resolve) => setTimeout(resolve, 20));
      });
      expect(listMyInvitations).toHaveBeenCalledTimes(2);
      expect(page.result.current.data).toEqual([]);
    } finally {
      page.unmount();
      client.disconnect();
      vi.unstubAllGlobals();
    }
  },
);
