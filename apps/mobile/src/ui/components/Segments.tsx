import { Pressable, ScrollView, StyleSheet } from "react-native";
import { useTheme } from "../theme";
import { Txt } from "./Text";

interface SegmentsProps<K extends string> {
  items: readonly { key: K; label: string }[];
  value: K;
  onChange: (key: K) => void;
}

/** Text tabs under a title; the chosen one sits on a grey pill. */
export function Segments<K extends string>({ items, value, onChange }: SegmentsProps<K>) {
  const palette = useTheme();
  return (
    <ScrollView horizontal showsHorizontalScrollIndicator={false} style={styles.bar} contentContainerStyle={styles.row} accessibilityRole="tablist">
      {items.map((item) => {
        const on = item.key === value;
        return (
          <Pressable
            key={item.key}
            accessibilityRole="tab"
            accessibilityState={{ selected: on }}
            onPress={() => onChange(item.key)}
            style={[styles.item, on && { backgroundColor: palette.fill }]}
          >
            <Txt variant="chip" tone={on ? "primary" : "secondary"}>
              {item.label}
            </Txt>
          </Pressable>
        );
      })}
    </ScrollView>
  );
}

const styles = StyleSheet.create({
  bar: { flexGrow: 0 },
  row: { paddingHorizontal: 14, gap: 2, paddingBottom: 6 },
  item: { height: 32, borderRadius: 16, paddingHorizontal: 12, justifyContent: "center" },
});
