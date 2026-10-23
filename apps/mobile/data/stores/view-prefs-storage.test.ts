// @vitest-environment node
import { beforeEach, describe, expect, it, vi } from "vitest";

const keychain = vi.hoisted(() => ({
  items: new Map<string, string>(),
  failReads: false,
}));

vi.mock("expo-secure-store", () => ({
  getItemAsync: vi.fn(async (key: string) => {
    if (keychain.failReads) throw new Error("keychain unavailable");
    return keychain.items.get(key) ?? null;
  }),
  setItemAsync: vi.fn(async (key: string, value: string) => {
    keychain.items.set(key, value);
  }),
  deleteItemAsync: vi.fn(async (key: string) => {
    keychain.items.delete(key);
  }),
}));

import { setViewPrefsScope } from "./view-prefs-storage";
import { useMyIssuesViewStore } from "./my-issues-view-store";
import { useIssuesViewStore } from "./issues-view-store";
import {
  loadProjectIssuesView,
  useProjectIssuesViewStore,
} from "./project-issues-view-store";

const MY_KEY = "multica_my_issues_view.user-1.ws-a";

// Hydration resolves over a few chained promises.
const flush = () => new Promise((resolve) => setTimeout(resolve, 0));

async function enter(userId: string | null, wsId: string | null) {
  setViewPrefsScope(userId, wsId);
  await flush();
}

function saved(key: string) {
  const raw = keychain.items.get(key);
  return raw ? JSON.parse(raw).state : undefined;
}

describe("view prefs persistence", () => {
  beforeEach(async () => {
    await enter(null, null);
    keychain.items.clear();
    keychain.failReads = false;
  });

  it("starts with completed issues hidden and no filters", async () => {
    await enter("user-1", "ws-a");
    const state = useMyIssuesViewStore.getState();
    expect(state.showCompleted).toBe(false);
    expect(state.statusFilters).toEqual([]);
    expect(state.scope).toBe("assigned");
  });

  it("saves changes under the active user and workspace", async () => {
    await enter("user-1", "ws-a");
    useMyIssuesViewStore.getState().toggleStatusFilter("in_review");
    useMyIssuesViewStore.getState().setScope("created");
    useMyIssuesViewStore.getState().setShowCompleted(true);
    await flush();
    expect(saved(MY_KEY)).toEqual({
      scope: "created",
      statusFilters: ["in_review"],
      priorityFilters: [],
      showCompleted: true,
    });
  });

  it("restores the saved view after a restart", async () => {
    keychain.items.set(
      MY_KEY,
      JSON.stringify({
        state: { scope: "agents", statusFilters: ["todo"], priorityFilters: ["high"], showCompleted: true },
        version: 0,
      }),
    );
    await enter("user-1", "ws-a");
    const state = useMyIssuesViewStore.getState();
    expect(state.scope).toBe("agents");
    expect(state.statusFilters).toEqual(["todo"]);
    expect(state.priorityFilters).toEqual(["high"]);
    expect(state.showCompleted).toBe(true);
  });

  it("keeps each workspace's view and restores it on return", async () => {
    await enter("user-1", "ws-a");
    useIssuesViewStore.getState().toggleStatusFilter("blocked");
    await flush();

    await enter("user-1", "ws-b");
    expect(useIssuesViewStore.getState().statusFilters).toEqual([]);

    await enter("user-1", "ws-a");
    expect(useIssuesViewStore.getState().statusFilters).toEqual(["blocked"]);
  });

  it("does not overwrite the previous workspace while switching", async () => {
    await enter("user-1", "ws-a");
    useMyIssuesViewStore.getState().toggleStatusFilter("todo");
    await flush();

    await enter("user-1", "ws-b");
    expect(saved(MY_KEY)?.statusFilters).toEqual(["todo"]);
  });

  it("does not show one account's view to another", async () => {
    await enter("user-1", "ws-a");
    useMyIssuesViewStore.getState().toggleStatusFilter("todo");
    await flush();

    await enter("user-2", "ws-a");
    expect(useMyIssuesViewStore.getState().statusFilters).toEqual([]);
  });

  it("drops fields a different app version saved in another shape", async () => {
    keychain.items.set(
      MY_KEY,
      JSON.stringify({
        state: { scope: "everything", statusFilters: ["todo", 3], priorityFilters: ["huge", "low"], showCompleted: "yes" },
        version: 0,
      }),
    );
    await enter("user-1", "ws-a");
    const state = useMyIssuesViewStore.getState();
    expect(state.scope).toBe("assigned");
    expect(state.statusFilters).toEqual(["todo"]);
    expect(state.priorityFilters).toEqual(["low"]);
    expect(state.showCompleted).toBe(false);
  });

  it("falls back to defaults when the keychain cannot be read", async () => {
    keychain.failReads = true;
    await enter("user-1", "ws-a");
    expect(useMyIssuesViewStore.persist.hasHydrated()).toBe(true);
    expect(useMyIssuesViewStore.getState().showCompleted).toBe(false);
  });

  describe("project issue filters", () => {
    const keyFor = (projectId: string, ws = "ws-a") =>
      `multica_project_issues_view.${projectId}.user-1.${ws}`;
    const view = (projectId: string) =>
      useProjectIssuesViewStore.getState().byProject[projectId];

    it("saves each project's filters separately", async () => {
      await enter("user-1", "ws-a");
      useProjectIssuesViewStore.getState().toggleStatusFilter("p1", "todo");
      useProjectIssuesViewStore.getState().setShowCompleted("p2", true);
      await flush();
      expect(JSON.parse(keychain.items.get(keyFor("p1"))!)).toEqual({
        statusFilters: ["todo"],
        priorityFilters: [],
        showCompleted: false,
      });
      expect(JSON.parse(keychain.items.get(keyFor("p2"))!).showCompleted).toBe(true);
    });

    it("restores a project's filters when it opens after a restart", async () => {
      keychain.items.set(
        keyFor("p1"),
        JSON.stringify({ statusFilters: ["in_progress"], priorityFilters: ["urgent", "huge"], showCompleted: true }),
      );
      await enter("user-1", "ws-a");
      await loadProjectIssuesView("p1");
      expect(view("p1")).toEqual({
        statusFilters: ["in_progress"],
        priorityFilters: ["urgent"],
        showCompleted: true,
      });
      await loadProjectIssuesView("p2");
      expect(view("p2")).toBeUndefined();
    });

    it("forgets loaded filters on a workspace switch", async () => {
      await enter("user-1", "ws-a");
      useProjectIssuesViewStore.getState().toggleStatusFilter("p1", "todo");
      const before = useProjectIssuesViewStore.getState().generation;
      await enter("user-1", "ws-b");
      // Open project screens depend on this to reload under the new scope.
      expect(useProjectIssuesViewStore.getState().generation).toBe(before + 1);
      expect(view("p1")).toBeUndefined();
      await loadProjectIssuesView("p1");
      expect(view("p1")).toBeUndefined();
    });

    it("drops a load that finishes after the workspace changed", async () => {
      keychain.items.set(
        keyFor("p1"),
        JSON.stringify({ statusFilters: ["todo"], priorityFilters: [], showCompleted: false }),
      );
      await enter("user-1", "ws-a");
      const loading = loadProjectIssuesView("p1");
      setViewPrefsScope("user-1", "ws-b");
      await loading;
      expect(view("p1")).toBeUndefined();
    });

    it("keeps a change made while the saved filters load", async () => {
      keychain.items.set(
        keyFor("p1"),
        JSON.stringify({ statusFilters: ["todo"], priorityFilters: [], showCompleted: false }),
      );
      await enter("user-1", "ws-a");
      const loading = loadProjectIssuesView("p1");
      useProjectIssuesViewStore.getState().togglePriorityFilter("p1", "high");
      await loading;
      expect(view("p1")).toEqual({
        statusFilters: [],
        priorityFilters: ["high"],
        showCompleted: false,
      });
    });
  });
});
