// @vitest-environment jsdom
import { act, cleanup, renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { api } from "../api";
import type { TaskMessagePayload } from "../types/events";
import { chatKeys, taskMessagesOptions, useTaskMessages } from "./queries";

vi.mock("../api", () => ({ api: { listTaskMessages: vi.fn() } }));
afterEach(() => { cleanup(); vi.clearAllMocks(); });
const id = "4a2e8d1c-7f9b-4e2a-9c1d-123456789abc";
const msg = (seq: number): TaskMessagePayload => ({ task_id: id, issue_id: "issue", type: "text", content: `part${seq}`, seq });

describe("useTaskMessages", () => {
  it("defers historical transcript requests and backfills cached data when opened", async () => {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    client.setQueryData(chatKeys.taskMessages(id), [msg(1)]);
    vi.mocked(api.listTaskMessages).mockResolvedValue([msg(1), msg(2)]);
    const wrapper = ({ children }: { children: ReactNode }) => <QueryClientProvider client={client}>{children}</QueryClientProvider>;
    const { result, rerender } = renderHook(({ enabled }) => useTaskMessages(id, false, enabled), {
      wrapper, initialProps: { enabled: false },
    });
    expect(api.listTaskMessages).not.toHaveBeenCalled();
    rerender({ enabled: true });
    await waitFor(() => expect(result.current.data).toHaveLength(2));
    expect(api.listTaskMessages).toHaveBeenCalledTimes(1);
    rerender({ enabled: false });
    await act(async () => { await client.invalidateQueries({ queryKey: chatKeys.taskMessagesAll() }); });
    expect(api.listTaskMessages).toHaveBeenCalledTimes(1);
    vi.mocked(api.listTaskMessages).mockResolvedValue([msg(1), msg(2), msg(3)]);
    rerender({ enabled: true });
    await waitFor(() => expect(result.current.data).toHaveLength(3));
    expect(api.listTaskMessages).toHaveBeenCalledTimes(2);
    client.clear();
  });

  it("backfills cached live data without dropping concurrent WS events and recovers the terminal tail", async () => {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    client.setQueryDefaults(chatKeys.taskMessages(id), { structuralSharing: taskMessagesOptions(id).structuralSharing });
    client.setQueryData(chatKeys.taskMessages(id), [msg(1)]);
    let resolve!: (messages: TaskMessagePayload[]) => void;
    vi.mocked(api.listTaskMessages).mockImplementationOnce(() => new Promise((done) => { resolve = done; }));
    const wrapper = ({ children }: { children: ReactNode }) => <QueryClientProvider client={client}>{children}</QueryClientProvider>;
    const { result, rerender } = renderHook(({ live }) => { const query = useTaskMessages(id, live); return { data: query.data }; }, { wrapper, initialProps: { live: true } });
    await waitFor(() => expect(api.listTaskMessages).toHaveBeenCalledTimes(1));
    act(() => client.setQueryData(chatKeys.taskMessages(id), [msg(1), msg(3)]));
    await act(async () => resolve([msg(1), msg(2)]));
    await waitFor(() => expect(result.current.data?.map((row) => row.seq)).toEqual([1, 2, 3]));
    vi.mocked(api.listTaskMessages).mockResolvedValue([msg(1), msg(2), msg(3), msg(4)]);
    rerender({ live: false });
    await waitFor(() => expect(result.current.data?.map((row) => row.seq)).toEqual([1, 2, 3, 4]));
    vi.mocked(api.listTaskMessages).mockResolvedValue([msg(1), msg(2), msg(3), msg(4), msg(5)]);
    await act(async () => { await client.invalidateQueries({ queryKey: chatKeys.taskMessagesAll() }); });
    await waitFor(() => expect(result.current.data).toHaveLength(5));
    client.clear();
  });
});
