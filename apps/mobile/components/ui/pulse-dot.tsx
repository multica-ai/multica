/**
 * Slow brand-coloured pulse — opacity oscillation on the UI thread via
 * Reanimated's `withRepeat`. 2-second cycle (1s in + 1s out). Same
 * animation library as comment-card.tsx, no new primitive.
 *
 * Used by:
 *   - apps/mobile/components/issue/agent-activity-row.tsx (in-card "Working" row)
 *   - apps/mobile/components/issue/agent-header-badge.tsx (Stack header ambient badge)
 *
 * Colour is the workspace `brand` token (mobile global.css:45 `--brand`),
 * matching the "in-progress / live" semantic used everywhere else:
 * `RunRow` paints dispatched/running state with `text-brand`, and web's
 * `agent-live-card.tsx` uses `text-info` (also blue) for the same
 * Loader2 spinner. **DO NOT** use the `success` token here — green means
 * "completed", not "running" (Apple HIG / shadcn convention).
 *
 * Inline backgroundColor (rather than NativeWind className) is required
 * because Reanimated's animated style merging doesn't compose cleanly
 * with NativeWind class-derived styles; sibling `comment-card.tsx`
 * follows the same pattern.
 */
import { useEffect } from "react";
import Animated, {
  useAnimatedStyle,
  useSharedValue,
  withRepeat,
  withTiming,
} from "react-native-reanimated";
import { useColorScheme } from "@/lib/use-color-scheme";
import { THEME } from "@/lib/theme";

interface Props {
  /** Diameter in pt. Default 8 (matches the in-card row). */
  size?: number;
}

export function PulseDot({ size = 8 }: Props) {
  const { colorScheme } = useColorScheme();
  const opacity = useSharedValue(0.3);
  useEffect(() => {
    opacity.value = withRepeat(
      withTiming(1, { duration: 1000 }),
      -1, // infinite
      true, // reverse — yields 0.3 ↔ 1.0 oscillation over 2s
    );
  }, [opacity]);

  const style = useAnimatedStyle(() => ({ opacity: opacity.value }));

  return (
    <Animated.View
      style={[
        {
          width: size,
          height: size,
          borderRadius: size / 2,
          backgroundColor: THEME[colorScheme].brand,
        },
        style,
      ]}
    />
  );
}
