/**
 * Long comment bodies open collapsed, showing only their opening.
 *
 * Nothing caps a comment's length: the server stores any size and a paste
 * lands in the body as typed. A single comment of a megabyte or more (a
 * pasted log or JSON export) used to go through the Markdown pipeline in full
 * the moment its issue opened, which froze the page. Collapsing a long body
 * keeps it out of the renderer until the reader asks for it, and a body too
 * large to format at all expands as plain text.
 *
 * The thresholds count characters, so the timeline can apply them from data
 * (in-page find, row height estimates) exactly as the rendered card does.
 */

/** Bodies longer than this open collapsed. Roughly several screens of prose. */
export const LONG_COMMENT_CHARS = 10_000;

/**
 * Expanded bodies longer than this show as plain text instead of Markdown:
 * formatting them takes seconds and the result is no easier to read.
 */
export const PLAIN_TEXT_COMMENT_CHARS = 100_000;

const PREVIEW_MAX_CHARS = 2_000;
const PREVIEW_MAX_LINES = 30;

export function isLongComment(content: string | null | undefined): boolean {
  return !!content && content.length > LONG_COMMENT_CHARS;
}

/**
 * The opening of a long body: at most {@link PREVIEW_MAX_LINES} lines and
 * {@link PREVIEW_MAX_CHARS} characters, ending at a line break when one falls
 * in the second half of that window. A single huge line is cut hard.
 */
export function longCommentPreview(body: string): string {
  // Leading blank lines render as nothing; start the window at the first line
  // with text, keeping its indentation (an indented code block stays code).
  let content = body.replace(/^(?:[ \t]*\r?\n)+/, "");
  if (!content.slice(0, PREVIEW_MAX_CHARS).trim()) content = content.trimStart();
  let end = Math.min(content.length, PREVIEW_MAX_CHARS);
  let lines = 0;
  for (let index = content.indexOf("\n"); index !== -1 && index < end; index = content.indexOf("\n", index + 1)) {
    if (++lines === PREVIEW_MAX_LINES) {
      end = index;
      break;
    }
  }
  if (end < content.length && content[end] !== "\n") {
    const lineBreak = content.lastIndexOf("\n", end - 1);
    if (lineBreak >= PREVIEW_MAX_CHARS / 2) end = lineBreak;
  }
  // Never leave half of a surrogate pair at the cut.
  const last = content.charCodeAt(end - 1);
  if (last >= 0xd800 && last <= 0xdbff) end -= 1;
  return content.slice(0, end).trimEnd();
}

/** Whether an expanded body shows as plain text instead of Markdown. */
export function isPlainTextComment(content: string): boolean {
  return content.length > PLAIN_TEXT_COMMENT_CHARS;
}

/** The Markdown a comment card puts on screen for `content`. */
export function visibleCommentMarkdown(content: string, expanded: boolean): string {
  return expanded || !isLongComment(content) ? content : longCommentPreview(content);
}
