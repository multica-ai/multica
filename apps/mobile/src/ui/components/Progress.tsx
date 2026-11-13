import { StyleSheet, View } from "react-native";
import Svg, { Circle } from "react-native-svg";
import { useTheme } from "../theme";

/** Progress ring, the way projects show how much is done. */
export function Ring({ progress, color, size = 18 }: { progress: number; color: string; size?: number }) {
  const palette = useTheme();
  const stroke = size / 8;
  const r = (size - stroke) / 2;
  const length = 2 * Math.PI * r;
  const filled = Math.max(0, Math.min(1, progress)) * length;
  return (
    <Svg width={size} height={size}>
      <Circle cx={size / 2} cy={size / 2} r={r} stroke={palette.fillStrong} strokeWidth={stroke} fill="none" />
      {filled > 0 ? (
        <Circle
          cx={size / 2}
          cy={size / 2}
          r={r}
          stroke={color}
          strokeWidth={stroke}
          fill="none"
          strokeDasharray={`${filled} ${length}`}
          strokeLinecap="round"
          transform={`rotate(-90 ${size / 2} ${size / 2})`}
        />
      ) : null}
    </Svg>
  );
}

/** One bar split into coloured segments, sized by value. */
export function SegmentBar({ segments }: { segments: readonly { value: number; color: string }[] }) {
  const palette = useTheme();
  return (
    <View style={[styles.bar, { backgroundColor: palette.fill }]}>
      {segments
        .filter((segment) => segment.value > 0)
        .map((segment, index) => (
          <View key={index} style={{ flex: segment.value, backgroundColor: segment.color }} />
        ))}
    </View>
  );
}

const styles = StyleSheet.create({
  bar: { height: 8, borderRadius: 4, overflow: "hidden", flexDirection: "row", gap: 2 },
});
