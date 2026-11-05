import { QueryClient } from "@tanstack/react-query";
import { describe, expect, it, vi } from "vitest";
import { AISessionClient } from "./client";

describe("mobile conversation navigation through the device link", () => {
  it("shares the bounded protocol, scopes requests to the selected device and leaves the live snapshot alone", async () => {
    const fetcher = vi.fn<typeof fetch>(async () => Response.json({ store_generation: "g", snapshot_seq: 100, node_count: 45, nodes: [], turns: [], range: null, directory_range: { start_index: 1, end_index: 6, has_before: true, has_after: true } }));
    const qc = new QueryClient();
    const client = new AISessionClient({ url: "http://localhost:1234", token: "token" }, { userId: "u", workspaceId: "w", deviceId: "peer 1" }, qc, () => { throw new Error("No socket required"); }, fetcher);
    expect((await client.navigation("a/b")).node_count).toBe(45);
    const metadata = await client.navigation("a/b", { nodeIndex: 3, before: 2, after: 3, generation: "g", includeBody: false });
    expect(metadata.directory_range).toEqual({ start_index: 1, end_index: 6, has_before: true, has_after: true });
    expect(metadata.turns).toEqual([]);
    const [url, init] = fetcher.mock.calls[1]!;
    expect(url).toBe("http://localhost:1234/devices/peer%201/local/v2/sessions/a%2Fb/navigation?node_index=3&before=2&after=3&store_generation=g&include_body=false");
    expect(init?.headers).toMatchObject({ Authorization: "Bearer token", "X-Workspace-ID": "w" });
    expect(qc.getQueryCache().getAll()).toHaveLength(0);
  });

  it("validates a remote response and forwards cancellation", async () => {
    const controller = new AbortController();
    const fetcher = vi.fn<typeof fetch>(async (_url, init) => {
      controller.abort();
      expect(init?.signal?.aborted).toBe(true);
      return Response.json({ node_count: -1 });
    });
    const client = new AISessionClient({ url: "http://localhost:1234", token: "token" }, { userId: "u", workspaceId: "w", deviceId: "peer" }, new QueryClient(), () => { throw new Error("No socket required"); }, fetcher);
    await expect(client.navigation("c", {}, controller.signal)).rejects.toThrow();
  });
});
