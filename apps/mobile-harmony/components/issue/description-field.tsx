/**
 * HarmonyOS port of apps/mobile/components/issue/description-field.tsx —
 * description input block shared by the new-issue screen and the issue edit
 * sheet. Focus-tinted rounded container wrapping the AutosizeTextArea —
 * matches the "write markdown body" treatment used by the comment composer
 * so all three surfaces feel like the same control.
 *
 * Pure UI shell. The mention pipeline lives in the caller's `useMentionInput`
 * instance, passed in as `description`. Callers also own the floating
 * MentionSuggestionBar (it has to sit above the keyboard, outside the
 * scroll view).
 */
import React, { useState } from "react";
import { StyleSheet, View, type StyleProp, type ViewStyle } from "react-native";
import { AutosizeTextArea } from "@/components/ui/autosize-textarea";
import { MIN_BODY_INPUT_HEIGHT_PX } from "@/components/ui/input-tokens";
import type { UseMentionInputReturn } from "@/lib/use-mention-input";
import { withAlpha } from "@/lib/theme";
import { useThemeColors } from "@/lib/use-theme-colors";

export function DescriptionField({
  description,
  disabled,
  placeholder = "Description… (type @ to mention)",
  style,
}: {
  description: UseMentionInputReturn;
  disabled: boolean;
  placeholder?: string;
  style?: StyleProp<ViewStyle>;
}) {
  const c = useThemeColors();
  const [focused, setFocused] = useState(false);
  return (
    <View
      style={[
        // rounded-xl border px-3
        styles.container,
        focused
          ? {
              borderColor: withAlpha(c.primary, 0.3),
              backgroundColor: c.secondary,
            }
          : {
              borderColor: "transparent",
              backgroundColor: withAlpha(c.secondary, 0.4),
            },
        style,
      ]}
    >
      <AutosizeTextArea
        value={description.text}
        onChangeText={description.handlers.onChangeText}
        selection={description.selection}
        onSelectionChange={description.handlers.onSelectionChange}
        onFocus={() => setFocused(true)}
        onBlur={() => setFocused(false)}
        placeholder={placeholder}
        placeholderTextColor={c.mutedForeground}
        style={[styles.input, { color: c.foreground }]}
        minHeight={MIN_BODY_INPUT_HEIGHT_PX}
        editable={!disabled}
      />
    </View>
  );
}

const styles = StyleSheet.create({
  // rounded-xl border px-3
  container: {
    borderRadius: 12,
    borderWidth: 1,
    paddingHorizontal: 12,
  },
  // py-2
  input: {
    paddingVertical: 8,
    fontSize: 15,
    textAlignVertical: "top",
  },
});
