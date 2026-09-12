/** Private, workspace-scoped reply metadata. Published replies remain Markdown. */
export interface ReplyAnnotation {
  id: string;
  /** Local source key. Descriptions use description:<issueId>, never a reply target. */
  sourceCommentId: string;
  sourceActorName: string;
  sourceRevision?: number;
  quote: string;
  note: string;
  /** Offsets refer to normalized rendered text, never Markdown or a live Range. */
  start: number;
  prefix: string;
  suffix: string;
}

export const MAX_REPLY_ANNOTATIONS = 20;
export const MAX_ANNOTATION_QUOTE_LENGTH = 4000;
export const EMPTY_REPLY_ANNOTATIONS: ReplyAnnotation[] = [];

export function normalizeReplyAnnotations(value: unknown): ReplyAnnotation[] {
  if (!Array.isArray(value)) return EMPTY_REPLY_ANNOTATIONS;
  const valid = value.filter((a): a is ReplyAnnotation =>
    a != null && typeof a === "object" &&
    typeof a.id === "string" && typeof a.sourceCommentId === "string" &&
    typeof a.sourceActorName === "string" && typeof a.quote === "string" &&
    a.quote.trim().length > 0 && a.quote.length <= MAX_ANNOTATION_QUOTE_LENGTH &&
    typeof a.note === "string" && a.note.trim().length > 0 && Number.isInteger(a.start) && a.start >= 0 &&
    typeof a.prefix === "string" && typeof a.suffix === "string" &&
    (a.sourceRevision === undefined || Number.isInteger(a.sourceRevision)),
  ).slice(0, MAX_REPLY_ANNOTATIONS);
  return valid.length ? valid : EMPTY_REPLY_ANNOTATIONS;
}

/** A quote alone is not an instruction to the agent. */
export function hasReplyIntent(content: string, annotations: readonly ReplyAnnotation[]): boolean {
  return !!content.trim() || annotations.some((a) => !!a.note.trim());
}

/** Escape visible text, including literal mention:// links in code samples.
 * The server scans raw Markdown for mentions even inside quotes and fences.
 * Entity-encoding punctuation keeps snapshots inert without changing their
 * rendered text. Escape ampersands first so source entities remain literal.
 */
function quoteText(text: string): string {
  return text.replace(/&/g, "&amp;").replace(/[<>\\`*_[\]{}()#!|~:$+.=-]/g,
    (char) => `&#${char.charCodeAt(0)};`)
    // Leading spaces must survive HTML whitespace collapsing and must not
    // become Markdown list nesting or an indented code block. Tabs use 4 stops.
    .replace(/^[ \t]+/, (indent) => {
      let columns = 0;
      for (const char of indent) columns += char === "\t" ? 4 - columns % 4 : 1;
      return "&nbsp;".repeat(columns);
    });
}

export function composeAnnotatedReply(content: string, annotations: readonly ReplyAnnotation[]): string {
  const body = content.trim();
  const saved = annotations.filter((a) => a.note.trim());
  if (!saved.length) return body;
  // /note must remain the first token so annotations cannot change triggering.
  const command = body.match(/^\/note(?=\s|$)/i)?.[0];
  const reply = command ? body.slice(command.length).trim() : body;
  const quotes = saved.map((a) =>
    a.quote.split(/\r?\n/).map((line) => `> ${quoteText(line)}`).join("\n") +
      `\n\n${a.note.trim()}`,
  );
  // Markdown collapses extra newlines. A blank paragraph keeps one visible
  // empty line between annotations.
  const sections = [quotes.join("\n\n&nbsp;\n\n"), reply].filter(Boolean).join("\n\n---\n\n");
  return command ? `${command}\n\n${sections}` : sections;
}

/** Relocate only an unambiguous quote with matching context after source edits. */
export function locateReplyAnnotation(text: string, annotation: ReplyAnnotation): number | null {
  const matches = (start: number) =>
    text.slice(start, start + annotation.quote.length) === annotation.quote &&
    text.slice(Math.max(0, start - annotation.prefix.length), start) === annotation.prefix &&
    text.slice(start + annotation.quote.length, start + annotation.quote.length + annotation.suffix.length) === annotation.suffix;
  const candidates: number[] = [];
  for (let start = text.indexOf(annotation.quote); start !== -1; start = text.indexOf(annotation.quote, start + 1)) {
    if (matches(start)) candidates.push(start);
    if (candidates.length > 1) return null;
  }
  return candidates[0] ?? null;
}
