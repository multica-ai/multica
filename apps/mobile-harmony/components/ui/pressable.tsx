/**
 * Drop-in replacement for react-native's Pressable on this matrix.
 *
 * RNOH's Pressable drops the function form of `style` (and function
 * children): the callback never resolves and the component renders with NO
 * styles at all — transparent background, no pressed feedback (observed on
 * the API 26 emulator, RNOH 0.82.30). This wrapper tracks the pressed state
 * manually, resolves function styles/children itself, and forwards
 * everything else to the underlying Pressable unchanged.
 */
import React, { useState } from "react";
import {
  Pressable as RNPressable,
  type PressableProps,
  type PressableStateCallbackType,
  type StyleProp,
  type View,
  type ViewStyle,
} from "react-native";

export type PressableStyleCallback = PressableStateCallbackType;

type HarmonyPressableProps = PressableProps & {
  /** Callers pass ref explicitly (React 19 ref-as-prop); forwarded via spread. */
  ref?: React.Ref<View>;
  style?:
    | StyleProp<ViewStyle>
    | ((state: PressableStateCallbackType) => StyleProp<ViewStyle>);
  children?: React.ReactNode | ((state: PressableStateCallbackType) => React.ReactNode);
};

export function Pressable({ style, children, onPressIn, onPressOut, ...props }: HarmonyPressableProps) {
  const [pressed, setPressed] = useState(false);
  // RNOH's PressableStateCallbackType only carries `pressed` (no hover/focus).
  const state: PressableStateCallbackType = { pressed };
  const resolvedStyle: StyleProp<ViewStyle> =
    typeof style === "function" ? style(state) : style;
  return (
    <RNPressable
      {...props}
      style={resolvedStyle}
      onPressIn={(event) => {
        setPressed(true);
        onPressIn?.(event);
      }}
      onPressOut={(event) => {
        setPressed(false);
        onPressOut?.(event);
      }}
    >
      {typeof children === "function" ? children(state) : children}
    </RNPressable>
  );
}
