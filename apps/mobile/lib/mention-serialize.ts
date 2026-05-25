/**
 * Mention serialization for the mobile comment composer.
 *
 * Mobile RN `<TextInput>` cannot host inline custom views (no equivalent of
 * web's TipTap NodeView), so we use a zero-width sentinel character —
 * U+2063 INVISIBLE SEPARATOR — to mark every `@` that came from the
 * suggestion bar (vs. ones the user typed manually).
 *
 * Display while editing: plain text `Hi @bohan please` (sentinel invisible).
 * On send: scan for `⁣@<word>` runs, zip with the ordered `markers`
 * list to produce `[@<name>](mention://<type>/<id>)` markdown that the
 * backend's `util.ParseMentions` regex (server/internal/util/mention.go:16)
 * already accepts. **Issues drop the `@` in the label** — they render as
 * `[MUL-123](mention://issue/<uuid>)` to match web (mention-extension.ts).
 *
 * Sentinel mismatch (e.g. user copy-paste broke a marker) → serializer
 * falls back to plain text with all sentinels stripped: never crash, never
 * lose user input, never claim a mention we can't prove.
 */

const SENTINEL = "⁣";

export type MentionType = "member" | "agent" | "squad" | "all" | "issue";

export interface MentionMarker {
  type: MentionType;
  /** UUID for member/agent/squad/issue, the literal "all" for @all. */
  id: string;
  /** Display name without the leading `@`. For issues this is the
   *  identifier (e.g. "MUL-123"). May contain non-ASCII chars. */
  name: string;
}

/**
 * Detects whether the cursor is currently inside an `@<token>` run that
 * should trigger the mention suggestion bar.
 *
 * Returns the start offset of the `@` and the query (text between `@` and
 * the cursor), or null when not in a mention token.
 *
 * Word boundary uses `/\s/` so non-ASCII names (中文 / 日本語) work — the
 * token ends at whitespace, not at ASCII word boundary.
 *
 * Skips runs that begin with the sentinel — those are completed mentions
 * inserted by the bar, not in-progress queries.
 */
export function tokenAtCursor(
  text: string,
  cursor: number,
): { start: number; query: string } | null {
  if (cursor < 1 || cursor > text.length) return null;

  // Walk back from cursor until we hit @ / whitespace / start of text.
  let i = cursor - 1;
  while (i >= 0) {
    const ch = text[i];
    if (ch === "@") break;
    if (ch === undefined || /\s/.test(ch)) return null;
    i--;
  }
  if (i < 0 || text[i] !== "@") return null;

  // Skip if the @ is preceded by the sentinel (= a completed mention chip).
  if (i > 0 && text[i - 1] === SENTINEL) return null;

  // The character before @ must be whitespace or start-of-string. This
  // prevents random in-word @ (e.g. "user@example.com") from triggering.
  if (i > 0) {
    const prev = text[i - 1];
    if (prev !== undefined && !/\s/.test(prev)) return null;
  }

  const query = text.slice(i + 1, cursor);
  // If the query already contains whitespace, the user has moved past the
  // token — abort.
  if (/\s/.test(query)) return null;
  return { start: i, query };
}

/**
 * Replaces the in-progress `@<query>` run with `⁣@<name> ` and returns
 * the new text + cursor + the marker the caller should push onto its
 * ordered list of markers.
 */
export function insertMention(
  text: string,
  query: { start: number; queryLength: number },
  mention: MentionMarker,
): {
  newText: string;
  newSelection: { start: number; end: number };
  marker: MentionMarker;
} {
  // Replace text[start .. start + 1 + queryLength] (the `@` plus the query)
  // with the sentinel + @<name> + trailing space.
  const before = text.slice(0, query.start);
  const after = text.slice(query.start + 1 + query.queryLength);
  const insert = `${SENTINEL}@${mention.name} `;
  const newText = before + insert + after;
  const cursor = before.length + insert.length;
  return {
    newText,
    newSelection: { start: cursor, end: cursor },
    marker: mention,
  };
}

/**
 * Walks the text left-to-right, finding each `⁣@<word>` run and
 * pairing it (in order) with the ordered `markers` list. Each pair is
 * replaced with the canonical `[@<name>](mention://<type>/<id>)` markdown
 * link that backend's `ParseMentions` recognises.
 *
 * Robustness:
 *   - Sentinel count != marker count → fallback. Strip all sentinels and
 *     return plain text. The user never loses content; mentions just
 *     don't get linked.
 *   - Stray sentinels not followed by `@` → stripped.
 *   - Sentinel-marked runs whose word doesn't match the marker name →
 *     fallback (defensive; users who hand-edit the inserted token).
 */
export function serializeMentions(
  text: string,
  markers: MentionMarker[],
): string {
  // Fast path: no sentinels.
  if (!text.includes(SENTINEL)) return text;

  const out: string[] = [];
  let cursor = 0;
  let markerIndex = 0;
  let abort = false;

  while (cursor < text.length) {
    const sentinelAt = text.indexOf(SENTINEL, cursor);
    if (sentinelAt === -1) {
      out.push(text.slice(cursor));
      break;
    }
    out.push(text.slice(cursor, sentinelAt));

    // Verify the sentinel is followed by `@<word>`.
    if (text[sentinelAt + 1] !== "@") {
      // Stray sentinel — drop it, keep walking.
      cursor = sentinelAt + 1;
      continue;
    }

    // Read the word after `@` until whitespace or end-of-text.
    let wordEnd = sentinelAt + 2;
    while (wordEnd < text.length && !/\s/.test(text[wordEnd]!)) wordEnd++;
    const word = text.slice(sentinelAt + 2, wordEnd);

    const marker = markers[markerIndex];
    if (!marker || marker.name !== word) {
      // Marker exhausted or word doesn't match — abort and fallback.
      abort = true;
      break;
    }

    // Issues render without the leading `@` in the link label (mirrors
    // web's mention-extension.ts:67-74). Members / agents / @all keep it.
    const label =
      marker.type === "issue" ? marker.name : `@${marker.name}`;
    out.push(`[${label}](mention://${marker.type}/${marker.id})`);
    markerIndex++;
    cursor = wordEnd;
  }

  if (abort || markerIndex !== markers.length) {
    // Mismatch — strip all sentinels and return plain text.
    return text.replace(new RegExp(SENTINEL, "g"), "");
  }
  return out.join("");
}
