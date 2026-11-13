// @vitest-environment jsdom
import { act, renderHook } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { expect, it, vi } from "vitest";
import { useAddToChatRun } from "./use-add-to-chat-run";
const add = vi.fn();
vi.mock("../api", () => ({ api: { addToChatRun: (...args: unknown[]) => add(...args) } }));
it("review: retry after remount must keep the ambiguous submission identity", async () => {
  const qc = new QueryClient();
  const wrapper = ({ children }: { children: ReactNode }) => <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
  add.mockRejectedValueOnce(new Error("server accepted, HTTP response lost")).mockResolvedValueOnce({});
  const first = renderHook(() => useAddToChatRun("chat", "task"), { wrapper });
  await act(async () => { await expect(first.result.current!("guidance")).rejects.toThrow(); });
  first.unmount();
  const second = renderHook(() => useAddToChatRun("chat", "task"), { wrapper });
  await act(async () => { await second.result.current!("guidance"); });
  expect(add.mock.calls[1]?.[3]).toBe(add.mock.calls[0]?.[3]);
  second.unmount();
  qc.clear();
});
