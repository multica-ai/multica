/**
 * Multiline text input that grows with its content. Replaces the bare
 * `<TextInput multiline />` + tailwind `min-h-* max-h-*` pattern,
 * which doesn't actually grow with content — RN's Yoga layout doesn't
 * read the native widget's `intrinsicContentSize` automatically
 * (facebook/react-native#54570, open 2025). The fix is to listen for
 * `onContentSizeChange` and feed the measured height back into a state-
 * driven `style.height`, which Yoga does honor.
 *
 * Behavior contract:
 *   - height = clamp(contentSize.height, minHeight, maxHeight)
 *   - When height reaches maxHeight, `scrollEnabled` flips to true so
 *     the TextInput becomes internally scrollable; otherwise it's false
 *     so the outer ScrollView (if any) owns scrolling — never nest two
 *     scrollables when not needed.
 *   - Same four RN cross-platform workarounds as TextField, plus
 *     `textAlignVertical: "top"` (multiline anchors at top).
 *
 * Refs are forwarded so callers can imperatively `.focus()` / `.blur()`
 * — used by comment-composer's tap-to-expand state machine.
 */
import * as React from "react";
import { useState } from "react";
import {
  TextInput,
  type NativeSyntheticEvent,
  type TextInputContentSizeChangeEventData,
  type TextInputProps,
} from "react-native";
import { cn } from "@/lib/utils";
import { MOBILE_PLACEHOLDER_COLOR } from "./input-tokens";

export interface AutosizeTextAreaProps extends TextInputProps {
  /** Floor for the input's height (px). Default 40. */
  minHeight?: number;
  /** Ceiling (px). Once content reaches this, the input becomes
   *  internally scrollable instead of growing further. Default 128. */
  maxHeight?: number;
  className?: string;
}

export const AutosizeTextArea = React.forwardRef<TextInput, AutosizeTextAreaProps>(
  (
    {
      minHeight = 40,
      maxHeight = 128,
      className,
      style,
      onContentSizeChange,
      ...rest
    },
    ref,
  ) => {
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
          {
            height,
            paddingVertical: 0,
            includeFontPadding: false,
            textAlignVertical: "top",
          },
          style,
        ]}
        className={cn("text-base text-foreground", className)}
        {...rest}
      />
    );
  },
);
AutosizeTextArea.displayName = "AutosizeTextArea";
