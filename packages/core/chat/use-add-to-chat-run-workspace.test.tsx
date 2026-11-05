// @vitest-environment jsdom
import { act, cleanup, renderHook } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { useAddToChatRun } from "./use-add-to-chat-run";
import { useChatRunDraftStore } from "./run-draft-store";
import { createChatStore } from "./store";
import { setCurrentWorkspace } from "../platform/workspace-storage";
import { defaultStorage } from "../platform/storage";
import { clearAllWorkspaceStorage, clearWorkspaceStorage } from "../platform/storage-cleanup";
import { resetAllRegisteredDrafts } from "../drafts/cleanup-registry";

const add = vi.fn();
vi.mock("../api", () => ({ api: { addToChatRun: (...args: unknown[]) => add(...args) } }));
const chat = createChatStore({ storage: defaultStorage });
const clients: QueryClient[] = [];
const key = (slug: string) => `multica:chat:run-drafts:${slug}`;

async function switchWorkspace(slug: string | null) {
  await act(async () => { setCurrentWorkspace(slug, slug); await Promise.resolve(); });
}

function mount(task = "task-old") {
  const qc = new QueryClient();
  clients.push(qc);
  const wrapper = ({ children }: { children: ReactNode }) => <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
  return renderHook(() => useAddToChatRun("chat-a", task), { wrapper });
}

async function beginRequest() {
  let resolve!: (value: object) => void;
  let reject!: (reason: Error) => void;
  add.mockImplementationOnce(() => new Promise((yes, no) => { resolve = yes; reject = no; })).mockResolvedValue({});
  const first = mount();
  let pending!: Promise<void>;
  act(() => { pending = first.result.current!("same"); });
  await act(async () => { await Promise.resolve(); });
  first.unmount();
  return { pending, resolve, reject };
}

beforeEach(async () => {
  localStorage.clear();
  vi.resetAllMocks();
  await switchWorkspace("review-a");
  useChatRunDraftStore.getState().clearDraft();
});
afterEach(() => {
  cleanup();
  for (const client of clients.splice(0)) client.clear();
});

// Regression supplied by review round 2: the old task/UUID must not survive
// a successful response received while a different workspace is selected.
it("confirmed response after workspace navigation must not turn a new instruction into an old retry", async () => {
  const request = await beginRequest();
  act(() => useChatRunDraftStore.getState().setDraft({ attempts: [
    ...useChatRunDraftStore.getState().draft.attempts,
    { id: "newer-a", session: "chat-a", task: "task-old", content: "newer A" },
  ] }));
  await switchWorkspace("review-b");
  act(() => {
    useChatRunDraftStore.getState().setDraft({ attempts: [{ id: "pending-b", session: "chat-b", task: "task-b", content: "B" }] });
    chat.getState().setInputDraft("chat-b", "newer B draft");
  });
  const bBefore = defaultStorage.getItem(key("review-b"));
  const bAttempts = useChatRunDraftStore.getState().draft.attempts;
  await act(async () => { request.resolve({}); await request.pending; });
  expect(defaultStorage.getItem(key("review-b"))).toBe(bBefore);
  expect(useChatRunDraftStore.getState().draft.attempts).toBe(bAttempts);
  expect(chat.getState().inputDrafts["chat-b"]).toBe("newer B draft");
  await switchWorkspace("review-a");
  expect(useChatRunDraftStore.getState().draft.attempts.map((a) => a.id)).toEqual(["newer-a"]);
  const second = mount("task-new");
  await act(async () => { await second.result.current!("same"); });
  expect(add.mock.calls[1]?.[1]).toBe("task-new");
  expect(add.mock.calls[1]?.[3]).not.toBe(add.mock.calls[0]?.[3]);
});

it("keeps the original identity in A when an ambiguous failure arrives in B", async () => {
  const request = await beginRequest();
  const aBefore = defaultStorage.getItem(key("review-a"));
  await switchWorkspace("review-b");
  await act(async () => {
    const failed = expect(request.pending).rejects.toThrow("response lost");
    request.reject(new Error("response lost"));
    await failed;
  });
  expect(defaultStorage.getItem(key("review-a"))).toBe(aBefore);
  expect(useChatRunDraftStore.getState().draft.attempts).toEqual([]);
  await switchWorkspace("review-a");
  const retry = mount("task-new");
  await act(async () => { await retry.result.current!("same"); });
  expect(add.mock.calls[1]).toEqual(add.mock.calls[0]);
});

it.each([false, true])("does not restore deleted workspace data after success (navigated: %s)", async (navigate) => {
  const request = await beginRequest();
  if (navigate) await switchWorkspace("review-b");
  clearWorkspaceStorage(defaultStorage, "review-a");
  await act(async () => { request.resolve({}); await request.pending; });
  expect(defaultStorage.getItem(key("review-a"))).toBeNull();
  await switchWorkspace("review-b");
  await switchWorkspace("review-a");
  expect(useChatRunDraftStore.getState().draft.attempts).toEqual([]);
});

it.each([false, true])("does not restore logged-out drafts after success (navigated: %s)", async (navigate) => {
  const request = await beginRequest();
  if (navigate) await switchWorkspace("review-b");
  act(() => resetAllRegisteredDrafts());
  clearAllWorkspaceStorage(defaultStorage);
  await switchWorkspace(null);
  await act(async () => { request.resolve({}); await request.pending; });
  expect(defaultStorage.getItem(key("review-a"))).toBeNull();
  expect(defaultStorage.getItem(key("review-b"))).toBeNull();
  expect(useChatRunDraftStore.getState().draft.attempts).toEqual([]);
});

it("registers persisted run drafts for cleanup even before chat mounts", async () => {
  vi.resetModules();
  const { clearAllWorkspaceStorage: cleanupColdStart } = await import("../platform/storage-cleanup");
  defaultStorage.setItem(key("old-workspace"), JSON.stringify({ state: { draft: { attempts: [{ id: "old" }] } }, version: 0 }));
  expect(cleanupColdStart(defaultStorage)).toBe(true);
  expect(defaultStorage.getItem(key("old-workspace"))).toBeNull();
});
