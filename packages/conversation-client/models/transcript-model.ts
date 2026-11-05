import {
action,
comparer,
makeObservable,
observable,
type ObservableMap
} from "mobx";
import "../mobx-config";

const NO_MESSAGES: readonly never[] = Object.freeze([]);

/**
 * Each conversation's committed messages. A commit replaces the list by reference,
 * and the messages inside keep their objects when they did not change, so whatever
 * is built from them per object can be kept.
 */
export class TranscriptModel<Message> {
  private readonly revisions = observable.map<string, number>();
  private readonly committed = observable.map<string, readonly Message[]>([], {
    deep: false
  });
  private readonly turns = observable.map<
    string,
    ObservableMap<string, readonly Message[]>
  >([], { deep: false });

  private readonly turnOrders = observable.map<string, readonly string[]>([], {
    deep: false
  });

  constructor() {
    makeObservable(this, { commit: action });
  }

  revision(conversationId: string): number {
    return this.revisions.get(conversationId) ?? 0;
  }

  messages(conversationId: string): readonly Message[] {
    return this.committed.get(conversationId) ?? NO_MESSAGES;
  }

  /** Reading one turn never subscribes to the full transcript. */
  turnMessages(conversationId: string, turnId: string): readonly Message[] {
    return this.turns.get(conversationId)?.get(turnId) ?? NO_MESSAGES;
  }

  turnIds(conversationId: string): readonly string[] {
    return this.turnOrders.get(conversationId) ?? NO_MESSAGES;
  }

  conversationIds(): readonly string[] {
    return [...this.committed.keys()];
  }

  /** A commit that changed nothing keeps the list, so nothing downstream runs again. */
  commit(conversationId: string, messages: readonly Message[]): void {
    this.revisions.set(conversationId, this.revision(conversationId) + 1);
    const current = this.committed.get(conversationId);
    if (
      current &&
      current.length === messages.length &&
      current.every((message, index) => message === messages[index])
    )
      return;
    this.committed.set(conversationId, messages);
    let turns = this.turns.get(conversationId);
    if (!turns) {
      turns = observable.map<string, readonly Message[]>([], { deep: false });
      this.turns.set(conversationId, turns);
    }
    const grouped = new Map<string, Message[]>();
    for (const message of messages) {
      if (!message || typeof message !== "object") continue;
      const turnId =
        "turnId" in message && typeof message.turnId === "string"
          ? message.turnId
          : "";
      const group = grouped.get(turnId) ?? [];
      group.push(message);
      grouped.set(turnId, group);
    }
    const order = [...grouped.keys()];
    if (!comparer.shallow(this.turnOrders.get(conversationId), order))
      this.turnOrders.set(conversationId, order);
    for (const turnId of turns.keys())
      if (!grouped.has(turnId)) turns.delete(turnId);
    for (const [turnId, group] of grouped) {
      const previous = turns.get(turnId);
      if (
        !previous ||
        previous.length !== group.length ||
        group.some((message, index) => message !== previous[index])
      ) {
        turns.set(turnId, group);
      }
    }
  }
}
