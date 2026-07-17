// @vitest-environment jsdom
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import { inboxKeys } from "@multica/core/inbox/queries";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { roundKeys, useCreateRound, usePauseRound } from "./queries";

const createRound = vi.fn();
const pauseRound = vi.fn();
vi.mock("./api", () => ({
  createRound: (...args: unknown[]) => createRound(...args),
  addIssueToRound: vi.fn(), deleteRound: vi.fn(), getRoundStatus: vi.fn(),
  listRounds: vi.fn(), pauseRound: (...args: unknown[]) => pauseRound(...args), removeIssueFromRound: vi.fn(), startRound: vi.fn(), updateRound: vi.fn(),
}));

describe("round mutations", () => {
  beforeEach(() => {
    createRound.mockReset().mockResolvedValue({ id: "round-1" });
    pauseRound.mockReset().mockResolvedValue(undefined);
  });

  it("refetches the active round list after create", async () => {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
    client.setQueryData(roundKeys.all("ws-1"), []);
    const invalidate = vi.spyOn(client, "invalidateQueries");
    const wrapper = ({ children }: { children: ReactNode }) => <QueryClientProvider client={client}>{children}</QueryClientProvider>;
    const { result } = renderHook(() => useCreateRound("ws-1"), { wrapper });
    await act(async () => result.current.mutate({ name: "Round Alpha" }));
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(invalidate).toHaveBeenCalledWith({ queryKey: roundKeys.all("ws-1") });
  });

  it("refetches the active round list after pause", async () => {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
    client.setQueryData(roundKeys.all("ws-1"), []);
    const invalidate = vi.spyOn(client, "invalidateQueries");
    const wrapper = ({ children }: { children: ReactNode }) => <QueryClientProvider client={client}>{children}</QueryClientProvider>;
    const { result } = renderHook(() => usePauseRound("ws-1"), { wrapper });
    await act(async () => result.current.mutate("round-1"));
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(pauseRound.mock.calls[0]?.[0]).toBe("round-1");
    expect(invalidate).toHaveBeenCalledWith({ queryKey: roundKeys.all("ws-1") });
    expect(invalidate).toHaveBeenCalledWith({ queryKey: inboxKeys.list("ws-1") });
  });
});
