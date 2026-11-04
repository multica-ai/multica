import { action, makeObservable, observable } from "mobx";
import "../mobx-config";
import type { LocalV2Event, LocalV2History, LocalV2Snapshot, LocalV2SnapshotTurn, LocalV2Summary, LocalV2Queue } from "@multica/core/local-conversation/v2";
import type { ConversationLocale, ConversationMessage } from "./conversation-message";
import { InteractionModel } from "./interaction-model";
import { ItemModel } from "./item-model";
import { PromptQueueModel } from "./prompt-queue-model";
import { TurnModel, type TurnError, type TurnStatus } from "./turn-model";
import { UserMessageModel } from "./user-message-model";
import { record, text } from "./values";

export type ConversationApplyResult = "applied" | "gap";

/** Turn errors that mean the turn was cut off rather than failed by itself. */
const INTERRUPTED_TURN_ERRORS = new Set(["daemon_restarted", "provider_exited", "interrupt_unconfirmed"]);

/**
 * Client state of one conversation: a snapshot plus the events after it, and
 * any older turns paged in. Events at or below the cursor are duplicates and are
 * skipped; an event past the next sequence means some were missed, and the caller
 * resubscribes. Paging never moves the cursor.
 *
 * One instance lives as long as the conversation is kept. A new snapshot resets it
 * in place, because views hold on to it; the turns and items inside are new.
 */
export class ConversationModel {
  readonly turns = new Map<string, TurnModel>();
  readonly items = new Map<string, ItemModel>();
  readonly messages = new Map<string, UserMessageModel>();
  readonly interactions = new Map<string, InteractionModel>();
  settings: Record<string, unknown> = {};
  usage: Record<string, unknown> | null = null;
  summary: LocalV2Summary;
  hasOlderTurns = false;
  readonly queue: PromptQueueModel;

  seq = 0;
  storeGeneration = "";
  createdAt = "";
  forkedFromId = "";
  forkedFromTurnId = "";
  importedFromSessionId = "";

  private constructor(snapshot: LocalV2Snapshot) {
    this.summary = snapshot.conversation;
    this.queue = new PromptQueueModel(snapshot.queue);
    makeObservable(this, {
      turns: observable.shallow,
      items: observable.shallow,
      messages: observable.shallow,
      interactions: observable.shallow,
      settings: observable.ref,
      usage: observable.ref,
      summary: observable.ref,
      hasOlderTurns: observable,
      replaceSnapshot: action,
      setSummary: action,
      applyEvents: action,
      mergeHistory: action,
      receiveQueue: action,
    });
    this.load(snapshot);
  }

  static fromSnapshot(snapshot: LocalV2Snapshot): ConversationModel {
    return new ConversationModel(snapshot);
  }

  /** Becomes the snapshot, keeping older turns paged in when it continues them without a hole. */
  replaceSnapshot(snapshot: LocalV2Snapshot): void {
    const sameStore = snapshot.store_generation === this.storeGeneration;
    const previous = {
      turns: [...this.turns.values()], items: [...this.items.values()],
      messages: [...this.messages.values()], interactions: [...this.interactions.values()],
    };
    this.turns.clear();
    this.items.clear();
    this.messages.clear();
    this.interactions.clear();
    this.usage = null;
    this.load(snapshot);
    this.queue.replace(snapshot.queue);
    if (sameStore) this.retainHistory(previous);
  }

  private load(snapshot: LocalV2Snapshot): void {
    this.storeGeneration = snapshot.store_generation;
    this.summary = snapshot.conversation;
    this.hasOlderTurns = snapshot.has_older_turns;
    this.settings = { ...snapshot.settings };
    this.createdAt = snapshot.created_at;
    this.forkedFromId = snapshot.forked_from_id;
    this.forkedFromTurnId = snapshot.forked_from_turn_id;
    this.importedFromSessionId = snapshot.imported_from_session_id;
    this.seq = 0;
    for (const turn of snapshot.turns) this.addTurn(turn);
    for (const event of snapshot.open_background_items) this.applyEvent(event);
    for (const pending of snapshot.pending_interactions) {
      if (!this.interactions.has(pending.interaction_id)) this.interactions.set(pending.interaction_id, new InteractionModel({
        id: pending.interaction_id, turnId: pending.turn_id, kind: pending.kind, request: pending.request, createdAt: pending.created_at,
      }));
    }
    this.seq = snapshot.snapshot_seq;
  }

  /**
   * A kept turn with an unfinished item would stay unfinished forever, since the
   * snapshot moved the cursor past its end, so it and everything older are dropped
   * and read again when needed.
   */
  private retainHistory(previous: { turns: TurnModel[]; items: ItemModel[]; messages: UserMessageModel[]; interactions: InteractionModel[] }): void {
    const oldest = this.oldestTurnIndex();
    if (oldest === null) return;
    const older = previous.turns.filter((turn) => turn.index < oldest).sort((a, b) => b.index - a.index);
    if (older[0]?.index !== oldest - 1) return;
    const kept: TurnModel[] = [];
    for (let i = 0; i < older.length; i++) {
      const turn = older[i]!;
      if (turn.index !== oldest - 1 - i) break;
      if (previous.items.some((item) => item.turnId === turn.id && item.status === "in_progress")) break;
      kept.push(turn);
    }
    if (!kept.length) return;
    const ids = new Set(kept.map((turn) => turn.id));
    for (const turn of kept) this.turns.set(turn.id, turn);
    for (const item of previous.items) if (ids.has(item.turnId)) this.items.set(item.id, item);
    for (const message of previous.messages) if (message.turnId && ids.has(message.turnId)) this.messages.set(message.id, message);
    for (const interaction of previous.interactions) if (ids.has(interaction.turnId) && !this.interactions.has(interaction.id)) this.interactions.set(interaction.id, interaction);
    this.hasOlderTurns = kept.at(-1)!.index > 0;
    this.sortTurns();
  }

  setSummary(summary: LocalV2Summary): void { this.summary = summary; }

  /** The lowest turn index loaded, the cursor for reading older turns; null with no turns. */
  oldestTurnIndex(): number | null {
    let oldest: number | null = null;
    for (const turn of this.turns.values()) if (oldest === null || turn.index < oldest) oldest = turn.index;
    return oldest;
  }

  /** Adds a page of older turns; events already applied, by a live stream, are skipped by item. */
  mergeHistory(history: LocalV2History): void {
    for (const turn of history.turns) this.addTurn(turn);
    this.hasOlderTurns = history.has_older_turns;
    this.sortTurns();
  }

  /** Whether a queue change arrived whose items have not been read. */
  queueItemsStale(): boolean { return this.queue.itemsStale(); }

  receiveQueue(queue: LocalV2Queue): void { this.queue.receive(queue); }

  private addTurn(turn: LocalV2SnapshotTurn): void {
    this.turns.set(turn.id, new TurnModel(this, {
      id: turn.id, index: turn.turn_index, requestId: turn.request_id, status: turnStatus(turn.status),
      startedAt: turn.started_at, completedAt: turn.completed_at ?? null, error: turn.error ?? null,
    }));
    for (const message of turn.messages) {
      // Assistant rows repeat the turn's assistant_text items.
      if (message.role !== "user") continue;
      this.messages.set(message.id, new UserMessageModel({
        id: message.id, requestId: "", turnId: turn.id, content: message.content, attachmentIds: message.attachment_ids,
        steered: message.steered, failed: message.failed, createdAt: message.created_at,
      }));
    }
    for (const event of turn.events) this.applyEvent(event);
  }

  private sortTurns(): void {
    const sorted = [...this.turns.values()].sort((a, b) => a.index - b.index);
    this.turns.clear();
    for (const turn of sorted) this.turns.set(turn.id, turn);
  }

  /**
   * Whether the latest turn was stopped by the user or cut off by a restart,
   * so the provider can continue it, and no turn is running.
   */
  continuable(): boolean {
    const turns = [...this.turns.values()];
    if (turns.some((turn) => turn.status === "running")) return false;
    const latest = turns.sort((a, b) => Date.parse(b.startedAt) - Date.parse(a.startedAt))[0];
    return latest?.status === "cancelled" || (latest?.status === "failed" && INTERRUPTED_TURN_ERRORS.has(latest.error?.kind ?? ""));
  }

  private nextTurnIndex(): number {
    let next = 0;
    for (const turn of this.turns.values()) next = Math.max(next, turn.index + 1);
    return next;
  }

  applyEvents(events: readonly LocalV2Event[]): ConversationApplyResult {
    for (const event of events) {
      if (event.seq <= this.seq) continue;
      if (event.seq !== this.seq + 1) return "gap";
      this.applyEvent(event);
      this.seq = event.seq;
    }
    return "applied";
  }

  /** Items of one turn in the order they started. */
  turnItems(turnId: string): ItemModel[] {
    return [...this.items.values()].filter((item) => item.turnId === turnId).sort((a, b) => a.order - b.order);
  }

  /** The transcript's rows: user messages, then each turn's output, then questions. */
  transcript(locale: ConversationLocale): ConversationMessage[] {
    const rows: ConversationMessage[] = [];
    for (const message of this.messages.values()) rows.push(message.toMessage());
    for (const turn of this.turns.values()) rows.push(...turn.assistantMessages(locale));
    for (const interaction of this.interactions.values()) if (interaction.kind === "question") rows.push(interaction.toMessage());
    return rows;
  }

  private applyEvent(event: LocalV2Event): void {
    const payload = event.payload;
    switch (event.type) {
      case "user.message": {
        const id = text(payload.message_id);
        if (!id) return;
        this.messages.set(id, new UserMessageModel({
          id, requestId: text(payload.request_id), turnId: event.turn_id, content: text(payload.content),
          attachmentIds: Array.isArray(payload.attachment_ids) ? payload.attachment_ids.filter((value): value is string => typeof value === "string") : [],
          steered: payload.steered === true, failed: false, createdAt: event.created_at,
        }));
        return;
      }
      case "turn.started":
        if (!event.turn_id) return;
        this.queue.clearDispatching();
        this.turns.set(event.turn_id, new TurnModel(this, {
          id: event.turn_id, requestId: text(payload.request_id), status: "running",
          index: typeof payload.turn_index === "number" ? payload.turn_index : this.nextTurnIndex(),
          startedAt: event.created_at, completedAt: null, error: null,
        }));
        return;
      case "turn.completed":
        if (event.turn_id) this.turns.get(event.turn_id)?.complete(turnStatus(text(payload.status)), event.created_at, errorValue(payload.error));
        return;
      case "item.started":
      case "item.delta":
      case "item.updated":
      case "item.completed": {
        if (!event.item_id || !event.turn_id) return;
        let item = this.items.get(event.item_id);
        if (!item) {
          item = new ItemModel(event, event.item_id, event.turn_id);
          this.items.set(item.id, item);
        }
        item.applyEvent(event);
        return;
      }
      case "interaction.requested": {
        const id = text(payload.interaction_id);
        if (!id || !event.turn_id) return;
        this.interactions.set(id, new InteractionModel({ id, turnId: event.turn_id, kind: text(payload.kind), request: record(payload.request) ?? {}, createdAt: event.created_at }));
        return;
      }
      case "interaction.resolved":
        this.interactions.get(text(payload.interaction_id))?.resolve(payload.status === "answered" ? "answered" : "cancelled", record(payload.answer), event.created_at);
        return;
      case "conversation.updated":
        this.settings = { ...this.settings, ...payload };
        return;
      case "usage.updated":
        this.usage = payload;
        return;
      case "queue.updated":
        this.queue.applyUpdate(payload);
        return;
    }
  }
}

function turnStatus(value: string): TurnStatus {
  return value === "completed" || value === "failed" || value === "cancelled" ? value : "running";
}

function errorValue(value: unknown): TurnError | null {
  const error = record(value);
  return error ? { kind: text(error.kind), message: text(error.message) } : null;
}
