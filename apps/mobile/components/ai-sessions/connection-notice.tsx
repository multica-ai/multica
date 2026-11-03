import { useEffect, useState } from "react";
import { View } from "react-native";
import { Text } from "@/components/ui/text";
import { Button } from "@/components/ui/button";
import { useT } from "@/lib/i18n";
import { useAIDeviceLink } from "@/data/ai-sessions/provider";
import { useAIConnection } from "@/data/ai-sessions/hooks";

/** Long enough for a dead path to be found and redialed after returning to the app. */
const OFFLINE_NOTICE_DELAY_MS = 8_000;

export function ConnectionNotice({ connected, subscriptionRejected = false, onRetrySubscription }: { connected: boolean; subscriptionRejected?: boolean; onRetrySubscription?: () => void }) {
  const { error: linkError, retry: retryLink } = useAIDeviceLink();
  const { client, error: sessionError } = useAIConnection();
  const error = sessionError ?? linkError;
  const retry = () => sessionError ? client?.retryConnection() : retryLink();
  const { t } = useT("aiSessions");
  const [offlineSettled, setOfflineSettled] = useState(false);
  useEffect(() => {
    if (connected) return;
    const timer = setTimeout(() => setOfflineSettled(true), OFFLINE_NOTICE_DELAY_MS);
    return () => { clearTimeout(timer); setOfflineSettled(false); };
  }, [connected]);
  if (!connected && !error && !offlineSettled) return null;
  if (connected && !error) {
    if (!subscriptionRejected) return null;
    return <View className="mx-4 my-2 rounded-lg bg-secondary p-3 gap-2" accessibilityRole="alert">
      <Text style={{ lineHeight: undefined }} className="text-sm text-foreground">{t("subscription_rejected")}</Text>
      <Button style={{ height: "auto", minHeight: 36 }} variant="outline" size="sm" hitSlop={4} onPress={onRetrySubscription}><Text style={{ lineHeight: undefined }}>{t("retry")}</Text></Button>
    </View>;
  }
  return <View className="mx-4 my-2 rounded-lg bg-secondary p-3 gap-2" accessibilityRole="alert">
    <Text style={{ lineHeight: undefined }} className="text-sm text-foreground">{t(error ?? "disconnected")}</Text>
    {!error && <Text style={{ lineHeight: undefined }} className="text-xs text-muted-foreground">{t("offline_help")}</Text>}
    <Button style={{ height: "auto", minHeight: 36 }} variant="outline" size="sm" hitSlop={4} onPress={retry}><Text style={{ lineHeight: undefined }}>{t("retry")}</Text></Button>
  </View>;
}
