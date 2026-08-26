import { ActivityIndicator, View } from "react-native";
import { SafeAreaView } from "react-native-safe-area-context";
import { router } from "expo-router";
import { Button } from "@/components/ui/button";
import { Text } from "@/components/ui/text";
import { useAuthStore } from "@/data/auth-store";

/**
 * Upgrade edge case: a pre-offline-release session has a token but no saved
 * user snapshot. It is intentionally not treated as a login failure.
 */
export default function OfflineAccountInfo() {
  const initialize = useAuthStore((s) => s.initialize);
  const logout = useAuthStore((s) => s.logout);
  const isLoading = useAuthStore((s) => s.isLoading);

  const retry = async () => {
    // initialize() resets isLoading in a finally, so a rejection here only
    // means "still no answer" — stay on this screen rather than surface an
    // unhandled rejection from the press handler.
    try {
      await initialize();
    } catch {
      return;
    }
    const { user, hasToken } = useAuthStore.getState();
    if (user) router.replace("/");
    else if (!hasToken) router.replace("/login");
  };

  return (
    <SafeAreaView className="flex-1 bg-background">
      <View className="flex-1 justify-center gap-4 px-6">
        <Text className="text-2xl font-semibold text-foreground">
          Account info unavailable offline
        </Text>
        <Text className="text-sm leading-5 text-muted-foreground">
          This session is still saved on this device, but it was created before
          offline account details could be stored. Connect once to restore it.
        </Text>
        <Button variant="outline" onPress={retry} disabled={isLoading}>
          {isLoading ? <ActivityIndicator /> : <Text>Retry</Text>}
        </Button>
        <Button variant="destructive" onPress={() => void logout()}>
          <Text>Sign out</Text>
        </Button>
      </View>
    </SafeAreaView>
  );
}
