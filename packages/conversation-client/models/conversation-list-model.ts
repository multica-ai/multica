import { action, makeObservable, observable } from "mobx";
import type { LocalV2Device, LocalV2Summary } from "@multica/core/local-conversation/v2";

/** One workspace's conversations on one device: the complete list, and who else is using each. */
export class ConversationListModel {
  private readonly byId = observable.map<string, LocalV2Summary>([], { deep: false });
  private readonly devices = observable.map<string, LocalV2Device[]>([], { deep: false });
  /** Every conversation; a new array on each change. */
  summaries: LocalV2Summary[] = [];
  /** Whether the list has been read; after that it stays current on its own. */
  read = false;

  constructor() {
    makeObservable(this, {
      summaries: observable.ref,
      read: observable,
      upsert: action,
      markRead: action,
      setPresence: action,
      clearPresence: action,
      clear: action,
    });
  }

  summary(id: string): LocalV2Summary | undefined { return this.byId.get(id); }

  /** Other devices using a conversation now. */
  presence(id: string): LocalV2Device[] { return this.devices.get(id) ?? []; }

  /** Takes what is newer than what is known; an older summary arriving late never replaces a newer one. */
  upsert(summaries: readonly LocalV2Summary[]): LocalV2Summary[] {
    const taken = summaries.filter((summary) => {
      const current = this.byId.get(summary.conversation_id);
      if (current && current.summary_seq >= summary.summary_seq) return false;
      this.byId.set(summary.conversation_id, summary);
      return true;
    });
    if (taken.length) this.summaries = [...this.byId.values()];
    return taken;
  }

  markRead(): void { this.read = true; }

  setPresence(id: string, devices: LocalV2Device[]): void {
    if (devices.length) this.devices.set(id, devices);
    else this.devices.delete(id);
  }

  clearPresence(): void { this.devices.clear(); }

  /** The daemon's database was replaced: nothing here describes it any more. */
  clear(): void {
    this.byId.clear();
    this.summaries = [];
    this.read = false;
  }
}
