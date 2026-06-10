export interface PlainMentionTarget {
  id: string;
  name: string;
  type: "member" | "agent";
}

interface Range {
  start: number;
  end: number;
}

const LINK_RE = /\[[^\]]*](?:\([^)]*\))/g;
const FENCED_CODE_RE = /(^|\n)(```|~~~)[\s\S]*?\n\2(?=\n|$)/g;
const INLINE_CODE_RE = /`[^`\n]*`/g;

function collectProtectedRanges(text: string): Range[] {
  const ranges: Range[] = [];
  for (const re of [FENCED_CODE_RE, INLINE_CODE_RE, LINK_RE]) {
    re.lastIndex = 0;
    let match: RegExpExecArray | null;
    while ((match = re.exec(text))) {
      ranges.push({ start: match.index, end: match.index + match[0].length });
    }
  }
  return ranges.sort((a, b) => a.start - b.start);
}

function isProtected(index: number, ranges: Range[]): boolean {
  return ranges.some((range) => index >= range.start && index < range.end);
}

function isEmailLikePrefix(text: string, atIndex: number): boolean {
  const prev = text[atIndex - 1];
  return Boolean(prev && /[\p{L}\p{N}._%+-]/u.test(prev));
}

function hasWordSuffix(text: string, index: number): boolean {
  const next = text[index];
  return Boolean(next && /[\p{L}\p{N}_-]/u.test(next));
}

function escapeMentionLabel(label: string): string {
  return label.replace(/\\/g, "\\\\").replace(/\[/g, "\\[").replace(/]/g, "\\]");
}

export function normalizePlainMentions(
  text: string,
  targets: PlainMentionTarget[],
): string {
  if (!text || targets.length === 0 || !text.includes("@")) return text;

  const ranges = collectProtectedRanges(text);
  const sortedTargets = [...targets]
    .filter((target) => target.id && target.name.trim())
    .sort((a, b) => b.name.length - a.name.length);

  if (sortedTargets.length === 0) return text;

  let out = "";
  let i = 0;
  while (i < text.length) {
    if (
      text[i] !== "@" ||
      isProtected(i, ranges) ||
      text[i - 1] === "\\" ||
      isEmailLikePrefix(text, i)
    ) {
      out += text[i];
      i += 1;
      continue;
    }

    const match = sortedTargets.find((target) => {
      const start = i + 1;
      const end = start + target.name.length;
      return text.startsWith(target.name, start) && !hasWordSuffix(text, end);
    });

    if (!match) {
      out += text[i];
      i += 1;
      continue;
    }

    const label = escapeMentionLabel(match.name);
    out += `[@${label}](mention://${match.type}/${match.id})`;
    i += match.name.length + 1;
  }

  return out;
}
