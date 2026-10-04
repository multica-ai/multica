/**
 * Left-swipe-to-reveal-Archive wrapper for inbox rows. HarmonyOS port of
 * apps/mobile/components/inbox/swipeable-inbox-row.tsx.
 *
 * iOS pattern reference: Mail.app / Linear iOS / Things — a destructive red
 * Archive action revealed by a leftward drag. **Reveal-only, no auto-fire**:
 * an explicit tap on the revealed action is required (matching iOS and the
 * old iOS file's rationale — full-swipe auto-fire felt aggressive). A medium
 * haptic fires once when the row crosses the action width during the drag so
 * the gesture still feels confirmed (via the expo-haptics-shaped
 * @/lib/haptics, a no-op when the native module is absent).
 *
 * Platform adaptation (RNOH 0.82 — see AGENTS.md): gesture-handler +
 * reanimated are not wired on this platform, so the same interaction
 * semantics are rebuilt on the core PanResponder + Animated primitives:
 *   - The drag claims the touch only once it is clearly horizontal
 *     (|dx| >= 8 and at least |dy|), which is what iOS's friction=2 was
 *     guarding against: a fast vertical scroll catching some horizontal
 *     motion must not open the row.
 *   - Tracking is 1:1 (no friction divisor) and clamped to a slight
 *     overshoot past ACTION_WIDTH so the threshold crossing stays
 *     detectable under the clamp.
 *   - `-ACTION_WIDTH` (80) is the open detent, mirroring the iOS
 *     `rightThreshold`: releasing at/beyond full reveal keeps the Archive
 *     button revealed; releasing short of it snaps closed. No auto-archive
 *     on cross.
 *   - Tapping the revealed content while the row is open closes the row
 *     instead of pressing through (Mail.app behaviour); when closed, taps
 *     pass straight through to the row's Pressable.
 *   - We spring closed before invoking onArchive so the row's exit from the
 *     FlatList (driven by the optimistic mutation flipping `archived: true`,
 *     which the parent's `deduplicateInboxItems` filters out) doesn't race
 *     the spring close.
 */
import { useCallback, useEffect, useRef } from "react";
import {
  Animated,
  PanResponder,
  Pressable,
  StyleSheet,
  View,
} from "react-native";
import type { InboxItem } from "@multica/core/types";
import { Text } from "@/components/ui/text";
import { Icon } from "@/components/ui/icon";
import { impactAsync, ImpactFeedbackStyle } from "@/lib/haptics";
import { useThemeColors } from "@/lib/use-theme-colors";
import { InboxRow } from "./inbox-row";

const ACTION_WIDTH = 80;
// Rubber-band room past the detent: lets the drag express intent (and cross
// the haptic threshold) without ever revealing more than ACTION_WIDTH.
const MAX_DRAG = ACTION_WIDTH * 1.25;
// A release under this movement is a tap, not a swipe.
const TAP_SLOP = 5;

interface Props {
  item: InboxItem;
  onPress: () => void;
  onArchive: () => void;
}

export function SwipeableInboxRow({ item, onPress, onArchive }: Props) {
  const c = useThemeColors();
  const translateX = useRef(new Animated.Value(0)).current;
  // Mirrors the animated value without touching private Animated fields.
  const offsetRef = useRef(0);
  const dragStartRef = useRef(0);
  const hapticFiredRef = useRef(false);
  // Whether the row is resting open at the detent. Read by the capture
  // handler; the ref object is stable, so closures made once by
  // PanResponder.create always see the live value.
  const openRef = useRef(false);

  useEffect(() => {
    const listener = translateX.addListener((v) => {
      offsetRef.current = v.value;
    });
    return () => translateX.removeListener(listener);
  }, [translateX]);

  const springTo = useCallback(
    (target: number) => {
      openRef.current = target !== 0;
      Animated.spring(translateX, {
        toValue: target,
        friction: 9,
        tension: 55,
        useNativeDriver: true,
      }).start();
    },
    [translateX],
  );

  const fireArchive = useCallback(() => {
    // Close first so the swipe spring doesn't fight the row's removal from
    // FlatList on the next render tick (mirrors the iOS wrapper).
    springTo(0);
    onArchive();
  }, [springTo, onArchive]);

  const settle = useCallback(
    (dx: number) => {
      const end = dragStartRef.current + dx;
      // A tap only reaches us when the row was open (the capture path); any
      // real release at/past the detent opens, everything else snaps closed.
      const target =
        Math.abs(dx) < TAP_SLOP || end > -ACTION_WIDTH ? 0 : -ACTION_WIDTH;
      springTo(target);
    },
    [springTo],
  );

  const panResponder = useRef(
    PanResponder.create({
      // Claim the touch once the gesture is clearly horizontal (a vertical
      // scroll keeps the list in charge)…
      onMoveShouldSetPanResponder: (_e, g) =>
        Math.abs(g.dx) >= 8 && Math.abs(g.dx) >= Math.abs(g.dy),
      // …and steal taps while open so they close the row instead of pressing
      // through to the content. (PanResponder's capture hook runs on the
      // start phase, before the row's inner Pressable can claim the touch.)
      onStartShouldSetPanResponderCapture: () => openRef.current,
      onPanResponderGrant: () => {
        dragStartRef.current = offsetRef.current;
        // Already at/past the detent (row open): don't re-fire on a wiggle.
        hapticFiredRef.current = offsetRef.current <= -ACTION_WIDTH;
      },
      onPanResponderMove: (_e, g) => {
        const next = Math.max(
          -MAX_DRAG,
          Math.min(0, dragStartRef.current + g.dx),
        );
        translateX.setValue(next);
        // One-shot haptic when the drag crosses the action width threshold —
        // the JS-thread equivalent of iOS's useAnimatedReaction + runOnJS.
        if (!hapticFiredRef.current && next <= -ACTION_WIDTH) {
          hapticFiredRef.current = true;
          void impactAsync(ImpactFeedbackStyle.Medium);
        }
      },
      onPanResponderRelease: (_e, g) => settle(g.dx),
      onPanResponderTerminate: (_e, g) => settle(g.dx),
    }),
  ).current;

  return (
    <View style={styles.container}>
      <View style={styles.actionWrap}>
        <Pressable
          onPress={fireArchive}
          accessibilityLabel="Archive"
          accessibilityRole="button"
          style={[styles.action, { backgroundColor: c.destructive }]}
        >
          <View style={styles.actionContent}>
            <Icon name="archive-outline" size={20} color="#ffffff" />
            <Text style={styles.actionLabel}>Archive</Text>
          </View>
        </Pressable>
      </View>
      <Animated.View
        style={[styles.content, { transform: [{ translateX }] }]}
        {...panResponder.panHandlers}
      >
        <InboxRow item={item} onPress={onPress} />
      </Animated.View>
    </View>
  );
}

const styles = StyleSheet.create({
  container: { position: "relative" },
  // Behind the row, anchored right: full-height destructive action strip.
  actionWrap: {
    position: "absolute",
    top: 0,
    right: 0,
    bottom: 0,
    width: ACTION_WIDTH,
  },
  // flex-1 items-center justify-center bg-destructive
  action: { flex: 1, alignItems: "center", justifyContent: "center" },
  // items-center gap-0.5
  actionContent: { alignItems: "center", gap: 2 },
  // text-xs text-white
  actionLabel: { fontSize: 12, color: "#ffffff" },
  content: { backgroundColor: "transparent" },
});
