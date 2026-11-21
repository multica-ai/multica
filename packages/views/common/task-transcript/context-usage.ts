// Context-window occupancy, as the transcript reports it.
//
// The daemon records one `context` event per meaningful change, carrying the
// figures in `input`. Two different things arrive on that one type, and they
// are read differently:
//
//   * an occupancy observation — the state of the window right then, which
//     only the newest one is worth showing;
//   * a compaction the runtime completed — an event, which belongs in the
//     timeline where it happened.
//
// Occupancy is NOT additive: consecutive turns re-send the same conversation,
// so these values are snapshots and summing them would invent a number.

/**
 * The shape both surfaces have: a transcript timeline item and a live task
 * message payload. Declared structurally so the chat panel can read its own
 * realtime stream with the same functions the run transcript uses — the two
 * must never disagree about what the figures mean.
 */
export interface ContextEventSource {
  type: string;
  input?: Record<string, unknown>;
  created_at?: string;
}

export interface ContextUsage {
  usedTokens: number;
  /** The window `usedTokens` is measured against, or undefined when the
   *  runtime did not say. A percentage is only meaningful with this. */
  limitTokens?: number;
  model?: string;
}

export interface ContextCompaction {
  /** Occupancy immediately before compacting, when the runtime reported it. */
  preTokens?: number;
  /** The runtime's own word for what triggered it ("auto", "manual"). */
  trigger?: string;
}

export type ContextEvent =
  | ({ kind: "usage" } & ContextUsage)
  | ({ kind: "compaction" } & ContextCompaction);

function readNumber(value: unknown): number | undefined {
  return typeof value === "number" && Number.isFinite(value) && value > 0 ? value : undefined;
}

function readString(value: unknown): string | undefined {
  return typeof value === "string" && value.length > 0 ? value : undefined;
}

/**
 * Parse one timeline item as a context event, or null when it is not one.
 *
 * A `context` item whose payload carries no usable figure still returns a
 * compaction event when it says it is one — the phase is the claim, and the
 * metadata around it is optional.
 */
export function readContextEvent(item: ContextEventSource): ContextEvent | null {
  if (item.type !== "context") return null;
  const input = item.input ?? {};
  if (readString(input.phase) === "compacted") {
    return {
      kind: "compaction",
      preTokens: readNumber(input.pre_tokens),
      trigger: readString(input.trigger),
    };
  }
  const usedTokens = readNumber(input.used_tokens);
  if (usedTokens === undefined) return null;
  return {
    kind: "usage",
    usedTokens,
    limitTokens: readNumber(input.limit_tokens),
    model: readString(input.model),
  };
}

/** True for rows the timeline drops: a superseded occupancy snapshot. */
export function isContextUsageItem(item: ContextEventSource): boolean {
  return readContextEvent(item)?.kind === "usage";
}

/**
 * The run's current occupancy: the newest observation, or undefined when the
 * runtime reported none. Absence means "not reported" — never "empty".
 */
export function latestContextUsage(
  items: readonly ContextEventSource[],
): ContextUsage | undefined {
  for (let i = items.length - 1; i >= 0; i--) {
    const item = items[i];
    if (item === undefined) continue;
    const event = readContextEvent(item);
    if (event?.kind === "usage") {
      const { kind: _kind, ...usage } = event;
      return usage;
    }
  }
  return undefined;
}

/**
 * Occupancy as whole percent, or undefined without a known window.
 *
 * Neither extreme is reached by rounding: 0% and 100% are read as states
 * ("fresh", "out of room"), so a window with anything in it reports at least
 * 1% and one with room left at most 99%. Mirrors the daemon's own formatting
 * of the same figures (`contextUsagePercent`).
 */
export function contextUsagePercent(usage: ContextUsage): number | undefined {
  const limit = usage.limitTokens;
  if (limit === undefined || limit <= 0 || usage.usedTokens <= 0) return undefined;
  const pct = Math.round((usage.usedTokens / limit) * 100);
  if (pct <= 0) return 1;
  if (pct >= 100 && usage.usedTokens < limit) return 99;
  return Math.min(100, pct);
}

/** One entry of the run's context history, in stream order. */
export type ContextObservation = { at?: string } & ContextEvent;

/**
 * Every context event the run reported, oldest first.
 *
 * The header shows only the newest occupancy, but the history is what answers
 * "when did it fill up, and what did the compaction cost us" — so it is kept
 * rather than collapsed. The daemon already throttles the stream to meaningful
 * changes, so this is the list of changes, not a sample log.
 */
export function contextUsageHistory(
  items: readonly ContextEventSource[],
): ContextObservation[] {
  const out: ContextObservation[] = [];
  for (const item of items) {
    const event = readContextEvent(item);
    if (event === null) continue;
    out.push({ ...event, at: item.created_at });
  }
  return out;
}

/**
 * The occupancy a task row carries, for surfaces that have the task but not
 * its transcript — a historical run in the issue timeline never fetches its
 * messages, so the row is the only source there.
 *
 * Returns undefined when the runtime reported nothing, which the chip renders
 * as no chip at all.
 */
export function taskContextUsage(task: {
  context_usage?: { used_tokens: number; limit_tokens?: number };
}): ContextUsage | undefined {
  const reported = task.context_usage;
  if (!reported || !(reported.used_tokens > 0)) return undefined;
  return {
    usedTokens: reported.used_tokens,
    limitTokens: reported.limit_tokens !== undefined && reported.limit_tokens > 0
      ? reported.limit_tokens
      : undefined,
  };
}
