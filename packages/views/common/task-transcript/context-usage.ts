// Context-window occupancy, as the transcript reports it.
//
// The daemon records one `context` event per meaningful change, carrying the
// figures in `input`. Each one is the state of the window right then: only
// the newest is worth showing as the figure, and the rest are its history.
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
  /**
   * The window `usedTokens` is measured against. Required: an occupancy with
   * no window is an absolute token count a reader cannot act on, so a runtime
   * that reports no window reports nothing at all. Events from before that
   * rule — and from a runtime that has since been dropped — carry no window,
   * and are read as "not an observation".
   */
  limitTokens: number;
  model?: string;
}

function readNumber(value: unknown): number | undefined {
  return typeof value === "number" && Number.isFinite(value) && value > 0 ? value : undefined;
}

function readString(value: unknown): string | undefined {
  return typeof value === "string" && value.length > 0 ? value : undefined;
}

/**
 * Parse one timeline item as an occupancy reading, or null when it is not one
 * — including a `context` event that carries no usable figure or no window,
 * which is what an older daemon's events and a dropped runtime's events look
 * like.
 */
export function readContextEvent(item: ContextEventSource): ContextUsage | null {
  if (item.type !== "context") return null;
  const input = item.input ?? {};
  const usedTokens = readNumber(input.used_tokens);
  const limitTokens = readNumber(input.limit_tokens);
  if (usedTokens === undefined || limitTokens === undefined) return null;
  return { usedTokens, limitTokens, model: readString(input.model) };
}

/**
 * True for every `context` item. They are state, not steps: the figure and
 * its history read them, and the timelines drop them whole — including the
 * ones this client cannot parse, which would otherwise render as empty rows.
 */
export function isContextUsageItem(item: ContextEventSource): boolean {
  return item.type === "context";
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
    const usage = readContextEvent(item);
    if (usage !== null) return usage;
  }
  return undefined;
}

/**
 * Occupancy as whole percent.
 *
 * Neither extreme is reached by rounding: 0% and 100% are read as states
 * ("fresh", "out of room"), so a window with anything in it reports at least
 * 1% and one with room left at most 99%. Mirrors the daemon's own formatting
 * of the same figures (`contextUsagePercent`).
 */
export function contextUsagePercent(usage: ContextUsage): number {
  const limit = usage.limitTokens;
  const pct = Math.round((usage.usedTokens / limit) * 100);
  if (pct <= 0) return 1;
  if (pct >= 100 && usage.usedTokens < limit) return 99;
  return Math.min(100, pct);
}

/** One entry of the run's context history, in stream order. */
export type ContextObservation = { at?: string } & ContextUsage;

/**
 * Every occupancy the run reported, oldest first.
 *
 * The chip shows only the newest, but the history is what answers "when did
 * it fill up" — so the rest are kept rather than collapsed. The daemon
 * already throttles the stream to meaningful changes, so this is the list of
 * changes, not a sample log.
 */
export function contextUsageHistory(
  items: readonly ContextEventSource[],
): ContextObservation[] {
  const out: ContextObservation[] = [];
  for (const item of items) {
    const usage = readContextEvent(item);
    if (usage === null) continue;
    // One measurement can be reported twice: a runtime that answers with its
    // real context window after the fact has the backend re-state the figure
    // against it. That correction is the same prompt, so it replaces the
    // entry rather than adding a second row for the same moment.
    const previous = out[out.length - 1];
    if (previous !== undefined && previous.usedTokens === usage.usedTokens) {
      out[out.length - 1] = { ...usage, at: item.created_at };
      continue;
    }
    out.push({ ...usage, at: item.created_at });
  }
  return out;
}

/**
 * The occupancy a task row carries, for surfaces that have the task but not
 * its transcript — a historical run in the issue timeline never fetches its
 * messages, so the row is the only source there.
 *
 * Returns undefined when the runtime reported nothing, or a figure stored
 * without a window: both render as no chip at all. The column is nullable and
 * older rows predate the window requirement, so this is a real case, not a
 * defensive one.
 */
export function taskContextUsage(task: {
  context_usage?: { used_tokens: number; limit_tokens?: number };
}): ContextUsage | undefined {
  const reported = task.context_usage;
  if (!reported || !(reported.used_tokens > 0)) return undefined;
  if (reported.limit_tokens === undefined || !(reported.limit_tokens > 0)) return undefined;
  return { usedTokens: reported.used_tokens, limitTokens: reported.limit_tokens };
}
