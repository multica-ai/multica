import { beforeEach, describe, expect, it, vi } from "vitest";

const backingStore = new Map<string, string>();
const storage = {
  getItem: vi.fn(async (key: string) => backingStore.get(key) ?? null),
  setItem: vi.fn(async (key: string, value: string) => {
    backingStore.set(key, value);
  }),
  removeItem: vi.fn(async (key: string) => {
    backingStore.delete(key);
  }),
  getAllKeys: vi.fn(async () => [...backingStore.keys()]),
  multiRemove: vi.fn(async (keys: string[]) => {
    keys.forEach((key) => backingStore.delete(key));
  }),
};

vi.mock("@react-native-async-storage/async-storage", () => ({ default: storage }));

const { hydrateChatDrafts, useChatDraftsStore } =
  await import("./chat-drafts-store");
const { hydrateNewIssueDraft, useNewIssueDraftStore } =
  await import("./new-issue-draft-store");
const { hydrateNewProjectDraft, useNewProjectDraftStore } =
  await import("./new-project-draft-store");
const { clearDraftsForUser } = await import("./draft-persistence");

const keyFor = (kind: string, userId: string, workspaceSlug: string) =>
  `multica_draft:${kind}:${userId}:${workspaceSlug}`;
const persist = (state: object) => JSON.stringify({ state, version: 0 });

describe("scoped draft persistence", () => {
  beforeEach(() => {
    backingStore.clear();
    vi.clearAllMocks();
    useChatDraftsStore.persist.setOptions({ name: "multica_draft:chat:unscoped" });
    useNewIssueDraftStore.persist.setOptions({ name: "multica_draft:new-issue:unscoped" });
    useNewProjectDraftStore.persist.setOptions({ name: "multica_draft:new-project:unscoped" });
    useChatDraftsStore.setState({ drafts: {} });
    useNewIssueDraftStore.setState({
      status: "todo",
      priority: "none",
      assignee: null,
      dueDate: null,
      project: null,
    });
    useNewProjectDraftStore.setState({ status: "planned", priority: "none" });
    backingStore.clear();
    vi.clearAllMocks();
  });

  it("restores each draft kind and preserves one workspace when switching away", async () => {
    const userId = "user-a";
    const firstWorkspace = "workspace-a";
    const chatKey = keyFor("chat", userId, firstWorkspace);
    const issueKey = keyFor("new-issue", userId, firstWorkspace);
    const projectKey = keyFor("new-project", userId, firstWorkspace);
    backingStore.set(chatKey, persist({ drafts: { "session-1": "Keep me" } }));
    backingStore.set(issueKey, persist({ status: "in_progress", priority: "high" }));
    backingStore.set(projectKey, persist({ status: "active", priority: "high" }));

    await hydrateChatDrafts(userId, firstWorkspace);
    await hydrateNewIssueDraft(userId, firstWorkspace);
    await hydrateNewProjectDraft(userId, firstWorkspace);

    expect(useChatDraftsStore.getState().drafts).toEqual({ "session-1": "Keep me" });
    expect(useNewIssueDraftStore.getState()).toMatchObject({
      status: "in_progress",
      priority: "high",
    });
    expect(useNewProjectDraftStore.getState()).toMatchObject({
      status: "active",
      priority: "high",
    });

    await hydrateChatDrafts(userId, "workspace-b");
    await hydrateNewIssueDraft(userId, "workspace-b");
    await hydrateNewProjectDraft(userId, "workspace-b");

    expect(useChatDraftsStore.getState().drafts).toEqual({});
    expect(useNewIssueDraftStore.getState()).toMatchObject({
      status: "todo",
      priority: "none",
    });
    expect(useNewProjectDraftStore.getState()).toMatchObject({
      status: "planned",
      priority: "none",
    });
    expect(JSON.parse(backingStore.get(chatKey) ?? "")).toMatchObject({
      state: { drafts: { "session-1": "Keep me" } },
    });
    expect(JSON.parse(backingStore.get(issueKey) ?? "")).toMatchObject({
      state: { status: "in_progress", priority: "high" },
    });
    expect(JSON.parse(backingStore.get(projectKey) ?? "")).toMatchObject({
      state: { status: "active", priority: "high" },
    });
  });

  it("clears every draft partition when a 401 has no cached user id", async () => {
    backingStore.set(keyFor("chat", "user-a", "workspace-a"), persist({ drafts: {} }));
    backingStore.set(keyFor("outbox", "user-b", "workspace-b"), persist({ items: [] }));
    backingStore.set("unrelated", "keep");

    await clearDraftsForUser(null);

    expect(backingStore.has(keyFor("chat", "user-a", "workspace-a"))).toBe(false);
    expect(backingStore.has(keyFor("outbox", "user-b", "workspace-b"))).toBe(false);
    expect(backingStore.get("unrelated")).toBe("keep");
  });
});
