// FIR-2669 follow-up: free-text search for the Agents list. Jesper asked the
// search box to also match the Account email shown on each row (the cerebro
// account the agent's runtime authenticates as), on top of the fields it
// already covered. Kept as a pure, testable function — the page resolves the
// row-derived strings (runtime name, owner name, account label) and passes
// them in, so there is no query/i18n/store dependency here.

export interface AgentSearchFields {
  /** Agent display name. */
  name: string;
  /** Agent description / system-prompt summary, if any. */
  description?: string | null;
  /** Model id (e.g. "claude-opus-4-7"). */
  model?: string | null;
  /** Thinking level slug, if set. */
  thinkingLevel?: string | null;
  /** Name of the runtime the agent is bound to. */
  runtimeName?: string | null;
  /** Owner member display name. */
  ownerName?: string | null;
  /** Runtime's cerebro_account "login_identity provider" (the Account column). */
  accountLabel?: string | null;
}

/**
 * True when `query` matches any searchable field of the agent row. Query is
 * normalised defensively; an empty query matches everything.
 */
export function matchesAgentSearch(
  fields: AgentSearchFields,
  query: string,
): boolean {
  const q = query.trim().toLowerCase();
  if (!q) return true;
  const haystack = [
    fields.name,
    fields.description ?? "",
    fields.model ?? "",
    fields.thinkingLevel ?? "",
    fields.runtimeName ?? "",
    fields.ownerName ?? "",
    fields.accountLabel ?? "",
  ]
    .join(" ")
    .toLowerCase();
  return haystack.includes(q);
}
