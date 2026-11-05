import {
  localSubmissionEventSchema,
  mergeSubmission,
  type LocalSubmission,
} from "@multica/core/local-conversation/submission";
import { action, makeObservable, observable } from "mobx";
import "../mobx-config";
import type { LocalV2Event, LocalV2History, LocalV2Snapshot, LocalV2SnapshotTurn, LocalV2Summary, LocalV2Queue } from "@multica/core/local-conversation/v2";
import type { ConversationMessage } from "./conversation-message";
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
  readonly submissions = new Map<string, LocalSubmission>();
  readonly turns = new Map<string, TurnModel>();
  readonly items = new Map<string, ItemModel>();
  readonly messages = new Map<string, UserMessageModel>();
  readonly interactions = new Map<string, InteractionModel>();
  settings: Record<string, unknown> = {};
  usage: Record<string, unknown> | null = null;
  summary: LocalV2Summary;
  hasOlderTurns = false;
  historyStartIndex = 0;
  readonly queue: PromptQueueModel;

  /** Per-turn lookups, so reading a turn costs that turn rather than the whole conversation. */
  private readonly itemsByTurn = new Map<string, ItemModel[]>();
  private readonly messagesByTurn = new Map<string | null, UserMessageModel[]>();
  private readonly interactionsByTurn = new Map<string, InteractionModel[]>();
  private readonly navigationTurnIds = new Set<string>();
  /** An unloaded turn can finish before its earlier body read reaches this client. */
  private readonly completedTurns = new Map<string, { status: TurnStatus; at: string; error: TurnError | null }>();

  seq = 0;
  storeGeneration = "";
  createdAt = "";
  forkedFromId = "";
  forkedFromTurnId = "";
  importedFromSessionId = "";

  private constructor(snapshot: LocalV2Snapshot) {
    this.summary = snapshot.conversation;
    this.queue = new PromptQueueModel(snapshot.queue);
    makeObservable<this, "load">(this, {
      submissions: observable.shallow,
      mergeSubmissions: action,
      turns: observable.shallow,
      items: observable.shallow,
      messages: observable.shallow,
      interactions: observable.shallow,
      settings: observable.ref,
      usage: observable.ref,
      summary: observable.ref,
      hasOlderTurns: observable,
      historyStartIndex: observable,
      mergeNavigation: action,
      replaceSnapshot: action,
      setSummary: action,
      applyEvents: action,
      mergeHistory: action,
      receiveQueue: action,
      load: action,
    });
    this.load(snapshot);
  }

  static fromSnapshot(snapshot: LocalV2Snapshot): ConversationModel {
    return new ConversationModel(snapshot);
  }

  /**
   * Becomes the snapshot, keeping older turns paged in when it continues them
   * without a hole. A snapshot older than the events applied here says nothing
   * new, except for its queue, which the daemon reads after the rest. Sequences
   * compare only within one store. `authoritative` is for when the daemon has said
   * the cursor is ahead of its store, so a smaller sequence is the truth.
   *
   * Returns whether the snapshot was taken.
   */
  replaceSnapshot(snapshot: LocalV2Snapshot, options: { authoritative?: boolean } = {}): boolean {
    const sameStore = snapshot.store_generation === this.storeGeneration;
    if (sameStore && !options.authoritative && snapshot.snapshot_seq < this.seq) {
      this.queue.receive(snapshot.queue);
      return false;
    }
    if (!sameStore || options.authoritative) {
      this.completedTurns.clear();
      this.submissions.clear();
    }
    const previous = {
      turns: [...this.turns.values()], items: [...this.items.values()],
      messages: [...this.messages.values()], interactions: [...this.interactions.values()],
    };
    this.turns.clear();
    this.items.clear();
    this.messages.clear();
    this.interactions.clear();
    this.itemsByTurn.clear();
    this.messagesByTurn.clear();
    this.interactionsByTurn.clear();
    this.usage = null;
    this.load(snapshot);
    this.queue.replace(snapshot.queue);
    if (sameStore) {
      this.retainHistory(previous);
      const kept = previous.turns.filter((turn) => this.navigationTurnIds.has(turn.id) && !this.turns.has(turn.id) && turn.status !== "running" && !previous.items.some((item) => item.turnId === turn.id && item.status === "in_progress"));
      const ids = new Set(kept.map((turn) => turn.id));
      for (const turn of kept) this.turns.set(turn.id, turn);
      for (const item of previous.items) if (ids.has(item.turnId)) this.setItem(item);
      for (const message of previous.messages) if (ids.has(message.turnId ?? "")) this.setMessage(message);
      for (const interaction of previous.interactions) if (ids.has(interaction.turnId)) this.setInteraction(interaction);
      this.sortTurns();
    } else this.navigationTurnIds.clear();
    return true;
  }

  private load(snapshot: LocalV2Snapshot): void {
    this.storeGeneration = snapshot.store_generation;
    this.summary = snapshot.conversation;
    this.hasOlderTurns = snapshot.has_older_turns;
    this.historyStartIndex = snapshot.turns[0]?.turn_index ?? 0;
    this.mergeSubmissions(snapshot.submissions);
    this.settings = { ...snapshot.settings };
    this.createdAt = snapshot.created_at;
    this.forkedFromId = snapshot.forked_from_id;
    this.forkedFromTurnId = snapshot.forked_from_turn_id;
    this.importedFromSessionId = snapshot.imported_from_session_id;
    this.seq = 0;
    for (const turn of snapshot.turns) this.addTurn(turn);
    for (const event of snapshot.open_background_items) this.applyEvent(event);
    for (const pending of snapshot.pending_interactions) {
      if (!this.interactions.has(pending.interaction_id)) this.setInteraction(new InteractionModel({
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
    for (const item of previous.items) if (ids.has(item.turnId)) this.setItem(item);
    for (const message of previous.messages) if (message.turnId && ids.has(message.turnId)) this.setMessage(message);
    for (const interaction of previous.interactions) if (ids.has(interaction.turnId) && !this.interactions.has(interaction.id)) this.setInteraction(interaction);
    this.hasOlderTurns = kept.at(-1)!.index > 0;
    this.historyStartIndex = kept.at(-1)!.index;
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
    if (history.turns.length) this.historyStartIndex = Math.min(this.historyStartIndex, history.turns[0]!.turn_index);
    this.sortTurns();
  }

  /** Navigation can cache disconnected windows without advancing the contiguous history cursor. */
  mergeNavigation(turns: readonly LocalV2SnapshotTurn[]): void {
    for (const turn of turns) {
      this.navigationTurnIds.add(turn.id);
      if (!this.turns.has(turn.id)) this.addTurn(turn);
    }
    this.sortTurns();
  }

  /** Whether a queue change arrived whose items have not been read. */
  queueItemsStale(): boolean { return this.queue.itemsStale(); }

  mergeSubmissions(values: readonly LocalSubmission[]): void {
    for (const value of values)
      this.submissions.set(
        value.request_id,
        mergeSubmission(this.submissions.get(value.request_id), value),
      );
  }

  receiveQueue(queue: LocalV2Queue): void {
    this.queue.receive(queue);
  }

  private addTurn(turn: LocalV2SnapshotTurn): void {
    const completion = this.completedTurns.get(turn.id);
    this.turns.set(turn.id, new TurnModel(this, {
      id: turn.id, index: turn.turn_index, requestId: turn.request_id, status: completion?.status ?? turnStatus(turn.status),
      startedAt: turn.started_at, completedAt: completion?.at ?? turn.completed_at ?? null, error: completion ? completion.error : turn.error ?? null,
    }));
    for (const message of turn.messages) {
      // Assistant rows repeat the turn's assistant_text items.
      if (message.role !== "user") continue;
      this.setMessage(new UserMessageModel({
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
  turnItems(turnId: string): readonly ItemModel[] { return this.itemsByTurn.get(turnId) ?? []; }

  /** User messages of one turn; null is those with no turn. */
  turnMessages(turnId: string | null): readonly UserMessageModel[] { return this.messagesByTurn.get(turnId) ?? []; }

  turnInteractions(turnId: string): readonly InteractionModel[] { return this.interactionsByTurn.get(turnId) ?? []; }

  /** Rows no loaded turn accounts for: a prompt before its turn starts, and questions of a turn not paged in. */
  looseMessages(): ConversationMessage[] {
    const rows: ConversationMessage[] = [];
    for (const [turnId, messages] of this.messagesByTurn) {
      if (turnId !== null && this.turns.has(turnId)) continue;
      for (const message of messages) rows.push(message.toMessage());
    }
    for (const [turnId, interactions] of this.interactionsByTurn) {
      if (this.turns.has(turnId)) continue;
      for (const interaction of interactions) if (interaction.kind === "question") rows.push(interaction.toMessage());
    }
    return rows;
  }

  private setItem(item: ItemModel): void {
    this.items.set(item.id, item);
    let list = this.itemsByTurn.get(item.turnId);
    if (!list) this.itemsByTurn.set(item.turnId, list = []);
    const at = list.findIndex((other) => other.id === item.id);
    if (at >= 0) { list[at] = item; return; }
    // Almost always the newest: an item arrives in the order it started, or a page arrives whole.
    let index = list.length;
    while (index > 0 && list[index - 1]!.order > item.order) index--;
    list.splice(index, 0, item);
  }

  private setMessage(message: UserMessageModel): void {
    this.messages.set(message.id, message);
    replaceInBucket(this.messagesByTurn, message.turnId, message);
  }

  private setInteraction(interaction: InteractionModel): void {
    this.interactions.set(interaction.id, interaction);
    replaceInBucket(this.interactionsByTurn, interaction.turnId, interaction);
  }

  private applyEvent(event: LocalV2Event): void {
    if (event.type === "submission.updated") {
      const parsed = localSubmissionEventSchema.safeParse(event.payload);
      if (parsed.success) this.mergeSubmissions([parsed.data.submission]);
    }

    const payload = event.payload;
    switch (event.type) {
      case "user.message": {
        const id = text(payload.message_id);
        if (!id) return;
        this.setMessage(new UserMessageModel({
          id, requestId: text(payload.submission_id)||text(payload.request_id), turnId: event.turn_id, content: text(payload.content),
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
        if (event.turn_id) {
          const completion = { status: turnStatus(text(payload.status)), at: event.created_at, error: errorValue(payload.error) };
          this.completedTurns.set(event.turn_id, completion);
          this.turns.get(event.turn_id)?.complete(completion.status, completion.at, completion.error);
        }
        return;
      case "item.started":
      case "item.delta":
      case "item.updated":
      case "item.completed": {
        if (!event.item_id || !event.turn_id) return;
        let item = this.items.get(event.item_id);
        if (!item) {
          item = new ItemModel(event, event.item_id, event.turn_id);
          this.setItem(item);
        }
        item.applyEvent(event);
        return;
      }
      case "interaction.requested": {
        const id = text(payload.interaction_id);
        if (!id || !event.turn_id) return;
        this.setInteraction(new InteractionModel({ id, turnId: event.turn_id, kind: text(payload.kind), request: record(payload.request) ?? {}, createdAt: event.created_at }));
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

function replaceInBucket<K, T extends { id: string }>(buckets: Map<K, T[]>, key: K, value: T): void {
  const list = buckets.get(key);
  if (!list) { buckets.set(key, [value]); return; }
  const at = list.findIndex((other) => other.id === value.id);
  if (at >= 0) list[at] = value;
  else list.push(value);
}

function turnStatus(value: string): TurnStatus {
  return value === "completed" || value === "failed" || value === "cancelled" ? value : "running";
}

function errorValue(value: unknown): TurnError | null {
  const error = record(value);
  return error ? { kind: text(error.kind), message: text(error.message) } : null;
}
