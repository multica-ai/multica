/**
 * Pure picker body for issue status — single-select over BOARD_STATUSES +
 * cancelled. No shell, no modal — the caller (a formSheet route screen, or
 * any embedding surface) renders it inside whatever container it needs.
 *
 * Split from the old `status-picker-sheet.tsx` so the same row UI can serve
 * both the issue-detail route (`issue/[id]/picker/status.tsx`, which writes
 * via useUpdateIssue) and the new-issue draft route
 * (`new-issue-picker/status.tsx`, which writes via useNewIssueDraftStore).
 */
import { Pressable, ScrollView, View } from "react-native";
import { Ionicons } from "@expo/vector-icons";
import { useColorScheme } from "nativewind";
import type { IssueStatus } from "@multica/core/types";
import { Text } from "@/components/ui/text";
import { StatusIcon } from "@/components/ui/status-icon";
import { BOARD_STATUSES, STATUS_LABEL } from "@/lib/issue-status";
import { THEME } from "@/lib/theme";

const ALL_STATUSES: IssueStatus[] = [...BOARD_STATUSES, "cancelled"];

interface Props {
  value: IssueStatus;
  onChange: (next: IssueStatus) => void;
}

export function StatusPickerBody({ value, onChange }: Props) {
  const { colorScheme } = useColorScheme();
  const checkColor =
    colorScheme === "dark" ? THEME.dark.primary : THEME.light.primary;

  return (
    <ScrollView showsVerticalScrollIndicator={false}>
      <View className="px-4 pt-3 pb-2">
        <Text className="text-lg font-semibold text-foreground">Status</Text>
      </View>
      <View className="px-2">
        {ALL_STATUSES.map((status) => {
          const selected = status === value;
          return (
            <Pressable
              key={status}
              onPress={() => onChange(status)}
              className="flex-row items-center gap-3 rounded-lg px-3 py-3 active:bg-secondary"
            >
              <StatusIcon status={status} size={18} />
              <Text className="flex-1 text-base text-foreground">
                {STATUS_LABEL[status]}
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
