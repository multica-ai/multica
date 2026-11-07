import { action, makeObservable, observable } from "mobx";
import { z } from "zod";

export const completionReadRecordSchema = z.object({
  version: z.literal(1),
  generation: z.string().min(1),
  baselineTurnIndex: z.number().int().nonnegative(),
  summarySeq: z.number().int().nonnegative(),
  turnCount: z.number().int().nonnegative(),
  activeTurnId: z.string().nullable(),
  status: z.string(),
  completedTurnIndex: z.number().int().min(-1),
  completedTurnId: z.string().nullable(),
  completedSeq: z.number().int().nonnegative(),
  readSeq: z.number().int().nonnegative(),
  readTurnIndex: z.number().int().min(-1),
});
export type CompletionReadRecord = z.infer<typeof completionReadRecordSchema>;
export type CompletionPosition = Pick<CompletionReadRecord, "generation" | "completedTurnIndex" | "completedTurnId" | "completedSeq">;

/** Read positions are daemon-owned; turn indices compare only within one daemon store. */
export class CompletionReadModel {
  private readonly records = observable.map<string, CompletionReadRecord>([], { deep: false });
  error: string | null = null;

  constructor() {
    makeObservable(this, { error: observable, set: action, fail: action, clear: action });
  }

  get(id: string): CompletionReadRecord | undefined { return this.records.get(id); }
  pending(id: string): CompletionReadRecord | undefined {
    const record = this.get(id);
    return record && record.completedTurnId && record.completedSeq > record.readSeq ? record : undefined;
  }
  set(id: string, record: CompletionReadRecord): void { this.records.set(id, record); this.error = null; }
  fail(error: unknown): void { this.error = error instanceof Error ? error.message : String(error); }
  clear(): void { this.records.clear(); this.error = null; }
}
