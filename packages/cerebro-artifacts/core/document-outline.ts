// TECH-3529 bølge 2: pure helpers for the document outline (heading navigation)
// and the word/character count shown on the document view. Pure functions so
// they are trivially unit-testable and shared by web + desktop.

export interface OutlineHeading {
  level: number; // 1–6
  text: string;
  index: number; // position among all headings, used to scroll to the Nth heading
}

const FENCE = /^```/;
const ATX_HEADING = /^(#{1,6})\s+(.+?)\s*#*\s*$/;

/**
 * Extracts ATX markdown headings (`#`–`######`) in document order, skipping
 * anything inside fenced code blocks so a `# comment` in a code sample is not
 * mistaken for a heading. Returns one entry per heading with its running index.
 */
export function parseOutline(markdown: string): OutlineHeading[] {
  const out: OutlineHeading[] = [];
  let inFence = false;
  let index = 0;
  for (const rawLine of markdown.split("\n")) {
    const line = rawLine.trimEnd();
    if (FENCE.test(line.trimStart())) {
      inFence = !inFence;
      continue;
    }
    if (inFence) continue;
    const m = ATX_HEADING.exec(line);
    if (!m || m[1] === undefined || m[2] === undefined) continue;
    out.push({ level: m[1].length, text: m[2].trim(), index: index++ });
  }
  return out;
}

export interface DocumentCounts {
  words: number;
  characters: number;
}

// Strips the most common markdown syntax so the word/char count reflects the
// readable text rather than the raw markup.
function stripMarkdown(markdown: string): string {
  return markdown
    .replace(/```[\s\S]*?```/g, " ") // fenced code blocks
    .replace(/`[^`]*`/g, " ") // inline code
    .replace(/!\[[^\]]*\]\([^)]*\)/g, " ") // images
    .replace(/\[([^\]]*)\]\([^)]*\)/g, "$1") // links → link text
    .replace(/^#{1,6}\s+/gm, "") // heading markers
    .replace(/^[\s>*+-]+/gm, " ") // list/quote markers
    .replace(/[*_~]/g, "") // emphasis markers
    .replace(/\|/g, " "); // table pipes
}

/**
 * Counts readable words and characters in a markdown document. Characters count
 * the visible text (markup stripped, whitespace excluded); words split on
 * whitespace runs.
 */
export function countDocument(markdown: string): DocumentCounts {
  const text = stripMarkdown(markdown).trim();
  if (text === "") return { words: 0, characters: 0 };
  const words = text.split(/\s+/).filter(Boolean).length;
  const characters = text.replace(/\s/g, "").length;
  return { words, characters };
}
