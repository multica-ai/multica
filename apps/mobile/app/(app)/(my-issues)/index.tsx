import { useMemo, useState } from "react";
import {
  ActivityIndicator,
  FlatList,
  Pressable,
  RefreshControl,
  Text,
  View,
} from "react-native";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import { useRouter } from "expo-router";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { useAuthStore } from "@multica/core/auth";
import {
  myIssueListOptions,
  type MyIssuesFilter,
} from "@multica/core/issues/queries";

import { MyIssueRow } from "@/components/my-issues/issue-row";
import { ScreenHeader } from "@/components/ui/screen-header";

type Scope = "assigned" | "created";

const SCOPE_LABEL: Record<Scope, string> = {
  assigned: "Assigned",
  created: "Created",
};

export default function MyIssuesScreen() {
  const router = useRouter();
  const wsId = useWorkspaceId();
  const user = useAuthStore((s) => s.user);
  const insets = useSafeAreaInsets();
  const [scope, setScope] = useState<Scope>("assigned");

  const filter = useMemo<MyIssuesFilter>(() => {
    if (!user?.id) return {};
    return scope === "assigned"
      ? { assignee_id: user.id }
      : { creator_id: user.id };
  }, [scope, user?.id]);

  const { data, refetch, isLoading, isRefetching, error } = useQuery({
    ...myIssueListOptions(wsId, scope, filter),
    enabled: !!user?.id,
  });

  return (
    <View className="flex-1 bg-background" style={{ paddingTop: insets.top }}>
      <ScreenHeader title="My Issues" />

      {/* Scope segments — sit just under the header, not sticky inside FlatList. */}
      <View className="flex-row gap-1 px-4 pb-3 bg-background">
        {(Object.keys(SCOPE_LABEL) as Scope[]).map((s) => (
          <ScopeTab
            key={s}
            label={SCOPE_LABEL[s]}
            active={scope === s}
            onPress={() => setScope(s)}
          />
        ))}
      </View>

      <FlatList
        data={data ?? []}
        keyExtractor={(it) => it.id}
        renderItem={({ item }) => (
          <MyIssueRow
            issue={item}
            onPress={() =>
              router.push(`/(app)/(my-issues)/issue/${item.id}` as never)
            }
          />
        )}
        ItemSeparatorComponent={() => (
          <View className="h-px bg-border ml-12" />
        )}
        refreshControl={
          <RefreshControl refreshing={isRefetching} onRefresh={refetch} />
        }
        ListEmptyComponent={
          isLoading ? (
            <View className="px-8 pt-16 items-center">
              <ActivityIndicator />
            </View>
          ) : error ? (
            <View className="px-8 pt-16 items-center">
              <Text className="text-destructive text-center">
                {error instanceof Error ? error.message : String(error)}
              </Text>
            </View>
          ) : (
            <View className="px-8 pt-16 items-center">
              <Text className="text-muted-foreground">No issues</Text>
            </View>
          )
        }
      />
    </View>
  );
}

function ScopeTab({
  label,
  active,
  onPress,
}: {
  label: string;
  active: boolean;
  onPress: () => void;
}) {
  return (
    <Pressable
      onPress={onPress}
      className={
        active
          ? "rounded-md bg-muted px-3 py-1.5"
          : "rounded-md px-3 py-1.5 active:bg-muted/50"
      }
    >
      <Text
        className={
          active
            ? "text-foreground text-sm font-medium"
            : "text-muted-foreground text-sm"
        }
      >
        {label}
      </Text>
    </Pressable>
  );
}
