/**
 * HarmonyOS port of apps/mobile/components/ui/switch.tsx. The iOS version
 * styles @rn-primitives/switch through NativeWind classes; the primitives
 * are unavailable on RNOH and NativeWind className styling regressed, so
 * this is a pure-RN implementation with the same API surface
 * (checked / onCheckedChange / disabled).
 *
 * Visual values are the tailwind defaults the classes compiled to:
 * track h-[1.15rem] (18.4) w-8 (32) rounded-full, 1px transparent border,
 * 16x16 thumb translating 14 (translate-x-3.5) when checked; checked track
 * bg-primary, unchecked bg-input, disabled opacity-50. The thumb slide is
 * a 150ms timing animation on RN's core Animated (Reanimated is not wired
 * into this app's Babel pipeline — see bottom-sheet.tsx for the same call).
 */
import React, { useEffect, useRef } from "react";
import {
  Animated,
  Easing,
  Pressable,
  StyleSheet,
} from "react-native";
import { useThemeColors } from "@/lib/use-theme-colors";

const TRACK_WIDTH = 32;
const TRACK_HEIGHT = 18.4;
const THUMB_SIZE = 16;
const TRAVEL = 14; // translate-x-3.5

interface Props {
  checked?: boolean;
  onCheckedChange?: (checked: boolean) => void;
  disabled?: boolean;
}

function Switch({ checked, onCheckedChange, disabled }: Props) {
  const c = useThemeColors();
  const offset = useRef(new Animated.Value(checked ? TRAVEL : 0)).current;

  useEffect(() => {
    Animated.timing(offset, {
      toValue: checked ? TRAVEL : 0,
      duration: 150,
      easing: Easing.out(Easing.quad),
      useNativeDriver: true,
    }).start();
  }, [checked, offset]);

  return (
    <Pressable
      role="switch"
      accessibilityState={{ checked: !!checked, disabled: !!disabled }}
      onPress={disabled ? undefined : () => onCheckedChange?.(!checked)}
      style={[
        styles.track,
        { backgroundColor: checked ? c.primary : c.input },
        disabled ? styles.disabled : null,
      ]}
    >
      <Animated.View
        style={[
          styles.thumb,
          { backgroundColor: c.background },
          { transform: [{ translateX: offset }] },
        ]}
      />
    </Pressable>
  );
}

const styles = StyleSheet.create({
  track: {
    width: TRACK_WIDTH,
    height: TRACK_HEIGHT,
    borderRadius: 9999,
    borderWidth: 1,
    borderColor: "transparent",
    flexDirection: "row",
    alignItems: "center",
  },
  thumb: {
    width: THUMB_SIZE,
    height: THUMB_SIZE,
    borderRadius: THUMB_SIZE / 2,
  },
  disabled: { opacity: 0.5 },
});

export { Switch, type Props as SwitchProps };
