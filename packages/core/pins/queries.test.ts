// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { QueryClient } from "@tanstack/react-query";
import { ApiClient, setApiInstance } from "../api";
import { setCurrentWorkspace } from "../platform/workspace-storage";
import type { PinnedItem } from "../types";
import { pinKeys, pinListOptions } from "./queries";

const pin = (workspaceId: string): PinnedItem => ({
  id: `pin-${workspaceId}`,
  workspace_id: workspaceId,
  user_id: "user-1",
  item_type: "issue",
  item_id: `issue-${workspaceId}`,
  position: 1,
  created_at: "2026-10-16T00:00:00Z",
});
const response = (data: unknown) => new Response(JSON.stringify(data), {
  headers: { "Content-Type": "application/json" },
});

afterEach(() => {
  setCurrentWorkspace(null, null);
  vi.unstubAllGlobals();
});

describe("workspace pin queries", () => {
  it("binds requests to their cache workspace and retains a delayed A response after switching to B", async () => {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
    setApiInstance(new ApiClient("https://api.example.test"));
    let resolveA!: (value: Response) => void;
    const delayedA = new Promise<Response>((resolve) => { resolveA = resolve; });
    const fetchMock = vi.fn((_url: string, init?: RequestInit) => {
      const headers = new Headers(init?.headers);
      // Model server precedence: a nonempty slug wins over a UUID header.
      const workspace = headers.get("X-Workspace-Slug") || headers.get("X-Workspace-ID");
      return workspace === "a" ? delayedA : Promise.resolve(response([pin("b")]));
    });
    vi.stubGlobal("fetch", fetchMock);
    setCurrentWorkspace("a", "a");
    const optionsA = pinListOptions("a", "user-1");
    setCurrentWorkspace("b", "b");
    const pendingA = client.fetchQuery(optionsA);
    await client.fetchQuery(pinListOptions("b", "user-1"));
    resolveA(response([pin("a")]));
    await pendingA;
    expect(client.getQueryData(pinKeys.list("a", "user-1"))).toEqual([pin("a")]);
    expect(client.getQueryData(pinKeys.list("b", "user-1"))).toEqual([pin("b")]);
    setCurrentWorkspace("a", "a");
    expect(await client.fetchQuery(optionsA)).toEqual([pin("a")]);
    expect(fetchMock).toHaveBeenCalledTimes(2);
    expect(fetchMock.mock.calls.every(([, init]) => !init?.method || init.method === "GET")).toBe(true);
    client.clear();
  });

  it("rejects a mismatched response without replacing cached pins", async () => {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    setApiInstance(new ApiClient("https://api.example.test"));
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(response([pin("b")])));
    client.setQueryData(pinKeys.list("a", "user-1"), [pin("a")]);
    await expect(client.fetchQuery(pinListOptions("a", "user-1"))).rejects.toThrow(/workspace/i);
    expect(client.getQueryData(pinKeys.list("a", "user-1"))).toEqual([pin("a")]);
    client.clear();
  });

  it("retains supported pins alongside a future item type", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(response([
      pin("a"), { ...pin("a"), id: "future", item_type: "future-kind" },
    ])));
    expect(await new ApiClient("https://api.example.test").listPins("a")).toEqual([pin("a")]);
  });

  it.each([null, {}, [{ ...pin("a"), item_id: null }]])(
    "rejects malformed responses without clearing cached pins: %j",
    async (data) => {
      const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
      setApiInstance(new ApiClient("https://api.example.test"));
      vi.stubGlobal("fetch", vi.fn().mockResolvedValue(response(data)));
      client.setQueryData(pinKeys.list("a", "user-1"), [pin("a")]);
      await expect(client.fetchQuery(pinListOptions("a", "user-1"))).rejects.toThrow(/pin/i);
      expect(client.getQueryData(pinKeys.list("a", "user-1"))).toEqual([pin("a")]);
      client.clear();
    },
  );
});
