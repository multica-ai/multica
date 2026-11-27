// @vitest-environment node
import { afterEach, expect, it, vi } from "vitest";
import { QueryClient } from "@tanstack/react-query";
import { ApiClient, setApiInstance } from "../api";
import { closeGraphScope } from "./graph-web";
import { graphFilterUnreadOptions } from "./filter-catalog";

const scope = { accountId: "00000000-0000-4000-8000-000000000001", workspaceId: "00000000-0000-4000-8000-000000000002" };
afterEach(() => vi.restoreAllMocks());
it("defers the unread read and pages it through the trajectory protocol", async () => {
  const api = new ApiClient("https://synthetic.invalid"), id = "00000000-0000-4000-8000-00000000000a";
  const request = vi.spyOn(api, "readTrajectoryFilterMetadata").mockImplementation(async path => path.includes("cursor") ? { items: [{ issue_id: id, outcome: "completed", created_at: "" }], next_cursor: null } : { items: [{ issue_id: id, outcome: "completed", created_at: "" }], next_cursor: "c1" });
  setApiInstance(api); const client = new QueryClient();
  const options = graphFilterUnreadOptions(scope); expect(request).not.toHaveBeenCalled();
  expect(await client.fetchQuery(options)).toEqual([id]);
  expect(request).toHaveBeenCalledWith("/api/issues/unread?cursor=c1", { signal: expect.any(AbortSignal), workspaceId: scope.workspaceId });
  client.clear();
});
it("rejects a missing unread projection rather than converting it into an empty membership", async () => {
  const api = new ApiClient("https://synthetic.invalid"); vi.spyOn(api, "readTrajectoryFilterMetadata").mockRejectedValue(Object.assign(new Error("synthetic private message"), { status: 404 }));
  setApiInstance(api); const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  await expect(client.fetchQuery(graphFilterUnreadOptions(scope))).rejects.toThrow("trajectory_unavailable"); client.clear();
});
it("cancels a scoped unread projection and cannot cache a late response after close", async () => {
  const api = new ApiClient("https://synthetic.invalid"); let resolve!: (value: unknown) => void;
  const request = vi.spyOn(api, "readTrajectoryFilterMetadata").mockImplementation(() => new Promise(done => { resolve = done; }));
  setApiInstance(api); const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const options = graphFilterUnreadOptions(scope), pending = client.fetchQuery(options).catch(() => undefined);
  await vi.waitFor(() => expect(request).toHaveBeenCalledOnce());
  const signal = request.mock.calls[0]![1]!.signal as AbortSignal;
  closeGraphScope(client, scope); expect(signal.aborted).toBe(true);
  resolve({ items: [], next_cursor: null }); await pending;
  expect(client.getQueryData(options.queryKey)).toBeUndefined(); client.clear();
});
