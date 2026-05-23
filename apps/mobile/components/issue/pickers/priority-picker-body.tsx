/**
 * Pure picker body for issue priority — single-select over the 5 priority
 * enum values. See status-picker-body.tsx for the split rationale.
 */
import { Pressable, ScrollView, View } from "react-native";
import { Ionicons } from "@expo/vector-icons";
import { useColorScheme } from "nativewind";
import type { IssuePriority } from "@multica/core/types";
import { Text } from "@/components/ui/text";
import { PriorityIcon } from "@/components/ui/priority-icon";
import { PRIORITY_LABEL } from "@/lib/issue-status";
import { THEME } from "@/lib/theme";

// Display order: severity descending (urgent → none).
const PRIORITY_OPTIONS: IssuePriority[] = [
  "urgent",
  "high",
  "medium",
  "low",
  "none",
];

interface Props {
  value: IssuePriority;
  onChange: (next: IssuePriority) => void;
}

export function PriorityPickerBody({ value, onChange }: Props) {
  const { colorScheme } = useColorScheme();
  const checkColor =
    colorScheme === "dark" ? THEME.dark.primary : THEME.light.primary;

  return (
    <ScrollView showsVerticalScrollIndicator={false}>
      <View className="px-4 pt-3 pb-2">
        <Text className="text-lg font-semibold text-foreground">Priority</Text>
      </View>
      <View className="px-2">
        {PRIORITY_OPTIONS.map((v) => {
          const selected = v === value;
          return (
            <Pressable
              key={v}
              onPress={() => onChange(v)}
              className="flex-row items-center gap-3 rounded-lg px-3 py-3 active:bg-secondary"
            >
              <PriorityIcon priority={v} size={16} />
              <Text className="flex-1 text-base text-foreground">
                {PRIORITY_LABEL[v]}
              </Text>
              {selected ? (
                <Ionicons name="checkmark" size={20} color={checkColor} />
              ) : null}
            </Pressable>
          );
        })}
      </View>
    </ScrollView>
  );
}
