// @vitest-environment node
import { afterEach, describe, expect, it } from "vitest";
import { MemoryIndexStore, SEARCH_INDEX_SCHEMA_VERSION } from "./store";
import { IndexFetchError, WorkspaceIndex, type WorkspaceIndexOptions } from "./sync";
import { FakeServer, commentRecord, issueRecord } from "./testing";

let clock = 1_000_000;
const indexes: WorkspaceIndex[] = [];

function makeIndex(server: FakeServer, store = new MemoryIndexStore(), options: Partial<WorkspaceIndexOptions> = {}) {
  const index = new WorkspaceIndex("user:ws", store, server, {
    pollIntervalMs: 60 * 60 * 1000,
    syncDebounceMs: 60 * 60 * 1000,
    retryBaseMs: 60 * 60 * 1000,
    now: () => clock,
    ...options,
  });
  indexes.push(index);
  return { index, store };
}

async function started(index: WorkspaceIndex): Promise<void> {
  await index.start();
}

afterEach(async () => {
  await Promise.all(indexes.splice(0).map((index) => index.dispose()));
});

describe("WorkspaceIndex", () => {
  it("bootstraps page by page, then serves searches with server-shaped rows", async () => {
    const server = new FakeServer();
    server.apply({ kind: "issue", record: issueRecord(1, "Alpha issue", "desc one") });
    server.apply({ kind: "issue", record: issueRecord(2, "Beta issue") });
    server.apply({ kind: "issue", record: issueRecord(3, "Gamma issue") });
    server.apply({ kind: "comment", record: commentRecord("c1", "issue-3", "mentions alpha too") });
    const { index, store } = makeIndex(server);

    expect(await index.searchIssues({ q: "alpha" })).toBeNull();
    await started(index);

    expect(server.calls).toEqual(["manifest", "snapshot", "snapshot", "changes"]);
    expect(index.isServing()).toBe(true);
    expect(store.meta).toMatchObject({ schemaVersion: SEARCH_INDEX_SCHEMA_VERSION, cursor: "c4", bootstrap: null });
    const result = await index.searchIssues({ q: "alpha" });
    expect(result?.issues.map((i) => [i.id, i.match_source])).toEqual([
      ["issue-1", "title"],
      ["issue-3", "comment"],
    ]);
    expect(result?.issues[1]).toMatchObject({ matched_comment_snippet: "mentions alpha too" });
    expect(result?.issues[0]).not.toHaveProperty("search_updated_at");
  });

  it("applies catch-up upserts and deletes, including an issue's comments", async () => {
    const server = new FakeServer();
    server.apply({ kind: "issue", record: issueRecord(1, "Keep me") });
    server.apply({ kind: "issue", record: issueRecord(2, "Delete me") });
    server.apply({ kind: "comment", record: commentRecord("c1", "issue-2", "orphaned needle") });
    const { index, store } = makeIndex(server);
    await started(index);

    server.apply({ kind: "issue", record: issueRecord(1, "Keep me renamed") });
    server.apply({ kind: "delete-issue", id: "issue-2" });
    server.apply({ kind: "issue", record: issueRecord(3, "Brand new") });
    await index.sync();

    expect((await index.searchIssues({ q: "renamed" }))?.issues.map((i) => i.id)).toEqual(["issue-1"]);
    expect((await index.searchIssues({ q: "needle" }))?.issues).toEqual([]);
    expect((await index.searchIssues({ q: "brand" }))?.issues.map((i) => i.id)).toEqual(["issue-3"]);
    expect(store.comments.size).toBe(0);
    expect(store.meta?.cursor).toBe(`c${server.version}`);
  });

  it("resumes an interrupted bootstrap without starting over", async () => {
    const server = new FakeServer();
    for (let n = 1; n <= 5; n++) server.apply({ kind: "issue", record: issueRecord(n, `Issue ${n}`) });
    const { index, store } = makeIndex(server);
    // Fail the second page.
    const originalSnapshot = server.snapshot.bind(server);
    let pages = 0;
    server.snapshot = async (after, limit) => {
      pages += 1;
      if (pages === 2) throw new Error("network down");
      return originalSnapshot(after, limit);
    };
    await started(index);
    expect(store.meta?.bootstrap).toEqual({ cursor: "c5", afterNumber: 2 });
    expect(index.isServing()).toBe(false);

    await index.sync();
    expect(server.calls.filter((c) => c === "manifest")).toHaveLength(1);
    expect(store.issues.size).toBe(5);
    expect(index.isServing()).toBe(true);
  });

  it("declines a workspace over the memory budget and rechecks it later", async () => {
    const server = new FakeServer();
    server.apply({ kind: "issue", record: issueRecord(1, "Huge") });
    server.textBytes = 10_000;
    const { index, store } = makeIndex(server, undefined, { maxTextBytes: 1_000, tooLargeRecheckMs: 1_000 });
    await started(index);

    expect(index.currentState).toBe("too_large");
    expect(server.calls).toEqual(["manifest"]);
    expect(store.meta?.tooLargeAt).toBe(clock);

    await index.sync();
    expect(server.calls).toEqual(["manifest"]);

    clock += 2_000;
    server.textBytes = 10;
    await index.sync();
    expect(index.isServing()).toBe(true);
  });

  it("rebuilds from a fresh manifest when the cursor expired", async () => {
    const server = new FakeServer();
    server.apply({ kind: "issue", record: issueRecord(1, "Before pruning") });
    const { index } = makeIndex(server, undefined, { retryBaseMs: 0 });
    await started(index);

    server.failNext.changes = new IndexFetchError("expired", 410);
    await index.sync();
    // The rebuild is scheduled immediately; let it run.
    await new Promise((resolve) => setTimeout(resolve, 5));
    await index.sync();

    expect(server.calls.filter((c) => c === "manifest")).toHaveLength(2);
    expect(index.isServing()).toBe(true);
  });

  it("drops the local copy when the workspace is no longer accessible", async () => {
    const server = new FakeServer();
    server.apply({ kind: "issue", record: issueRecord(1, "Private title") });
    const { index, store } = makeIndex(server);
    await started(index);

    server.failNext.changes = new IndexFetchError("workspace not found", 404);
    await index.sync();

    expect(index.currentState).toBe("unavailable");
    expect(store.issues.size).toBe(0);
    expect(store.meta?.cursor).toBeNull();
    expect(await index.searchIssues({ q: "private" })).toBeNull();
  });

  it("keeps serving through brief failures, then defers to the server", async () => {
    const server = new FakeServer();
    server.apply({ kind: "issue", record: issueRecord(1, "Steady") });
    const { index } = makeIndex(server, undefined, { staleAfterMs: 1_000 });
    await started(index);

    server.failNext.changes = new Error("offline");
    clock += 10;
    await index.sync();
    expect(index.isServing()).toBe(true);

    clock += 2_000;
    expect(index.isServing()).toBe(false);
    expect(await index.searchIssues({ q: "steady" })).toBeNull();

    await index.sync();
    expect(index.isServing()).toBe(true);
  });

  it("reloads from the store when another context advanced the copy", async () => {
    const server = new FakeServer();
    server.apply({ kind: "issue", record: issueRecord(1, "Shared db") });
    const store = new MemoryIndexStore();
    const { index } = makeIndex(server, store);
    await started(index);

    // Another tab's worker caught up and wrote to the same database.
    server.apply({ kind: "issue", record: issueRecord(2, "Written elsewhere") });
    await store.write({ issues: [server.issues.get("issue-2")!], meta: { ...store.meta!, cursor: `c${server.version}` } });

    await index.sync();
    expect((await index.searchIssues({ q: "elsewhere" }))?.issues.map((i) => i.id)).toEqual(["issue-2"]);
  });

  it("drops the copy when catch-up grows it past the memory budget", async () => {
    const server = new FakeServer();
    server.apply({ kind: "issue", record: issueRecord(1, "Small") });
    const { index, store } = makeIndex(server, undefined, { maxTextBytes: 1_000, tooLargeRecheckMs: 60_000 });
    await started(index);
    expect(index.isServing()).toBe(true);

    server.apply({ kind: "issue", record: issueRecord(2, "Large", "x".repeat(2_000)) });
    await index.sync();

    expect(index.currentState).toBe("too_large");
    expect(index.isServing()).toBe(false);
    expect(await index.searchIssues({ q: "small" })).toBeNull();
    expect(store.issues.size).toBe(0);
    expect(store.meta).toMatchObject({ cursor: null, tooLargeAt: clock });

    // Not re-downloaded on every sync; measured again after the recheck window.
    const callsBefore = server.calls.length;
    await index.sync();
    expect(server.calls.length).toBe(callsBefore);
  });

  it("drops a stored copy that is already over the budget when it loads", async () => {
    const server = new FakeServer();
    server.apply({ kind: "issue", record: issueRecord(1, "Grew", "y".repeat(2_000)) });
    const store = new MemoryIndexStore();
    const first = makeIndex(server, store);
    await started(first.index);
    await first.index.dispose();

    // The budget shrank (or the copy predates the check): the next load declines it.
    const { index } = makeIndex(server, store, { maxTextBytes: 1_000 });
    await started(index);
    expect(index.currentState).toBe("too_large");
    expect(store.issues.size).toBe(0);
  });

  it("stops a bootstrap whose pages outgrow the manifest estimate", async () => {
    const server = new FakeServer();
    for (let n = 1; n <= 4; n++) server.apply({ kind: "issue", record: issueRecord(n, `Page ${n}`, "z".repeat(600)) });
    server.textBytes = 10; // an estimate far below what the pages carry
    const { index, store } = makeIndex(server, undefined, { maxTextBytes: 1_000 });
    await started(index);

    expect(index.currentState).toBe("too_large");
    expect(server.calls.filter((c) => c === "snapshot")).toHaveLength(1);
    expect(store.issues.size).toBe(0);
  });
});
