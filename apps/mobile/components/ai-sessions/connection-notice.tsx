import { View } from "react-native";
import { Text } from "@/components/ui/text";
import { Button } from "@/components/ui/button";
import { useT } from "@/lib/i18n";
import { useAIDeviceLink } from "@/data/ai-sessions/provider";

export function ConnectionNotice({ connected, subscriptionRejected = false, onRetrySubscription }: { connected: boolean; subscriptionRejected?: boolean; onRetrySubscription?: () => void }) {
  const { error, retry } = useAIDeviceLink();
  const { t } = useT("aiSessions");
  if (connected && !error) {
    if (!subscriptionRejected) return null;
    return <View className="mx-4 my-2 rounded-lg bg-secondary p-3 gap-2" accessibilityRole="alert">
      <Text className="text-sm text-foreground">{t("subscription_rejected")}</Text>
      <Button variant="outline" size="sm" onPress={onRetrySubscription}><Text>{t("retry")}</Text></Button>
    </View>;
  }
  return <View className="mx-4 my-2 rounded-lg bg-secondary p-3 gap-2" accessibilityRole="alert">
    <Text className="text-sm text-foreground">{t(error ?? "disconnected")}</Text>
    {!error && <Text className="text-xs text-muted-foreground">{t("offline_help")}</Text>}
    <Button variant="outline" size="sm" onPress={retry}><Text>{t("retry")}</Text></Button>
  </View>;
}
