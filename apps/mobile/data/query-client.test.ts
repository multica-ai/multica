// @vitest-environment node
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { queryClient } from "./query-client";
import { inboxKeys, inboxListOptions, inboxUnreadSummaryOptions } from "./queries/inbox";

const api = vi.hoisted(() => ({ listInbox: vi.fn(), getInboxUnreadSummary: vi.fn() }));
vi.mock("@/data/api", () => ({ api }));
vi.mock("react-native", () => ({ AppState: { addEventListener: () => ({ remove: vi.fn() }) } }));
vi.mock("@react-native-community/netinfo", () => ({ default: { addEventListener: () => vi.fn() } }));

beforeEach(() => vi.resetAllMocks());
afterEach(() => queryClient.clear());

it("does not retry an overloaded inbox list and still forwards cancellation", async () => {
  const busy = Object.assign(new Error("busy"), { status: 503 });
  api.listInbox.mockRejectedValue(busy);
  await expect(queryClient.fetchQuery(inboxListOptions("ws-1"))).rejects.toBe(busy);
  expect(api.listInbox).toHaveBeenCalledTimes(1);
  expect(api.listInbox.mock.calls[0]![0].signal).toBeInstanceOf(AbortSignal);
});

it("retains cached unread counts on 503 and refreshes on the next request", async () => {
  const before = [{ workspace_id: "ws-1", count: 3 }];
  const after = [{ workspace_id: "ws-1", count: 4 }];
  const busy = Object.assign(new Error("busy"), { status: 503 });
  api.getInboxUnreadSummary.mockRejectedValueOnce(busy).mockResolvedValue(after);
  queryClient.setQueryData(inboxKeys.unreadSummary(), before);
  const options = { ...inboxUnreadSummaryOptions("ws-1"), staleTime: 0 };
  await expect(queryClient.fetchQuery(options)).rejects.toBe(busy);
  expect(api.getInboxUnreadSummary).toHaveBeenCalledTimes(1);
  expect(queryClient.getQueryData(inboxKeys.unreadSummary())).toEqual(before);
  await expect(queryClient.fetchQuery(options)).resolves.toEqual(after);
});

it("keeps one retry for other inbox failures", async () => {
  api.listInbox.mockRejectedValueOnce(Object.assign(new Error("failed"), { status: 500 })).mockResolvedValue([]);
  await expect(queryClient.fetchQuery({ ...inboxListOptions("ws-1"), retryDelay: 0 })).resolves.toEqual([]);
  expect(api.listInbox).toHaveBeenCalledTimes(2);
});

it("keeps the retry policy for other query families", async () => {
  const queryFn = vi.fn().mockRejectedValueOnce(Object.assign(new Error("busy"), { status: 503 })).mockResolvedValue("ok");
  await expect(queryClient.fetchQuery({ queryKey: ["other"], queryFn, retryDelay: 0 })).resolves.toBe("ok");
  expect(queryFn).toHaveBeenCalledTimes(2);
});
