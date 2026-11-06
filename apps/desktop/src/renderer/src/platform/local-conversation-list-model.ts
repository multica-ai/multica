import type { ConversationListModel } from "@multica/conversation-client/models";
import { comparer, computed, makeObservable, type IComputedValue } from "mobx";
import { compareLocalConversations, sessionEntry, type LocalSessionEntry } from "./local-conversation-projects";

type Summary = Pick<LocalSessionEntry, "id" | "title" | "provider" | "pinned" | "preview" | "waitingForAnswer" | "running" | "backgroundTasks"> & { hasUnreadCompletion: boolean };
type Activity = Pick<Summary, "id" | "title" | "provider" | "waitingForAnswer" | "running" | "backgroundTasks"> & { hasUnreadCompletion: boolean };

/** List ordering and each visible row observe separate projections of the existing list model. */
export class LocalConversationListModel {
  private readonly entries = new Map<string, IComputedValue<LocalSessionEntry | undefined>>();
  private readonly summaries = new Map<string, IComputedValue<Summary | undefined>>();
  private readonly activities = new Map<string, IComputedValue<Activity | undefined>>();

  constructor(private readonly source: ConversationListModel) {
    makeObservable(this, { sessions: computed({ equals: comparer.shallow }) });
  }

  get sessions(): LocalSessionEntry[] {
    return this.source.summaries
      .filter((summary) => !summary.side_source_id && summary.turn_count > 0)
      .map((summary) => this.entry(summary.conversation_id)!)
      .sort(compareLocalConversations);
  }

  entry(id: string): LocalSessionEntry | undefined {
    return this.read(this.entries, id, () => {
      const summary = this.source.summary(id);
      return summary ? sessionEntry(summary) : undefined;
    });
  }

  summary(id: string): Summary | undefined {
    return this.read(this.summaries, id, () => {
      const entry = this.entry(id);
      if (!entry) return;
      const { title, provider, pinned, preview, waitingForAnswer, running, backgroundTasks } = entry;
      return { id, title, provider, pinned, preview, waitingForAnswer, running, backgroundTasks, hasUnreadCompletion: !!this.source.completions.pending(id) };
    });
  }

  activity(id: string): Activity | undefined {
    return this.read(this.activities, id, () => {
      const entry = this.entry(id);
      if (!entry) return;
      const { title, provider, waitingForAnswer, running, backgroundTasks } = entry;
      return { id, title, provider, waitingForAnswer, running, backgroundTasks, hasUnreadCompletion: !!this.source.completions.pending(id) };
    });
  }

  private read<T>(cache: Map<string, IComputedValue<T>>, id: string, read: () => T): T {
    let value = cache.get(id);
    if (!value) {
      value = computed(read, { equals: comparer.structural });
      cache.set(id, value);
    }
    return value.get();
  }
}
