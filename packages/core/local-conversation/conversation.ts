import type { LocalV2Event, LocalV2History, LocalV2Queue, LocalV2Snapshot, LocalV2SnapshotTurn, LocalV2Summary } from "./v2";

export type V2TurnStatus = "running" | "completed" | "failed" | "cancelled";

export interface V2Turn {
  id: string;
  index: number;
  requestId: string;
  status: V2TurnStatus;
  startedAt: string;
  completedAt: string | null;
  error: { kind: string; message: string } | null;
}

/** One output block. Deltas append to its text fields; a final or updated state replaces it. */
export interface V2Item {
  id: string;
  turnId: string;
  /** Sequence of the item's first event: the item's place in the turn. */
  order: number;
  /** Sequence of the last event applied: an older event, such as one in a late history page, is skipped. */
  lastSeq: number;
  kind: string;
  status: string;
  state: Record<string, unknown>;
  startedAt: string;
  updatedAt: string;
}

export interface V2UserMessage {
  id: string;
  requestId: string;
  turnId: string | null;
  content: string;
  attachmentIds: string[];
  steered: boolean;
  failed: boolean;
  createdAt: string;
}

export interface V2Interaction {
  id: string;
  turnId: string;
  kind: string;
  request: Record<string, unknown>;
  status: "pending" | "answered" | "cancelled";
  answer: Record<string, unknown> | null;
  createdAt: string;
  updatedAt: string;
}

export type V2ApplyResult = "applied" | "gap";

/**
 * Client state of one conversation: a snapshot plus the events after it, and
 * any older turns paged in. Events at or below the cursor are duplicates and are
 * skipped; an event past the next sequence means some were missed, and the caller
 * resubscribes. Paging never moves the cursor.
 */
export class V2Conversation {
  readonly turns = new Map<string, V2Turn>();
  readonly items = new Map<string, V2Item>();
  readonly messages = new Map<string, V2UserMessage>();
  readonly interactions = new Map<string, V2Interaction>();
  settings: Record<string, unknown> = {};
  usage: Record<string, unknown> | null = null;
  /** The queue as far as known; items are as of itemsRevision. */
  queue: LocalV2Queue & { count?: number };
  /** The revision of queue.items; behind queue.revision when they need reading. */
  itemsRevision: number;
  readonly createdAt: string;
  readonly forkedFromId: string;
  readonly forkedFromTurnId: string;
  readonly importedFromSessionId: string;
  hasOlderTurns: boolean;

  private constructor(
    readonly storeGeneration: string,
    public summary: LocalV2Summary,
    public seq: number,
    snapshot: LocalV2Snapshot,
  ) {
    this.hasOlderTurns = snapshot.has_older_turns;
    this.settings = { ...snapshot.settings };
    this.queue = snapshot.queue;
    this.itemsRevision = snapshot.queue.revision;
    this.createdAt = snapshot.created_at;
    this.forkedFromId = snapshot.forked_from_id;
    this.forkedFromTurnId = snapshot.forked_from_turn_id;
    this.importedFromSessionId = snapshot.imported_from_session_id;
  }

  static fromSnapshot(snapshot: LocalV2Snapshot): V2Conversation {
    const conversation = new V2Conversation(snapshot.store_generation, snapshot.conversation, 0, snapshot);
    for (const turn of snapshot.turns) conversation.addTurn(turn);
    for (const event of snapshot.open_background_items) conversation.applyEvent(event);
    for (const pending of snapshot.pending_interactions) {
      if (!conversation.interactions.has(pending.interaction_id)) conversation.interactions.set(pending.interaction_id, {
        id: pending.interaction_id, turnId: pending.turn_id, kind: pending.kind, request: pending.request,
        status: "pending", answer: null, createdAt: pending.created_at, updatedAt: pending.created_at,
      });
    }
    conversation.seq = snapshot.snapshot_seq;
    return conversation;
  }

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

  /**
   * Keeps older turns this conversation paged in, when a new snapshot of the same
   * store continues them without a hole. A kept turn with an unfinished item would
   * stay unfinished forever, since the snapshot moved the cursor past its end, so
   * it and everything older are dropped and read again when needed.
   */
  retainHistoryFrom(previous: V2Conversation): void {
    if (previous.storeGeneration !== this.storeGeneration) return;
    const oldest = this.oldestTurnIndex();
    if (oldest === null) return;
    const older = [...previous.turns.values()].filter((turn) => turn.index < oldest).sort((a, b) => b.index - a.index);
    if (older[0]?.index !== oldest - 1) return;
    const kept: V2Turn[] = [];
    for (let i = 0; i < older.length; i++) {
      const turn = older[i]!;
      if (turn.index !== oldest - 1 - i) break;
      if (previous.turnItems(turn.id).some((item) => item.status === "in_progress")) break;
      kept.push(turn);
    }
    if (!kept.length) return;
    for (const turn of kept) {
      this.turns.set(turn.id, turn);
      for (const item of previous.turnItems(turn.id)) this.items.set(item.id, item);
      for (const message of previous.messages.values()) if (message.turnId === turn.id) this.messages.set(message.id, message);
      for (const interaction of previous.interactions.values()) if (interaction.turnId === turn.id && !this.interactions.has(interaction.id)) this.interactions.set(interaction.id, interaction);
    }
    this.hasOlderTurns = kept.at(-1)!.index > 0;
    this.sortTurns();
  }

  /** Whether a queue change arrived whose items have not been read. */
  queueItemsStale(): boolean { return this.queue.revision > this.itemsRevision; }

  /** The queue as read from the daemon; an older answer than what is known is ignored. */
  receiveQueue(queue: LocalV2Queue): void {
    if (queue.revision < this.itemsRevision) return;
    this.itemsRevision = queue.revision;
    if (queue.revision >= this.queue.revision) this.queue = queue;
    else this.queue = { ...this.queue, items: queue.items };
  }

  private addTurn(turn: LocalV2SnapshotTurn): void {
    this.turns.set(turn.id, {
      id: turn.id, index: turn.turn_index, requestId: turn.request_id, status: turnStatus(turn.status),
      startedAt: turn.started_at, completedAt: turn.completed_at ?? null, error: turn.error ?? null,
    });
    for (const message of turn.messages) {
      // Assistant rows repeat the turn's assistant_text items.
      if (message.role !== "user") continue;
      this.messages.set(message.id, {
        id: message.id, requestId: "", turnId: turn.id, content: message.content, attachmentIds: message.attachment_ids,
        steered: message.steered, failed: message.failed, createdAt: message.created_at,
      });
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

  apply(events: readonly LocalV2Event[]): V2ApplyResult {
    for (const event of events) {
      if (event.seq <= this.seq) continue;
      if (event.seq !== this.seq + 1) return "gap";
      this.applyEvent(event);
      this.seq = event.seq;
    }
    return "applied";
  }

  /** Items of one turn in the order they started. */
  turnItems(turnId: string): V2Item[] {
    return [...this.items.values()].filter((item) => item.turnId === turnId).sort((a, b) => a.order - b.order);
  }

  private applyEvent(event: LocalV2Event): void {
    const payload = event.payload;
    switch (event.type) {
      case "user.message": {
        const id = text(payload.message_id);
        if (!id) return;
        this.messages.set(id, {
          id, requestId: text(payload.request_id), turnId: event.turn_id, content: text(payload.content),
          attachmentIds: Array.isArray(payload.attachment_ids) ? payload.attachment_ids.filter((value): value is string => typeof value === "string") : [],
          steered: payload.steered === true, failed: false, createdAt: event.created_at,
        });
        return;
      }
      case "turn.started":
        if (!event.turn_id) return;
        // The dispatch this flag announced has become a turn; no later event clears it.
        if (this.queue.dispatching_request_id) this.queue = { ...this.queue, dispatching_request_id: undefined };
        this.turns.set(event.turn_id, {
          id: event.turn_id, requestId: text(payload.request_id), status: "running",
          index: typeof payload.turn_index === "number" ? payload.turn_index : this.nextTurnIndex(),
          startedAt: event.created_at, completedAt: null, error: null,
        });
        return;
      case "turn.completed": {
        const turn = event.turn_id ? this.turns.get(event.turn_id) : undefined;
        if (!turn) return;
        turn.status = turnStatus(text(payload.status));
        turn.completedAt = event.created_at;
        turn.error = errorValue(payload.error);
        return;
      }
      case "item.started":
      case "item.delta":
      case "item.updated":
      case "item.completed":
        this.applyItem(event);
        return;
      case "interaction.requested": {
        const id = text(payload.interaction_id);
        if (!id || !event.turn_id) return;
        this.interactions.set(id, {
          id, turnId: event.turn_id, kind: text(payload.kind), request: record(payload.request) ?? {},
          status: "pending", answer: null, createdAt: event.created_at, updatedAt: event.created_at,
        });
        return;
      }
      case "interaction.resolved": {
        const interaction = this.interactions.get(text(payload.interaction_id));
        if (!interaction) return;
        interaction.status = payload.status === "answered" ? "answered" : "cancelled";
        interaction.answer = record(payload.answer);
        interaction.updatedAt = event.created_at;
        return;
      }
      case "conversation.updated":
        this.settings = { ...this.settings, ...payload };
        return;
      case "usage.updated":
        this.usage = payload;
        return;
      case "queue.updated": {
        const revision = typeof payload.revision === "number" ? payload.revision : this.queue.revision;
        if (revision < this.queue.revision) return;
        // Replaced whole: a cleared error is absent. The items come from the queue route.
        this.queue = { revision, paused: payload.paused === true, count: typeof payload.count === "number" ? payload.count : undefined,
          error: typeof payload.error === "string" ? payload.error : undefined, items: this.queue.items };
        return;
      }
    }
  }

  private applyItem(event: LocalV2Event): void {
    if (!event.item_id || !event.turn_id) return;
    const payload = event.payload;
    const kind = payload.kind === "subagent" ? text(payload.item_kind) : text(payload.kind);
    let item = this.items.get(event.item_id);
    if (!item) {
      item = {
        id: event.item_id, turnId: event.turn_id, order: event.first_seq ?? event.seq, lastSeq: 0, kind,
        status: "in_progress", state: {}, startedAt: event.first_at ?? event.created_at, updatedAt: event.created_at,
      };
      this.items.set(item.id, item);
    }
    // An item's events can arrive twice: a live stream and a page read while it ran.
    if (event.seq <= item.lastSeq) return;
    item.lastSeq = event.seq;
    // Its first event may have been a delta, which names no kind.
    if (!item.kind && kind) item.kind = kind;
    item.updatedAt = event.created_at;
    if (event.type === "item.started") {
      item.state = { ...payload, ...item.state };
    } else if (event.type === "item.delta") {
      const field = text(payload.field);
      if (field === "text" || field === "output") item.state = { ...item.state, [field]: text(item.state[field]) + text(payload.text) };
    } else {
      item.state = { ...payload };
      item.status = text(payload.status) || item.status;
    }
  }
}

/** Turn errors that mean the turn was cut off rather than failed by itself. */
const INTERRUPTED_TURN_ERRORS = new Set(["daemon_restarted", "provider_exited", "interrupt_unconfirmed"]);

function turnStatus(value: string): V2TurnStatus {
  return value === "completed" || value === "failed" || value === "cancelled" ? value : "running";
}

function text(value: unknown): string {
  return typeof value === "string" ? value : "";
}

function record(value: unknown): Record<string, unknown> | null {
  return value && typeof value === "object" && !Array.isArray(value) ? value as Record<string, unknown> : null;
}

function errorValue(value: unknown): V2Turn["error"] {
  const error = record(value);
  return error ? { kind: text(error.kind), message: text(error.message) } : null;
}
