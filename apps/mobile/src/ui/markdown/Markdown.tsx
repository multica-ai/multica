import { Fragment, useMemo, type ReactNode } from "react";
import { ScrollView, StyleSheet, Text, View, type TextStyle } from "react-native";
import { Icon } from "../icons/Icon";
import { type, useTheme, type Palette } from "../theme";
import { useMarkdownHost, type MarkdownHost } from "./host";
import { MarkdownImage } from "./MarkdownImage";
import { inlineText, parseMarkdown, type Block, type Inline, type ListItem } from "./parse";

interface MarkdownProps {
  source: string;
  /** Issue mentions open in the app instead of the browser. */
  onOpenIssue?: (issueId: string) => void;
  size?: "body" | "callout";
}

interface Context {
  palette: Palette;
  host: MarkdownHost;
  onOpenIssue?: (issueId: string) => void;
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
      case "strike":
        return (
          <Text key={k} style={styles.strike}>
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
          <Text key={k} style={{ color: ctx.palette.accent }} onPress={() => ctx.host.openLink(node.url)} accessibilityRole="link">
            {renderInline(node.children, ctx, `${k}.`)}
          </Text>
        );
      case "image":
        // Only reached inside a table cell or heading; paragraphs lay images out as blocks.
        return (
          <Text key={k} style={{ color: ctx.palette.accent }} onPress={() => ctx.host.openLink(node.url)} accessibilityRole="link">
            {node.alt || node.url}
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

/** A paragraph's text runs, with each image on a line of its own. */
function Paragraph({ content, ctx, textStyle }: { content: readonly Inline[]; ctx: Context; textStyle: TextStyle }) {
  const parts: ({ kind: "text"; nodes: Inline[] } | { kind: "image"; url: string; alt: string })[] = [];
  for (const node of content) {
    if (node.kind === "image") {
      parts.push({ kind: "image", url: node.url, alt: node.alt });
      continue;
    }
    const last = parts[parts.length - 1];
    if (last?.kind === "text") last.nodes.push(node);
    else parts.push({ kind: "text", nodes: [node] });
  }
  const shown = parts.filter((part) => part.kind === "image" || inlineText(part.nodes).trim() !== "");
  if (shown.length === 1 && shown[0]!.kind === "text") return <Text style={[textStyle, { color: ctx.palette.label }]}>{renderInline(shown[0]!.nodes, ctx)}</Text>;
  return (
    <View style={styles.paragraph}>
      {shown.map((part, index) =>
        part.kind === "image" ? (
          <MarkdownImage key={index} url={part.url} alt={part.alt} />
        ) : (
          <Text key={index} style={[textStyle, { color: ctx.palette.label }]}>
            {renderInline(trimRun(part.nodes), ctx)}
          </Text>
        ),
      )}
    </View>
  );
}

/** Drops the line breaks left either side of an image. */
function trimRun(nodes: readonly Inline[]): Inline[] {
  return nodes.map((node, index) => {
    if (node.kind !== "text") return node;
    let text = node.text;
    if (index === 0) text = text.replace(/^\s+/, "");
    if (index === nodes.length - 1) text = text.replace(/\s+$/, "");
    return { ...node, text };
  });
}

function TaskBox({ checked, palette }: { checked: boolean; palette: Palette }) {
  return (
    <View style={[styles.box, checked ? { backgroundColor: palette.label, borderColor: palette.label } : { borderColor: palette.label3 }]}>
      {checked ? <Icon name="Check" size={11} color={palette.background} strokeWidth={3} /> : null}
    </View>
  );
}

function ListView({ ordered, start, items, ctx, textStyle }: { ordered: boolean; start: number; items: readonly ListItem[]; ctx: Context; textStyle: TextStyle }) {
  return (
    <View style={styles.list}>
      {items.map((item, index) => (
        <View key={index} style={styles.item}>
          {item.checked !== null ? (
            <View style={[styles.marker, { height: textStyle.lineHeight }]}>
              <TaskBox checked={item.checked} palette={ctx.palette} />
            </View>
          ) : (
            <Text style={[textStyle, styles.marker, { color: ctx.palette.label2 }]}>{ordered ? `${start + index}.` : "•"}</Text>
          )}
          <Text style={[textStyle, { color: item.checked ? ctx.palette.label2 : ctx.palette.label }, styles.flex]}>{renderInline(item.content, ctx)}</Text>
        </View>
      ))}
    </View>
  );
}

/** Rough display width of a cell, so columns line up without measuring text. */
function cellWidth(content: readonly Inline[]): number {
  let units = 0;
  for (const ch of inlineText(content)) units += /[\u1100-\uffff]/.test(ch) ? 2 : 1;
  return Math.min(Math.max(units * 7.4 + 24, 64), 260);
}

function TableView({ block, ctx }: { block: Extract<Block, { kind: "table" }>; ctx: Context }) {
  const widths = block.head.map((cell, column) => Math.max(cellWidth(cell), ...block.rows.map((row) => cellWidth(row[column] ?? []))));
  const cell = (content: readonly Inline[], column: number, head: boolean) => (
    <View
      key={column}
      style={[styles.cell, { width: widths[column] }, column < widths.length - 1 && { borderRightWidth: StyleSheet.hairlineWidth, borderRightColor: ctx.palette.separator }]}
    >
      <Text style={[styles.cellText, head && styles.strong, { color: ctx.palette.label, textAlign: block.align[column] ?? "left" }]}>{renderInline(content, ctx)}</Text>
    </View>
  );
  return (
    <ScrollView horizontal showsHorizontalScrollIndicator={false} style={[styles.table, { borderColor: ctx.palette.separator }]}>
      <View>
        <View style={[styles.row, { backgroundColor: ctx.palette.grouped }]}>{block.head.map((content, column) => cell(content, column, true))}</View>
        {block.rows.map((row, index) => (
          <View key={index} style={[styles.row, { borderTopWidth: StyleSheet.hairlineWidth, borderTopColor: ctx.palette.separator }]}>
            {block.head.map((_, column) => cell(row[column] ?? [], column, false))}
          </View>
        ))}
      </View>
    </ScrollView>
  );
}

function BlockView({ block, ctx, textStyle }: { block: Block; ctx: Context; textStyle: TextStyle }) {
  const color = { color: ctx.palette.label };
  switch (block.kind) {
    case "heading":
      return <Text style={[block.level <= 2 ? styles.h2 : styles.h3, color]}>{renderInline(block.content, ctx)}</Text>;
    case "paragraph":
      return <Paragraph content={block.content} ctx={ctx} textStyle={textStyle} />;
    case "list":
      return <ListView ordered={block.ordered} start={block.start} items={block.items} ctx={ctx} textStyle={textStyle} />;
    case "table":
      return <TableView block={block} ctx={ctx} />;
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
  const host = useMarkdownHost();
  const blocks = useMemo(() => parseMarkdown(source), [source]);
  const ctx = { palette, host, onOpenIssue };
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
  paragraph: { gap: 8 },
  strong: { fontWeight: "600" },
  em: { fontStyle: "italic" },
  strike: { textDecorationLine: "line-through" },
  inlineCode: { fontFamily: "Menlo", fontSize: 13 },
  h2: { fontSize: 18, lineHeight: 24, fontWeight: "600", marginTop: 4 },
  h3: { fontSize: 16, lineHeight: 22, fontWeight: "600", marginTop: 2 },
  callout: { fontSize: 15, lineHeight: 22 },
  list: { gap: 4 },
  item: { flexDirection: "row", gap: 8 },
  marker: { minWidth: 14, justifyContent: "center" },
  box: { width: 15, height: 15, borderRadius: 4, borderWidth: 1.5, alignItems: "center", justifyContent: "center" },
  flex: { flex: 1 },
  table: { alignSelf: "flex-start", maxWidth: "100%", borderWidth: StyleSheet.hairlineWidth, borderRadius: 10, overflow: "hidden" },
  row: { flexDirection: "row" },
  cell: { paddingHorizontal: 10, paddingVertical: 8 },
  cellText: { fontSize: 14, lineHeight: 19 },
  codeBlock: { borderRadius: 12 },
  codeInner: { padding: 12 },
  codeText: { fontSize: 13, lineHeight: 19 },
  quote: { borderLeftWidth: 3, paddingLeft: 12, gap: 8 },
  rule: { height: StyleSheet.hairlineWidth, marginVertical: 4 },
});
