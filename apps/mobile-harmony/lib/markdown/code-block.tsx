/**
 * Fenced code block — HarmonyOS port of apps/mobile/lib/markdown/
 * code-block.tsx. Same structure (header row: lang label + copy button;
 * horizontally scrollable, selectable code) with one documented
 * divergence: Shiki syntax highlighting is NOT rendered — the iOS
 * highlighter is a JSI native module with no RNOH port, and full JS Shiki
 * is too heavy for the bundle (README roadmap). Code renders as plain mono
 * text on the shared code-surface token; copy + haptic feedback match.
 */
import { useRef, useState } from "react";
import { Pressable, ScrollView, StyleSheet, Text, View } from "react-native";
import { Icon } from "@/components/ui/icon";
import { useThemeColors } from "@/lib/use-theme-colors";
import { MOBILE_RADIUS } from "@/lib/radius";
import { setStringAsync } from "@/lib/clipboard";
import { impactAsync, ImpactFeedbackStyle } from "@/lib/haptics";

export function CodeBlock({
  code,
  lang,
  selectable = true,
}: {
  code: string;
  lang?: string;
  selectable?: boolean;
}) {
  const c = useThemeColors();
  const [copied, setCopied] = useState(false);
  const resetTimer = useRef<ReturnType<typeof setTimeout> | null>(null);

  const onCopy = async () => {
    await setStringAsync(code);
    void impactAsync(ImpactFeedbackStyle.Light);
    setCopied(true);
    if (resetTimer.current) clearTimeout(resetTimer.current);
    // iOS/HarmonyOS surface no system notice on clipboard write — own the
    // feedback with a 2s check-mark flip.
    resetTimer.current = setTimeout(() => setCopied(false), 2000);
  };

  return (
    <View
      style={[
        styles.container,
        { backgroundColor: c.codeSurface, borderColor: c.border },
      ]}
    >
      <View style={styles.header}>
        {lang ? (
          <Text style={[styles.langLabel, { color: c.mutedForeground }]}>{lang}</Text>
        ) : (
          <View />
        )}
        <PressableCopy copied={copied} tint={c.mutedForeground} onPress={onCopy} />
      </View>
      <ScrollView
        horizontal
        showsHorizontalScrollIndicator={false}
        style={styles.scroll}
      >
        <Text style={[styles.code, { color: c.foreground }]} selectable={selectable}>
          {code}
        </Text>
      </ScrollView>
    </View>
  );
}

function PressableCopy({
  copied,
  tint,
  onPress,
}: {
  copied: boolean;
  tint: string;
  onPress: () => void;
}) {
  return (
    <Pressable
      accessibilityRole="button"
      accessibilityLabel={copied ? "Copied" : "Copy code"}
      hitSlop={8}
      onPress={onPress}
      style={styles.copyButton}
    >
      <Icon name={copied ? "checkmark" : "copy-outline"} size={14} color={tint} />
    </Pressable>
  );
}

const styles = StyleSheet.create({
  container: {
    borderRadius: MOBILE_RADIUS.lg,
    borderWidth: 1,
    paddingHorizontal: 12,
    paddingVertical: 8,
  },
  header: {
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "space-between",
    marginBottom: 4,
  },
  langLabel: { fontSize: 12 },
  copyButton: { padding: 2 },
  scroll: { flexGrow: 0 },
  code: { fontSize: 13, lineHeight: 20, fontFamily: "monospace" },
});
