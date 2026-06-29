/**
 * Pure helpers for the Cerebro features settings UI: turn a flag's raw state
 * (personal value + workspace lock) into a plain-language status, and decide
 * whether a flag matches the search query / filter chip. Kept free of React so
 * the precedence and copy can be unit-tested without rendering. FIR-2104.
 */

/**
 * The effective, human-facing state of a flag for the current user:
 *   - "forced_on"  — owner locked it on for everyone.
 *   - "forced_off" — owner locked it off for everyone (the org-wide kill switch).
 *   - "on" / "off" — not locked; members choose, and this is the resolved value.
 */
export type FlagEffectiveState = "on" | "off" | "forced_on" | "forced_off";

export function flagEffectiveState(args: {
  enabled: boolean;
  locked: boolean;
  workspaceValue: boolean | undefined;
}): FlagEffectiveState {
  const { enabled, locked, workspaceValue } = args;
  if (locked && workspaceValue === true) return "forced_on";
  if (locked && workspaceValue === false) return "forced_off";
  return enabled ? "on" : "off";
}

export type FlagStatusTone = "on" | "off" | "forced";

export interface FlagStatusCopy {
  tone: FlagStatusTone;
  /** One short, plain-language line: is it on, and for whom. */
  text: string;
}

/** Plain-language status line + tone for the colored dot. */
export function flagStatusCopy(state: FlagEffectiveState): FlagStatusCopy {
  switch (state) {
    case "forced_on":
      return { tone: "forced", text: "Forced on for everyone" };
    case "forced_off":
      return { tone: "off", text: "Forced off for everyone" };
    case "on":
      return { tone: "on", text: "On — members choose" };
    case "off":
      return { tone: "off", text: "Off — members choose" };
    default:
      // Enum drift downgrades to a safe generic line, never crashes.
      return { tone: "off", text: "Off" };
  }
}

/** The filter chips above the list. */
export type FlagFilter = "all" | "on" | "off" | "forced";

export function matchesFilter(state: FlagEffectiveState, filter: FlagFilter): boolean {
  switch (filter) {
    case "all":
      return true;
    case "on":
      return state === "on" || state === "forced_on";
    case "off":
      return state === "off" || state === "forced_off";
    case "forced":
      return state === "forced_on" || state === "forced_off";
    default:
      return true;
  }
}

/** Case-insensitive match of the query against a flag's label or description. */
export function matchesQuery(
  flag: { label: string; description: string },
  query: string,
): boolean {
  const q = query.trim().toLowerCase();
  if (!q) return true;
  return (
    flag.label.toLowerCase().includes(q) ||
    flag.description.toLowerCase().includes(q)
  );
}
