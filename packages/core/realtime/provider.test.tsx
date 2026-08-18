/**
 * @vitest-environment jsdom
 */
import { act, render, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { StoreApi, UseBoundStore } from "zustand";
import type { AuthState } from "../auth/store";
import { setCurrentWorkspace } from "../platform";
import type { StorageAdapter } from "../types/storage";

const socketState = vi.hoisted(() => ({
  clients: [] as Array<{
    url: string;
    cookieAuth: boolean | undefined;
    auth: { token: string | null; workspaceSlug: string } | null;
    connectOrder: number | null;
    disconnectOrder: number | null;
    on: (event: unknown, handler: unknown) => () => void;
    onAny: (handler: unknown) => () => void;
    onReconnect: (callback: unknown) => () => void;
    onConnectionState: (callback: unknown) => () => void;
  }>,
  order: 0,
}));

vi.mock("../api/ws-client", () => ({
  WSClient: class FakeWSClient {
    private record: (typeof socketState.clients)[number];

    constructor(url: string, options?: { cookieAuth?: boolean }) {
      this.record = {
        url,
        cookieAuth: options?.cookieAuth,
        auth: null,
        connectOrder: null,
        disconnectOrder: null,
        on: () => () => {},
        onAny: () => () => {},
        onReconnect: () => () => {},
        onConnectionState: () => () => {},
      };
      socketState.clients.push(this.record);
    }

    setAuth(token: string | null, workspaceSlug: string) {
      this.record.auth = { token, workspaceSlug };
    }

    connect() {
      this.record.connectOrder = ++socketState.order;
    }

    disconnect() {
      this.record.disconnectOrder = ++socketState.order;
    }

    on(event: unknown, handler: unknown) {
      return this.record.on(event, handler);
    }

    onAny(handler: unknown) {
      return this.record.onAny(handler);
    }

    onReconnect(callback: unknown) {
      return this.record.onReconnect(callback);
    }

    onConnectionState(callback: unknown) {
      return this.record.onConnectionState(callback);
    }
  },
}));

vi.mock("./use-realtime-sync", () => ({
  useRealtimeSync: vi.fn(),
}));

import { WSProvider } from "./provider";

const storage: StorageAdapter = {
  getItem: () => null,
  setItem: () => {},
  removeItem: () => {},
};

const authState = { user: { id: "user-1" } } as AuthState;
const authStore = Object.assign(
  (selector: (state: AuthState) => unknown) => selector(authState),
  {
    getState: () => authState,
    subscribe: () => () => {},
    setState: () => {},
    getInitialState: () => authState,
  },
) as unknown as UseBoundStore<StoreApi<AuthState>>;

async function flushWorkspaceNotification() {
  await new Promise<void>((resolve) => queueMicrotask(resolve));
}

describe("WSProvider workspace isolation", () => {
  beforeEach(async () => {
    socketState.clients = [];
    socketState.order = 0;
    setCurrentWorkspace("workspace-a", "id-a");
    await flushWorkspaceNotification();
  });

  afterEach(async () => {
    setCurrentWorkspace(null, null);
    await flushWorkspaceNotification();
  });

  it("disconnects the old workspace before connecting realtime to the new one", async () => {
    render(
      <WSProvider
        wsUrl="wss://vibes.test/ws/tag"
        authStore={authStore}
        storage={storage}
        cookieAuth
      >
        <div>Tag</div>
      </WSProvider>,
    );

    await waitFor(() => expect(socketState.clients).toHaveLength(1));
    expect(socketState.clients[0]).toMatchObject({
      url: "wss://vibes.test/ws/tag",
      cookieAuth: true,
      auth: { token: null, workspaceSlug: "workspace-a" },
      connectOrder: 1,
    });

    await act(async () => {
      setCurrentWorkspace("workspace-b", "id-b");
      await flushWorkspaceNotification();
    });

    await waitFor(() => expect(socketState.clients).toHaveLength(2));
    expect(socketState.clients[0]?.disconnectOrder).toBe(2);
    expect(socketState.clients[1]).toMatchObject({
      url: "wss://vibes.test/ws/tag",
      cookieAuth: true,
      auth: { token: null, workspaceSlug: "workspace-b" },
      connectOrder: 3,
    });
  });
});
