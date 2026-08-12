import { useNetInfo } from "@react-native-community/netinfo";
import { View } from "react-native";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import { Text } from "@/components/ui/text";

/** Persistent network state, deliberately not a transient toast. */
export function NetworkOfflineBanner() {
  const netInfo = useNetInfo();
  const insets = useSafeAreaInsets();
  const isOffline = netInfo.isConnected === false;

  if (!isOffline) return null;

  return (
    <View
      className="absolute left-0 right-0 z-50 bg-amber-500 px-4 py-2"
      style={{ top: insets.top }}
      accessibilityRole="alert"
    >
      <Text className="text-center text-xs font-medium text-black">
        You&apos;re offline. Showing the last synced content.
      </Text>
    </View>
  );
}
