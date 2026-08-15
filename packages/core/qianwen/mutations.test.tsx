/**
 * @vitest-environment jsdom
 */
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";

import { setApiInstance } from "../api";
import type { ApiClient } from "../api/client";
import type {
  QianwenInstallResponse,
  QianwenPairingCodeResponse,
} from "../types";
import {
  useInstallQianwenPersonal,
  useMintQianwenPairingCode,
  useRevokeQianwenInstallation,
  useUnbindQianwenCurrentUser,
} from "./mutations";
import { qianwenKeys } from "./queries";

function createWrapper(queryClient: QueryClient) {
  return function Wrapper({ children }: { children: ReactNode }) {
    return (
      <QueryClientProvider client={queryClient}>
        {children}
      </QueryClientProvider>
    );
  };
}

describe("Qianwen management mutations", () => {
  let queryClient: QueryClient;

  beforeEach(() => {
    queryClient = new QueryClient({
      defaultOptions: {
        queries: { retry: false },
        mutations: { retry: false },
      },
    });
  });

  afterEach(() => {
    cleanup();
    queryClient.clear();
    vi.restoreAllMocks();
  });

  it("keeps a one-time install credential out of Query cache and evicts its mutation immediately when unused", async () => {
    const response: QianwenInstallResponse = {
      id: "installation-1",
      agentId: "agent-1",
      connectionId: "qwc_connection-1",
      mode: "personal_polling",
      status: "active",
      accessToken: "qws_one-time-secret",
      tokenVisibleOnce: true,
      submitPath: "/qianwen/requests",
      statusPathPattern: "/qianwen/requests/{request_id}",
    };
    const installQianwenPersonal = vi.fn(async () => response);
    setApiInstance({ installQianwenPersonal } as unknown as ApiClient);
    queryClient.setQueryData(
      qianwenKeys.installations("workspace-1", "user-1"),
      { installations: [], configured: true },
    );
    const invalidateQueries = vi.spyOn(queryClient, "invalidateQueries");

    const { result, unmount } = renderHook(
      () => useInstallQianwenPersonal("workspace-1"),
      { wrapper: createWrapper(queryClient) },
    );

    await act(async () => {
      await expect(
        result.current.mutateAsync({ agentId: "agent-1" }),
      ).resolves.toEqual(response);
    });

    expect(installQianwenPersonal).toHaveBeenCalledWith("workspace-1", "agent-1");
    expect(invalidateQueries).toHaveBeenCalledWith({
      queryKey: qianwenKeys.installationsRoot("workspace-1"),
    });
    expect(
      JSON.stringify(queryClient.getQueryCache().getAll().map((query) => query.state.data)),
    ).not.toContain(response.accessToken);
    expect(queryClient.getMutationCache().getAll()[0]?.options.gcTime).toBe(0);

    unmount();
    await waitFor(() => {
      expect(queryClient.getMutationCache().getAll()).toHaveLength(0);
    });
  });

  it("keeps a one-time pairing code out of Query cache and gives the mutation zero retention", async () => {
    const response: QianwenPairingCodeResponse = {
      pairingCode: "00001234",
      expiresAt: "2026-08-15T03:00:00Z",
      codeVisibleOnce: true,
    };
    const mintQianwenPairingCode = vi.fn(async () => response);
    setApiInstance({ mintQianwenPairingCode } as unknown as ApiClient);
    const invalidateQueries = vi.spyOn(queryClient, "invalidateQueries");

    const { result, unmount } = renderHook(
      () => useMintQianwenPairingCode("workspace-1"),
      { wrapper: createWrapper(queryClient) },
    );

    await act(async () => {
      await expect(
        result.current.mutateAsync({ installationId: "installation-1" }),
      ).resolves.toEqual(response);
    });

    expect(mintQianwenPairingCode).toHaveBeenCalledWith(
      "workspace-1",
      "installation-1",
    );
    expect(invalidateQueries).toHaveBeenCalledWith({
      queryKey: qianwenKeys.installationsRoot("workspace-1"),
    });
    expect(JSON.stringify(queryClient.getQueryCache().getAll())).not.toContain(
      response.pairingCode,
    );
    expect(queryClient.getMutationCache().getAll()[0]?.options.gcTime).toBe(0);

    unmount();
    await waitFor(() => {
      expect(queryClient.getMutationCache().getAll()).toHaveLength(0);
    });
  });

  it("refreshes the secret-free list after an ambiguous install failure", async () => {
    const installQianwenPersonal = vi.fn(async () => {
      throw new Error("network response lost");
    });
    setApiInstance({ installQianwenPersonal } as unknown as ApiClient);
    const invalidateQueries = vi.spyOn(queryClient, "invalidateQueries");
    const { result } = renderHook(
      () => useInstallQianwenPersonal("workspace-1"),
      { wrapper: createWrapper(queryClient) },
    );

    await act(async () => {
      await expect(
        result.current.mutateAsync({ agentId: "agent-1" }),
      ).rejects.toThrow("network response lost");
    });

    expect(invalidateQueries).toHaveBeenCalledWith({
      queryKey: qianwenKeys.installationsRoot("workspace-1"),
    });
  });

  it("invalidates every caller-specific installation list after unbinding the current user", async () => {
    const unbindQianwenCurrentUser = vi.fn(async () => undefined);
    setApiInstance({ unbindQianwenCurrentUser } as unknown as ApiClient);
    const invalidateQueries = vi.spyOn(queryClient, "invalidateQueries");
    const { result } = renderHook(
      () => useUnbindQianwenCurrentUser("workspace-1"),
      { wrapper: createWrapper(queryClient) },
    );

    await act(async () => {
      await result.current.mutateAsync({ installationId: "installation-1" });
    });

    expect(unbindQianwenCurrentUser).toHaveBeenCalledWith(
      "workspace-1",
      "installation-1",
    );
    expect(invalidateQueries).toHaveBeenCalledWith({
      queryKey: qianwenKeys.installationsRoot("workspace-1"),
    });
  });

  it("invalidates every caller-specific installation list after revoking an installation", async () => {
    const revokeQianwenInstallation = vi.fn(async () => undefined);
    setApiInstance({ revokeQianwenInstallation } as unknown as ApiClient);
    const invalidateQueries = vi.spyOn(queryClient, "invalidateQueries");
    const { result } = renderHook(
      () => useRevokeQianwenInstallation("workspace-1"),
      { wrapper: createWrapper(queryClient) },
    );

    await act(async () => {
      await result.current.mutateAsync({ installationId: "installation-1" });
    });

    expect(revokeQianwenInstallation).toHaveBeenCalledWith(
      "workspace-1",
      "installation-1",
    );
    expect(invalidateQueries).toHaveBeenCalledWith({
      queryKey: qianwenKeys.installationsRoot("workspace-1"),
    });
  });
});
