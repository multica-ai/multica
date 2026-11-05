// @vitest-environment node
import { describe,expect,it } from "vitest";
import { LegacyConversationMigration } from "./legacy-conversation-migration";

const queue = {
  agentSessionId: "old",
  prompts: [
    {
      id: "submit",
      content: [{ type: "text", text: "Recover me" }],
      createdAtUnixMs: 1
    }
  ]
};
describe("legacy conversation migration", () => {
  it("canonicalizes queued drafts and keeps accepted and ambiguous receipts separate", () => {
    const migration = new LegacyConversationMigration();
    const raw = JSON.stringify({
      aliases: { old: "server" },
      creations: { pending: "pending" },
      promptQueues: [queue],
      submissions: {
        accepted: { state: "accepted", response: { message_id: "m" } },
        ambiguous: { state: "pending" },
        malformed: { state: "accepted" }
      }
    });
    migration.hydrate(raw, null);
    expect(migration.needsCheckpoint).toBe(true);
    expect(migration.canonicalId("old")).toBe("server");
    expect(migration.canonicalId("new")).toBe("new");
    expect(migration.queuedDrafts()[0]).toMatchObject({
      agentSessionId: "server",
      prompts: queue.prompts
    });
    expect(migration.clientSubmitId("m")).toBe("accepted");
    expect(migration.receipt("ambiguous")?.state).toBe("unknown");
    expect(migration.receipt("malformed")?.state).toBe("unknown");
    expect(migration.creationUnknown("pending")).toBe(true);
  });

  it("does not import a completed migration again, retaining only unresolved queue contents", () => {
    const first = new LegacyConversationMigration();
    const raw = JSON.stringify({
      aliases: { old: "server" },
      promptQueues: [queue]
    });
    first.hydrate(raw, null);
    first.retainQueues(new Set());
    const restarted = new LegacyConversationMigration();
    restarted.hydrate(raw, first.serialize());
    expect(restarted.needsCheckpoint).toBe(false);
    expect(restarted.queuedDrafts()).toEqual([]);
    expect(restarted.aliases()).toEqual({ old: "server" });
    first.retainQueues(new Set(["server"]));
    expect(restarted.queuedDrafts()).toEqual([]);
  });

  it("refuses a malformed checkpoint so it cannot silently lose recoverable contents", () => {
    expect(() =>
      new LegacyConversationMigration().hydrate(null, '{"version":1}')
    ).toThrow("Invalid conversation migration checkpoint");
  });
});
