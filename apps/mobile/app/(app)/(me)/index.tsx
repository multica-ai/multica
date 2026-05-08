import { Pressable, ScrollView, Text, View } from "react-native";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import * as Haptics from "expo-haptics";
import { useQuery } from "@tanstack/react-query";
import { useAuthStore } from "@multica/core/auth";
import { workspaceListOptions } from "@multica/core/workspace/queries";
import { useWorkspaceId } from "@multica/core/hooks";

import { ScreenHeader } from "@/components/ui/screen-header";

export default function MeScreen() {
  const user = useAuthStore((s) => s.user);
  const logout = useAuthStore((s) => s.logout);
  const wsId = useWorkspaceId();
  const insets = useSafeAreaInsets();
  const { data: workspaces } = useQuery(workspaceListOptions());
  const currentWs = workspaces?.find((w) => w.id === wsId);

  const handleLogout = () => {
    Haptics.selectionAsync().catch(() => {});
    logout();
  };

  return (
    <View className="flex-1 bg-background" style={{ paddingTop: insets.top }}>
      <ScreenHeader title="Me" />
      <ScrollView className="flex-1">
        <View className="px-6 py-4 gap-6">
          <View className="gap-1">
            <Text className="text-muted-foreground text-xs uppercase tracking-wider">
              Account
            </Text>
            <Text className="text-foreground text-base font-medium">
              {user?.name ?? "—"}
            </Text>
            <Text className="text-muted-foreground text-sm">
              {user?.email ?? ""}
            </Text>
          </View>

          <View className="gap-1">
            <Text className="text-muted-foreground text-xs uppercase tracking-wider">
              Workspace
            </Text>
            <Text className="text-foreground text-base font-medium">
              {currentWs?.name ?? "—"}
            </Text>
            <Text className="text-muted-foreground text-sm">
              {currentWs?.slug ?? ""}
            </Text>
          </View>

          <Pressable
            onPress={handleLogout}
            className="bg-secondary rounded-md py-3 items-center mt-4 active:bg-secondary/80"
          >
            <Text className="text-secondary-foreground font-medium">Sign out</Text>
          </Pressable>
        </View>
      </ScrollView>
    </View>
  );
}
