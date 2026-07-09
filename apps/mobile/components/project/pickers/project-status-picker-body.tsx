/**
 * Pure picker body for project status — single-select over the 5
 * ProjectStatus enum values. See issue/pickers/status-picker-body.tsx for
 * the "extract body, route owns shell" rationale.
 */
import { Pressable, ScrollView, View } from "react-native";
import { Ionicons } from "@expo/vector-icons";
import { useColorScheme } from "nativewind";
import type { ProjectStatus } from "@multica/core/types";
import { Text } from "@/components/ui/text";
import { ProjectStatusIcon } from "@/components/ui/project-status-icon";
import {
  PROJECT_STATUSES,
  PROJECT_STATUS_LABEL,
} from "@/lib/project-status";
import { THEME } from "@/lib/theme";

interface Props {
  value: ProjectStatus | string;
  onChange: (next: ProjectStatus) => void;
}

export function ProjectStatusPickerBody({ value, onChange }: Props) {
  const { colorScheme } = useColorScheme();
  const checkColor =
    colorScheme === "dark" ? THEME.dark.primary : THEME.light.primary;

  return (
    <ScrollView showsVerticalScrollIndicator={false}>
      <View className="px-4 pt-3 pb-2">
        <Text className="text-lg font-semibold text-foreground">Status</Text>
      </View>
      <View className="px-2">
        {PROJECT_STATUSES.map((status) => {
          const selected = status === value;
          return (
            <Pressable
              key={status}
              onPress={() => onChange(status)}
              className="flex-row items-center gap-3 rounded-lg px-3 py-3 active:bg-secondary"
            >
              <ProjectStatusIcon status={status} size={18} />
              <Text className="flex-1 text-base text-foreground">
                {PROJECT_STATUS_LABEL[status]}
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
