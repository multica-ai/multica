import { useMemo, useState } from "react";
import { Alert, FlatList, Modal, Pressable, StyleSheet, Text, View } from "react-native";
import { useNavigation } from "@react-navigation/native";
import type { NativeStackNavigationProp } from "@react-navigation/native-stack";
import { useTranslation } from "react-i18next";
import { Archive, ChevronLeft, Inbox as InboxIcon, MoreHorizontal } from "lucide-react-native";
import {
  deduplicateInboxItems,
  formatInboxDetailText,
  formatInboxTimeAgo,
  useArchiveAllInbox,
  useInboxList,
  useArchiveInbox,
  useMarkAllInboxRead,
  useMarkInboxRead,
} from "@multica/core/inbox";
import { useActorName } from "@multica/core/workspace/hooks";
import type { InboxItem } from "@multica/core/types";
import { EmptyState, LoadingState, Screen } from "../../components/ui/primitives";
import type { RootStackParamList } from "../../navigation/root-navigator";
import { useMobileWorkspace } from "../../navigation/workspace-context";
import { colors, radii, spacing } from "../../theme/tokens";

type InboxNavigation = NativeStackNavigationProp<RootStackParamList>;

export function InboxScreen() {
  const { t } = useTranslation();
  const navigation = useNavigation<InboxNavigation>();
  const { workspace } = useMobileWorkspace();
  const { getActorName, getActorInitials } = useActorName();
  const [bulkMenuOpen, setBulkMenuOpen] = useState(false);
  const {
    data: rawItems = [],
    isError,
    isLoading,
    isRefetching,
    refetch,
  } = useInboxList(workspace.id);
  const items = useMemo(() => deduplicateInboxItems(rawItems), [rawItems]);
  const markRead = useMarkInboxRead();
  const archive = useArchiveInbox();
  const markAllRead = useMarkAllInboxRead();
  const archiveAll = useArchiveAllInbox();
  const unreadCount = items.filter((item) => !item.read).length;
  const bulkActionPending = markAllRead.isPending || archiveAll.isPending;

  const openItem = (item: InboxItem) => {
    if (!item.read) markRead.mutate(item.id);
    if (item.issue_id) {
      navigation.navigate("IssueDetail", { issueId: item.issue_id });
      return;
    }
    navigation.navigate("InboxDetail", { inboxItemId: item.id });
  };

  const archiveItem = (item: InboxItem) => {
    archive.mutate(item.id);
  };

  const confirmMarkAllRead = () => {
    if (markAllRead.isPending) return;
    setBulkMenuOpen(false);
    Alert.alert(
      t("inbox.mark_all_read_confirm_title"),
      t("inbox.mark_all_read_confirm_description"),
      [
        { text: t("common.cancel"), style: "cancel" },
        {
          text: t("inbox.mark_all_read_confirm"),
          onPress: () => {
            markAllRead.mutate();
          },
        },
      ],
    );
  };

  const confirmArchiveAll = () => {
    if (archiveAll.isPending) return;
    setBulkMenuOpen(false);
    Alert.alert(
      t("inbox.archive_all_confirm_title"),
      t("inbox.archive_all_confirm_description"),
      [
        { text: t("common.cancel"), style: "cancel" },
        {
          text: t("inbox.archive_all_confirm"),
          style: "destructive",
          onPress: () => {
            archiveAll.mutate();
          },
        },
      ],
    );
  };

  return (
    <Screen padded={false}>
      <View style={styles.header}>
        <Pressable
          accessibilityLabel={t("common.go_back")}
          accessibilityRole="button"
          onPress={() => navigation.goBack()}
          style={({ pressed }) => [styles.iconButton, pressed && styles.pressed]}
        >
          <ChevronLeft color={colors.foreground} size={22} />
        </Pressable>
        <View style={styles.headerTitleWrap}>
          <Text style={styles.title}>{t("inbox.title")}</Text>
          {unreadCount > 0 ? <Text style={styles.count}>{unreadCount}</Text> : null}
        </View>
        <Pressable
          accessibilityLabel={t("inbox.bulk_actions")}
          accessibilityRole="button"
          onPress={() => setBulkMenuOpen(true)}
          style={({ pressed }) => [styles.iconButton, pressed && styles.pressed]}
        >
          <MoreHorizontal color={colors.foreground} size={22} />
        </Pressable>
      </View>
      <InboxBulkMenu
        disabled={bulkActionPending}
        onArchiveAll={confirmArchiveAll}
        onClose={() => setBulkMenuOpen(false)}
        onMarkAllRead={confirmMarkAllRead}
        open={bulkMenuOpen}
      />
      {isLoading ? <LoadingState /> : null}
      {isError ? (
        <EmptyState detail={t("common.pull_to_retry")} title={t("inbox.unable_to_load")} />
      ) : null}
      {!isLoading && !isError ? (
        <FlatList
          contentContainerStyle={items.length === 0 ? styles.emptyList : styles.list}
          data={items}
          keyExtractor={(item) => item.id}
          ListEmptyComponent={<InboxEmpty />}
          onRefresh={() => {
            void refetch();
          }}
          refreshing={isRefetching && !isLoading}
          renderItem={({ item }) => (
            <InboxRow
              getActorInitials={getActorInitials}
              item={item}
              onArchive={() => archiveItem(item)}
              onPress={() => openItem(item)}
              subtitle={formatInboxDetailText(item, getActorName)}
            />
          )}
        />
      ) : null}
    </Screen>
  );
}

function InboxBulkMenu({
  disabled,
  onArchiveAll,
  onClose,
  onMarkAllRead,
  open,
}: {
  disabled: boolean;
  onArchiveAll: () => void;
  onClose: () => void;
  onMarkAllRead: () => void;
  open: boolean;
}) {
  const { t } = useTranslation();
  if (!open) return null;
  return (
    <Modal animationType="fade" onRequestClose={onClose} transparent visible>
      <View style={styles.menuModalOverlay}>
        <Pressable style={StyleSheet.absoluteFill} onPress={onClose} />
        <View style={styles.bulkDropdown}>
          <DropdownItem
            disabled={disabled}
            label={t("inbox.mark_all_read")}
            onPress={onMarkAllRead}
          />
          <DropdownItem
            destructive
            disabled={disabled}
            label={t("inbox.archive_all")}
            onPress={onArchiveAll}
          />
        </View>
      </View>
    </Modal>
  );
}

function DropdownItem({
  destructive,
  disabled,
  label,
  onPress,
}: {
  destructive?: boolean;
  disabled?: boolean;
  label: string;
  onPress: () => void;
}) {
  return (
    <Pressable
      accessibilityRole="button"
      disabled={disabled}
      onPress={onPress}
      style={({ pressed }) => [
        styles.dropdownItem,
        pressed && !disabled && styles.pressed,
        disabled && styles.dropdownItemDisabled,
      ]}
    >
      <Text style={[styles.dropdownItemText, destructive && styles.dropdownItemTextDestructive]}>
        {label}
      </Text>
    </Pressable>
  );
}

function InboxRow({
  getActorInitials,
  item,
  onArchive,
  onPress,
  subtitle,
}: {
  getActorInitials: (type: string, id: string) => string;
  item: InboxItem;
  onArchive: () => void;
  onPress: () => void;
  subtitle: string;
}) {
  const { t } = useTranslation();
  const actorType = item.actor_type ?? item.recipient_type;
  const actorId = item.actor_id ?? item.recipient_id;
  const initials = getActorInitials(actorType, actorId);

  return (
    <Pressable
      accessibilityRole="button"
      onPress={onPress}
      style={({ pressed }) => [styles.row, pressed && styles.pressed]}
    >
      <View style={styles.avatar}>
        <Text style={styles.avatarText}>{initials}</Text>
      </View>
      <View style={styles.rowText}>
        <View style={styles.rowTitleLine}>
          {!item.read ? <View style={styles.unreadDot} /> : null}
          <Text
            numberOfLines={1}
            style={[styles.rowTitle, !item.read && styles.rowTitleUnread]}
          >
            {item.title}
          </Text>
        </View>
        <Text numberOfLines={1} style={[styles.subtitle, item.read && styles.subtitleRead]}>
          {subtitle}
        </Text>
      </View>
      <View style={styles.rowTrailing}>
        <Text style={[styles.time, item.read && styles.timeRead]}>
          {formatInboxTimeAgo(item.created_at)}
        </Text>
        <Pressable
          accessibilityLabel={t("inbox.archive_notification")}
          accessibilityRole="button"
          hitSlop={8}
          onPress={(event) => {
            event.stopPropagation();
            onArchive();
          }}
          style={({ pressed }) => [styles.archiveButton, pressed && styles.pressed]}
        >
          <Archive color={colors.mutedForeground} size={16} />
        </Pressable>
      </View>
    </Pressable>
  );
}

function InboxEmpty() {
  const { t } = useTranslation();
  return (
    <View style={styles.empty}>
      <InboxIcon color={colors.mutedForeground} size={28} />
      <Text style={styles.emptyTitle}>{t("inbox.no_notifications")}</Text>
    </View>
  );
}

const styles = StyleSheet.create({
  header: {
    alignItems: "center",
    borderBottomColor: colors.border,
    borderBottomWidth: StyleSheet.hairlineWidth,
    flexDirection: "row",
    minHeight: 52,
    paddingHorizontal: spacing.md,
  },
  headerTitleWrap: {
    alignItems: "center",
    flex: 1,
    flexDirection: "row",
    gap: spacing.xs,
    justifyContent: "center",
  },
  iconButton: {
    alignItems: "center",
    borderRadius: radii.md,
    height: 36,
    justifyContent: "center",
    width: 36,
  },
  title: {
    color: colors.foreground,
    fontSize: 17,
    fontWeight: "600",
  },
  count: {
    color: colors.mutedForeground,
    fontSize: 13,
    fontWeight: "500",
  },
  menuModalOverlay: {
    flex: 1,
  },
  bulkDropdown: {
    backgroundColor: colors.card,
    borderColor: colors.border,
    borderRadius: radii.md,
    borderWidth: StyleSheet.hairlineWidth,
    minWidth: 168,
    overflow: "hidden",
    position: "absolute",
    right: spacing.md,
    shadowColor: "#000",
    shadowOffset: { width: 0, height: 8 },
    shadowOpacity: 0.14,
    shadowRadius: 20,
    top: 52,
  },
  dropdownItem: {
    minHeight: 44,
    justifyContent: "center",
    paddingHorizontal: spacing.md,
  },
  dropdownItemDisabled: {
    opacity: 0.5,
  },
  dropdownItemText: {
    color: colors.foreground,
    fontSize: 15,
    fontWeight: "500",
  },
  dropdownItemTextDestructive: {
    color: colors.destructive,
  },
  list: {
    paddingVertical: spacing.sm,
  },
  emptyList: {
    flexGrow: 1,
  },
  row: {
    alignItems: "center",
    flexDirection: "row",
    gap: spacing.md,
    minHeight: 68,
    paddingHorizontal: spacing.lg,
    paddingVertical: spacing.sm,
  },
  avatar: {
    alignItems: "center",
    backgroundColor: colors.muted,
    borderRadius: 16,
    height: 32,
    justifyContent: "center",
    width: 32,
  },
  avatarText: {
    color: colors.foreground,
    fontSize: 12,
    fontWeight: "600",
  },
  rowText: {
    flex: 1,
    minWidth: 0,
  },
  rowTitleLine: {
    alignItems: "center",
    flexDirection: "row",
    gap: spacing.xs,
  },
  unreadDot: {
    backgroundColor: colors.info,
    borderRadius: 3,
    height: 6,
    width: 6,
  },
  rowTitle: {
    color: colors.foreground,
    flex: 1,
    fontSize: 14,
  },
  rowTitleUnread: {
    fontWeight: "600",
  },
  subtitle: {
    color: colors.mutedForeground,
    fontSize: 12,
    marginTop: 2,
  },
  subtitleRead: {
    opacity: 0.7,
  },
  rowTrailing: {
    alignItems: "flex-end",
    gap: spacing.xs,
  },
  time: {
    color: colors.mutedForeground,
    fontSize: 12,
  },
  timeRead: {
    opacity: 0.7,
  },
  archiveButton: {
    alignItems: "center",
    borderRadius: radii.sm,
    height: 28,
    justifyContent: "center",
    width: 28,
  },
  pressed: {
    opacity: 0.7,
  },
  empty: {
    alignItems: "center",
    flex: 1,
    gap: spacing.sm,
    justifyContent: "center",
    padding: spacing.xl,
  },
  emptyTitle: {
    color: colors.mutedForeground,
    fontSize: 14,
  },
});
