/**
 * Left-swipe-to-reveal-Archive wrapper for inbox rows.
 *
 * iOS pattern reference: Mail.app / Linear iOS / Things — a destructive
 * red Archive action revealed by a leftward drag. The user must swipe at
 * least 50 % of the row width to arm the panel. Once revealed, the panel
 * auto-commits archive after ARCHIVE_HOLD_MS unless the user swipes back.
 * Swiping back at any point before the timer fires cancels silently — the
 * row is never archived from a back-swipe. The user can also tap the
 * revealed Archive button to commit immediately.
 *
 * A medium haptic fires once when the drag crosses the commit threshold so
 * the armed state has tactile confirmation even before the timer fires.
 *
 * Why ReanimatedSwipeable (not the legacy Swipeable): RNGH 2.20+ ships the
 * Reanimated-driven implementation that integrates cleanly with the
 * existing reanimated@4 install and runs the swipe on the UI thread (the
 * legacy version uses Animated, which janks on heavy lists). The
 * gesture-handler root is already mounted in apps/mobile/app/_layout.tsx.
 *
 * Behaviour notes:
 *   - ARCHIVE_REVEAL_FRACTION = 0.50: the archive panel only becomes
 *     reachable once the drag exceeds 50 % of the row width, matching the
 *     intended behaviour from TECH-2885. A slow vertical scroll that drifts
 *     horizontally cannot cross this threshold accidentally.
 *   - `friction=2` slightly slows the drag so the action doesn't open by
 *     accident on a fast vertical scroll that catches some horizontal motion.
 *   - `rightThreshold` (= revealThreshold) is the open-detent — releasing
 *     past it keeps the Archive panel revealed and starts the hold timer;
 *     releasing short of it snaps closed without starting any timer.
 *   - `onSwipeableOpen` starts a 450 ms hold timer; `onSwipeableWillClose`
 *     cancels it. This means a quick swipe-and-back never archives.
 *   - We `swipeable.close()` before invoking onArchive so the row's exit
 *     from the FlatList (driven by the optimistic mutation flipping
 *     `archived: true`, which the parent's `deduplicateInboxItems` filters
 *     out) doesn't race the spring close.
 */
import { useCallback, useEffect, useRef, useState } from "react";
import { Pressable, View } from "react-native";
import Animated, {
  type SharedValue,
  useAnimatedReaction,
  runOnJS,
} from "react-native-reanimated";
import ReanimatedSwipeable, {
  type SwipeableMethods,
} from "react-native-gesture-handler/ReanimatedSwipeable";
import { Ionicons } from "@expo/vector-icons";
import * as Haptics from "expo-haptics";
import type { InboxItem } from "@multica/core/types";
import { Text } from "@/components/ui/text";
import { InboxRow } from "./inbox-row";

const ACTION_WIDTH = 80;
/** Archive panel only arms after swiping ≥ 50 % of the row width. */
const ARCHIVE_REVEAL_FRACTION = 0.5;
/** Milliseconds the panel must stay open before archive auto-commits. */
const ARCHIVE_HOLD_MS = 450;

interface Props {
  item: InboxItem;
  onPress: () => void;
  onArchive: () => void;
}

export function SwipeableInboxRow({ item, onPress, onArchive }: Props) {
  const ref = useRef<SwipeableMethods>(null);
  const holdTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  /** Stable ref so the setTimeout callback always calls the latest onArchive. */
  const onArchiveRef = useRef(onArchive);
  /** Guard against tap + timer firing within the same event loop tick. */
  const firedRef = useRef(false);
  const [rowWidth, setRowWidth] = useState(0);

  useEffect(() => {
    onArchiveRef.current = onArchive;
  }, [onArchive]);

  const revealThreshold =
    rowWidth > 0
      ? Math.max(ACTION_WIDTH, Math.round(rowWidth * ARCHIVE_REVEAL_FRACTION))
      : ACTION_WIDTH;

  const clearHoldTimer = useCallback(() => {
    if (holdTimerRef.current !== null) {
      clearTimeout(holdTimerRef.current);
      holdTimerRef.current = null;
    }
  }, []);

  const fireArchive = useCallback(() => {
    if (firedRef.current) return;
    firedRef.current = true;
    clearHoldTimer();
    ref.current?.close();
    onArchiveRef.current();
  }, [clearHoldTimer]);

  const onSwipeableOpen = useCallback(() => {
    holdTimerRef.current = setTimeout(fireArchive, ARCHIVE_HOLD_MS);
  }, [fireArchive]);

  const onSwipeableWillClose = useCallback(() => {
    clearHoldTimer();
  }, [clearHoldTimer]);

  useEffect(() => () => clearHoldTimer(), [clearHoldTimer]);

  return (
    <View onLayout={(e) => setRowWidth(e.nativeEvent.layout.width)}>
      <ReanimatedSwipeable
        ref={ref}
        friction={2}
        rightThreshold={revealThreshold}
        onSwipeableOpen={onSwipeableOpen}
        onSwipeableWillClose={onSwipeableWillClose}
        renderRightActions={(_progress, drag) => (
          <ArchiveAction
            onPress={fireArchive}
            drag={drag}
            revealThreshold={revealThreshold}
          />
        )}
      >
        <InboxRow item={item} onPress={onPress} />
      </ReanimatedSwipeable>
    </View>
  );
}

function ArchiveAction({
  onPress,
  drag,
  revealThreshold,
}: {
  onPress: () => void;
  drag: SharedValue<number>;
  revealThreshold: number;
}) {
  // One-shot haptic when the drag crosses the commit threshold — gives tactile
  // confirmation that the panel is now armed. useAnimatedReaction runs on the
  // UI thread; runOnJS bridges to Haptics.impactAsync on JS.
  useAnimatedReaction(
    () => drag.value <= -revealThreshold,
    (crossed, prev) => {
      if (crossed && !prev) {
        runOnJS(Haptics.impactAsync)(Haptics.ImpactFeedbackStyle.Medium);
      }
    },
    [revealThreshold],
  );

  return (
    <Animated.View style={{ width: ACTION_WIDTH }}>
      <Pressable
        onPress={onPress}
        accessibilityLabel="Archive"
        className="flex-1 items-center justify-center bg-destructive"
      >
        <View className="items-center gap-0.5">
          <Ionicons name="archive-outline" size={20} color="white" />
          <Text className="text-xs text-white">Archive</Text>
        </View>
      </Pressable>
    </Animated.View>
  );
}
