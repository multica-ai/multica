import { memo, useMemo } from "react";
import { Linking, StyleSheet, Text, View } from "react-native";
import { colors, radii, spacing } from "../../theme/tokens";

type MarkdownBlock =
  | { type: "code"; content: string }
  | { type: "heading"; level: number; content: string }
  | { type: "quote"; content: string }
  | { type: "list"; ordered: boolean; items: string[] }
  | { type: "paragraph"; content: string };

type InlineToken =
  | { type: "text"; content: string }
  | { type: "code"; content: string }
  | { type: "bold"; content: string }
  | { type: "italic"; content: string }
  | { type: "mention"; content: string }
  | { type: "link"; content: string; href: string };

export const MarkdownText = memo(function MarkdownText({ content }: { content: string }) {
  const blocks = useMemo(() => parseMarkdownBlocks(content), [content]);

  return (
    <View style={styles.root}>
      {blocks.map((block, index) => (
        <MarkdownBlockView block={block} key={`${block.type}-${index}`} />
      ))}
    </View>
  );
});

function MarkdownBlockView({ block }: { block: MarkdownBlock }) {
  switch (block.type) {
    case "code":
      return (
        <View style={styles.codeBlock}>
          <Text style={styles.codeBlockText}>{block.content}</Text>
        </View>
      );
    case "heading":
      return (
        <Text style={[styles.text, styles.heading, block.level >= 3 && styles.smallHeading]}>
          {renderInline(block.content)}
        </Text>
      );
    case "quote":
      return (
        <View style={styles.quote}>
          <Text style={[styles.text, styles.quoteText]}>{renderInline(block.content)}</Text>
        </View>
      );
    case "list":
      return (
        <View style={styles.list}>
          {block.items.map((item, index) => (
            <View key={`${item}-${index}`} style={styles.listItem}>
              <Text style={styles.listMarker}>
                {block.ordered ? `${index + 1}.` : "•"}
              </Text>
              <Text style={[styles.text, styles.listText]}>{renderInline(item)}</Text>
            </View>
          ))}
        </View>
      );
    case "paragraph":
      return <Text style={styles.text}>{renderInline(block.content)}</Text>;
  }
}

function parseMarkdownBlocks(content: string): MarkdownBlock[] {
  const normalized = content.replace(/\r\n/g, "\n").trim();
  if (!normalized) return [];

  const lines = normalized.split("\n");
  const blocks: MarkdownBlock[] = [];
  let index = 0;

  while (index < lines.length) {
    const line = lines[index] ?? "";
    if (!line.trim()) {
      index += 1;
      continue;
    }

    if (line.trim().startsWith("```")) {
      const codeLines: string[] = [];
      index += 1;
      while (index < lines.length && !(lines[index] ?? "").trim().startsWith("```")) {
        codeLines.push(lines[index] ?? "");
        index += 1;
      }
      if (index < lines.length) index += 1;
      blocks.push({ type: "code", content: codeLines.join("\n") });
      continue;
    }

    const headingMatch = line.match(/^(#{1,6})\s+(.+)$/);
    if (headingMatch) {
      const marker = headingMatch[1] ?? "";
      const headingContent = headingMatch[2] ?? "";
      blocks.push({
        type: "heading",
        level: marker.length,
        content: headingContent,
      });
      index += 1;
      continue;
    }

    if (line.trimStart().startsWith(">")) {
      const quoteLines: string[] = [];
      while (index < lines.length && (lines[index] ?? "").trimStart().startsWith(">")) {
        quoteLines.push((lines[index] ?? "").trimStart().replace(/^>\s?/, ""));
        index += 1;
      }
      blocks.push({ type: "quote", content: quoteLines.join("\n") });
      continue;
    }

    const unorderedMatch = line.match(/^\s*[-*]\s+(.+)$/);
    const orderedMatch = line.match(/^\s*\d+[.)]\s+(.+)$/);
    if (unorderedMatch || orderedMatch) {
      const ordered = Boolean(orderedMatch);
      const items: string[] = [];
      while (index < lines.length) {
        const current = lines[index] ?? "";
        const match = ordered
          ? current.match(/^\s*\d+[.)]\s+(.+)$/)
          : current.match(/^\s*[-*]\s+(.+)$/);
        if (!match) break;
        items.push(match[1] ?? "");
        index += 1;
      }
      blocks.push({ type: "list", ordered, items });
      continue;
    }

    const paragraphLines: string[] = [];
    while (index < lines.length) {
      const current = lines[index] ?? "";
      if (!current.trim()) break;
      if (
        current.trim().startsWith("```") ||
        current.match(/^(#{1,6})\s+(.+)$/) ||
        current.trimStart().startsWith(">") ||
        current.match(/^\s*[-*]\s+(.+)$/) ||
        current.match(/^\s*\d+[.)]\s+(.+)$/)
      ) {
        break;
      }
      paragraphLines.push(current);
      index += 1;
    }
    blocks.push({ type: "paragraph", content: paragraphLines.join("\n") });
  }

  return blocks;
}

function renderInline(content: string) {
  return tokenizeInline(content).map((token, index) => {
    switch (token.type) {
      case "code":
        return (
          <Text key={index} style={styles.inlineCode}>
            {token.content}
          </Text>
        );
      case "bold":
        return (
          <Text key={index} style={styles.bold}>
            {renderInline(token.content)}
          </Text>
        );
      case "italic":
        return (
          <Text key={index} style={styles.italic}>
            {renderInline(token.content)}
          </Text>
        );
      case "link":
        return (
          <Text
            key={index}
            onPress={() => void Linking.openURL(token.href)}
            style={styles.link}
          >
            {renderInline(token.content)}
          </Text>
        );
      case "mention":
        return (
          <Text key={index} style={styles.mention}>
            {token.content}
          </Text>
        );
      case "text":
        return token.content;
    }
  });
}

function tokenizeInline(content: string): InlineToken[] {
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

    const mentionMatch = rest.match(/^\[(@?[^\]]+)\]\(mention:\/\/(?:all|member|agent)\/[^)\s]+\)/);
    if (mentionMatch) {
      tokens.push({ type: "mention", content: mentionMatch[1] ?? "" });
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

const styles = StyleSheet.create({
  root: {
    gap: spacing.sm,
  },
  text: {
    color: colors.foreground,
    fontSize: 14,
    lineHeight: 20,
  },
  heading: {
    fontSize: 16,
    fontWeight: "600",
    lineHeight: 22,
  },
  smallHeading: {
    fontSize: 14,
    lineHeight: 20,
  },
  bold: {
    fontWeight: "700",
  },
  italic: {
    fontStyle: "italic",
  },
  link: {
    color: colors.info,
    textDecorationLine: "underline",
  },
  mention: {
    backgroundColor: colors.muted,
    borderRadius: radii.sm,
    color: colors.info,
    fontWeight: "600",
  },
  inlineCode: {
    backgroundColor: colors.muted,
    borderRadius: radii.sm,
    color: colors.foreground,
    fontFamily: "Courier",
    fontSize: 13,
  },
  codeBlock: {
    backgroundColor: colors.muted,
    borderRadius: radii.md,
    padding: spacing.md,
  },
  codeBlockText: {
    color: colors.foreground,
    fontFamily: "Courier",
    fontSize: 13,
    lineHeight: 18,
  },
  quote: {
    borderLeftColor: colors.border,
    borderLeftWidth: 3,
    paddingLeft: spacing.md,
  },
  quoteText: {
    color: colors.mutedForeground,
  },
  list: {
    gap: spacing.xs,
  },
  listItem: {
    alignItems: "flex-start",
    flexDirection: "row",
    gap: spacing.sm,
  },
  listMarker: {
    color: colors.mutedForeground,
    fontSize: 14,
    lineHeight: 20,
    minWidth: 18,
  },
  listText: {
    flex: 1,
  },
});
