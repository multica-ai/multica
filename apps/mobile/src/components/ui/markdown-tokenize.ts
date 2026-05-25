export type InlineToken =
  | { type: "text"; content: string }
  | { type: "code"; content: string }
  | { type: "bold"; content: string }
  | { type: "italic"; content: string }
  | { type: "mention"; content: string; mentionType: string; mentionId: string }
  | { type: "link"; content: string; href: string };

const trailingUrlPunctuation = new Set([".", ",", "!", "?", ";", ":", "，", "。", "！", "？", "；", "：", "、", ")", "）", "]"]);

export function tokenizeInline(content: string): InlineToken[] {
  const tokens: InlineToken[] = [];
  let index = 0;

  while (index < content.length) {
    const rest = content.slice(index);
    const codeEnd = rest.startsWith("`") ? rest.indexOf("`", 1) : -1;
    if (codeEnd > 0) {
      tokens.push({ type: "code", content: rest.slice(1, codeEnd) });
      index += codeEnd + 1;
      continue;
    }

    const markdownLink = parseMarkdownHttpLink(rest);
    if (markdownLink) {
      tokens.push({ type: "link", content: markdownLink.content, href: markdownLink.href });
      index += markdownLink.length;
      continue;
    }

    const mentionMatch = rest.match(/^\[(@?[^\]]+)\]\(mention:\/\/(all|member|agent|issue)\/([^)]+)\)/);
    if (mentionMatch) {
      tokens.push({
        type: "mention",
        content: mentionMatch[1] ?? "",
        mentionType: mentionMatch[2] ?? "",
        mentionId: mentionMatch[3] ?? "",
      });
      index += mentionMatch[0].length;
      continue;
    }

    const bareUrl = parseBareHttpUrl(rest);
    if (bareUrl) {
      tokens.push({ type: "link", content: bareUrl.href, href: bareUrl.href });
      if (bareUrl.trailing) {
        tokens.push({ type: "text", content: bareUrl.trailing });
      }
      index += bareUrl.length;
      continue;
    }

    if (rest.startsWith("**")) {
      const end = rest.indexOf("**", 2);
      if (end > 1) {
        tokens.push({ type: "bold", content: rest.slice(2, end) });
        index += end + 2;
        continue;
      }
    }

    if (rest.startsWith("*")) {
      const end = rest.indexOf("*", 1);
      if (end > 0) {
        tokens.push({ type: "italic", content: rest.slice(1, end) });
        index += end + 1;
        continue;
      }
    }

    const nextSpecial = findNextSpecial(content, index + 1);
    tokens.push({ type: "text", content: content.slice(index, nextSpecial) });
    index = nextSpecial;
  }

  return tokens;
}

function parseMarkdownHttpLink(content: string) {
  if (!content.startsWith("[")) {
    return null;
  }

  const textEnd = content.indexOf("](", 1);
  if (textEnd <= 1) {
    return null;
  }

  const hrefStart = textEnd + 2;
  if (!content.startsWith("http://", hrefStart) && !content.startsWith("https://", hrefStart)) {
    return null;
  }

  const hrefEnd = content.indexOf(")", hrefStart);
  if (hrefEnd < 0) {
    return null;
  }

  const href = content.slice(hrefStart, hrefEnd);
  if (hasUrlTerminator(href)) {
    return null;
  }

  return {
    content: content.slice(1, textEnd),
    href,
    length: hrefEnd + 1,
  };
}

function parseBareHttpUrl(content: string) {
  if (!content.startsWith("http://") && !content.startsWith("https://")) {
    return null;
  }

  let end = 0;
  while (end < content.length && !isBareUrlTerminator(content[end] ?? "")) {
    end += 1;
  }

  if (end === 0) {
    return null;
  }

  let hrefEnd = end;
  while (hrefEnd > 0 && trailingUrlPunctuation.has(content[hrefEnd - 1] ?? "")) {
    hrefEnd -= 1;
  }

  if (hrefEnd === 0) {
    return null;
  }

  return {
    href: content.slice(0, hrefEnd),
    trailing: content.slice(hrefEnd, end),
    length: end,
  };
}

function hasUrlTerminator(content: string) {
  for (const char of content) {
    if (isMarkdownLinkUrlTerminator(char)) {
      return true;
    }
  }
  return false;
}

function isMarkdownLinkUrlTerminator(char: string) {
  return char === "" || isWhitespace(char);
}

function isBareUrlTerminator(char: string) {
  return char === "" || char === "<" || char === ">" || char === "(" || char === ")" || isWhitespace(char);
}

function isWhitespace(char: string) {
  return char === " " || char === "\n" || char === "\r" || char === "\t" || char === "\f" || char === "\v";
}

function findNextSpecial(content: string, start: number) {
  const positions = ["`", "[", "*", "http://", "https://"]
    .map((marker) => content.indexOf(marker, start))
    .filter((position) => position >= 0);
  return positions.length > 0 ? Math.min(...positions) : content.length;
}
