// FIR-2810: pairing per-line attribution (kept per MARKDOWN line on the
// server) with the blocks the editor actually renders (paragraphs, headings,
// list items, code blocks). Blank markdown lines render nothing, a fenced code
// block renders as one <pre>, and markdown syntax (#, -, **, links) is not
// part of the rendered text — so the pairing strips syntax and matches by
// content, in document order. Anything it cannot confidently match simply gets
// no label; the gutter must never lie.

import type { NoteLineAttr } from "../core/types";

// stripMarkdownLine reduces one markdown source line to (roughly) the text the
// browser renders for it.
export function stripMarkdownLine(line: string): string {
  let s = line;
  s = s.replace(/^\s*>+\s?/, ""); // blockquote markers
  s = s.replace(/^\s*#{1,6}\s+/, ""); // heading hashes
  s = s.replace(/^\s*(?:[-*+]|\d+[.)])\s+/, ""); // list markers
  s = s.replace(/^\s*\[[ xX]\]\s+/, ""); // task checkbox
  s = s.replace(/!\[([^\]]*)\]\([^)]*\)/g, "$1"); // images → alt
  s = s.replace(/\[([^\]]*)\]\([^)]*\)/g, "$1"); // links → text
  s = s.replace(/(\*\*|__)(.*?)\1/g, "$2"); // bold
  s = s.replace(/(\*|_)(.*?)\1/g, "$2"); // italics
  s = s.replace(/~~(.*?)~~/g, "$1"); // strikethrough
  s = s.replace(/`([^`]*)`/g, "$1"); // inline code
  return s.trim();
}

// normalize collapses a rendered/stripped text to a comparison key.
function normalize(s: string): string {
  return s.toLowerCase().replace(/\s+/g, " ").trim();
}

// keysMatch: exact match, or one is a prefix of the other (a line that is
// mid-edit in the browser vs. its saved version, or truncated block text).
function keysMatch(a: string, b: string): boolean {
  if (a.length === 0 || b.length === 0) return a === b;
  if (a === b) return true;
  const shorter = a.length <= b.length ? a : b;
  const longer = a.length <= b.length ? b : a;
  return shorter.length >= 8 && longer.startsWith(shorter);
}

interface Candidate {
  key: string;
  attr: NoteLineAttr;
}

// candidatesFromMarkdown flattens a markdown body + aligned attrs into the
// ordered list of renderable entries (blank lines dropped, a fenced code block
// folded into one entry attributed to its opening line).
export function candidatesFromMarkdown(
  markdown: string,
  attrs: NoteLineAttr[],
): Candidate[] {
  const lines = markdown === "" ? [] : markdown.split("\n");
  const out: Candidate[] = [];
  let fence: { key: string[]; attr: NoteLineAttr } | null = null;
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i] ?? "";
    const attr = attrs[i] ?? {
      created_by: "",
      created_at: "",
      updated_by: "",
      updated_at: "",
    };
    const isFenceDelimiter = /^\s*(```|~~~)/.test(line);
    if (fence) {
      if (isFenceDelimiter) {
        out.push({ key: normalize(fence.key.join(" ")), attr: fence.attr });
        fence = null;
      } else {
        fence.key.push(line);
      }
      continue;
    }
    if (isFenceDelimiter) {
      fence = { key: [], attr };
      continue;
    }
    if (line.trim() === "") continue;
    out.push({ key: normalize(stripMarkdownLine(line)), attr });
  }
  if (fence) {
    // Unclosed fence: still one rendered block.
    out.push({ key: normalize(fence.key.join(" ")), attr: fence.attr });
  }
  return out;
}

// attrsForBlockTexts maps each rendered block's textContent to the attribution
// of the markdown line it came from. Both sequences are in document order, so
// this walks them with a cursor: each block takes the first matching candidate
// at or after the cursor; blocks with no match get null (no label) and do NOT
// advance the cursor, so one unmapped block never derails the rest.
export function attrsForBlockTexts(
  markdown: string,
  attrs: NoteLineAttr[],
  blockTexts: string[],
): (NoteLineAttr | null)[] {
  const candidates = candidatesFromMarkdown(markdown, attrs);
  const result: (NoteLineAttr | null)[] = [];
  let cursor = 0;
  for (const text of blockTexts) {
    const key = normalize(text);
    let match: Candidate | null = null;
    for (let i = cursor; i < candidates.length; i++) {
      const candidate = candidates[i];
      if (candidate && keysMatch(key, candidate.key)) {
        match = candidate;
        cursor = i + 1;
        break;
      }
    }
    result.push(match ? match.attr : null);
  }
  return result;
}
