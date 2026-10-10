/**
 * HarmonyOS port of apps/mobile/lib/markdown/markdown-style.ts. The iOS
 * file feeds enriched-markdown's imperative style object; here the same
 * HIG-calibrated constants drive the in-house prose renderer (prose.tsx).
 * Values MUST stay in sync with the iOS file — the scale (MD_FONT /
 * MD_LINE / MD_GAP) was calibrated against Apple HIG Dynamic Type and
 * cross-checked against GitHub Mobile / Linear iOS.
 */
import { useMemo } from "react";
import { StyleSheet, type TextStyle, type ViewStyle } from "react-native";
import { MOBILE_RADIUS } from "@/lib/radius";
import { useThemeColors } from "@/lib/use-theme-colors";

const MD_FONT = {
  body: 14,
  h1: 20,
  h2: 18,
  h3: 16,
  h4: 15,
  h5: 14,
  h6: 13,
  // Mirrors the in-house CodeBlock's 13px so top-level fenced code and
  // list-nested code look identical.
  codeBlock: 13,
} as const;

const MD_LINE = {
  body: 24,
  h1: 28,
  h2: 24,
  h3: 22,
  h4: 22,
  h5: 20,
  h6: 19,
} as const;

const MD_GAP = {
  paragraph: 12,
  headingTopLarge: 16,
  headingTopSmall: 12,
  headingBottomLarge: 8,
  headingBottomSmall: 6,
} as const;

export type MarkdownStyles = ReturnType<typeof useMarkdownStyles>;

export function useMarkdownStyles(compact = false) {
  const t = useThemeColors();

  return useMemo(
    () => ({
      font: MD_FONT,
      paragraph: {
        fontSize: MD_FONT.body,
        lineHeight: MD_LINE.body,
        color: t.foreground,
        marginBottom: compact ? 0 : MD_GAP.paragraph,
        // compact tightens the line box so single-paragraph bubbles center.
        ...(compact ? { lineHeight: 20 } : {}),
      } as TextStyle,
      h1: {
        fontSize: MD_FONT.h1,
        lineHeight: MD_LINE.h1,
        fontWeight: "700",
        color: t.foreground,
        marginTop: MD_GAP.headingTopLarge,
        marginBottom: MD_GAP.headingBottomLarge,
      } as TextStyle,
      h2: {
        fontSize: MD_FONT.h2,
        lineHeight: MD_LINE.h2,
        fontWeight: "600",
        color: t.foreground,
        marginTop: MD_GAP.headingTopLarge,
        marginBottom: MD_GAP.headingBottomLarge,
      } as TextStyle,
      h3: {
        fontSize: MD_FONT.h3,
        lineHeight: MD_LINE.h3,
        fontWeight: "600",
        color: t.foreground,
        marginTop: MD_GAP.headingTopSmall,
        marginBottom: MD_GAP.headingBottomSmall,
      } as TextStyle,
      h4: {
        fontSize: MD_FONT.h4,
        lineHeight: MD_LINE.h4,
        fontWeight: "600",
        color: t.foreground,
        marginTop: MD_GAP.headingTopSmall,
        marginBottom: MD_GAP.headingBottomSmall,
      } as TextStyle,
      h5: {
        fontSize: MD_FONT.h5,
        lineHeight: MD_LINE.h5,
        fontWeight: "600",
        color: t.foreground,
        marginTop: MD_GAP.headingTopSmall,
        marginBottom: MD_GAP.headingBottomSmall,
      } as TextStyle,
      h6: {
        fontSize: MD_FONT.h6,
        lineHeight: MD_LINE.h6,
        fontWeight: "600",
        color: t.foreground,
        marginTop: MD_GAP.headingTopSmall,
        marginBottom: MD_GAP.headingBottomSmall,
      } as TextStyle,
      strong: { fontWeight: "bold", color: t.foreground } as TextStyle,
      em: { fontStyle: "italic", color: t.foreground } as TextStyle,
      del: { textDecorationLine: "line-through", color: t.mutedForeground } as TextStyle,
      link: { color: t.brand, textDecorationLine: "underline" } as TextStyle,
      // Inline code — monospace + muted-foreground tint, no background chip
      // (same rationale as the iOS file: avoids the CJK line-box asymmetry).
      inlineCode: {
        color: t.mutedForeground,
        backgroundColor: "transparent",
        fontSize: MD_FONT.body,
        fontFamily: "monospace",
      } as TextStyle,
      // Fallback for code that stays inside the prose stream (nested in a
      // list item) — top-level fenced blocks route to CodeBlock instead.
      codeBlockBox: {
        backgroundColor: t.surface2,
        borderColor: t.border,
        borderWidth: StyleSheet.hairlineWidth,
        borderRadius: MOBILE_RADIUS.md,
        padding: 12,
        marginBottom: MD_GAP.paragraph,
      } as ViewStyle,
      codeBlockText: {
        fontSize: MD_FONT.codeBlock,
        color: t.foreground,
        fontFamily: "monospace",
      } as TextStyle,
      blockquote: {
        borderLeftWidth: 3,
        borderLeftColor: t.border,
        paddingLeft: 10,
        marginBottom: MD_GAP.paragraph,
        backgroundColor: "transparent",
      } as ViewStyle,
      blockquoteText: { color: t.mutedForeground } as TextStyle,
      listRow: { flexDirection: "row", marginBottom: 4 } as ViewStyle,
      listText: {
        color: t.foreground,
        fontSize: MD_FONT.body,
        lineHeight: MD_LINE.body,
      } as TextStyle,
      listMarker: {
        color: t.mutedForeground,
        fontSize: MD_FONT.body,
        lineHeight: MD_LINE.body,
        width: 20,
      } as TextStyle,
      listNested: { marginLeft: 16 } as ViewStyle,
      taskCheckbox: {
        width: 16,
        height: 16,
        borderRadius: 4,
        borderWidth: 1.5,
        marginTop: 4,
        alignItems: "center",
        justifyContent: "center",
      } as ViewStyle,
      taskTextChecked: { color: t.mutedForeground } as TextStyle,
      table: {
        borderWidth: StyleSheet.hairlineWidth,
        borderColor: t.border,
        borderRadius: MOBILE_RADIUS.md,
        overflow: "hidden",
        marginBottom: MD_GAP.paragraph,
      } as ViewStyle,
      tableHeaderRow: { flexDirection: "row", backgroundColor: t.surface2 } as ViewStyle,
      tableRow: { flexDirection: "row", borderTopWidth: StyleSheet.hairlineWidth, borderTopColor: t.border } as ViewStyle,
      tableCell: {
        flex: 1,
        paddingHorizontal: 10,
        paddingVertical: 6,
        fontSize: MD_FONT.body,
        lineHeight: 20,
        color: t.foreground,
      } as TextStyle,
      tableHeaderText: {
        fontWeight: "600",
        color: t.foreground,
      } as TextStyle,
      hr: {
        height: StyleSheet.hairlineWidth,
        backgroundColor: t.border,
        marginTop: 16,
        marginBottom: 16,
      } as ViewStyle,
      segmentGap: 12,
    }),
    [t, compact],
  );
}
