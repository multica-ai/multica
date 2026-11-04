import type { AgentActivityDurableMessage, PromptQueueState } from "@tutti-os/agent-activity-core";
import type { Attachment, SendChatMessageResponse } from "@multica/core/types";
import { readSavedPromptQueues, snapshotPromptQueues, type SavedPromptQueue } from "./saved-prompt-queues";

export interface SubmissionRecord {
  sessionId: string;
  state: "pending" | "accepted" | "unknown";
  response?: SendChatMessageResponse;
}

interface MessageRecord { fingerprint: string; version: number }
interface SessionVersions { next: number; messages: Record<string, MessageRecord> }
interface PersistedLedger {
  aliases: Record<string, string>;
  creations: Record<string, "pending" | "unknown">;
  submissions: Record<string, SubmissionRecord>;
  versions: Record<string, SessionVersions>;
  promptQueues: SavedPromptQueue[];
}

/** Adapter-owned submission receipts, queued drafts and projection cursors. */
export class RuntimeLedger {
  private state: PersistedLedger = { aliases: {}, creations: {}, submissions: {}, versions: {}, promptQueues: [] };

  hydrate(raw: string | null): void {
    if (!raw) return;
    const value: unknown = JSON.parse(raw);
    if (!record(value)) throw new Error("Invalid Agent GUI storage data");
    this.state.promptQueues = readSavedPromptQueues(value.promptQueues);
    if (record(value.aliases)) {
      for (const [guiId, serverId] of Object.entries(value.aliases)) {
        if (typeof serverId === "string" && guiId && serverId) this.bind(guiId, serverId);
      }
    }
    if (record(value.creations)) {
      for (const id of Object.keys(value.creations)) this.state.creations[id] = "unknown";
    }
    if (record(value.submissions)) {
      for (const [id, entry] of Object.entries(value.submissions)) {
        if (!record(entry) || typeof entry.sessionId !== "string") continue;
        const response = entry.response;
        if (entry.state === "accepted" && record(response)
          && typeof response.message_id === "string" && typeof response.task_id === "string"
          && typeof response.created_at === "string") {
          this.state.submissions[id] = { sessionId: entry.sessionId, state: "accepted", response: response as unknown as SendChatMessageResponse };
        } else {
          this.state.submissions[id] = { sessionId: entry.sessionId, state: "unknown" };
        }
      }
    }
    if (record(value.versions)) {
      for (const [sessionId, entry] of Object.entries(value.versions)) {
        if (!record(entry) || !Number.isSafeInteger(entry.next) || !record(entry.messages)) continue;
        const versions: SessionVersions = { next: Math.max(0, entry.next as number), messages: {} };
        for (const [id, message] of Object.entries(entry.messages)) {
          if (record(message) && typeof message.fingerprint === "string"
            && Number.isSafeInteger(message.version) && (message.version as number) > 0) {
            versions.messages[id] = { fingerprint: message.fingerprint, version: message.version as number };
            versions.next = Math.max(versions.next, message.version as number);
          }
        }
        this.state.versions[sessionId] = versions;
      }
    }
  }

  serialize(): string { return JSON.stringify(this.state); }
  savedPromptQueues(): SavedPromptQueue[] { return this.state.promptQueues; }
  savePromptQueues(state: PromptQueueState, retainedSessionIds: ReadonlySet<string>, attachments: readonly Attachment[]): void {
    this.state.promptQueues = [
      ...this.state.promptQueues.filter((queue) => retainedSessionIds.has(queue.agentSessionId)),
      ...snapshotPromptQueues(state, attachments).filter((queue) => !retainedSessionIds.has(queue.agentSessionId)),
    ];
  }
  serverId(guiId: string): string { return this.state.aliases[guiId] ?? guiId; }
  guiId(serverId: string): string {
    return Object.entries(this.state.aliases).find(([, id]) => id === serverId)?.[0] ?? serverId;
  }
  bind(guiId: string, serverId: string): void {
    const existing = this.state.aliases[guiId];
    if (existing && existing !== serverId) throw new Error("Agent GUI session identity changed");
    const existingGui = this.guiId(serverId);
    if (existingGui !== serverId && existingGui !== guiId) throw new Error("Agent GUI session identity collision");
    this.state.aliases[guiId] = serverId;
    delete this.state.creations[guiId];
  }
  creation(id: string): "pending" | "unknown" | undefined { return this.state.creations[id]; }
  beginCreation(id: string): void { this.state.creations[id] = "pending"; }
  failCreation(id: string): void { this.state.creations[id] = "unknown"; }
  discardCreation(id: string): void { delete this.state.creations[id]; }
  submission(id: string): SubmissionRecord | undefined { return this.state.submissions[id]; }
  setSubmission(id: string, value: SubmissionRecord): void { this.state.submissions[id] = value; }
  discardSubmission(id: string): void { delete this.state.submissions[id]; }
  clientSubmitId(messageId: string): string | undefined {
    return Object.entries(this.state.submissions)
      .find(([, entry]) => entry.response?.message_id === messageId)?.[0];
  }
  version(sessionId: string, messageId: string, content: unknown): number {
    const state = this.state.versions[sessionId] ??= { next: 0, messages: {} };
    const fingerprint = hash(JSON.stringify(content));
    const previous = state.messages[messageId];
    if (previous?.fingerprint === fingerprint) return previous.version;
    const version = ++state.next;
    state.messages[messageId] = { fingerprint, version };
    return version;
  }
  latestVersion(sessionId: string): number { return this.state.versions[sessionId]?.next ?? 0; }
  project(sessionId: string, messages: Omit<AgentActivityDurableMessage, "version" | "sequence">[], turnStarts: ReadonlyMap<string, number> = new Map(), paged = false): AgentActivityDurableMessage[] {
    const anchors = new Map([...turnStarts].filter(([, start]) => Number.isFinite(start) && start > 0));
    const promptTurns = new Set<string>();
    for (const message of messages) {
      if (message.role !== "user" || !message.turnId || promptTurns.has(message.turnId)) continue;
      anchors.set(message.turnId, message.occurredAtUnixMs);
      promptTurns.add(message.turnId);
    }
    const fallbackAnchors = new Map<string, number>();
    for (const message of messages) if (message.turnId && !anchors.has(message.turnId)) {
      fallbackAnchors.set(message.turnId, Math.min(fallbackAnchors.get(message.turnId) ?? Infinity, message.occurredAtUnixMs));
    }
    for (const [turnId, at] of fallbackAnchors) anchors.set(turnId, at);
    const anchor = (message: typeof messages[number]) => message.turnId ? anchors.get(message.turnId) ?? message.occurredAtUnixMs : message.occurredAtUnixMs;
    // A late/backfilled event must not move its Turn ahead of the prompt that
    // started it, or split a Turn into duplicate virtual-list groups.
    const turnOrder = new Map<string, number>();
    for (const message of messages) if (message.turnId && !turnOrder.has(message.turnId)) turnOrder.set(message.turnId, turnOrder.size);
    return [...messages].sort((a, b) => {
      if (a.turnId && a.turnId === b.turnId) {
        return Number(b.role === "user") - Number(a.role === "user") || a.occurredAtUnixMs - b.occurredAtUnixMs;
      }
      return anchor(a) - anchor(b) || (a.turnId && b.turnId ? turnOrder.get(a.turnId)! - turnOrder.get(b.turnId)! : 0);
    }).map((message, index) => {
      const positioned = { ...message, sequence: index + 1 };
      // Older turns paged in move every message without changing any, and a full
      // snapshot replaces what readers hold, so position is not a new version there.
      return { ...positioned, version: this.version(sessionId, message.messageId, paged ? message : positioned) };
    });
  }
}

function record(value: unknown): value is Record<string, unknown> {
  return Boolean(value) && typeof value === "object" && !Array.isArray(value);
}

function hash(value: string): string {
  let result = 2166136261;
  for (let index = 0; index < value.length; index++) {
    result ^= value.charCodeAt(index);
    result = Math.imul(result, 16777619);
  }
  return `${value.length}:${(result >>> 0).toString(16)}`;
}
