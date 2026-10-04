/**
 * HarmonyOS port of apps/mobile/components/ui/autosize-textarea.tsx —
 * multiline text input that grows with its content. Replaces the bare
 * `<TextInput multiline />` + fixed min/max height pattern, which doesn't
 * actually grow with content — RN's Yoga layout doesn't read the native
 * widget's `intrinsicContentSize` automatically
 * (facebook/react-native#54570, open 2025). The fix is to listen for
 * `onContentSizeChange` and feed the measured height back into a state-
 * driven `style.height`, which Yoga does honor.
 *
 * Behavior contract (unchanged from iOS):
 *   - height = clamp(contentSize.height, minHeight, maxHeight)
 *   - When height reaches maxHeight, `scrollEnabled` flips to true so
 *     the TextInput becomes internally scrollable; otherwise it's false
 *     so the outer ScrollView (if any) owns scrolling — never nest two
 *     scrollables when not needed.
 *   - Same input workarounds as TextField, plus `textAlignVertical: "top"`
 *     (multiline anchors at top).
 *
 * Refs are forwarded so callers can imperatively `.focus()` / `.blur()`
 * — used by comment-composer's tap-to-expand state machine.
 */
import * as React from "react";
import { useState } from "react";
import {
  StyleSheet,
  TextInput,
  type NativeSyntheticEvent,
  type TextInputContentSizeChangeEventData,
  type TextInputProps,
} from "react-native";
import { useThemeColors } from "@/lib/use-theme-colors";
import { MOBILE_PLACEHOLDER_COLOR } from "./input-tokens";

export interface AutosizeTextAreaProps extends TextInputProps {
  /** Floor for the input's height (px). Default 40. */
  minHeight?: number;
  /** Ceiling (px). Once content reaches this, the input becomes
   *  internally scrollable instead of growing further. Default 128. */
  maxHeight?: number;
}

export const AutosizeTextArea = React.forwardRef<TextInput, AutosizeTextAreaProps>(
  (
    {
      minHeight = 40,
      maxHeight = 128,
      style,
      onContentSizeChange,
      ...rest
    },
    ref,
  ) => {
    const c = useThemeColors();
    const [height, setHeight] = useState(minHeight);

    const handleContentSizeChange = (
      e: NativeSyntheticEvent<TextInputContentSizeChangeEventData>,
    ) => {
      const next = Math.min(
        Math.max(minHeight, e.nativeEvent.contentSize.height),
        maxHeight,
      );
      setHeight(next);
      onContentSizeChange?.(e);
    };

    return (
      <TextInput
        ref={ref}
        multiline
        scrollEnabled={height >= maxHeight}
        placeholderTextColor={MOBILE_PLACEHOLDER_COLOR}
        onContentSizeChange={handleContentSizeChange}
        style={[
          styles.base,
          { height, color: c.foreground },
          style,
        ]}
        {...rest}
      />
    );
  },
);
AutosizeTextArea.displayName = "AutosizeTextArea";

const styles = StyleSheet.create({
  // text-base (16) + the multiline input workarounds
  base: {
    fontSize: 16,
    paddingVertical: 0,
    includeFontPadding: false,
    textAlignVertical: "top",
  },
});
