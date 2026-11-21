// @vitest-environment jsdom
import { act, renderHook, waitFor } from "@testing-library/react";
import { focusManager, QueryClientProvider, useQuery } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { expect, it, vi } from "vitest";
import { api } from "../api";
import { createQueryClient } from "../query-client";
import { runtimeListOptions } from "./queries";

vi.mock("../api", () => ({ api: { listRuntimes: vi.fn() } }));

it("refreshes a cached runtime list on focus after a missed lifecycle event", async () => {
  const client = createQueryClient();
  const options = runtimeListOptions("ws-1");
  const runtime = {
    id: "runtime-1",
    workspace_id: "ws-1",
    daemon_id: "daemon-1",
    name: "Claude",
    runtime_mode: "local",
    provider: "claude",
    launch_header: "",
    status: "online",
    device_info: "",
    metadata: {},
    owner_id: null,
    visibility: "private",
    last_seen_at: null,
    created_at: "2026-11-17T12:00:00Z",
    updated_at: "2026-11-17T12:00:00Z",
  } as const;
  client.setQueryData(options.queryKey, [runtime]);
  vi.mocked(api.listRuntimes).mockResolvedValue([{ ...runtime, status: "offline" }]);
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );
  focusManager.setFocused(false);
  const { result, unmount } = renderHook(() => useQuery(options), { wrapper });
  try {
    expect(result.current.data?.[0]?.status).toBe("online");
    expect(api.listRuntimes).not.toHaveBeenCalled();
    await act(async () => { focusManager.setFocused(true); });
    await waitFor(() => expect(result.current.data?.[0]?.status).toBe("offline"));
  } finally {
    unmount();
    client.clear();
    focusManager.setFocused(undefined);
  }
});
