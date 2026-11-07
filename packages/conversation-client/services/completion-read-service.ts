import { localV2SummarySchema, type LocalV2Summary } from "@multica/core/local-conversation/v2";
import { CompletionReadModel, type CompletionPosition, type CompletionReadRecord } from "../models/completion-read-model";

interface CompletionReadOptions {
  generation(): Promise<string>;
  read(id: string, position: CompletionPosition): Promise<unknown>;
}

/** Projects authoritative completion positions independently of mounted views. */
export class CompletionReadService {
  private readonly jobs = new Map<string, Promise<void>>();
  private readonly reading = new Set<string>();
  private epoch = 0;
  private disposed = false;

  constructor(readonly model: CompletionReadModel, private readonly options: CompletionReadOptions) {}

  pending(id: string): CompletionReadRecord | undefined { return this.model.pending(id); }

  observe(summary: LocalV2Summary): void {
    this.enqueue(summary.conversation_id, async (current) => {
      const generation = await this.options.generation();
      if (!generation || !current() || summary.read_turn_index === undefined || summary.read_completion_seq === undefined) return;
      const prior = this.model.get(summary.conversation_id);
      if (prior?.generation === generation && prior.summarySeq >= summary.summary_seq) return;
      this.model.set(summary.conversation_id, {
        version: 1, generation, baselineTurnIndex: 0, summarySeq: summary.summary_seq,
        turnCount: summary.turn_count, activeTurnId: summary.active_turn_id ?? null, status: summary.status,
        completedTurnIndex: summary.completion?.turn_index ?? -1, completedTurnId: summary.completion?.turn_id ?? null,
        readTurnIndex: summary.read_turn_index, completedSeq: summary.completion?.completion_seq ?? 0, readSeq: summary.read_completion_seq,
      });
    });
  }

  /** Acknowledges exactly what the view displayed; the daemon retains later results. */
  markRead(id: string, position: CompletionPosition): void {
    const record = this.model.get(id);
    if (!record || record.generation !== position.generation || !position.completedTurnId || position.completedSeq > record.completedSeq || position.completedSeq <= record.readSeq) return;
    const key = JSON.stringify([id, position.generation, position.completedSeq]);
    if (this.reading.has(key)) return;
    this.reading.add(key);
    this.enqueue(id, async (current) => {
      try {
        const summary = localV2SummarySchema.parse(await this.options.read(id, position));
        if (current()) this.observe(summary);
      } finally { this.reading.delete(key); }
    });
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
  reset(): void { this.epoch++; this.reading.clear(); this.model.clear(); }
  dispose(): void { this.disposed = true; this.epoch++; }
}
