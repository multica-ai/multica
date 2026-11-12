import { Ionicons } from "@expo/vector-icons";
import { View } from "react-native";

import { Text } from "@/components/ui/text";
import { useT } from "@/lib/i18n";

/**
 * App-wide connectivity strip. Sits above the navigation tree in normal flow
 * (the root layout compensates the safe-area top inset) so it never covers a
 * screen header, and it disappears on the first online NetInfo event.
 */
export function OfflineBanner({ topInset }: { topInset: number }) {
  const { t } = useT("common");

  return (
    <View
      className="border-b border-warning/30 bg-warning/15"
      style={{ paddingTop: topInset }}
      accessibilityRole="alert"
      accessibilityLiveRegion="polite"
    >
      <View className="h-7 flex-row items-center justify-center gap-1.5 px-4">
        <Ionicons name="cloud-offline-outline" size={13} color="#a16207" />
        <Text className="text-[11px] text-warning" numberOfLines={1}>
          {t("network.offline")}
        </Text>
      </View>
    </View>
  );
}
