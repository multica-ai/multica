// @vitest-environment node
import { mkdtemp, rm } from "node:fs/promises";
import { DatabaseSync } from "node:sqlite";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, expect, it } from "vitest";
import { exportSQLite } from "./sqlite-source.js";

const cleanups: string[] = [];
afterEach(async () => {
  for (const path of cleanups.splice(0))
    await rm(path, { recursive: true, force: true });
});

// The columns the exporter reads, plus schema 6 submission state.
function database(
  path: string,
  options: { withoutTurnRequestId?: boolean } = {},
) {
  const db = new DatabaseSync(path);
  db.exec(`
    CREATE TABLE scope (id INTEGER PRIMARY KEY, api_origin TEXT, user_id TEXT, workspace_id TEXT);
    CREATE TABLE store_meta (key TEXT, value TEXT);
    CREATE TABLE conversation (id TEXT, scope_id INTEGER, forked_from_id TEXT, forked_from_turn_id TEXT, side_source_id TEXT, last_seq INTEGER, side TEXT, provider TEXT, title TEXT);
    CREATE TABLE turn (id TEXT, conversation_id TEXT, turn_index INTEGER, ${options.withoutTurnRequestId ? "" : "request_id TEXT,"} status TEXT, started_at TEXT, completed_at TEXT);
    CREATE TABLE message (id TEXT, conversation_id TEXT, turn_id TEXT, role TEXT, content TEXT, attachment_ids TEXT, steered INTEGER, failed INTEGER, created_at TEXT);
    CREATE TABLE event (conversation_id TEXT, turn_id TEXT, seq INTEGER, type TEXT, item_id TEXT, final INTEGER, payload TEXT, created_at TEXT);
    CREATE TABLE prompt_queue (conversation_id TEXT, revision INTEGER, paused INTEGER);
    CREATE TABLE prompt_queue_item (conversation_id TEXT, request_id TEXT, item TEXT, position INTEGER);
    CREATE TABLE attachment (id TEXT, scope_id INTEGER, conversation_id TEXT, file_name TEXT, media_type TEXT, path TEXT, size INTEGER, sha256 TEXT);
    CREATE TABLE prompt_submission (conversation_id TEXT, request_id TEXT, item TEXT, state TEXT, revision INTEGER, blocked_reason TEXT, updated_at TEXT);
    INSERT INTO scope VALUES (1, 'http://api.test', 'user', 'workspace');
    INSERT INTO store_meta VALUES ('store_generation', 'generation-1');
    INSERT INTO conversation VALUES ('c1', 1, NULL, NULL, NULL, 7, NULL, 'claude', 'Copied');
    INSERT INTO prompt_queue VALUES ('c1', 3, 1);
    INSERT INTO prompt_queue_item VALUES ('c1', 'queued-a', '{"requestId":"queued-a","content":"A"}', 0);
    INSERT INTO prompt_submission VALUES ('c1', 'queued-a', '{"requestId":"queued-a","content":"A"}', 'queued', 2, 'Paused by restart', '2026-01-01T00:00:00Z');
    INSERT INTO prompt_submission VALUES ('c1', 'review-b', '{"requestId":"review-b","content":"B"}', 'recovery_required', 4, 'Delivery unknown', '2026-01-01T00:00:01Z');
    INSERT INTO prompt_submission VALUES ('c1', 'done-c', '{}', 'attached', 5, '', '2026-01-01T00:00:02Z');
  `);
  db.close();
}

async function source(options?: { withoutTurnRequestId?: boolean }) {
  const directory = await mkdtemp(join(tmpdir(), "verification-sqlite-"));
  cleanups.push(directory);
  const databaseFile = join(directory, "conversations.sqlite");
  database(databaseFile, options);
  return {
    ref: "source",
    kind: "daemon" as const,
    baseUrl: "http://127.0.0.1:1",
    scope: {
      apiOrigin: "http://api.test",
      userId: "user",
      workspaceId: "workspace",
    },
    tokenFile: join(directory, "token"),
    access: "readOnly" as const,
    identity: {},
    databaseFile,
  };
}

it("exports open submissions and lists review items in the queue like the Local API", async () => {
  const content = (await exportSQLite(await source(), ["c1"])) as {
    conversations: {
      snapshot: {
        queue: { items: { requestId: string }[] };
        submissions: {
          request_id: string;
          state: string;
          blocked_reason?: string;
        }[];
      };
    }[];
  };
  const snapshot = content.conversations[0]!.snapshot;
  expect(snapshot.submissions).toEqual([
    expect.objectContaining({
      request_id: "queued-a",
      state: "queued",
      blocked_reason: "Paused by restart",
    }),
    expect.objectContaining({
      request_id: "review-b",
      state: "recovery_required",
      blocked_reason: "Delivery unknown",
    }),
  ]);
  expect(snapshot.queue.items.map((item) => item.requestId)).toEqual([
    "queued-a",
    "review-b",
  ]);
});

it("refuses a store that lacks a column it reads, naming the column", async () => {
  await expect(
    exportSQLite(await source({ withoutTurnRequestId: true }), ["c1"]),
  ).rejects.toMatchObject({
    problem: {
      code: "unsupportedVersion",
      message: expect.stringContaining("turn.request_id"),
    },
  });
});
