// @vitest-environment jsdom

import type { PropsWithChildren } from "react";
import { act, renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { api } from "../api";
import { runtimeKeys } from "../runtimes/queries";
import { workspaceKeys } from "../workspace/queries";
import { extensionKeys } from "./queries";
import { useImportPlatformExtension } from "./mutations";

vi.mock("../api", () => ({
  api: { importPlatformExtension: vi.fn() },
}));

const importResult = {
  release: {
    id: "11111111-1111-4111-8111-111111111111",
    extension_key: "research-team",
    version: "1.0.0",
    digest: `sha256:${"a".repeat(64)}`,
  },
  runtime: {
    id: "22222222-2222-4222-8222-222222222222",
    provider: "platform-agent-cli" as const,
    name: "Platform Agent CLI",
  },
  squad: {
    id: "33333333-3333-4333-8333-333333333333",
    name: "Research Team v1.0.0",
  },
  agents: [],
  skills: [],
  idempotent: false,
};

describe("useImportPlatformExtension", () => {
  let queryClient: QueryClient;

  beforeEach(() => {
    vi.mocked(api.importPlatformExtension).mockReset();
    vi.mocked(api.importPlatformExtension).mockResolvedValue(importResult);
    queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
    });
  });

  function wrapper({ children }: PropsWithChildren) {
    return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
  }

  it("invalidates Extensions and every native resource projection after success", async () => {
    const invalidate = vi.spyOn(queryClient, "invalidateQueries");
    const { result } = renderHook(() => useImportPlatformExtension("ws-1"), { wrapper });

    await act(async () => {
      await result.current.mutateAsync(new TextEncoder().encode("{}"));
    });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    expect(invalidate.mock.calls.map(([filters]) => filters?.queryKey)).toEqual([
      extensionKeys.all("ws-1"),
      workspaceKeys.agents("ws-1"),
      workspaceKeys.skills("ws-1"),
      workspaceKeys.squads("ws-1"),
      runtimeKeys.all("ws-1"),
    ]);
  });
});
