export type ConversationWorkStage = "source_load" | "source_projection" | "rows_projection" | "complexity" | "partition" | "locator" | "participant_headers" | "work_section";
export interface ConversationWorkSample {
  stage: ConversationWorkStage;
  durationMs: number;
}
interface Subscription {
  active: boolean;
  record(sample: ConversationWorkSample): void;
}
const subscriptions = new Map<string, Set<Subscription>>();

/** Only an explicit development capture subscribes; inactive calls do not read a clock. */
export function observeConversationWork(sessionId: string, record: Subscription["record"]) {
  let entries = subscriptions.get(sessionId);
  if (!entries) subscriptions.set(sessionId, entries = new Set());
  const entry: Subscription = { active: true, record };
  entries.add(entry);
  return () => {
    entry.active = false;
    entries.delete(entry);
    if (!entries.size && subscriptions.get(sessionId) === entries) subscriptions.delete(sessionId);
  };
}

function start(sessionId: string, stage: ConversationWorkStage) {
  const entries = subscriptions.get(sessionId);
  if (!entries?.size) return undefined;
  const listeners = [...entries];
  const began = performance.now();
  return () => {
    const sample = { stage, durationMs: performance.now() - began };
    for (const listener of listeners) {
      if (!listener.active) continue;
      try { listener.record(sample); } catch { /* Diagnostics never affect the measured work. */ }
    }
  };
}

export function measureConversationWork<T>(sessionId: string, stage: ConversationWorkStage, action: () => T): T {
  const finish = start(sessionId, stage);
  try { return action(); } finally { finish?.(); }
}

export function measureConversationAsyncWork<T>(sessionId: string, stage: ConversationWorkStage, action: () => Promise<T>): Promise<T> {
  const finish = start(sessionId, stage);
  if (!finish) return action();
  try { return action().finally(finish); }
  catch (error) { finish(); throw error; }
}
