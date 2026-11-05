// @vitest-environment jsdom
import { act, renderHook } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useAddToChatRun } from "./use-add-to-chat-run";

import { useChatRunDraftStore, forgetChatRunDraft } from "./run-draft-store";

import { setCurrentWorkspace } from "../platform/workspace-storage";
beforeEach(async () => { setCurrentWorkspace("test", "workspace"); await Promise.resolve(); useChatRunDraftStore.getState().clearDraft(); vi.resetAllMocks(); });
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

it("retains independent attempts, the original ended target, and newer drafts", async () => {
  add.mockRejectedValueOnce(new Error("lost A")).mockRejectedValueOnce(new Error("lost B"));
  const first = mount("chat", "original");
  await act(async () => { await expect(first.result.current!("A")).rejects.toThrow(); });
  await act(async () => { await expect(first.result.current!("B")).rejects.toThrow(); });
  first.unmount();
  add.mockResolvedValue({});
  const reopened = mount("chat", "successor");
  await act(async () => { await reopened.result.current!("A"); });
  expect(add.mock.calls[2]).toEqual(add.mock.calls[0]);
  expect(useChatRunDraftStore.getState().draft.attempts.map((a) => a.content)).toEqual(["B"]);
  reopened.unmount();
  const ended = mount("chat", undefined);
  await act(async () => { await ended.result.current!("B"); });
  expect(add.mock.calls[3]).toEqual(add.mock.calls[1]);
  first.qc.clear(); reopened.qc.clear(); ended.qc.clear();
});

it("distinguishes a new identical instruction after confirmation or explicit choice", async () => {
  add.mockResolvedValue({});
  const hook = mount("chat", "task");
  await act(async () => { await hook.result.current!("same"); });
  await act(async () => { await hook.result.current!("same"); });
  expect(add.mock.calls[1]?.[3]).not.toBe(add.mock.calls[0]?.[3]);
  add.mockRejectedValueOnce(new Error("lost"));
  await act(async () => { await expect(hook.result.current!("same")).rejects.toThrow(); });
  act(() => forgetChatRunDraft(add.mock.calls[2]![3]));
  await act(async () => { await hook.result.current!("same"); });
  expect(add.mock.calls[3]?.[3]).not.toBe(add.mock.calls[2]?.[3]);
  hook.qc.clear();
});

it("does not clear another session's attempt when a response arrives after navigation", async () => {
  let resolve!: (value: object) => void;
  add.mockImplementationOnce(() => new Promise((r) => { resolve = r; })).mockRejectedValueOnce(new Error("lost"));
  const first = mount("first", "task1");
  let pending!: Promise<void>;
  act(() => { pending = first.result.current!("A"); });
  await act(async () => { await Promise.resolve(); });
  first.unmount();
  const second = mount("second", "task2");
  await act(async () => { await expect(second.result.current!("B")).rejects.toThrow(); });
  await act(async () => { resolve({}); await pending; });
  expect(useChatRunDraftStore.getState().draft.attempts).toMatchObject([{ session: "second", content: "B" }]);
  first.qc.clear(); second.qc.clear();
});

it("recovers the UUID from persisted storage after a page reload", async () => {
  add.mockRejectedValueOnce(new Error("lost"));
  const first = mount("chat", "task");
  await act(async () => { await expect(first.result.current!("persisted")).rejects.toThrow(); });
  const saved = localStorage.getItem("multica:chat:run-drafts:test");
  expect(saved).toContain(add.mock.calls[0]![3]);
  first.unmount();
  vi.resetModules();
  const { setCurrentWorkspace: setReloadedWorkspace } = await import("../platform/workspace-storage");
  setReloadedWorkspace("test", "workspace");
  await Promise.resolve();
  const { useAddToChatRun: reloaded } = await import("./use-add-to-chat-run");
  const qc = new QueryClient();
  const wrapper = ({ children }: { children: ReactNode }) => <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
  const second = renderHook(() => reloaded("chat", undefined), { wrapper });
  add.mockResolvedValue({});
  await act(async () => { await second.result.current!("persisted"); });
  expect(add.mock.calls[1]).toEqual(add.mock.calls[0]);
  first.qc.clear(); qc.clear();
});
