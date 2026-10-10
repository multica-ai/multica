/**
 * Bottom sheet in pure React Native, standing in for expo-router's
 * `presentation: "formSheet"` / `"modal"` routes (see AGENTS.md — the iOS
 * app's sheet tables map onto this component). Content-sized height with a
 * screen-height cap, slide-up/slide-down animation, drag-the-grabber to
 * dismiss, backdrop tap and hardware back to close.
 */
import React, { useEffect, useRef, useState } from "react";
import {
  Animated,
  BackHandler,
  Dimensions,
  Easing,
  PanResponder,
  Pressable,
  StyleSheet,
  View,
} from "react-native";
import { useKeyboardHeight } from "@/lib/use-keyboard-height";
import { useSafeAreaInsets } from "@/lib/safe-area";
import { useThemeColors } from "@/lib/use-theme-colors";

const ANIM_MS = 260;
// Drag past this distance, or release with a downward flick faster than
// this velocity (px/ms), and the sheet dismisses.
const DISMISS_DISTANCE = 120;
const DISMISS_VELOCITY = 0.9;

export function BottomSheet({
  visible,
  onClose,
  children,
  /** Fraction of screen height the sheet may occupy. */
  maxHeightRatio = 0.92,
  avoidKeyboard = true,
}: {
  visible: boolean;
  onClose: () => void;
  children: React.ReactNode;
  maxHeightRatio?: number;
  avoidKeyboard?: boolean;
}) {
  const c = useThemeColors();
  const insets = useSafeAreaInsets();
  const keyboardHeight = useKeyboardHeight();
  const [mounted, setMounted] = useState(visible);
  const [contentHeight, setContentHeight] = useState(0);
  const progress = useRef(new Animated.Value(0)).current;
  const dragY = useRef(new Animated.Value(0)).current;
  const visibleRef = useRef(visible);
  visibleRef.current = visible;
  // The pan responder is created once; route onClose through a ref so the
  // closure can't go stale when callers pass a fresh arrow function.
  const onCloseRef = useRef(onClose);
  onCloseRef.current = onClose;

  useEffect(() => {
    if (visible) {
      setMounted(true);
      dragY.setValue(0);
      Animated.timing(progress, {
        toValue: 1,
        duration: ANIM_MS,
        easing: Easing.out(Easing.cubic),
        useNativeDriver: true,
      }).start();
      return;
    }
    if (mounted) {
      Animated.timing(progress, {
        toValue: 0,
        duration: ANIM_MS,
        easing: Easing.in(Easing.cubic),
        useNativeDriver: true,
      }).start(() => {
        if (!visibleRef.current) setMounted(false);
      });
    }
    // `mounted` is intentionally not a dependency: visible drives both paths.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [visible]);

  // While open, hardware back closes the sheet instead of popping the stack
  // (last-registered BackHandler wins in RN).
  useEffect(() => {
    if (!mounted) return;
    const sub = BackHandler.addEventListener("hardwareBackPress", () => {
      if (visibleRef.current) {
        onClose();
        return true;
      }
      return false;
    });
    return () => sub.remove();
  }, [mounted, onClose]);

  // Drag the grabber zone down to dismiss. Lives on the handle strip only —
  // a sheet-wide responder would fight the content's ScrollView and
  // Pressables. Only claims the gesture after a clearly downward move, so
  // taps inside the sheet are unaffected.
  const panResponder = useRef(
    PanResponder.create({
      onMoveShouldSetPanResponder: (_e, g) =>
        visibleRef.current && g.dy > 8 && Math.abs(g.dy) > Math.abs(g.dx),
      onPanResponderMove: (_e, g) => {
        dragY.setValue(Math.max(0, g.dy));
      },
      onPanResponderRelease: (_e, g) => {
        const shouldDismiss =
          g.dy > DISMISS_DISTANCE || (g.dy > 0 && g.vy > DISMISS_VELOCITY);
        if (shouldDismiss) {
          const exitTo =
            Math.max(contentHeight, Dimensions.get("window").height * 0.6) +
            insets.bottom +
            60;
          Animated.timing(dragY, {
            toValue: exitTo,
            duration: 160,
            easing: Easing.in(Easing.cubic),
            useNativeDriver: true,
          }).start(() => {
            // dragY stays at the exit offset so the sheet keeps travelling
            // down while `visible` flips and progress animates out; it is
            // reset on the next open.
            onCloseRef.current();
          });
        } else {
          Animated.spring(dragY, {
            toValue: 0,
            speed: 24,
            bounciness: 4,
            useNativeDriver: true,
          }).start();
        }
      },
      onPanResponderTerminate: () => {
        Animated.spring(dragY, {
          toValue: 0,
          speed: 24,
          bounciness: 4,
          useNativeDriver: true,
        }).start();
      },
    }),
  ).current;

  if (!mounted) return null;

  const maxHeight = Math.round(
    Dimensions.get("window").height * maxHeightRatio - insets.top,
  );
  const translateY = Animated.add(
    progress.interpolate({
      inputRange: [0, 1],
      outputRange: [contentHeight + insets.bottom || 400, 0],
    }),
    dragY,
  );

  return (
    <View style={StyleSheet.absoluteFill} pointerEvents="auto">
      <Animated.View style={[styles.backdropWrap, { opacity: progress }]}>
        <Pressable
          accessibilityLabel="Close sheet"
          style={styles.backdrop}
          onPress={onClose}
        />
      </Animated.View>
      <View
        style={[
          styles.avoidWrap,
          avoidKeyboard ? { paddingBottom: keyboardHeight } : null,
        ]}
        pointerEvents="box-none"
      >
        <Animated.View
          style={[
            styles.sheet,
            {
              backgroundColor: c.card,
              maxHeight,
              marginBottom: insets.bottom,
              transform: [{ translateY }],
            },
          ]}
        >
          <View style={styles.handleZone} {...panResponder.panHandlers}>
            <View
              style={[styles.handle, { backgroundColor: c.mutedForeground }]}
            />
          </View>
          <View
            style={styles.content}
            onLayout={(e) => setContentHeight(e.nativeEvent.layout.height)}
          >
            {children}
          </View>
        </Animated.View>
      </View>
    </View>
  );
}

const styles = StyleSheet.create({
  backdropWrap: { ...StyleSheet.absoluteFillObject },
  backdrop: { flex: 1, backgroundColor: "rgba(0,0,0,0.4)" },
  avoidWrap: { flex: 1, justifyContent: "flex-end" },
  sheet: {
    borderTopLeftRadius: 10,
    borderTopRightRadius: 10,
    overflow: "hidden",
  },
  // Full-width grabber touch target; taller than the visible pill so the
  // drag gesture is easy to pick up.
  handleZone: {
    alignItems: "center",
    paddingTop: 6,
    paddingBottom: 8,
    minHeight: 28,
  },
  handle: {
    width: 36,
    height: 5,
    borderRadius: 3,
    opacity: 0.4,
  },
  content: { paddingBottom: 8 },
});
