// @vitest-environment node
import { afterEach, expect, it, vi } from "vitest";
import { QueryClient } from "@tanstack/react-query";
import { ApiClient, ApiError, setApiInstance } from "../api";
import { createQueryClient } from "../query-client";
import { inboxKeys, inboxUnreadSummaryOptions } from "./queries";

afterEach(() => vi.unstubAllGlobals());

it.each(["listInbox", "listSnoozedInbox", "getInboxUnreadSummary"] as const)(
  "%s forwards cancellation to HTTP",
  async (method) => {
    const fetch = vi.fn().mockResolvedValue(Response.json([]));
    vi.stubGlobal("fetch", fetch);
    const controller = new AbortController();
    await new ApiClient("https://api.example.test")[method](controller.signal);
    expect(fetch.mock.calls[0]![1].signal).toBe(controller.signal);
  },
);

it("keeps cached counts on 503 without retrying, then refreshes on the next request", async () => {
  const summary = [{ workspace_id: "ws-1", count: 3 }];
  const fetch = vi.fn()
    .mockResolvedValueOnce(Response.json({ error: "busy" }, { status: 503, headers: { "Retry-After": "1" } }))
    .mockResolvedValueOnce(Response.json([{ workspace_id: "ws-1", count: 4 }]));
  vi.stubGlobal("fetch", fetch);
  setApiInstance(new ApiClient("https://api.example.test"));
  const qc = createQueryClient();
  qc.setQueryData(inboxKeys.unreadSummary(), summary);
  const options = { ...inboxUnreadSummaryOptions(), staleTime: 0 };
  try {
    await expect(qc.fetchQuery(options)).rejects.toBeInstanceOf(ApiError);
    expect(fetch).toHaveBeenCalledTimes(1);
    expect(qc.getQueryData(inboxKeys.unreadSummary())).toEqual(summary);
    await expect(qc.fetchQuery(options)).resolves.toEqual([{ workspace_id: "ws-1", count: 4 }]);
    expect(fetch).toHaveBeenCalledTimes(2);
  } finally { qc.clear(); }
});

it("Query cancellation aborts the superseded summary HTTP request", async () => {
  let signal: AbortSignal | undefined;
  const fetch = vi.fn((_url: string, init: RequestInit) => new Promise<Response>((_resolve, reject) => {
    signal = init.signal as AbortSignal;
    signal.addEventListener("abort", () => reject(new DOMException("Aborted", "AbortError")));
  }));
  vi.stubGlobal("fetch", fetch);
  setApiInstance(new ApiClient("https://api.example.test"));
  const qc = createQueryClient();
  const result = qc.fetchQuery(inboxUnreadSummaryOptions()).catch(() => undefined);
  try {
    await vi.waitFor(() => expect(fetch).toHaveBeenCalledTimes(1));
    await qc.cancelQueries({ queryKey: inboxKeys.unreadSummary() });
    await result;
    expect(signal?.aborted).toBe(true);
  } finally { qc.clear(); }
});

it("does not depend on an API error class to recognize overload", async () => {
  const busy = Object.assign(new Error("busy"), { status: 503 });
  const getInboxUnreadSummary = vi.fn().mockRejectedValue(busy);
  setApiInstance({ getInboxUnreadSummary } as unknown as ApiClient);
  const qc = createQueryClient();
  try {
    await expect(qc.fetchQuery(inboxUnreadSummaryOptions())).rejects.toBe(busy);
    expect(getInboxUnreadSummary).toHaveBeenCalledTimes(1);
  } finally { qc.clear(); }
});

it("respects the retry settings of a custom QueryClient", async () => {
  const failed = new Error("unavailable");
  const getInboxUnreadSummary = vi.fn().mockRejectedValueOnce(failed).mockResolvedValue([]);
  setApiInstance({ getInboxUnreadSummary } as unknown as ApiClient);
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  try {
    await expect(qc.fetchQuery(inboxUnreadSummaryOptions())).rejects.toBe(failed);
    expect(getInboxUnreadSummary).toHaveBeenCalledTimes(1);
    await expect(qc.fetchQuery(inboxUnreadSummaryOptions())).resolves.toEqual([]);
  } finally { qc.clear(); }
});
