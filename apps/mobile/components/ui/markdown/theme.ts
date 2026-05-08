import type { TextStyle, ViewStyle } from "react-native";
import type { MarkedStyles } from "react-native-marked";

// Token-aligned theme. Hex/HSL values mirror multica's mobile tailwind config —
// keep in sync if either changes. The library accepts a MarkedStyles object;
// we precompute one per variant ("default" for issue body, "comment" for
// chat-style rows) since variants are a closed set of two.

export const COLOR = {
  foreground: "hsl(240 10% 4%)",
  mutedForeground: "hsl(240 4% 46%)",
  muted: "hsl(240 5% 96%)",
  border: "hsl(240 6% 90%)",
  brand: "hsl(220 60% 50%)",
  brandForeground: "hsl(0 0% 98%)",
  destructive: "hsl(0 84% 60%)",
} as const;

export const FONT_MONO = "Menlo";

export type Variant = "default" | "comment";

const VARIANT_TOKENS = {
  default: { bodySize: 16, lineHeight: 24, paragraphSpacing: 6 },
  comment: { bodySize: 15, lineHeight: 22, paragraphSpacing: 4 },
} as const;

function buildStyles(variant: Variant): MarkedStyles {
  const v = VARIANT_TOKENS[variant];
  const text: TextStyle = {
    color: COLOR.foreground,
    fontSize: v.bodySize,
    lineHeight: v.lineHeight,
  };

  return {
    text,
    paragraph: { marginVertical: v.paragraphSpacing } as ViewStyle,
    strong: { fontWeight: "700" },
    em: { fontStyle: "italic" },
    strikethrough: { textDecorationLine: "line-through" },
    link: { color: COLOR.brand },
    h1: { color: COLOR.foreground, fontSize: 24, lineHeight: 32, fontWeight: "700", marginTop: 16, marginBottom: 6 },
    h2: { color: COLOR.foreground, fontSize: 20, lineHeight: 26, fontWeight: "700", marginTop: 16, marginBottom: 6 },
    h3: { color: COLOR.foreground, fontSize: 18, lineHeight: 24, fontWeight: "600", marginTop: 12, marginBottom: 6 },
    h4: { color: COLOR.foreground, fontSize: 16, lineHeight: 22, fontWeight: "600", marginTop: 12, marginBottom: 6 },
    h5: { color: COLOR.foreground, fontSize: 15, lineHeight: 20, fontWeight: "600", marginTop: 12, marginBottom: 6 },
    h6: { color: COLOR.foreground, fontSize: 14, lineHeight: 19, fontWeight: "600", marginTop: 12, marginBottom: 6 },
    blockquote: {
      borderLeftWidth: 3,
      borderLeftColor: COLOR.border,
      paddingLeft: 12,
      paddingVertical: 4,
      backgroundColor: COLOR.muted,
      marginVertical: 8,
    },
    codespan: {
      fontFamily: FONT_MONO,
      fontSize: 13,
      backgroundColor: COLOR.muted,
      color: COLOR.foreground,
    },
    hr: { height: 1, backgroundColor: COLOR.border, marginVertical: 12 },
    list: { marginVertical: 6 },
    li: text,
    table: { borderWidth: 1, borderColor: COLOR.border, borderRadius: 6, marginVertical: 8 },
  };
}

export const MARKDOWN_STYLES: Record<Variant, MarkedStyles> = {
  default: buildStyles("default"),
  comment: buildStyles("comment"),
};
