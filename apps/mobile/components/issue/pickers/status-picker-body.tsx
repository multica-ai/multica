/**
 * Pure picker body for issue status — single-select over the workspace's
 * status catalog. No shell, no modal — the caller (a formSheet route screen, or
 * any embedding surface) renders it inside whatever container it needs.
 *
 * Split from the old `status-picker-sheet.tsx` so the same row UI can serve
 * both the issue-detail route (`issue/[id]/picker/status.tsx`, which writes
 * via useUpdateIssue) and the new-issue draft route
 * (`new-issue-picker/status.tsx`, which writes via useNewIssueDraftStore).
 *
 * Options come from `statusOptions()` — the same list the status filter reads,
 * so a status offered in one and missing from the other can't happen (that is
 * how an issue becomes unfindable). Until the catalog lands, or against a
 * backend that predates it, that list is exactly the 7 built-ins this picker
 * always offered. (MUL-6243)
 */
import { Pressable, ScrollView, View } from "react-native";
import { Ionicons } from "@expo/vector-icons";
import { useColorScheme } from "nativewind";
import type { IssueStatus } from "@multica/core/types";
import { Text } from "@/components/ui/text";
import { StatusIcon } from "@/components/ui/status-icon";
import { statusOptions } from "@/lib/issue-status";
import { useIssueStatuses } from "@/lib/use-issue-statuses";
import { THEME } from "@/lib/theme";

interface Props {
  value: IssueStatus;
  onChange: (next: IssueStatus) => void;
}

export function StatusPickerBody({ value, onChange }: Props) {
  const { colorScheme } = useColorScheme();
  const checkColor =
    colorScheme === "dark" ? THEME.dark.primary : THEME.light.primary;
  const catalog = useIssueStatuses();
  const options = statusOptions(catalog);

  return (
    <ScrollView showsVerticalScrollIndicator={false}>
      <View className="px-4 pt-3 pb-2">
        <Text className="text-lg font-semibold text-foreground">Status</Text>
      </View>
      <View className="px-2">
        {options.map((option) => {
          const selected = option.key === value;
          return (
            <Pressable
              key={option.key}
              onPress={() => onChange(option.key)}
              className="flex-row items-center gap-3 rounded-lg px-3 py-3 active:bg-secondary"
            >
              <StatusIcon
                status={option.key}
                category={option.category}
                color={option.color}
                size={18}
              />
              <Text className="flex-1 text-base text-foreground">
                {option.label}
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
