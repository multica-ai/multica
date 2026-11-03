import type { LocalV2Event, LocalV2Snapshot, LocalV2Summary } from "@multica/core/local-conversation/v2";

export type V2TurnStatus = "running" | "completed" | "failed" | "cancelled";

export interface V2Turn {
  id: string;
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
 * Client state of one conversation: a snapshot plus the events after it.
 * Events at or below the cursor are duplicates and are skipped; an event
 * past the next sequence means some were missed, and the caller resubscribes.
 */
export class V2Conversation {
  readonly turns = new Map<string, V2Turn>();
  readonly items = new Map<string, V2Item>();
  readonly messages = new Map<string, V2UserMessage>();
  readonly interactions = new Map<string, V2Interaction>();
  settings: Record<string, unknown> = {};
  usage: Record<string, unknown> | null = null;
  queue: Record<string, unknown> | null = null;

  private constructor(
    readonly storeGeneration: string,
    public summary: LocalV2Summary,
    public seq: number,
    readonly hasOlderTurns: boolean,
  ) {}

  static fromSnapshot(snapshot: LocalV2Snapshot): V2Conversation {
    const conversation = new V2Conversation(snapshot.store_generation, snapshot.conversation, 0, snapshot.has_older_turns);
    for (const turn of snapshot.turns) {
      conversation.turns.set(turn.id, {
        id: turn.id, requestId: turn.request_id, status: turnStatus(turn.status),
        startedAt: turn.started_at, completedAt: turn.completed_at ?? null, error: turn.error ?? null,
      });
      for (const message of turn.messages) {
        // Assistant rows repeat the turn's assistant_text items.
        if (message.role !== "user") continue;
        conversation.messages.set(message.id, {
          id: message.id, requestId: "", turnId: turn.id, content: message.content, attachmentIds: message.attachment_ids,
          steered: message.steered, failed: message.failed, createdAt: message.created_at,
        });
      }
      for (const event of turn.events) conversation.applyEvent(event);
    }
    for (const pending of snapshot.pending_interactions) {
      if (!conversation.interactions.has(pending.interaction_id)) conversation.interactions.set(pending.interaction_id, {
        id: pending.interaction_id, turnId: pending.turn_id, kind: pending.kind, request: pending.request,
        status: "pending", answer: null, createdAt: pending.created_at, updatedAt: pending.created_at,
      });
    }
    conversation.seq = snapshot.snapshot_seq;
    return conversation;
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
        this.turns.set(event.turn_id, {
          id: event.turn_id, requestId: text(payload.request_id), status: "running",
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
      case "queue.updated":
        this.queue = payload;
        return;
    }
  }

  private applyItem(event: LocalV2Event): void {
    if (!event.item_id || !event.turn_id) return;
    const payload = event.payload;
    let item = this.items.get(event.item_id);
    if (!item) {
      item = {
        id: event.item_id, turnId: event.turn_id, order: event.seq, kind: payload.kind === "subagent" ? text(payload.item_kind) : text(payload.kind),
        status: "in_progress", state: {}, startedAt: event.created_at, updatedAt: event.created_at,
      };
      this.items.set(item.id, item);
    }
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
