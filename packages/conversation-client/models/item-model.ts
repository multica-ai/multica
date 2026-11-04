import { action, makeObservable, observable } from "mobx";
import type { LocalV2Event } from "@multica/core/local-conversation/v2";
import { text } from "./values";

/** One output block. Deltas append to its text fields; a final or updated state replaces it. */
export class ItemModel {
  readonly id: string;
  readonly turnId: string;
  /** Sequence of the item's first event: the item's place in the turn. */
  readonly order: number;
  readonly startedAt: string;
  /** Sequence of the last event applied: an older event, such as one in a late history page, is skipped. */
  lastSeq = 0;
  updatedAt: string;

  kind: string;
  status = "in_progress";
  /** The streamed fields, kept apart from state so a delta does not copy the rest of it. */
  text = "";
  output = "";
  /** Everything else the provider reported. Replaced whole, so no old field outlives an update. */
  state: Record<string, unknown> = {};

  constructor(event: LocalV2Event, id: string, turnId: string) {
    this.id = id;
    this.turnId = turnId;
    this.order = event.first_seq ?? event.seq;
    this.startedAt = event.first_at ?? event.created_at;
    this.updatedAt = event.created_at;
    this.kind = eventKind(event);
    makeObservable(this, {
      updatedAt: observable,
      kind: observable,
      status: observable,
      text: observable,
      output: observable,
      state: observable.ref,
      applyEvent: action,
    });
  }

  applyEvent(event: LocalV2Event): void {
    // An item's events can arrive twice: a live stream and a page read while it ran.
    if (event.seq <= this.lastSeq) return;
    this.lastSeq = event.seq;
    const kind = eventKind(event);
    // Its first event may have been a delta, which names no kind.
    if (!this.kind && kind) this.kind = kind;
    this.updatedAt = event.created_at;
    const { text: fullText, output, ...rest } = event.payload;
    if (event.type === "item.started") {
      this.state = { ...rest, ...this.state };
      if (!this.text) this.text = text(fullText);
      if (!this.output) this.output = text(output);
    } else if (event.type === "item.delta") {
      const field = text(event.payload.field);
      if (field === "text") this.text += text(fullText);
      else if (field === "output") this.output += text(fullText);
    } else {
      this.state = rest;
      this.text = text(fullText);
      this.output = text(output);
      this.status = text(event.payload.status) || this.status;
    }
  }
}

function eventKind(event: LocalV2Event): string {
  return event.payload.kind === "subagent" ? text(event.payload.item_kind) : text(event.payload.kind);
}
