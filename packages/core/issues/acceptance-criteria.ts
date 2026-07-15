const CLOSING_LINE_RE = /^Closes when:\s*(.+)$/i;

/**
 * Pull explicit exit criteria lines out of an issue description. The create
 * surfaces can keep treating the description as free-form markdown while still
 * forwarding any `Closes when:` checks the user included.
 */
export function extractAcceptanceCriteria(
  description?: string | null,
): string[] | undefined {
  if (!description) return undefined;

  const criteria = description
    .split(/\r?\n/)
    .map((line) => line.trim())
    .filter((line) => CLOSING_LINE_RE.test(line));

  return criteria.length > 0 ? criteria : undefined;
}
