import type { ReactNode } from "react";
import { View } from "react-native";
import { SafeAreaView } from "react-native-safe-area-context";
import { router } from "expo-router";
import { Text } from "@/components/ui/text";
import { Button } from "@/components/ui/button";
import { IconButton } from "@/components/ui/icon-button";
import { useT } from "@/lib/i18n";
import { cn } from "@/lib/utils";

/** Shared chrome for the AI settings, queue, folder and usage sheets. */
export function AISheet({ title, onBack, trailing, maxHeight, children }: { title: string; onBack?: () => void; trailing?: ReactNode; maxHeight?: number; children: ReactNode }) {
  const { t } = useT("aiSessions");
  return <SafeAreaView edges={["bottom"]} style={{ maxHeight }} className={cn("bg-background", maxHeight === undefined ? "flex-1" : "shrink")}>
    <View className="shrink-0 flex-row items-center gap-2 px-4 pt-3 pb-2">
      {onBack && <IconButton name="chevron-back" hitSlop={4} accessibilityLabel={t("settings")} onPress={onBack} />}
      <Text style={{ lineHeight: undefined }} accessibilityRole="header" className="flex-1 text-lg font-semibold" numberOfLines={2}>{title}</Text>
      {trailing}
      <Button style={{ height: "auto", minHeight: 36 }} variant="ghost" size="sm" hitSlop={4} onPress={() => router.back()}><Text style={{ lineHeight: undefined }}>{t("done")}</Text></Button>
    </View>
    <View className={cn("px-4 gap-3", maxHeight === undefined ? "flex-1" : "shrink")}>{children}</View>
  </SafeAreaView>;
}
