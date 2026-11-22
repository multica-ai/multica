import type { ReplyAnnotation } from "./reply-annotation";

/** v1 coordinates: UTF-16 code units in indexCommentText's rendered text.
 * The snapshot is text, never HTML. It distinguishes an original repeated
 * passage from another copy when descriptions change. Issue revisions are not
 * source versions: comments and status changes also increment them.
 */
export interface DescriptionAnchor {
  version: 1;
  id: string;
  issue_id: string;
  quote: string;
  start: number;
  prefix: string;
  suffix: string;
  source_text: string;
}

export function descriptionAnchorsForSend(issueId: string, annotations: readonly ReplyAnnotation[]): DescriptionAnchor[] {
  return annotations.flatMap((a) => a.sourceCommentId === `description:${issueId}` && a.note.trim()
    ? [{ version: 1 as const, id: a.id, issue_id: issueId, quote: a.quote, start: a.start,
      prefix: a.prefix, suffix: a.suffix, source_text: a.sourceText ?? "" }] : []);
}

function occurrences(text: string, quote: string): number[] {
  const found: number[] = [];
  for (let at = text.indexOf(quote); at !== -1; at = text.indexOf(quote, at + 1)) found.push(at);
  return found;
}

/** Conservative relocation. Ambiguity is outdated, never a nearest-match guess.
 * An unchanged snapshot retains a known occurrence even in identical paragraphs.
 * After edits a uniquely identifying context may move; removing one of several
 * indistinguishable copies cannot identify which copy survived.
 */
export function locateDescriptionAnchor(text: string, a: DescriptionAnchor): number | null {
  const before = a.source_text;
  if (!a.quote || before.slice(a.start, a.start + a.quote.length) !== a.quote) return null;
  if (text === before) return a.start;
  const matching = (source: string) => occurrences(source, a.quote).filter((start) =>
    source.slice(Math.max(0, start - a.prefix.length), start) === a.prefix &&
    source.slice(start + a.quote.length, start + a.quote.length + a.suffix.length) === a.suffix);
  const oldMatches = matching(before), matches = matching(text);
  if (matches.length > 1 && matches.length !== oldMatches.length) return null;
  if (oldMatches.length === 1 && matches.length === 1) return matches[0]!;
  if (oldMatches.length > 1 && (matches.length !== oldMatches.length || occurrences(text, a.quote).length !== occurrences(before, a.quote).length)) return null;
  // A single changed interval outside this quote preserves its exact identity.
  let prefix = 0;
  while (prefix < Math.min(before.length, text.length) && before[prefix] === text[prefix]) prefix++;
  let suffix = 0;
  while (suffix < Math.min(before.length, text.length) - prefix && before[before.length - 1 - suffix] === text[text.length - 1 - suffix]) suffix++;
  const uniqueQuote = occurrences(before, a.quote).length === 1 && occurrences(text, a.quote).length === 1;
  const beforeContext = Math.max(0, a.start - a.prefix.length);
  const afterContext = Math.min(before.length, a.start + a.quote.length + a.suffix.length);
  const start = a.start + (a.start >= before.length - suffix ? text.length - before.length : 0);
  if ((uniqueQuote ? (a.start + a.quote.length <= prefix || a.start >= before.length - suffix) : (afterContext <= prefix || beforeContext >= before.length - suffix)) && text.slice(start, start + a.quote.length) === a.quote) return start;
  return null;
}
