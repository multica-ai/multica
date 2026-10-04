/**
 * In-house prose renderer — the HarmonyOS stand-in for
 * react-native-enriched-markdown (native md4c; no RNOH port, see README
 * roadmap "markdown rendering" item). Renders marked.lexer tokens as RN
 * <Text>/<View> trees using the HIG-calibrated styles from
 * markdown-style.ts, matching the enriched styling the iOS app passes in.
 *
 * Handles: paragraphs, headings h1-h6, ordered/unordered/task lists
 * (nested), blockquotes (nested), tables, thematic breaks, inline code,
 * links (mention:// aware), emphasis/strikethrough, hard breaks. Code
 * blocks at the TOP level never reach this renderer (split-markdown
 * routes them to CodeBlock); list-nested code renders as a plain box.
 */
import React from "react";
import { Linking, Text, View } from "react-native";
import { marked, type Tokens } from "marked";
import type { MarkdownStyles } from "@/lib/markdown/markdown-style";

type Token = Record<string, unknown> & { type: string; raw?: string };

export type ProseLinkPress = (url: string) => void;

export function renderProse(
  content: string,
  styles: MarkdownStyles,
  onLinkPress?: ProseLinkPress,
  selectable = true,
): React.ReactNode {
  const tokens = marked.lexer(content) as unknown as Token[];
  return (
    <>
      {tokens.map((token, i) => (
        <BlockToken
          key={i}
          token={token}
          styles={styles}
          onLinkPress={onLinkPress}
          selectable={selectable}
        />
      ))}
    </>
  );
}

function BlockToken({
  token,
  styles,
  onLinkPress,
  selectable,
}: {
  token: Token;
  styles: MarkdownStyles;
  onLinkPress?: ProseLinkPress;
  selectable: boolean;
}) {
  switch (token.type) {
    case "paragraph":
      return (
        <Text style={styles.paragraph} selectable={selectable}>
          {renderInline((token.tokens ?? []) as Token[], styles, onLinkPress, selectable)}
        </Text>
      );
    case "heading": {
      const t = token as unknown as Tokens.Heading;
      const depth = Math.min(Math.max(t.depth, 1), 6);
      const style =
        depth === 1 ? styles.h1
        : depth === 2 ? styles.h2
        : depth === 3 ? styles.h3
        : depth === 4 ? styles.h4
        : depth === 5 ? styles.h5
        : styles.h6;
      return (
        <Text style={style} selectable={selectable}>
          {renderInline((token.tokens ?? []) as Token[], styles, onLinkPress, selectable)}
        </Text>
      );
    }
    case "list":
      return (
        <ListBlock
          token={token as unknown as Tokens.List}
          styles={styles}
          onLinkPress={onLinkPress}
          selectable={selectable}
        />
      );
    case "blockquote":
      return (
        <View style={styles.blockquote}>
          {((token.tokens ?? []) as Token[]).map((child, i) => (
            <BlockToken
              key={i}
              token={child}
              styles={styles}
              onLinkPress={onLinkPress}
              selectable={selectable}
            />
          ))}
        </View>
      );
    case "code": {
      // List-nested code (top-level code is split out before prose).
      const t = token as unknown as Tokens.Code;
      return (
        <View style={styles.codeBlockBox}>
          <Text style={styles.codeBlockText} selectable={selectable}>
            {t.text}
          </Text>
        </View>
      );
    }
    case "table": {
      const t = token as unknown as Tokens.Table;
      return (
        <View style={styles.table}>
          <View style={styles.tableHeaderRow}>
            {t.header.map((cell, i) => (
              <Text key={i} style={[styles.tableCell, styles.tableHeaderText]} selectable={selectable}>
                {renderInline((cell.tokens ?? []) as Token[], styles, onLinkPress, selectable)}
              </Text>
            ))}
          </View>
          {t.rows.map((row, r) => (
            <View key={r} style={styles.tableRow}>
              {row.map((cell, i) => (
                <Text key={i} style={styles.tableCell} selectable={selectable}>
                  {renderInline((cell.tokens ?? []) as Token[], styles, onLinkPress, selectable)}
                </Text>
              ))}
            </View>
          ))}
        </View>
      );
    }
    case "hr":
      return <View style={styles.hr} />;
    case "space":
    case "def":
    case "html":
      return null;
    default:
      // Unknown block — render its raw text so content is never dropped.
      return token.raw ? (
        <Text style={styles.paragraph} selectable={selectable}>
          {token.raw}
        </Text>
      ) : null;
  }
}

function ListBlock({
  token,
  styles,
  onLinkPress,
  selectable,
}: {
  token: Tokens.List;
  styles: MarkdownStyles;
  onLinkPress?: ProseLinkPress;
  selectable: boolean;
}) {
  return (
    <View>
      {token.items.map((item, i) => {
        const marker = token.ordered ? `${Number(token.start ?? 1) + i}.` : "•";
        const first = (item.tokens ?? [])[0] as Token | undefined;
        const firstIsText =
          !!first && first.type === "text" && (item.tokens ?? []).length === 1;
        return (
          <View key={i} style={styles.listRow}>
            {item.task ? (
              <View
                style={[
                  styles.taskCheckbox,
                  {
                    borderColor: item.checked ? markerCheckedColor(styles) : markerUncheckedColor(styles),
                    backgroundColor: item.checked ? markerCheckedColor(styles) : "transparent",
                    marginRight: 4,
                  },
                ]}
              >
                {item.checked ? (
                  <Text style={{ color: "#fff", fontSize: 11, fontWeight: "700", lineHeight: 14 }}>
                    ✓
                  </Text>
                ) : null}
              </View>
            ) : (
              <Text style={styles.listMarker} selectable={selectable}>
                {marker}
              </Text>
            )}
            <View style={{ flex: 1 }}>
              {firstIsText ? (
                <Text
                  style={[
                    styles.listText,
                    item.task && item.checked ? styles.taskTextChecked : null,
                  ]}
                  selectable={selectable}
                >
                  {renderInline((first.tokens ?? []) as Token[], styles, onLinkPress, selectable)}
                </Text>
              ) : (
                ((item.tokens ?? []) as Token[]).map((child, j) => (
                  <View key={j} style={j > 0 ? styles.listNested : undefined}>
                    <BlockToken
                      token={child}
                      styles={styles}
                      onLinkPress={onLinkPress}
                      selectable={selectable}
                    />
                  </View>
                ))
              )}
            </View>
          </View>
        );
      })}
    </View>
  );
}

function markerCheckedColor(styles: MarkdownStyles): string {
  // Brand color from the link style — same token the iOS taskList uses for
  // checkedColor (checkedTextColor is mutedForeground there).
  return (styles.link as { color: string }).color;
}

function markerUncheckedColor(styles: MarkdownStyles): string {
  return (styles.hr as { backgroundColor: string }).backgroundColor;
}

function renderInline(
  tokens: Token[],
  styles: MarkdownStyles,
  onLinkPress?: ProseLinkPress,
  selectable = true,
): React.ReactNode {
  return tokens.map((token, i) => {
    switch (token.type) {
      case "text": {
        const inner = token.tokens as Token[] | undefined;
        return inner && inner.length > 0
          ? renderInline(inner, styles, onLinkPress, selectable)
          : ((token.raw ?? (token as { text?: string }).text) ?? "");
      }
      case "escape":
        return token.raw ?? "";
      case "strong":
        return (
          <Text key={i} style={styles.strong}>
            {renderInline((token.tokens ?? []) as Token[], styles, onLinkPress, selectable)}
          </Text>
        );
      case "em":
        return (
          <Text key={i} style={styles.em}>
            {renderInline((token.tokens ?? []) as Token[], styles, onLinkPress, selectable)}
          </Text>
        );
      case "del":
        return (
          <Text key={i} style={styles.del}>
            {renderInline((token.tokens ?? []) as Token[], styles, onLinkPress, selectable)}
          </Text>
        );
      case "codespan":
        return (
          <Text key={i} style={styles.inlineCode}>
            {(token as unknown as Tokens.Codespan).text}
          </Text>
        );
      case "br":
        return "\n";
      case "link": {
        const t = token as unknown as Tokens.Link;
        const href = t.href;
        return (
          <Text
            key={i}
            style={styles.link}
            onPress={() => handleLink(href, onLinkPress)}
            suppressHighlighting
          >
            {renderInline((token.tokens ?? []) as Token[], styles, onLinkPress, selectable)}
          </Text>
        );
      }
      case "image": {
        // Inline images inside list items / running text: the splitter
        // promotes top-level ones to tappable blocks; here fall back to an
        // alt-text link so nothing disappears silently.
        const t = token as unknown as Tokens.Image;
        return (
          <Text
            key={i}
            style={styles.link}
            onPress={() => handleLink(t.href, onLinkPress)}
          >
            {t.text ? `🖼 ${t.text}` : "🖼 image"}
          </Text>
        );
      }
      default:
        return token.raw ?? "";
    }
  });
}

function handleLink(url: string, onLinkPress?: ProseLinkPress) {
  if (onLinkPress) {
    onLinkPress(url);
    return;
  }
  if (url.startsWith("mention://")) return;
  Linking.openURL(url).catch(() => {
    // Silent: failing loudly is worse than a no-op tap.
  });
}
