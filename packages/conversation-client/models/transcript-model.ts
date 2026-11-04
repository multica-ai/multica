import { action, makeObservable, observable } from "mobx";
import "../mobx-config";

const NO_MESSAGES: readonly never[] = Object.freeze([]);

/**
 * Each conversation's committed messages. A commit replaces the list by reference,
 * and the messages inside keep their objects when they did not change, so whatever
 * is built from them per object can be kept.
 */
export class TranscriptModel<Message> {
  private readonly committed = observable.map<string, readonly Message[]>([], { deep: false });

  constructor() {
    makeObservable(this, { commit: action });
  }

  messages(conversationId: string): readonly Message[] {
    return this.committed.get(conversationId) ?? NO_MESSAGES;
  }

  /** A commit that changed nothing keeps the list, so nothing downstream runs again. */
  commit(conversationId: string, messages: readonly Message[]): void {
    const current = this.committed.get(conversationId);
    if (current && current.length === messages.length && current.every((message, index) => message === messages[index])) return;
    this.committed.set(conversationId, messages);
  }
}
