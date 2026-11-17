import { Pressable } from "react-native";
import { Ionicons } from "@expo/vector-icons";
import { Text } from "@/components/ui/text";
import { StatusIcon } from "@/components/ui/status-icon";
import { useIssueStatuses } from "@/lib/use-issue-statuses";
import { useColorScheme } from "@/lib/use-color-scheme";
import { THEME } from "@/lib/theme";

export function IssueSectionHeader({
  status,
  count,
  expanded,
  onPress,
}: {
  status: string;
  count: number;
  expanded: boolean;
  onPress: () => void;
}) {
  const catalog = useIssueStatuses();
  const { colorScheme } = useColorScheme();
  return (
    <Pressable
      onPress={onPress}
      accessibilityRole="button"
      accessibilityLabel={`${catalog.labelOf(status)} (${count})`}
      accessibilityState={{ expanded }}
      className="min-h-12 flex-row items-center gap-2 px-4 py-3 bg-background"
    >
      <Ionicons
        name={expanded ? "chevron-down" : "chevron-forward"}
        size={14}
        color={THEME[colorScheme].mutedForeground}
      />
      <StatusIcon status={status} category={catalog.categoryOf(status)} icon={catalog.iconOf(status)} color={catalog.colorOf(status)} size={14} />
      <Text className="text-xs uppercase tracking-wider text-muted-foreground font-medium">
        {catalog.labelOf(status)}
      </Text>
      <Text className="text-xs text-muted-foreground/60">{count}</Text>
    </Pressable>
  );
}
