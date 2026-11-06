import { localV2HistorySchema, type LocalV2Snapshot, type LocalV2Summary } from "@multica/core/local-conversation/v2";
import { CompletionReadModel, type CompletionPosition, type CompletionReadRecord } from "../models/completion-read-model";

export interface CompletionReadStorage {
  read(id: string): Promise<CompletionReadRecord | null>;
  write(id: string, record: CompletionReadRecord): Promise<void>;
}

interface CompletionReadOptions {
  storage: CompletionReadStorage;
  generation(): Promise<string>;
  snapshot(id: string): Promise<LocalV2Snapshot>;
  history(id: string, before: number): Promise<unknown>;
}

/** Tracks results independently of mounted views, including changes missed over a restart. */
export class CompletionReadService {
  private readonly jobs = new Map<string, Promise<void>>();
  private readonly dirty = new Set<string>();
  private epoch = 0;
  private disposed = false;

  constructor(readonly model: CompletionReadModel, private readonly options: CompletionReadOptions) {}

  pending(id: string): CompletionReadRecord | undefined { return this.model.pending(id); }

  observe(summary: LocalV2Summary): void {
    this.enqueue(summary.conversation_id, async (current) => {
      const id = summary.conversation_id;
      const generation = await this.options.generation();
      if (!generation || !current()) return;
      let record = this.model.get(id);
      if (!record) {
        const stored = await this.options.storage.read(id);
        if (!current()) return;
        if (stored?.generation === generation) record = stored;
      }
      if (!record || record.generation !== generation) {
        // Existing history is the baseline. A running last turn can still produce a new result.
        const baseline = Math.max(0, summary.turn_count - Number(summary.status === "running"));
        record = {
          version: 1, generation, baselineTurnIndex: baseline, summarySeq: summary.summary_seq,
          turnCount: summary.turn_count, activeTurnId: summary.active_turn_id ?? null, status: summary.status,
          completedTurnIndex: baseline - 1, completedTurnId: null, readTurnIndex: baseline - 1,
        };
        this.model.set(id, record);
        await this.persist(id, record);
        return;
      }
      this.model.set(id, record);
      if (summary.summary_seq <= record.summarySeq) {
        if (this.dirty.has(id)) await this.persist(id, record);
        return;
      }
      const changed = record.turnCount !== summary.turn_count || record.activeTurnId !== (summary.active_turn_id ?? null) || record.status !== summary.status;
      if (changed) {
        const snapshot = await this.options.snapshot(id);
        if (!current() || snapshot.store_generation !== generation) return;
        let turns = snapshot.turns;
        let more = snapshot.has_older_turns;
        const minimum = Math.max(record.baselineTurnIndex, record.completedTurnIndex + 1);
        for (;;) {
          const completed = turns.filter((turn) => turn.status === "completed" && turn.turn_index >= minimum)
            .sort((a, b) => b.turn_index - a.turn_index)[0];
          if (completed) {
            // markRead may have advanced while the snapshot travelled.
            const latest = this.model.get(id)!;
            record = { ...record, readTurnIndex: latest.readTurnIndex, completedTurnIndex: completed.turn_index, completedTurnId: completed.id };
            break;
          }
          const before = turns[0]?.turn_index;
          if (!more || before === undefined || before <= minimum) break;
          const history = localV2HistorySchema.parse(await this.options.history(id, before));
          if (!current() || history.store_generation !== generation) return;
          if (!history.turns.length || history.turns[0]!.turn_index >= before) throw new Error("Completion history did not advance");
          turns = history.turns;
          more = history.has_older_turns;
        }
      }
      if (!current()) return;
      const latest = this.model.get(id)!;
      record = { ...record, readTurnIndex: latest.readTurnIndex, summarySeq: summary.summary_seq, turnCount: summary.turn_count, activeTurnId: summary.active_turn_id ?? null, status: summary.status };
      this.model.set(id, record);
      await this.persist(id, record);
    });
  }

  /** Acknowledges exactly what the view displayed; a later completion stays unread. */
  markRead(id: string, position: CompletionPosition): void {
    const record = this.model.get(id);
    if (!record || record.generation !== position.generation || position.completedTurnIndex > record.completedTurnIndex || position.completedTurnIndex <= record.readTurnIndex) return;
    this.model.set(id, { ...record, readTurnIndex: position.completedTurnIndex });
    this.enqueue(id, async (current) => {
      const latest = this.model.get(id);
      if (latest && current()) await this.persist(id, latest);
    });
  }

  private async persist(id: string, record: CompletionReadRecord): Promise<void> {
    this.dirty.add(id);
    await this.options.storage.write(id, record);
    this.dirty.delete(id);
  }

  private enqueue(id: string, work: (current: () => boolean) => Promise<void>): void {
    if (this.disposed) return;
    const epoch = this.epoch;
    const current = () => !this.disposed && epoch === this.epoch;
    const job = (this.jobs.get(id) ?? Promise.resolve()).then(async () => {
      if (current()) await work(current);
    }).catch((error: unknown) => {
      if (current()) {
        this.model.fail(error);
        console.warn("[conversations] completion read state failed", error);
      }
    }).finally(() => { if (this.jobs.get(id) === job) this.jobs.delete(id); });
    this.jobs.set(id, job);
  }

  async settled(): Promise<void> {
    while (this.jobs.size) await Promise.all(this.jobs.values());
  }
  reset(): void { this.epoch++; this.dirty.clear(); this.model.clear(); }
  dispose(): void { this.disposed = true; this.epoch++; }
}
