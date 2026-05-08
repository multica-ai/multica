import { useMemo } from "react";
import {
  ActionSheetIOS,
  ActivityIndicator,
  FlatList,
  Pressable,
  RefreshControl,
  Text,
  View,
} from "react-native";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import { Swipeable } from "react-native-gesture-handler";
import { useRouter } from "expo-router";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  inboxListOptions,
  deduplicateInboxItems,
} from "@multica/core/inbox/queries";
import {
  useArchiveAllInbox,
  useArchiveAllReadInbox,
  useArchiveCompletedInbox,
  useArchiveInbox,
  useMarkAllInboxRead,
  useMarkInboxRead,
} from "@multica/core/inbox/mutations";

import { InboxRow } from "@/components/inbox/inbox-row";
import {
  HeaderMenuButton,
  ScreenHeader,
} from "@/components/ui/screen-header";

export default function InboxScreen() {
  const wsId = useWorkspaceId();
  const router = useRouter();
  const insets = useSafeAreaInsets();
  const markRead = useMarkInboxRead();
  const archive = useArchiveInbox();
  const markAllRead = useMarkAllInboxRead();
  const archiveRead = useArchiveAllReadInbox();
  const archiveCompleted = useArchiveCompletedInbox();
  const archiveAll = useArchiveAllInbox();

  const { data, refetch, isLoading, isRefetching, error } = useQuery(
    inboxListOptions(wsId),
  );

  const items = useMemo(() => {
    if (!data) return [];
    return deduplicateInboxItems(data);
  }, [data]);

  const showBulkMenu = () => {
    ActionSheetIOS.showActionSheetWithOptions(
      {
        options: [
          "Cancel",
          "Mark all read",
          "Archive read",
          "Archive completed",
          "Archive all",
        ],
        cancelButtonIndex: 0,
        destructiveButtonIndex: 4,
      },
      (i) => {
        if (i === 1) markAllRead.mutate();
        else if (i === 2) archiveRead.mutate();
        else if (i === 3) archiveCompleted.mutate();
        else if (i === 4) archiveAll.mutate();
      },
    );
  };

  return (
    <View
      className="flex-1 bg-background"
      style={{ paddingTop: insets.top }}
    >
      <ScreenHeader
        title="Inbox"
        right={<HeaderMenuButton onPress={showBulkMenu} />}
      />
      {isLoading ? (
        <View className="flex-1 items-center justify-center">
          <ActivityIndicator />
        </View>
      ) : error ? (
        <View className="flex-1 items-center justify-center px-8">
          <Text className="text-destructive text-center">
            {error instanceof Error ? error.message : String(error)}
          </Text>
        </View>
      ) : (
        <FlatList
          data={items}
          keyExtractor={(it) => it.id}
          renderItem={({ item }) => (
            <Swipeable
              renderRightActions={() => (
                <Pressable
                  onPress={() => archive.mutate(item.id)}
                  className="bg-destructive justify-center items-center"
                  style={{ width: 88 }}
                >
                  <Text className="text-white font-medium">Archive</Text>
                </Pressable>
              )}
              overshootRight={false}
            >
              <InboxRow
                item={item}
                onPress={() => {
                  if (!item.read) markRead.mutate(item.id);
                  if (!item.issue_id) return;
                  router.push(`/(app)/(inbox)/issue/${item.issue_id}`);
                }}
              />
            </Swipeable>
          )}
          ItemSeparatorComponent={() => (
            <View className="h-px bg-border ml-16" />
          )}
          refreshControl={
            <RefreshControl refreshing={isRefetching} onRefresh={refetch} />
          }
          ListEmptyComponent={
            <View className="px-8 pt-16 items-center">
              <Text className="text-muted-foreground">No notifications yet</Text>
            </View>
          }
        />
      )}
    </View>
  );
}
