import { View } from "react-native";
import { Text } from "@/components/ui/text";

export function UsageStatCard({
  label,
  value,
  hint,
}: {
  label: string;
  value: string;
  hint?: string;
}) {
  return (
    <View className="flex-1 min-w-[45%] gap-2 p-4">
      <Text className="text-[11px] font-medium uppercase tracking-wider text-muted-foreground">
        {label}
      </Text>
      <Text className="text-2xl font-semibold text-foreground tabular-nums">
        {value}
      </Text>
      {hint ? (
        <Text className="text-xs text-muted-foreground">{hint}</Text>
      ) : null}
    </View>
  );
}
