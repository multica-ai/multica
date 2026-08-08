/**
 * @vitest-environment jsdom
 */
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, renderHook } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";

import { setApiInstance } from "../api";
import type { ApiClient } from "../api/client";
import type { RuntimeModelsResult } from "../types";
import { runtimeModelsKeys } from "./models";
import {
  useDeleteRuntimeModelConnection,
  useUpdateRuntimeModelConnection,
} from "./mutations";
import { runtimeKeys } from "./queries";

const WS_ID = "ws-1";
const RT_ID = "rt-1";

function createWrapper(qc: QueryClient) {
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
  };
}

function seedModelCache(qc: QueryClient) {
  qc.setQueryData<RuntimeModelsResult>(runtimeModelsKeys.forRuntime(RT_ID), {
    models: [{ id: "No/models", label: "No/models" }],
    supported: true,
  });
}

describe("runtime model connection mutations", () => {
  let qc: QueryClient;
  let updateRuntimeModelConnection: ReturnType<typeof vi.fn>;
  let deleteRuntimeModelConnection: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    qc = new QueryClient({
      defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
    });
    updateRuntimeModelConnection = vi.fn(async () => ({
      runtime_id: RT_ID,
      config: {},
      has_api_key: true,
      configured: true,
    }));
    deleteRuntimeModelConnection = vi.fn(async () => ({
      runtime_id: RT_ID,
      config: {},
      has_api_key: false,
      configured: false,
    }));
    setApiInstance({
      updateRuntimeModelConnection,
      deleteRuntimeModelConnection,
    } as unknown as ApiClient);
  });

  afterEach(() => {
    qc.clear();
    vi.restoreAllMocks();
  });

  it("invalidates the runtime model list when a Pi model connection is updated", async () => {
    seedModelCache(qc);
    qc.setQueryData(runtimeKeys.list(WS_ID), []);
    const invalidate = vi.spyOn(qc, "invalidateQueries");

    const { result } = renderHook(() => useUpdateRuntimeModelConnection(WS_ID), {
      wrapper: createWrapper(qc),
    });

    await act(async () => {
      await result.current.mutateAsync({
        runtimeId: RT_ID,
        connection: {
          provider: "deepseek",
          api: "openai-completions",
          base_url: "https://api.deepseek.com",
          model: "deepseek-chat",
          api_key: "secret",
        },
      });
    });

    expect(invalidate).toHaveBeenCalledWith({
      queryKey: runtimeKeys.all(WS_ID),
    });
    expect(invalidate).toHaveBeenCalledWith({
      queryKey: runtimeModelsKeys.forRuntime(RT_ID),
    });
  });

  it("invalidates the runtime model list when a Pi model connection is deleted", async () => {
    seedModelCache(qc);
    const invalidate = vi.spyOn(qc, "invalidateQueries");

    const { result } = renderHook(() => useDeleteRuntimeModelConnection(WS_ID), {
      wrapper: createWrapper(qc),
    });

    await act(async () => {
      await result.current.mutateAsync(RT_ID);
    });

    expect(invalidate).toHaveBeenCalledWith({
      queryKey: runtimeModelsKeys.forRuntime(RT_ID),
    });
  });
});
