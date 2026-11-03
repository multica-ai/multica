import { ActivityIndicator, Pressable, ScrollView, View } from "react-native";
import { Ionicons } from "@expo/vector-icons";
import { Text } from "@/components/ui/text";
import { useT } from "@/lib/i18n";
import type { TranscriptRow } from "@/data/ai-sessions/projection";
import { useColorScheme } from "@/lib/use-color-scheme";
import { THEME } from "@/lib/theme";

export function AIActivityGroup({ items, expanded, onToggle }: { items: TranscriptRow[]; expanded: boolean; onToggle(): void }) {
  const { t } = useT("aiSessions");
  const { colorScheme } = useColorScheme();
  return <View className="mb-4 rounded-xl bg-surface-1">
    <Pressable accessibilityRole="button" accessibilityState={{ expanded }} accessibilityLabel={t("activity_count", { count: items.length })} className="min-h-11 flex-row items-center gap-2 p-3" onPress={onToggle}>
      {items.some((item) => item.running) && <ActivityIndicator size="small" />}
      <Ionicons name={expanded ? "chevron-down" : "chevron-forward"} size={14} color={THEME[colorScheme].mutedForeground} />
      <Text style={{ lineHeight: undefined }} className="text-xs text-muted-foreground flex-1">{t("activity_count", { count: items.length })}</Text>
    </Pressable>
    {expanded && <ScrollView nestedScrollEnabled style={{ maxHeight: 280 }} contentContainerStyle={{ padding: 12, paddingTop: 0, gap: 12 }}>{items.map((item, index) => <View key={item.id} className="gap-1">
      <Text style={{ lineHeight: undefined }} className="text-xs font-medium text-muted-foreground">{index + 1}. {t(item.role === "reasoning" ? item.running ? "thinking" : "thought" : "view_activity")}</Text>
      {!!item.content && <Text style={{ lineHeight: undefined }} selectable className="text-xs text-muted-foreground">{item.content}</Text>}
    </View>)}</ScrollView>}
  </View>;
}
