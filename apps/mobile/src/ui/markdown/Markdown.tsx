import { Fragment, useMemo, type ReactNode } from "react";
import { Linking, ScrollView, StyleSheet, Text, View } from "react-native";
import { type, useTheme, type Palette } from "../theme";
import { parseMarkdown, type Block, type Inline } from "./parse";

interface MarkdownProps {
  source: string;
  /** Issue mentions open in the app instead of the browser. */
  onOpenIssue?: (issueId: string) => void;
  size?: "body" | "callout";
}

interface Context {
  palette: Palette;
  onOpenIssue?: (issueId: string) => void;
}

function openUrl(url: string) {
  if (/^(https?|mailto):/i.test(url)) void Linking.openURL(url);
}

function renderInline(nodes: readonly Inline[], ctx: Context, key = ""): ReactNode[] {
  return nodes.map((node, index) => {
    const k = `${key}${index}`;
    switch (node.kind) {
      case "text":
        return <Fragment key={k}>{node.text}</Fragment>;
      case "strong":
        return (
          <Text key={k} style={styles.strong}>
            {renderInline(node.children, ctx, `${k}.`)}
          </Text>
        );
      case "em":
        return (
          <Text key={k} style={styles.em}>
            {renderInline(node.children, ctx, `${k}.`)}
          </Text>
        );
      case "code":
        return (
          <Text key={k} style={[styles.inlineCode, { backgroundColor: ctx.palette.fill }]}>
            {node.text}
          </Text>
        );
      case "link":
        return (
          <Text key={k} style={{ color: ctx.palette.accent }} onPress={() => openUrl(node.url)} accessibilityRole="link">
            {renderInline(node.children, ctx, `${k}.`)}
          </Text>
        );
      case "mention":
        if (node.type === "issue") {
          return (
            <Text key={k} style={[styles.strong, { color: ctx.palette.accent }]} onPress={() => ctx.onOpenIssue?.(node.id)} accessibilityRole="link">
              {node.label}
            </Text>
          );
        }
        return (
          <Text key={k} style={styles.strong}>
            @{node.label}
          </Text>
        );
    }
  });
}

function BlockView({ block, ctx, textStyle }: { block: Block; ctx: Context; textStyle: object }) {
  const color = { color: ctx.palette.label };
  switch (block.kind) {
    case "heading":
      return <Text style={[block.level <= 2 ? styles.h2 : styles.h3, color]}>{renderInline(block.content, ctx)}</Text>;
    case "paragraph":
      return <Text style={[textStyle, color]}>{renderInline(block.content, ctx)}</Text>;
    case "list":
      return (
        <View style={styles.list}>
          {block.items.map((item, index) => (
            <View key={index} style={styles.item}>
              <Text style={[textStyle, styles.marker, { color: ctx.palette.label2 }]}>{block.ordered ? `${block.start + index}.` : "•"}</Text>
              <Text style={[textStyle, color, styles.flex]}>{renderInline(item, ctx)}</Text>
            </View>
          ))}
        </View>
      );
    case "code":
      return (
        <ScrollView horizontal style={[styles.codeBlock, { backgroundColor: ctx.palette.grouped }]} contentContainerStyle={styles.codeInner}>
          <Text style={[type.mono, styles.codeText, color]}>{block.text}</Text>
        </ScrollView>
      );
    case "quote":
      return (
        <View style={[styles.quote, { borderLeftColor: ctx.palette.fillStrong }]}>
          {block.blocks.map((child, index) => (
            <BlockView key={index} block={child} ctx={{ ...ctx, palette: { ...ctx.palette, label: ctx.palette.label2 } }} textStyle={textStyle} />
          ))}
        </View>
      );
    case "rule":
      return <View style={[styles.rule, { backgroundColor: ctx.palette.separator }]} />;
  }
}

/** Renders issue and chat Markdown with the app's type ramp. */
export function Markdown({ source, onOpenIssue, size = "body" }: MarkdownProps) {
  const palette = useTheme();
  const blocks = useMemo(() => parseMarkdown(source), [source]);
  const ctx = { palette, onOpenIssue };
  return (
    <View style={styles.root}>
      {blocks.map((block, index) => (
        <BlockView key={index} block={block} ctx={ctx} textStyle={size === "body" ? type.body : styles.callout} />
      ))}
    </View>
  );
}

const styles = StyleSheet.create({
  root: { gap: 10 },
  strong: { fontWeight: "600" },
  em: { fontStyle: "italic" },
  inlineCode: { fontFamily: "Menlo", fontSize: 14 },
  h2: { fontSize: 18, lineHeight: 24, fontWeight: "600", marginTop: 4 },
  h3: { fontSize: 16, lineHeight: 22, fontWeight: "600", marginTop: 2 },
  callout: { fontSize: 15, lineHeight: 22 },
  list: { gap: 4 },
  item: { flexDirection: "row", gap: 8 },
  marker: { minWidth: 14 },
  flex: { flex: 1 },
  codeBlock: { borderRadius: 12 },
  codeInner: { padding: 12 },
  codeText: { fontSize: 13, lineHeight: 19 },
  quote: { borderLeftWidth: 3, paddingLeft: 12, gap: 8 },
  rule: { height: StyleSheet.hairlineWidth, marginVertical: 4 },
});
