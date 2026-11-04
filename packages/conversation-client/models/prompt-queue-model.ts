import { action, makeObservable, observable } from "mobx";
import type { LocalV2Queue } from "@multica/core/local-conversation/v2";

/** The queue as far as known: its state follows events, its items are read when they fall behind. */
export class PromptQueueModel {
  /** The revision of items; behind revision when they need reading. */
  itemsRevision: number;

  revision: number;
  paused: boolean;
  count: number | undefined = undefined;
  error: string | undefined;
  dispatchingRequestId: string | undefined;
  items: LocalV2Queue["items"];

  constructor(queue: LocalV2Queue) {
    this.itemsRevision = queue.revision;
    this.revision = queue.revision;
    this.paused = queue.paused;
    this.error = queue.error;
    this.dispatchingRequestId = queue.dispatching_request_id;
    this.items = queue.items;
    makeObservable(this, {
      revision: observable,
      paused: observable,
      count: observable,
      error: observable,
      dispatchingRequestId: observable,
      items: observable.ref,
      replace: action,
      receive: action,
      applyUpdate: action,
      clearDispatching: action,
    });
  }

  /** Whether a queue change arrived whose items have not been read. */
  itemsStale(): boolean { return this.revision > this.itemsRevision; }

  /** Takes a snapshot's queue, whatever came before it. */
  replace(queue: LocalV2Queue): void {
    this.itemsRevision = queue.revision;
    this.revision = queue.revision;
    this.paused = queue.paused;
    this.count = undefined;
    this.error = queue.error;
    this.dispatchingRequestId = queue.dispatching_request_id;
    this.items = queue.items;
  }

  /** The queue as read from the daemon; an older answer than what is known is ignored. */
  receive(queue: LocalV2Queue): void {
    if (queue.revision < this.itemsRevision) return;
    if (queue.revision >= this.revision) this.replace(queue);
    else {
      this.itemsRevision = queue.revision;
      this.items = queue.items;
    }
  }

  /** A queue.updated event. Replaced whole, so a cleared error is absent; the items come from the queue route. */
  applyUpdate(payload: Record<string, unknown>): void {
    const revision = typeof payload.revision === "number" ? payload.revision : this.revision;
    if (revision < this.revision) return;
    this.revision = revision;
    this.paused = payload.paused === true;
    this.count = typeof payload.count === "number" ? payload.count : undefined;
    this.error = typeof payload.error === "string" ? payload.error : undefined;
    this.dispatchingRequestId = undefined;
  }

  /** The dispatch this flag announced has become a turn; no later event clears it. */
  clearDispatching(): void { this.dispatchingRequestId = undefined; }

  /** The wire shape, for code that still takes it. */
  toQueue(): LocalV2Queue {
    return {
      revision: this.revision, paused: this.paused, items: this.items,
      ...(this.error !== undefined ? { error: this.error } : {}),
      ...(this.dispatchingRequestId !== undefined ? { dispatching_request_id: this.dispatchingRequestId } : {}),
    };
  }
}
