import { ActivityIndicator, ScrollView, View } from "react-native";
import { SafeAreaView } from "react-native-safe-area-context";
import { useQuery } from "@tanstack/react-query";
import { router } from "expo-router";
import { Text } from "@/components/ui/text";
import { Button } from "@/components/ui/button";
import { CardPressable } from "@/components/ui/card";
import { workspaceListOptions } from "@/data/queries/workspaces";
import { useAuthStore } from "@/data/auth-store";
import { useWorkspaceStore } from "@/data/workspace-store";

export default function SelectWorkspace() {
  const user = useAuthStore((s) => s.user);
  const logout = useAuthStore((s) => s.logout);
  const setCurrentWorkspace = useWorkspaceStore((s) => s.setCurrentWorkspace);
  const { data, isLoading, error, refetch } = useQuery(workspaceListOptions());

  const onSelect = async (id: string, slug: string) => {
    await setCurrentWorkspace(id, slug);
    router.replace(`/${slug}/inbox`);
  };

  return (
    <SafeAreaView className="flex-1 bg-background">
      <ScrollView contentContainerClassName="px-6 py-6 gap-6">
        <View className="gap-1">
          <Text className="text-xs uppercase tracking-wider text-muted-foreground">
            Signed in as
          </Text>
          <Text className="text-base text-foreground">{user?.email}</Text>
        </View>

        <View className="gap-3">
          <Text className="text-2xl font-semibold text-foreground">
            Select a workspace
          </Text>

          {isLoading ? (
            <View className="py-8 items-center">
              <ActivityIndicator />
            </View>
          ) : error ? (
            <View className="gap-3">
              <Text className="text-sm text-destructive">
                Failed to load workspaces:{" "}
                {error instanceof Error ? error.message : "unknown error"}
              </Text>
              <Button variant="outline" onPress={() => refetch()}>
                <Text>Retry</Text>
              </Button>
            </View>
          ) : !data || data.length === 0 ? (
            <Text className="text-sm text-muted-foreground">
              You don&apos;t belong to any workspaces yet. Contact your workspace
              admin to be invited.
            </Text>
          ) : (
            <View className="gap-3">
              {data.map((ws) => (
                <CardPressable
                  key={ws.id}
                  onPress={() => onSelect(ws.id, ws.slug)}
                >
                  <Text className="text-base font-semibold text-foreground">
                    {ws.name}
                  </Text>
                  <Text className="text-xs text-muted-foreground mt-1">
                    /{ws.slug}
                  </Text>
                  {ws.description ? (
                    <Text className="text-sm text-muted-foreground mt-2">
                      {ws.description}
                    </Text>
                  ) : null}
                </CardPressable>
              ))}
            </View>
          )}
        </View>

        <View className="pt-4 border-t border-border">
          <Button variant="outline" onPress={() => logout()}>
            <Text>Sign out</Text>
          </Button>
        </View>
      </ScrollView>
    </SafeAreaView>
  );
}
