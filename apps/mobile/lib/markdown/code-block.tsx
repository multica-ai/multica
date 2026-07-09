/**
 * Fenced code block. Three pieces stacked:
 *
 *   ┌───────────────────────────────┐
 *   │ TS                       ⎘    │  ← header: lang label + copy button
 *   ├───────────────────────────────┤
 *   │ const x = 1;                  │  ← code, horizontal-scroll, selectable
 *   └───────────────────────────────┘
 *
 * Two render paths:
 *
 *   1. Known language + Shiki ready: token runs from `codeToTokensBase`
 *      rendered as nested `<Text>` children, each carrying its theme color
 *      via inline `style.color`. Per-line wrapping is preserved by giving
 *      each line its own outer `<Text>`.
 *
 *   2. Unknown language, engine unavailable, or first frame before init
 *      finishes: plain `<Text>` of the raw source. Visually identical to
 *      pre-Shiki behavior.
 *
 * Theme tracks `useColorScheme()` so the palette flips with system dark
 * mode without a remount.
 *
 * Copy button is PERSISTENTLY visible (no hover on touch). Tap copies the
 * raw code to the system clipboard, plays a light haptic, and flips to a
 * check mark for 2s — iOS does not surface a system notice on clipboard
 * write, so we own the feedback.
 */
import { useEffect, useRef, useState } from "react";
import {
  Pressable,
  ScrollView,
  View,
} from "react-native";
import * as Clipboard from "expo-clipboard";
import * as Haptics from "expo-haptics";
import Svg, { Path, Rect } from "react-native-svg";
import { Text } from "@/components/ui/text";
import { THEME } from "@/lib/theme";
import { useColorScheme } from "@/lib/use-color-scheme";
import {
  CODE_BLOCK_CONTAINER_CLASS,
  CODE_BLOCK_LANG_LABEL_CLASS,
  CODE_BLOCK_TEXT_CLASS,
} from "./tokens";
import {
  highlight,
  resolveLang,
  SHIKI_THEME_DARK,
  SHIKI_THEME_LIGHT,
  type HighlightedLine,
} from "./shiki";

interface Props {
  code: string;
  lang?: string;
  /**
   * When `false`, opts the code lines out of RN `<Text selectable>` so the
   * UIKit long-press selection magnifier doesn't compete with an outer
   * Pressable's onLongPress. Default true preserves the reader-surface
   * behaviour (issue description / chat message code blocks where users
   * expect to be able to copy via selection). See `Markdown.selectable`
   * for the full rationale.
   */
  selectable?: boolean;
}

export function CodeBlock({ code, lang, selectable = true }: Props) {
  const { isDarkColorScheme } = useColorScheme();
  const theme = isDarkColorScheme ? SHIKI_THEME_DARK : SHIKI_THEME_LIGHT;
  const resolvedLang = resolveLang(lang);
  const [lines, setLines] = useState<HighlightedLine[] | null>(null);

  useEffect(() => {
    if (!resolvedLang) {
      setLines(null);
      return;
    }
    let cancelled = false;
    void highlight(code, resolvedLang, theme).then((result) => {
      if (!cancelled) setLines(result);
    });
    return () => {
      cancelled = true;
    };
  }, [code, resolvedLang, theme]);

  return (
    <View className={CODE_BLOCK_CONTAINER_CLASS}>
      <CodeBlockHeader code={code} lang={lang} />
      <ScrollView horizontal showsHorizontalScrollIndicator={false}>
        {lines ? (
          <HighlightedCode lines={lines} selectable={selectable} />
        ) : (
          <PlainCode code={code} selectable={selectable} />
        )}
      </ScrollView>
    </View>
  );
}

function PlainCode({
  code,
  selectable,
}: {
  code: string;
  selectable: boolean;
}) {
  return (
    <Text className={CODE_BLOCK_TEXT_CLASS} selectable={selectable}>
      {code}
    </Text>
  );
}

function HighlightedCode({
  lines,
  selectable,
}: {
  lines: HighlightedLine[];
  selectable: boolean;
}) {
  // One outer <Text> per line so RN treats each line as its own typographic
  // run. An empty line gets a single space so it still occupies a row's
  // height — otherwise blank lines collapse to zero pixels.
  return (
    <View>
      {lines.map((line, i) => (
        <Text
          key={i}
          className={CODE_BLOCK_TEXT_CLASS}
          selectable={selectable}
        >
          {line.tokens.length === 0
            ? " "
            : line.tokens.map((t, j) => (
                <Text
                  key={j}
                  style={t.color ? { color: t.color } : undefined}
                >
                  {t.content}
                </Text>
              ))}
        </Text>
      ))}
    </View>
  );
}

function CodeBlockHeader({ code, lang }: Props) {
  const { isDarkColorScheme } = useColorScheme();
  const t = isDarkColorScheme ? THEME.dark : THEME.light;
  const [copied, setCopied] = useState(false);
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  // Cancel pending reset on unmount so an in-flight setTimeout doesn't fire
  // setState on a dead component.
  useEffect(() => {
    return () => {
      if (timerRef.current) clearTimeout(timerRef.current);
    };
  }, []);

  const onCopy = async () => {
    try {
      await Clipboard.setStringAsync(code);
      void Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
      setCopied(true);
      if (timerRef.current) clearTimeout(timerRef.current);
      timerRef.current = setTimeout(() => setCopied(false), 2000);
    } catch {
      // Clipboard write failed (extremely rare on iOS). Silent — no
      // recovery path beats a confusing toast.
    }
  };

  return (
    <View className="flex-row items-center justify-between mb-1">
      {lang ? (
        // flex-1 + mr-2 + numberOfLines guarantees the copy button never gets
        // pushed off-screen by a long language alias (e.g. `typescript-react-native`).
        <Text
          className={`${CODE_BLOCK_LANG_LABEL_CLASS} flex-1 mr-2`}
          numberOfLines={1}
        >
          {lang}
        </Text>
      ) : (
        <View className="flex-1" />
      )}
      <Pressable
        onPress={onCopy}
        hitSlop={8}
        accessibilityRole="button"
        accessibilityLabel={copied ? "Code copied" : "Copy code"}
      >
        {copied ? (
          <CheckIcon color={t.success} />
        ) : (
          <CopyIcon color={t.mutedForeground} />
        )}
      </Pressable>
    </View>
  );
}

// Inline SVG icons. react-native-svg primitives don't accept className via
// NativeWind, so colors are passed in as props from the parent — which
// reads them from `THEME[scheme]` so light/dark tracks the app theme.

function CopyIcon({ color }: { color: string }) {
  return (
    <Svg width={14} height={14} viewBox="0 0 16 16" fill="none">
      <Rect
        x={5}
        y={5}
        width={9}
        height={9}
        rx={1.5}
        stroke={color}
        strokeWidth={1.4}
      />
      <Path
        d="M11 4.5V3.5A1.5 1.5 0 0 0 9.5 2H3.5A1.5 1.5 0 0 0 2 3.5v6A1.5 1.5 0 0 0 3.5 11h1"
        stroke={color}
        strokeWidth={1.4}
        strokeLinecap="round"
      />
    </Svg>
  );
}

function CheckIcon({ color }: { color: string }) {
  return (
    <Svg width={14} height={14} viewBox="0 0 16 16" fill="none">
      <Path
        d="M3.5 8.5L6.5 11.5L12.5 5"
        stroke={color}
        strokeWidth={1.6}
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </Svg>
  );
}
