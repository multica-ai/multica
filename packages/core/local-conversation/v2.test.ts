// @vitest-environment node
import { localSubmissionSchema } from "./submission";
import { readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it } from "vitest";
import type { ZodType } from "zod";
import { localV2ClientMessageSchema, localV2HelloSchema, localV2ServerMessageSchema, localV2SessionGroupsSchema, localV2SessionPageSchema, localV2SnapshotSchema, localV2PromptStatusesSchema, localV2NavigationSchema, localV2NavigationPath, localV2SummarySchema } from "./v2";

const root = join(__dirname, "../../../server/internal/localconversation/protocol/testdata/local-v2");
const schemas: Record<string, ZodType> = { submission:localSubmissionSchema, server_message: localV2ServerMessageSchema, client_message: localV2ClientMessageSchema, hello: localV2HelloSchema, session_groups: localV2SessionGroupsSchema, session_page: localV2SessionPageSchema, navigation: localV2NavigationSchema };

describe("local conversation v2 wire schemas", () => {
  it("rejects malformed metadata ranges without requiring body data", () => {
    const page = JSON.parse(readFileSync(join(root, "valid/navigation__metadata.json"), "utf8"));
    expect(localV2NavigationSchema.safeParse({ ...page, directory_range: { ...page.directory_range, end_index: -1 } }).success).toBe(false);
    expect(localV2NavigationSchema.safeParse({ ...page, nodes: [{ ...page.nodes[0], turn_index: page.directory_range.end_index + 1 }] }).success).toBe(false);
  });
  it("builds the same navigation path for local, P2P and mobile transports", () => {
    expect(localV2NavigationPath("a/b")).toBe("/sessions/a%2Fb/navigation");
    expect(localV2NavigationPath("c", { nodeIndex: 2, before: 4, after: 5, generation: "g", includeBody: false })).toBe("/sessions/c/navigation?node_index=2&before=4&after=5&store_generation=g&include_body=false");
  });
  const valid = readdirSync(join(root, "valid")).filter((file) => file.endsWith(".json"));

  it("has golden samples to decode", () => {
    expect(valid.length).toBeGreaterThan(5);
  });

  for (const file of valid) {
    const def = file.split("__")[0]!;
    it(`decodes the Go golden sample ${file}`, () => {
      const schema = schemas[def];
      expect(schema, `no schema for ${def}`).toBeDefined();
      const parsed = schema!.safeParse(JSON.parse(readFileSync(join(root, "valid", file), "utf8")));
      expect(parsed.success, parsed.success ? "" : JSON.stringify(parsed.error.issues)).toBe(true);
    });
  }

  it("keeps unknown event types and statuses from a newer daemon", () => {
    const parsed = localV2ServerMessageSchema.parse({
      version: 2, type: "conversation.events", conversation_id: "c", from_seq: 1, to_seq: 1,
      events: [{ seq: 1, turn_id: null, turn_seq: null, item_id: null, type: "future.event", final: false, created_at: "2026-10-29T00:00:00Z", payload: {} }],
    });
    expect(parsed.type === "conversation.events" && parsed.events[0]!.type).toBe("future.event");
  });

  it("rejects malformed page cursors and negative folder counts", () => {
    expect(localV2SessionPageSchema.safeParse({ scope_seq: 1, summaries: [], has_more: true, next_cursor: null }).success).toBe(false);
    expect(localV2SessionPageSchema.safeParse({ scope_seq: 1, summaries: [], has_more: false, next_cursor: "stale" }).success).toBe(false);
    expect(localV2SessionGroupsSchema.safeParse({ scope_seq: 1, groups: [{ id: "", title: "", path: "", total: -1 }] }).success).toBe(false);
  });

  it("rejects a message from another protocol version", () => {
    expect(localV2ClientMessageSchema.safeParse({ version: 1, type: "unsubscribe", conversation_id: "c" }).success).toBe(false);
  });

  it("defaults optional snapshot fields", () => {
    const snapshot = localV2SnapshotSchema.parse({
      version: 2, store_generation: "g", snapshot_seq: 0, turns: [],
      conversation: { conversation_id: "c", summary_seq: 1, provider: "claude", title: "", status: "idle", pinned: false, archived: false, updated_at: "2026-10-29T00:00:00Z" },
    });
    expect(snapshot.pending_interactions).toEqual([]);
    expect(snapshot.conversation.project_id).toBeNull();
  });

  it("decodes send time separately from activity and rejects malformed timestamps", () => {
    const summary = { conversation_id: "c", summary_seq: 1, provider: "codex", title: "", status: "idle", pinned: false, archived: false, updated_at: "2026-11-03T09:02:00Z" };
    expect(localV2SummarySchema.parse(summary).last_turn_sent_at).toBeUndefined();
    expect(localV2SummarySchema.parse({ ...summary, last_turn_sent_at: null }).last_turn_sent_at).toBeNull();
    expect(localV2SummarySchema.parse({ ...summary, last_turn_sent_at: "2026-11-03T09:00:00Z" }).last_turn_sent_at).toBe("2026-11-03T09:00:00Z");
    for (const value of [42, "yesterday", ""]) {
      expect(localV2SummarySchema.safeParse({ ...summary, last_turn_sent_at: value }).success).toBe(false);
    }
  });
});

describe("prompt statuses", () => {
  it("reads unknown fates as none and tolerates a missing map", () => {
    expect(localV2PromptStatusesSchema.parse({ statuses: { a: "delivered", b: "from-the-future" } }).statuses).toEqual({ a: "delivered", b: "none" });
    expect(localV2PromptStatusesSchema.parse({}).statuses).toEqual({});
  });
  it("rejects a response of the wrong shape, for the caller's fallback", () => {
    expect(localV2PromptStatusesSchema.safeParse({ statuses: "nope" }).success).toBe(false);
  });
});
