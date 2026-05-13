import { memo, useMemo } from "react";
import { Linking, StyleSheet, Text, View } from "react-native";
import { colors, radii, spacing } from "../../theme/tokens";
import { tokenizeInline } from "./markdown-tokenize";

type MarkdownBlock =
  | { type: "code"; content: string }
  | { type: "heading"; level: number; content: string }
  | { type: "quote"; content: string }
  | { type: "list"; ordered: boolean; items: string[] }
  | { type: "paragraph"; content: string };

type MarkdownTextProps = {
  content: string;
  onIssueMentionPress?: (issueId: string) => void;
};

export const MarkdownText = memo(function MarkdownText({
  content,
  onIssueMentionPress,
}: MarkdownTextProps) {
  const blocks = useMemo(() => parseMarkdownBlocks(content), [content]);

  return (
    <View style={styles.root}>
      {blocks.map((block, index) => (
        <MarkdownBlockView
          block={block}
          key={`${block.type}-${index}`}
          onIssueMentionPress={onIssueMentionPress}
        />
      ))}
    </View>
  );
});

function MarkdownBlockView({
  block,
  onIssueMentionPress,
}: {
  block: MarkdownBlock;
  onIssueMentionPress?: (issueId: string) => void;
}) {
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
          {renderInline(block.content, onIssueMentionPress)}
        </Text>
      );
    case "quote":
      return (
        <View style={styles.quote}>
          <Text style={[styles.text, styles.quoteText]}>
            {renderInline(block.content, onIssueMentionPress)}
          </Text>
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
              <Text style={[styles.text, styles.listText]}>
                {renderInline(item, onIssueMentionPress)}
              </Text>
            </View>
          ))}
        </View>
      );
    case "paragraph":
      return <Text style={styles.text}>{renderInline(block.content, onIssueMentionPress)}</Text>;
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

function renderInline(content: string, onIssueMentionPress?: (issueId: string) => void) {
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
            {renderInline(token.content, onIssueMentionPress)}
          </Text>
        );
      case "italic":
        return (
          <Text key={index} style={styles.italic}>
            {renderInline(token.content, onIssueMentionPress)}
          </Text>
        );
      case "link":
        return (
          <Text
            key={index}
            onPress={() => void Linking.openURL(token.href)}
            style={styles.link}
          >
            {renderInline(token.content, onIssueMentionPress)}
          </Text>
        );
      case "mention":
        return (
          <Text
            key={index}
            onPress={
              token.mentionType === "issue" && onIssueMentionPress
                ? () => onIssueMentionPress(token.mentionId)
                : undefined
            }
            style={[styles.mention, token.mentionType === "issue" && styles.issueMention]}
          >
            {token.content}
          </Text>
        );
      case "text":
        return token.content;
    }
  });
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
  issueMention: {
    textDecorationLine: "underline",
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
