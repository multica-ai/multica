// @vitest-environment node
import { afterEach, expect, it, vi } from "vitest";
import { QueryClient, QueryObserver } from "@tanstack/react-query";
import { ApiClient, setApiInstance } from "../api";
import { issueTimelineOptions } from "./queries";

const client = new QueryClient({
  defaultOptions: { queries: { retry: false, gcTime: Infinity } },
});
let finishFirst: (() => void) | undefined;
afterEach(() => {
  finishFirst?.();
  finishFirst = undefined;
  client.clear();
  vi.unstubAllGlobals();
});

it("abandons a pending initial Activity request when leaving and fetches again on return", async () => {
  let initialSignal: AbortSignal | null | undefined;
  const fetchMock = vi.fn()
    .mockImplementationOnce((_url: string, init?: RequestInit) => {
      initialSignal = init?.signal;
      return new Promise<Response>((resolve, reject) => {
        finishFirst = () => resolve(new Response("[]"));
        initialSignal?.addEventListener("abort", () => reject(initialSignal?.reason), { once: true });
      });
    })
    .mockResolvedValueOnce(new Response("[]"));
  vi.stubGlobal("fetch", fetchMock);
  setApiInstance(new ApiClient("https://api.example.test"));

  const observer = new QueryObserver(client, issueTimelineOptions("issue-1"));
  const leave = observer.subscribe(() => {});
  await vi.waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1));
  leave();

  const returning = new QueryObserver(client, issueTimelineOptions("issue-1"));
  const stop = returning.subscribe(() => {});
  try {
    await vi.waitFor(() => expect(returning.getCurrentResult().isSuccess).toBe(true));
    expect(fetchMock).toHaveBeenCalledTimes(2);
    expect(initialSignal?.aborted).toBe(true);
  } finally {
    stop();
  }
});
