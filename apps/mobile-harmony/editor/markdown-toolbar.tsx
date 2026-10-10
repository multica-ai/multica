/**
 * Shared keyboard-bar toolbar for any markdown body input (issue
 * description, comment, agent prompt). Linear-mobile range of buttons:
 *
 *   @  ·  list  ·  checkbox  ·  code  ·  quote  ·  image  ·  file
 *
 * All buttons map to **literal-character insertion** — no WYSIWYG. After
 * a button fires, the user sees the raw markdown they just inserted; the
 * read-only renderer (mobile hybrid markdown) shows the final visual.
 *
 * No bold / italic / heading: those are "style" tools that require live
 * styled-text rendering inside `<TextInput>`, which RN can't do without
 * swapping to `react-native-enriched`. See plan / research doc.
 *
 * Image / file props are optional so step 3 can ship the literal buttons
 * before step 4 wires the picker + upload pipeline.
 */
import type { ReactNode } from "react";
import { Pressable, StyleSheet, View } from "react-native";
import { Icon } from "@/components/ui/icon";
import { Text } from "@/components/ui/text";
import { useThemeColors } from "@/lib/use-theme-colors";
export interface MarkdownToolbarProps {
  /** Toolbar `@` button → hook.handlers.onAtButtonPress. */
  onAt: () => void;
  /** Insert `- ` at the start of the current line. */
  onList: () => void;
  /** Insert `- [ ] ` at the start of the current line. */
  onCheckbox: () => void;
  /** Insert a fenced code block; caret lands in the empty middle line. */
  onCode: () => void;
  /** Insert `> ` at the start of the current line. */
  onQuote: () => void;
  /** Open image picker → upload → insert `![](url)`. Hidden when omitted. */
  onImage?: () => void;
  /** Open document picker → upload → insert `[📎 name](url)`. Hidden when omitted. */
  onFile?: () => void;
  /** Disable all buttons (during submit / upload-in-flight). */
  disabled?: boolean;
}

export function MarkdownToolbar({
  onAt,
  onList,
  onCheckbox,
  onCode,
  onQuote,
  onImage,
  onFile,
  disabled,
}: MarkdownToolbarProps) {
  const c = useThemeColors();
  return (
    <View
      style={[
        styles.toolbar,
        { borderTopColor: c.border, backgroundColor: c.background },
      ]}
    >
      <ToolbarButton
        accessibilityLabel="Mention someone"
        onPress={onAt}
        disabled={disabled}
      >
        <Text style={[styles.atGlyph, { color: c.mutedForeground }]}>@</Text>
      </ToolbarButton>
      <ToolbarButton
        accessibilityLabel="Bullet list"
        onPress={onList}
        disabled={disabled}
      >
        <Icon name="list-outline" size={18} color={ICON_COLOR} />
      </ToolbarButton>
      <ToolbarButton
        accessibilityLabel="Checklist"
        onPress={onCheckbox}
        disabled={disabled}
      >
        <Icon name="checkbox-outline" size={18} color={ICON_COLOR} />
      </ToolbarButton>
      <ToolbarButton
        accessibilityLabel="Code block"
        onPress={onCode}
        disabled={disabled}
      >
        <Icon name="code-slash-outline" size={18} color={ICON_COLOR} />
      </ToolbarButton>
      <ToolbarButton
        accessibilityLabel="Quote"
        onPress={onQuote}
        disabled={disabled}
      >
        {/* Ionicons has no good quote glyph — use the literal " character at
         *   a slightly larger size for visual parity with adjacent icons. */}
        <Text style={[styles.quoteGlyph, { color: c.mutedForeground }]}>
          &quot;
        </Text>
      </ToolbarButton>
      {onImage ? (
        <ToolbarButton
          accessibilityLabel="Attach image"
          onPress={onImage}
          disabled={disabled}
        >
          <Icon name="image-outline" size={18} color={ICON_COLOR} />
        </ToolbarButton>
      ) : null}
      {onFile ? (
        <ToolbarButton
          accessibilityLabel="Attach file"
          onPress={onFile}
          disabled={disabled}
        >
          <Icon name="attach-outline" size={18} color={ICON_COLOR} />
        </ToolbarButton>
      ) : null}
    </View>
  );
}

const ICON_COLOR = "#71717a"; // muted-foreground

const styles = StyleSheet.create({
  // flex-row items-center gap-1 px-2 py-1.5 border-t bg-background
  toolbar: {
    flexDirection: "row",
    alignItems: "center",
    gap: 4,
    paddingHorizontal: 8,
    paddingVertical: 6,
    borderTopWidth: 1,
  },
  // text-base leading-none
  atGlyph: { fontSize: 16, lineHeight: 16 },
  // text-xl leading-none -mt-1
  quoteGlyph: { fontSize: 20, lineHeight: 20, marginTop: -4 },
  // h-9 w-9 items-center justify-center rounded-md active:bg-secondary + disabled:opacity-40
  button: {
    height: 36,
    width: 36,
    alignItems: "center",
    justifyContent: "center",
    borderRadius: 6,
  },
  buttonDisabled: { opacity: 0.4 },
});

function ToolbarButton({
  onPress,
  disabled,
  accessibilityLabel,
  children,
}: {
  onPress: () => void;
  disabled?: boolean;
  accessibilityLabel: string;
  children: ReactNode;
}) {
  const c = useThemeColors();
  return (
    <Pressable
      onPress={onPress}
      disabled={disabled}
      accessibilityRole="button"
      accessibilityLabel={accessibilityLabel}
      hitSlop={6}
      style={({ pressed }) => [
        styles.button,
        pressed && { backgroundColor: c.secondary },
        disabled && styles.buttonDisabled,
      ]}
    >
      {children}
    </Pressable>
  );
}
