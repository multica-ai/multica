// @vitest-environment node
import { afterEach, expect, it, vi } from "vitest";
import { QueryClient } from "@tanstack/react-query";
import { ApiClient, ApiError, setApiInstance } from "../api";
import { createQueryClient } from "../query-client";
import { inboxKeys, inboxUnreadSummaryOptions } from "./queries";
import { INBOX_TRANSIENT_RETRIES } from "./retry";

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

it("retries a busy summary after Retry-After, keeping cached counts until it answers", async () => {
  vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] });
  const summary = [{ workspace_id: "ws-1", count: 3 }];
  const busy = () => Response.json({ error: "busy" }, { status: 503, headers: { "Retry-After": "2" } });
  const fetch = vi.fn()
    .mockResolvedValueOnce(busy())
    .mockResolvedValueOnce(Response.json([{ workspace_id: "ws-1", count: 4 }]));
  vi.stubGlobal("fetch", fetch);
  setApiInstance(new ApiClient("https://api.example.test"));
  const qc = createQueryClient();
  qc.setQueryData(inboxKeys.unreadSummary(), summary);
  const options = { ...inboxUnreadSummaryOptions(), staleTime: 0 };
  try {
    const result = qc.fetchQuery(options);
    await vi.advanceTimersByTimeAsync(1999);
    // Not before the server said to come back.
    expect(fetch).toHaveBeenCalledTimes(1);
    expect(qc.getQueryData(inboxKeys.unreadSummary())).toEqual(summary);
    await vi.advanceTimersByTimeAsync(1000);
    await expect(result).resolves.toEqual([{ workspace_id: "ws-1", count: 4 }]);
    expect(fetch).toHaveBeenCalledTimes(2);
  } finally {
    qc.clear();
    vi.useRealTimers();
  }
});

it("gives up on a server that stays busy after three retries, keeping cached counts", async () => {
  vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] });
  const summary = [{ workspace_id: "ws-1", count: 3 }];
  const fetch = vi.fn(async () => Response.json({ error: "busy" }, { status: 503, headers: { "Retry-After": "1" } }));
  vi.stubGlobal("fetch", fetch);
  setApiInstance(new ApiClient("https://api.example.test"));
  const qc = createQueryClient();
  qc.setQueryData(inboxKeys.unreadSummary(), summary);
  try {
    const result = qc.fetchQuery({ ...inboxUnreadSummaryOptions(), staleTime: 0 }).catch((error: unknown) => error);
    await vi.advanceTimersByTimeAsync(60_000);
    expect(await result).toBeInstanceOf(ApiError);
    expect(fetch).toHaveBeenCalledTimes(1 + INBOX_TRANSIENT_RETRIES);
    expect(qc.getQueryData(inboxKeys.unreadSummary())).toEqual(summary);
  } finally {
    qc.clear();
    vi.useRealTimers();
  }
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
  const busy = Object.assign(new Error("busy"), { status: 503, retryAfterMs: 0 });
  const getInboxUnreadSummary = vi.fn().mockRejectedValueOnce(busy).mockResolvedValue([]);
  setApiInstance({ getInboxUnreadSummary } as unknown as ApiClient);
  const qc = createQueryClient();
  try {
    await expect(qc.fetchQuery({ ...inboxUnreadSummaryOptions(), retryDelay: 0 })).resolves.toEqual([]);
    expect(getInboxUnreadSummary).toHaveBeenCalledTimes(2);
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
