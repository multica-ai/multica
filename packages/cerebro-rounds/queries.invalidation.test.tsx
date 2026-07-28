// @vitest-environment jsdom
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import { inboxKeys } from "@multica/core/inbox/queries";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { roundKeys, useCreateRound, usePauseRound, useReorderRounds } from "./queries";

const createRound = vi.fn();
const pauseRound = vi.fn();
const reorderRounds = vi.fn();
vi.mock("./api", () => ({
  createRound: (...args: unknown[]) => createRound(...args),
  addIssueToRound: vi.fn(), deleteRound: vi.fn(), getRoundStatus: vi.fn(),
  listRounds: vi.fn(), pauseRound: (...args: unknown[]) => pauseRound(...args), removeIssueFromRound: vi.fn(),
  reorderRounds: (...args: unknown[]) => reorderRounds(...args), startRound: vi.fn(), updateRound: vi.fn(),
}));

const roundStatus = (id: string) => ({
  round: { id, workspace_id: "ws-1", owner_id: "owner", name: id, created_at: "", updated_at: "" },
  members: [],
  active_cycle: null,
});

describe("round mutations", () => {
  beforeEach(() => {
    createRound.mockReset().mockResolvedValue({ id: "round-1" });
    pauseRound.mockReset().mockResolvedValue(undefined);
    reorderRounds.mockReset().mockResolvedValue([]);
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

  // FIR-3646 — the dropped order must show immediately; the list must not snap
  // back to the server order while the request is in flight.
  it("applies the new round order to the cache before the request settles", async () => {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
    client.setQueryData(roundKeys.all("ws-1"), [roundStatus("r1"), roundStatus("r2"), roundStatus("r3")]);
    const invalidate = vi.spyOn(client, "invalidateQueries");
    const wrapper = ({ children }: { children: ReactNode }) => <QueryClientProvider client={client}>{children}</QueryClientProvider>;
    const { result } = renderHook(() => useReorderRounds("ws-1"), { wrapper });

    await act(async () => result.current.mutate(["r3", "r1", "r2"]));
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    expect(reorderRounds.mock.calls[0]?.[0]).toEqual(["r3", "r1", "r2"]);
    const cached = client.getQueryData<{ round: { id: string } }[]>(roundKeys.all("ws-1"));
    expect(cached?.map((status) => status.round.id)).toEqual(["r3", "r1", "r2"]);
    expect(invalidate).toHaveBeenCalledWith({ queryKey: roundKeys.all("ws-1") });
  });

  it("rolls the order back when the server rejects it", async () => {
    reorderRounds.mockRejectedValue(new Error("nope"));
    const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
    client.setQueryData(roundKeys.all("ws-1"), [roundStatus("r1"), roundStatus("r2")]);
    const wrapper = ({ children }: { children: ReactNode }) => <QueryClientProvider client={client}>{children}</QueryClientProvider>;
    const { result } = renderHook(() => useReorderRounds("ws-1"), { wrapper });

    await act(async () => result.current.mutate(["r2", "r1"]));
    await waitFor(() => expect(result.current.isError).toBe(true));

    const cached = client.getQueryData<{ round: { id: string } }[]>(roundKeys.all("ws-1"));
    expect(cached?.map((status) => status.round.id)).toEqual(["r1", "r2"]);
  });
});
