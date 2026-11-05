// @vitest-environment jsdom
import { act, renderHook } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { useAddToChatRun } from "./use-add-to-chat-run";

const add = vi.fn();
vi.mock("../api", () => ({ api: { addToChatRun: (...args: unknown[]) => add(...args) } }));
afterEach(() => vi.clearAllMocks());

function mount(session: string | null, task: string | undefined) {
  const qc = new QueryClient();
  const wrapper = ({ children }: { children: ReactNode }) => <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
  return { qc, ...renderHook(() => useAddToChatRun(session, task), { wrapper }) };
}

describe("useAddToChatRun", () => {
  it("has no action without an exact negotiated target", () => {
    const { result, qc } = mount("chat", undefined);
    expect(result.current).toBeUndefined();
    qc.clear();
  });
  it("never retries automatically and reuses the ID on explicit retry", async () => {
    add.mockRejectedValueOnce(new Error("response lost")).mockResolvedValueOnce({});
    const { result, qc } = mount("chat", "exact-task");
    await act(async () => { await expect(result.current!("marker")).rejects.toThrow("response lost"); });
    expect(add).toHaveBeenCalledTimes(1);
    await act(async () => { await result.current!("marker"); });
    expect(add).toHaveBeenCalledTimes(2);
    expect(add.mock.calls[0]).toEqual(add.mock.calls[1]);
    expect(add.mock.calls[0]?.slice(0, 3)).toEqual(["chat", "exact-task", "marker"]);
    qc.clear();
  });
});
