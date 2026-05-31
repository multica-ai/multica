import {
  FILE_CARD_URL_PATTERN,
  isAllowedFileCardHref,
} from "@multica/ui/markdown/file-cards";

const FILE_CARD_LINE_RE = new RegExp(
  `^!file\\[([^\\]]*)\\]\\((${FILE_CARD_URL_PATTERN.source})\\)$`,
);

const INLINE_FILE_LINK_TITLE = "multica-file";
const INLINE_FILE_RE = new RegExp(
  `!file\\[([^\\]]*)\\]\\((${FILE_CARD_URL_PATTERN.source})\\)`,
  "g",
);
const STANDALONE_IMAGE_RE = /^!\[[^\]]*\]\([^)]+\)$/;

const MOBILE_FILE_CARD_RE =
  /^<div data-type="fileCard" data-href="([^"]*)" data-filename="([^"]*)"><\/div>$/;

export type MobileFileCard = {
  filename: string;
  href: string;
};

export type MobileIssueLinkTarget = {
  commentId?: string;
  issueId: string;
  workspaceSlug: string;
};

export function preprocessMobileMarkdown(content: string): string {
  const normalized = content.replace(/\r\n/g, "\n");
  const lines = normalized.split("\n");
  const skipRanges = findCodeRanges(normalized);
  const processedLines: string[] = [];
  let offset = 0;

  lines.forEach((line, index) => {
    const start = offset;
    offset += line.length + 1;

    if (isRangeInsideCode(skipRanges, start, start + line.length)) {
      processedLines.push(line);
      return;
    }

    const card = parseFileCardLine(line.trim());
    if (card) {
      processedLines.push(`<div data-type="fileCard" data-href="${escapeAttr(card.href)}" data-filename="${escapeAttr(card.filename)}"></div>`);
      return;
    }

    const value = preprocessInlineFileLinks(line, skipRanges, start);
    if (isStandaloneImageLine(value.trim())) {
      if (processedLines.length > 0 && processedLines.at(-1)?.trim()) {
        processedLines.push("");
      }
      processedLines.push(value);
      if (lines[index + 1]?.trim()) {
        processedLines.push("");
      }
      return;
    }

    processedLines.push(value);
  });

  return processedLines.join("\n");
}

export function parseFileCardLine(line: string): MobileFileCard | null {
  const match = line.match(FILE_CARD_LINE_RE);
  if (!match) return null;

  const filename = match[1] ?? "";
  const href = match[2] ?? "";
  if (!isAllowedFileCardHref(href)) return null;

  return { filename, href };
}

export function parseMobileFileCardHtml(raw: string): MobileFileCard | null {
  const match = raw.trim().match(MOBILE_FILE_CARD_RE);
  if (!match) return null;

  const href = unescapeAttr(match[1] ?? "");
  if (!isAllowedFileCardHref(href)) return null;

  return {
    href,
    filename: unescapeAttr(match[2] ?? "") || "file",
  };
}

export function resolveMobileFileCardUrl(href: string, apiBaseUrl: string): string | null {
  if (/^https?:\/\//i.test(href)) return href;
  if (!href.startsWith("/uploads/")) return null;

  return `${apiBaseUrl.replace(/\/$/, "")}${href}`;
}

export function getIssueMentionId(href: string): string | null {
  const match = href.match(/^mention:\/\/issue\/(.+)$/);
  if (!match?.[1]) return null;

  try {
    return decodeURIComponent(match[1]);
  } catch {
    return match[1];
  }
}

export function parseMobileIssueLink(
  href: string,
  allowedBaseUrls: readonly (string | undefined)[],
): MobileIssueLinkTarget | null {
  let parsed: URL;
  try {
    parsed = new URL(href);
  } catch {
    return null;
  }

  if (!/^https?:$/i.test(parsed.protocol)) return null;

  const allowedOrigins = new Set<string>();
  for (const baseUrl of allowedBaseUrls) {
    if (!baseUrl) continue;
    try {
      allowedOrigins.add(new URL(baseUrl).origin);
    } catch {
      // Ignore invalid runtime config values.
    }
  }
  if (!allowedOrigins.has(parsed.origin)) return null;

  const parts = parsed.pathname.split("/").filter(Boolean);
  if (parts.length !== 3 || parts[1] !== "issues") return null;

  const workspaceSlug = safeDecodeURIComponent(parts[0] ?? "");
  const issueId = safeDecodeURIComponent(parts[2] ?? "");
  if (!workspaceSlug || !issueId) return null;

  const commentId = parsed.searchParams.get("comment")?.trim() || undefined;
  return { workspaceSlug, issueId, commentId };
}

export function isMentionHref(href: string): boolean {
  return /^mention:\/\/(all|member|agent|squad|issue)\/.+/.test(href);
}

export function isSafeHttpUrl(href: string): boolean {
  return /^https?:\/\//i.test(href);
}

export function isInlineFileLinkTitle(title?: string): boolean {
  return title === INLINE_FILE_LINK_TITLE;
}

function preprocessInlineFileLinks(
  line: string,
  skipRanges: readonly CodeRange[],
  lineStart: number,
): string {
  return line.replace(INLINE_FILE_RE, (match, filename: string, href: string, index: number) => {
    const start = lineStart + index;
    const end = start + match.length;
    if (isRangeInsideCode(skipRanges, start, end) || !isAllowedFileCardHref(href)) {
      return match;
    }

    return `[${filename}](${href} "${INLINE_FILE_LINK_TITLE}")`;
  });
}

function isStandaloneImageLine(line: string): boolean {
  return STANDALONE_IMAGE_RE.test(line) && !line.startsWith("!file[");
}

type CodeRange = {
  start: number;
  end: number;
};

function findCodeRanges(content: string): CodeRange[] {
  const ranges: CodeRange[] = [];
  const normalized = content.replace(/\r\n/g, "\n");
  let inFence = false;
  let fenceStart = 0;
  let lineStart = 0;

  for (const line of normalized.split("\n")) {
    if (/^\s*```/.test(line)) {
      if (inFence) {
        ranges.push({ start: fenceStart, end: lineStart + line.length });
        inFence = false;
      } else {
        inFence = true;
        fenceStart = lineStart;
      }
    }

    if (!inFence) {
      findInlineCodeRanges(line, lineStart, ranges);
    }

    lineStart += line.length + 1;
  }

  if (inFence) {
    ranges.push({ start: fenceStart, end: normalized.length });
  }

  return ranges;
}

function findInlineCodeRanges(line: string, lineStart: number, ranges: CodeRange[]): void {
  let index = 0;
  while (index < line.length) {
    const start = line.indexOf("`", index);
    if (start === -1) return;

    const end = line.indexOf("`", start + 1);
    if (end === -1) return;

    ranges.push({ start: lineStart + start, end: lineStart + end + 1 });
    index = end + 1;
  }
}

function isRangeInsideCode(
  ranges: readonly CodeRange[],
  start: number,
  end: number,
): boolean {
  return ranges.some((range) => start >= range.start && end <= range.end);
}

function escapeAttr(value: string): string {
  return value.replace(/&/g, "&amp;").replace(/"/g, "&quot;").replace(/</g, "&lt;");
}

function unescapeAttr(value: string): string {
  return value
    .replace(/&lt;/g, "<")
    .replace(/&quot;/g, '"')
    .replace(/&amp;/g, "&");
}

function safeDecodeURIComponent(value: string): string {
  try {
    return decodeURIComponent(value);
  } catch {
    return value;
  }
}
