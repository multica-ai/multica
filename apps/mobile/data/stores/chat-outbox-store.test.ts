import { beforeEach, describe, expect, it, vi } from "vitest";
import type { ChatOutboxItem } from "./chat-outbox";

const backingStore = new Map<string, string>();
const storage = {
  getItem: vi.fn(async (key: string) => backingStore.get(key) ?? null),
  setItem: vi.fn(async (key: string, value: string) => {
    backingStore.set(key, value);
  }),
  removeItem: vi.fn(async (key: string) => {
    backingStore.delete(key);
  }),
};

vi.mock("@react-native-async-storage/async-storage", () => ({ default: storage }));

const { clearChatOutbox, hydrateChatOutbox, useChatOutboxStore } =
  await import("./chat-outbox-store");

const keyFor = (userId: string, workspaceSlug: string) =>
  `multica_draft:outbox:${userId}:${workspaceSlug}`;

const queuedItem: ChatOutboxItem = {
  clientId: "00000000-0000-4000-8000-000000000001",
  sessionId: "session-1",
  workspaceSlug: "workspace-a",
  userId: "user-a",
  content: "Send this after reconnecting",
  attachmentIds: [],
  createdAt: "2026-08-12T00:00:00.000Z",
  status: "queued",
  attemptCount: 0,
  retryable: true,
  lastError: null,
  nextAttemptAt: null,
};

const persist = (items: unknown[]) =>
  JSON.stringify({ state: { items }, version: 0 });

describe("chat outbox persistence", () => {
  beforeEach(() => {
    backingStore.clear();
    vi.clearAllMocks();
    useChatOutboxStore.persist.setOptions({
      name: "multica_draft:outbox:unscoped",
    });
    useChatOutboxStore.setState({ items: [] });
    backingStore.clear();
    vi.clearAllMocks();
  });

  it("restores a queue after relaunch without overwriting its partition", async () => {
    const key = keyFor("user-a", "workspace-a");
    backingStore.set(key, persist([queuedItem]));

    await hydrateChatOutbox("user-a", "workspace-a");

    expect(useChatOutboxStore.getState().items).toEqual([queuedItem]);
    expect(JSON.parse(backingStore.get(key) ?? "")).toMatchObject({
      state: { items: [queuedItem] },
    });
  });

  it("switches to an empty scope without destroying the previous queue", async () => {
    const firstKey = keyFor("user-a", "workspace-a");
    backingStore.set(firstKey, persist([queuedItem]));

    await hydrateChatOutbox("user-a", "workspace-a");
    await hydrateChatOutbox("user-b", "workspace-b");

    expect(useChatOutboxStore.getState().items).toEqual([]);
    expect(JSON.parse(backingStore.get(firstKey) ?? "")).toMatchObject({
      state: { items: [queuedItem] },
    });
  });

  it("clears memory without recreating the partition after auth cleanup", async () => {
    const key = keyFor("user-a", "workspace-a");
    backingStore.set(key, persist([queuedItem]));
    await hydrateChatOutbox("user-a", "workspace-a");
    vi.clearAllMocks();

    clearChatOutbox();

    expect(useChatOutboxStore.getState().items).toEqual([]);
    expect(storage.setItem).not.toHaveBeenCalled();
  });

  it("persists an enqueued message across a simulated process restart", async () => {
    const key = keyFor("user-a", "workspace-a");
    await hydrateChatOutbox("user-a", "workspace-a");

    useChatOutboxStore.getState().enqueue(queuedItem);

    await vi.waitFor(() =>
      expect(JSON.parse(backingStore.get(key) ?? "")).toMatchObject({
        state: { items: [queuedItem] },
      }),
    );

    // `resetModules` gives the second hydrate a fresh Zustand store while
    // retaining the AsyncStorage backing map, like a full app relaunch.
    vi.resetModules();
    const reloaded = await import("./chat-outbox-store");
    await reloaded.hydrateChatOutbox("user-a", "workspace-a");

    expect(reloaded.useChatOutboxStore.getState().items).toEqual([queuedItem]);
  });
});
