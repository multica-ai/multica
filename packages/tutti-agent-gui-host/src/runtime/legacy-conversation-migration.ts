import {
readSavedPromptQueues,
type SavedPromptQueue
} from "./saved-prompt-queues";

export const CONVERSATION_MIGRATION_KEY = "conversation-view-migration-v2";
interface Receipt {
  state: "accepted" | "unknown";
  messageId?: string;
}
interface MigrationState {
  version: 2;
  aliases: Record<string, string>;
  unknownCreations: string[];
  receipts: Record<string, Receipt>;
  queues: SavedPromptQueue[];
}
const record = (value: unknown): value is Record<string, unknown> =>
  Boolean(value) && typeof value === "object" && !Array.isArray(value);

/** Read-only legacy identities and recoverable drafts. New operations never write aliases or receipts. */
export class LegacyConversationMigration {
  needsCheckpoint = false;
  private state: MigrationState = {
    version: 2,
    aliases: {},
    unknownCreations: [],
    receipts: {},
    queues: []
  };

  hydrate(legacyRaw: string | null, checkpointRaw: string | null): void {
    const saved: unknown = checkpointRaw ? JSON.parse(checkpointRaw) : null;
    if (checkpointRaw && (!record(saved) || saved.version !== 2))
      throw new Error("Invalid conversation migration checkpoint");
    this.needsCheckpoint = !checkpointRaw && Boolean(legacyRaw);
    const legacy: unknown = !saved && legacyRaw ? JSON.parse(legacyRaw) : null;
    const source =
      record(saved) && saved.version === 2
        ? saved
        : record(legacy)
          ? legacy
          : null;
    if (!source) return;
    const aliases: Record<string, string> = {};
    if (record(source.aliases))
      for (const [oldId, id] of Object.entries(source.aliases)) {
        if (oldId && typeof id === "string" && id) aliases[oldId] = id;
      }
    const receipts: Record<string, Receipt> = {};
    if (source === saved && record(source.receipts)) {
      for (const [id, entry] of Object.entries(source.receipts))
        if (record(entry)) {
          receipts[id] = {
            state:
              entry.state === "accepted" && typeof entry.messageId === "string"
                ? "accepted"
                : "unknown",
            ...(typeof entry.messageId === "string"
              ? { messageId: entry.messageId }
              : {})
          };
        }
    } else if (record(source.submissions)) {
      for (const [id, entry] of Object.entries(source.submissions))
        if (record(entry)) {
          receipts[id] = {
            state:
              entry.state === "accepted" &&
              record(entry.response) &&
              typeof entry.response.message_id === "string"
                ? "accepted"
                : "unknown",
            ...(record(entry.response) &&
            typeof entry.response.message_id === "string"
              ? { messageId: entry.response.message_id }
              : {})
          };
        }
    }
    const unknownCreations = Array.isArray(source.unknownCreations)
      ? source.unknownCreations.filter(
          (id): id is string => typeof id === "string"
        )
      : record(source.creations)
        ? Object.keys(source.creations)
        : [];
    this.state = {
      version: 2,
      aliases,
      receipts,
      unknownCreations,
      queues: readSavedPromptQueues(
        source === saved ? source.queues : source.promptQueues
      ).map((queue) => ({
        ...queue,
        agentSessionId: aliases[queue.agentSessionId] ?? queue.agentSessionId
      }))
    };
  }

  canonicalId(id: string): string {
    return this.state.aliases[id] ?? id;
  }
  aliases(): Readonly<Record<string, string>> {
    return this.state.aliases;
  }
  creationUnknown(id: string): boolean {
    return this.state.unknownCreations.includes(id) && !this.state.aliases[id];
  }
  receipt(id: string): Receipt | undefined {
    return this.state.receipts[id];
  }
  clientSubmitId(messageId: string): string | undefined {
    return Object.entries(this.state.receipts).find(
      ([, entry]) => entry.messageId === messageId
    )?.[0];
  }
  queuedDrafts(): readonly SavedPromptQueue[] {
    return this.state.queues;
  }
  retainQueues(ids: ReadonlySet<string>): void {
    this.state = {
      ...this.state,
      queues: this.state.queues.filter((queue) => ids.has(queue.agentSessionId))
    };
  }
  serialize(): string {
    return JSON.stringify(this.state);
  }
}
