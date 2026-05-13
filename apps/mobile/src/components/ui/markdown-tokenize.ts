export type InlineToken =
  | { type: "text"; content: string }
  | { type: "code"; content: string }
  | { type: "bold"; content: string }
  | { type: "italic"; content: string }
  | { type: "mention"; content: string; mentionType: string; mentionId: string }
  | { type: "link"; content: string; href: string };

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

    const linkMatch = rest.match(/^\[([^\]]+)\]\((https?:\/\/[^)\s]+)\)/);
    if (linkMatch) {
      tokens.push({
        type: "link",
        content: linkMatch[1] ?? "",
        href: linkMatch[2] ?? "",
      });
      index += linkMatch[0].length;
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

function findNextSpecial(content: string, start: number) {
  const positions = ["`", "[", "*"]
    .map((char) => content.indexOf(char, start))
    .filter((position) => position >= 0);
  return positions.length > 0 ? Math.min(...positions) : content.length;
}
