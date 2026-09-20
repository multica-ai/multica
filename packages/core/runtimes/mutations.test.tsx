// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, renderHook } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { ApiClient, setApiInstance } from "../api";
import { createAuthStore, registerAuthStore } from "../auth";
import { defaultStorage } from "../platform/storage";
import type { AgentRuntimePreference } from "../types";
import { workspaceKeys } from "../workspace/queries";
import { useUpdateAgentRuntimePreference } from "./mutations";
import { runtimeKeys } from "./queries";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

function setup() {
  const client = new ApiClient("https://example.test");
  const auth = createAuthStore({ api: client, storage: defaultStorage });
  registerAuthStore(auth);
  auth.setState({ user: { id: "account-a" } as NonNullable<ReturnType<typeof auth.getState>["user"]> });
  setApiInstance(client);
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity } } });
  function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
  }
  const hook = renderHook(() => useUpdateAgentRuntimePreference("ws", "agent"), { wrapper: Wrapper });
  return { client, auth, qc, ...hook };
}

describe("personal runtime preference mutation session boundary", () => {
  it.each([undefined, { runtimeId: "account-b-runtime", modelMode: "inherit" as const }])(
    "does not publish a late account A response into account B's preference cache (%j)",
    async (nextPreference) => {
      let resolveResponse!: (response: Response) => void;
      let notifyRequested!: () => void;
      const requested = new Promise<void>((resolve) => { notifyRequested = resolve; });
      vi.stubGlobal("fetch", vi.fn(() => {
        notifyRequested();
        return new Promise<Response>((resolve) => { resolveResponse = resolve; });
      }));
      const { auth, qc, result, unmount } = setup();
      let saving!: Promise<AgentRuntimePreference | null>;
      await act(async () => {
        saving = result.current.mutateAsync("account-a-runtime");
        await requested;
      });

      unmount();
      qc.clear();
      auth.setState({ user: { id: "account-b" } as NonNullable<ReturnType<typeof auth.getState>["user"]> });
      const key = runtimeKeys.preference("ws", "agent");
      if (nextPreference) qc.setQueryData(key, nextPreference);
      qc.setQueryData(workspaceKeys.agents("ws"), [{ id: "agent", personal_runtime_id: "account-b-runtime" }]);

      resolveResponse(new Response(JSON.stringify({
        runtime_id: "account-a-runtime", model_mode: "custom", model: "account-a-model",
      })));
      await saving;

      expect(qc.getQueryData(key)).toEqual(nextPreference);
      expect(qc.getQueryState(key)?.isInvalidated ?? false).toBe(false);
      expect(qc.getQueryState(workspaceKeys.agents("ws"))?.isInvalidated).toBe(false);
      qc.clear();
    },
  );

  it("publishes a saved preference and refreshes agent presence within the same session", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify({
      runtime_id: "mine", model_mode: "custom", model: "saved-model", max_concurrent_tasks: 3,
    }))));
    const { qc, result } = setup();
    qc.setQueryData(workspaceKeys.agents("ws"), [{ id: "agent" }]);
    const saved: AgentRuntimePreference = {
      runtimeId: "mine", modelMode: "custom", model: "saved-model", maxConcurrentTasks: 3,
    };

    await act(async () => { await result.current.mutateAsync(saved); });

    expect(qc.getQueryData(runtimeKeys.preference("ws", "agent"))).toEqual(saved);
    expect(qc.getQueryState(workspaceKeys.agents("ws"))?.isInvalidated).toBe(true);
    qc.clear();
  });
});
