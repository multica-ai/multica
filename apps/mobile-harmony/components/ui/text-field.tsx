/**
 * HarmonyOS port of apps/mobile/components/ui/text-field.tsx — single-line
 * text input. Notes carried over from the iOS version:
 * - `fontSize` is set as a plain style, never a NativeWind `text-*` class:
 *   those map to fontSize+lineHeight, and `lineHeight` on text inputs clips
 *   descenders (RN issues #41240, #28012, #45268, #49886).
 * - Height anchored at 40 (h-10) so vertical centering doesn't depend on
 *   font metrics.
 * - `includeFontPadding` / `textAlignVertical` are Android-only; iOS no-op,
 *   kept for the Android/HarmonyOS story.
 * - Focus state is tracked manually — NativeWind's `focus:` variant on
 *   TextInput was unreliable across SDK upgrades, and className styling is
 *   gone entirely on this side.
 *
 * Colors come from useThemeColors(): rounded-md=6 px-3=12 h-10=40 with a
 * 1px border; invalid → destructive/10 bg + destructive/60 border; focused
 * → secondary bg + ring border; idle → secondary/50 bg, transparent border.
 */
import { useState } from "react";
import { StyleSheet, TextInput, type TextInputProps, type ViewStyle } from "react-native";
import { withAlpha } from "@/lib/theme";
import { useThemeColors } from "@/lib/use-theme-colors";
import { MOBILE_PLACEHOLDER_COLOR } from "./input-tokens";

export interface TextFieldProps extends TextInputProps {
  invalid?: boolean;
}

export function TextField({
  style,
  invalid,
  onFocus,
  onBlur,
  ...rest
}: TextFieldProps) {
  const c = useThemeColors();
  const [focused, setFocused] = useState(false);

  const stateStyle: ViewStyle = invalid
    ? {
        backgroundColor: withAlpha(c.destructive, 0.1),
        borderColor: withAlpha(c.destructive, 0.6),
      }
    : focused
      ? { backgroundColor: c.secondary, borderColor: c.ring }
      : {
          backgroundColor: withAlpha(c.secondary, 0.5),
          borderColor: "transparent",
        };

  return (
    <TextInput
      placeholderTextColor={MOBILE_PLACEHOLDER_COLOR}
      style={[
        styles.base,
        stateStyle,
        { color: c.foreground },
        style,
      ]}
      onFocus={(e) => {
        setFocused(true);
        onFocus?.(e);
      }}
      onBlur={(e) => {
        setFocused(false);
        onBlur?.(e);
      }}
      {...rest}
    />
  );
}

const styles = StyleSheet.create({
  base: {
    fontSize: 14,
    includeFontPadding: false,
    textAlignVertical: "center",
    borderRadius: 6,
    paddingHorizontal: 12,
    height: 40,
    borderWidth: 1,
  },
});
