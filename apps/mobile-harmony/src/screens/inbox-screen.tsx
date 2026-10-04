/**
 * Inbox tab root — HarmonyOS port of
 * apps/mobile/app/(app)/[workspace]/(tabs)/inbox.tsx, keeping this slice's
 * props interface (workspaceName, onOpenItem) that app-shell.tsx wires:
 * navigation target resolution (issue page vs notice sheet) stays in the
 * shell, where the iOS screen derived an expo-router target itself.
 *
 * Trailing batch menu — mirrors web's dropdown
 * (packages/views/inbox/components/inbox-page.tsx). "Mark all read" is
 * first (most common batch op); "Archive all" is destructive so it gets the
 * iOS red treatment + confirm. iOS presented these through ActionSheetIOS +
 * Alert; RNOH has no ActionSheet/Alert bridge wired, so the same options,
 * order and copy render in the ported BottomSheet (a sheet + a confirm
 * sheet with Cancel / destructive "Archive all").
 *
 * Platform deltas (RNOH 0.82 — see AGENTS.md): NativeWind classes are
 * explicit StyleSheet styles; navigation goes through the `onOpenSearch` /
 * `onCreateIssue` callbacks instead of expo-router pushes, and those header
 * actions render only when the shell wired the corresponding callback (same
 * pattern as MyIssuesScreen); the workspace name prop is kept for shell
 * compatibility but the header shows only "Inbox", exactly like the iOS
 * screen.
 */
import { useMemo, useState } from "react";
import { FlatList, StyleSheet, View } from "react-native";
import { Pressable } from "@/components/ui/pressable";
import { useQuery } from "@tanstack/react-query";
import type { InboxItem } from "@multica/core/types";
import { Text } from "@/components/ui/text";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { Header } from "@/components/ui/header";
import { IconButton } from "@/components/ui/icon-button";
import { Icon } from "@/components/ui/icon";
import { BottomSheet } from "@/src/navigation/bottom-sheet";
import { SwipeableInboxRow } from "@/components/inbox/swipeable-inbox-row";
import { inboxListOptions } from "@/data/queries/inbox";
import { useInboxRealtime } from "@/data/realtime/use-inbox-realtime";
import {
  useArchiveAllInbox,
  useArchiveAllReadInbox,
  useArchiveCompletedInbox,
  useArchiveInbox,
  useMarkAllInboxRead,
  useMarkInboxRead,
} from "@/data/mutations/inbox";
import { useWorkspaceStore } from "@/data/workspace-store";
import type { ThemeColors } from "@/lib/theme";
import { useThemeColors } from "@/lib/use-theme-colors";
import { deduplicateInboxItems } from "@/lib/inbox-display";

interface Props {
  /** Kept for shell-interface compatibility; the header shows only "Inbox". */
  workspaceName: string;
  /** Open an inbox item (shell resolves issue vs notice route). */
  onOpenItem: (item: InboxItem) => void;
  /** Header search action; the button renders only when wired. */
  onOpenSearch?: () => void;
  /** Header create-issue action; the button renders only when wired. */
  onCreateIssue?: () => void;
}

export function InboxScreen({
  workspaceName,
  onOpenItem,
  onOpenSearch,
  onCreateIssue,
}: Props) {
  const c = useThemeColors();
  const s = styles(c);
  const wsId = useWorkspaceStore((st) => st.currentWorkspaceId);
  const {
    data: rawItems,
    isLoading,
    error,
    refetch,
    isRefetching,
  } = useQuery(inboxListOptions(wsId));
  // Dedup + drop archived to match web/desktop. See CLAUDE.md
  // "Behavioral parity" → inbox dedup incident.
  const data = useMemo(
    () => deduplicateInboxItems(rawItems ?? []),
    [rawItems],
  );
  const markRead = useMarkInboxRead();
  const markAllRead = useMarkAllInboxRead();
  const archive = useArchiveInbox();
  const archiveAll = useArchiveAllInbox();
  const archiveAllRead = useArchiveAllReadInbox();
  const archiveCompleted = useArchiveCompletedInbox();

  // WebSocket subscriptions patch the list/unread caches in place.
  useInboxRealtime();

  const [menuOpen, setMenuOpen] = useState(false);
  const [confirmOpen, setConfirmOpen] = useState(false);

  const onPressItem = (item: InboxItem) => {
    if (!item.read) {
      // Optimistic read flip lives in useMarkInboxRead.onMutate — fires
      // setQueryData synchronously before the cancelQueries await, so the
      // row is already styled "read" by the time the stack push transition
      // captures its source snapshot.
      markRead.mutate(item.id);
    }
    onOpenItem(item);
  };

  const closeMenu = () => setMenuOpen(false);

  return (
    <View style={s.screen}>
      <Header
        title="Inbox"
        right={
          <>
            {/* iOS order: batch menu, then HeaderActions (search, new-issue
                — apps/mobile/components/ui/app-header-actions.tsx). The
                search/add buttons render only when the shell wired them. */}
            <IconButton
              name="ellipsis-horizontal"
              onPress={() => setMenuOpen(true)}
              accessibilityLabel="Inbox actions"
            />
            {onOpenSearch ? (
              <IconButton
                name="search"
                onPress={onOpenSearch}
                accessibilityLabel="Search"
              />
            ) : null}
            {onCreateIssue ? (
              <IconButton
                name="add"
                iconSize={24}
                onPress={onCreateIssue}
                accessibilityLabel="New issue"
              />
            ) : null}
          </>
        }
      />
      {isLoading ? (
        <InboxLoading />
      ) : error ? (
        <View style={s.errorWrap}>
          <Text style={s.errorText}>
            Failed to load inbox:{" "}
            {error instanceof Error ? error.message : "unknown error"}
          </Text>
          <Button variant="outline" onPress={() => void refetch()}>
            <Text>Retry</Text>
          </Button>
        </View>
      ) : !data || data.length === 0 ? (
        <InboxEmpty iconColor={c.mutedForeground} />
      ) : (
        <FlatList
          data={data}
          keyExtractor={(item) => item.id}
          ItemSeparatorComponent={() => (
            <View style={s.separator} />
          )}
          contentContainerStyle={s.listContent}
          renderItem={({ item }) => (
            <SwipeableInboxRow
              item={item}
              onPress={() => onPressItem(item)}
              onArchive={() => archive.mutate(item.id)}
            />
          )}
          refreshing={isRefetching}
          onRefresh={() => void refetch()}
        />
      )}

      {/* Action-sheet stand-in for iOS's ActionSheetIOS — same option order
          and copy; "Archive all" is the destructive entry (index 4 there). */}
      <BottomSheet visible={menuOpen} onClose={closeMenu} avoidKeyboard={false}>
        <Text style={s.sheetTitle}>Inbox</Text>
        <SheetAction
          label="Mark all read"
          onPress={() => {
            closeMenu();
            markAllRead.mutate();
          }}
        />
        <SheetAction
          label="Archive all read"
          onPress={() => {
            closeMenu();
            archiveAllRead.mutate();
          }}
        />
        <SheetAction
          label="Archive completed"
          onPress={() => {
            closeMenu();
            archiveCompleted.mutate();
          }}
        />
        <View style={s.sheetSeparator} />
        <SheetAction
          label="Archive all"
          destructive
          onPress={() => {
            closeMenu();
            setConfirmOpen(true);
          }}
        />
        <View style={s.sheetSeparator} />
        <SheetAction label="Cancel" bold onPress={closeMenu} />
      </BottomSheet>

      {/* iOS's Alert.alert("Archive all?", …) confirm — same copy and
          Cancel / destructive button pair. */}
      <BottomSheet
        visible={confirmOpen}
        onClose={() => setConfirmOpen(false)}
        avoidKeyboard={false}
      >
        <Text style={s.confirmTitle}>Archive all?</Text>
        <Text style={s.confirmMessage}>
          This archives every inbox item, read or unread. You can still find
          them via the issue pages.
        </Text>
        <View style={s.confirmActions}>
          <Button
            variant="outline"
            style={s.confirmButton}
            onPress={() => setConfirmOpen(false)}
          >
            <Text>Cancel</Text>
          </Button>
          <Button
            variant="destructive"
            style={s.confirmButton}
            onPress={() => {
              setConfirmOpen(false);
              archiveAll.mutate();
            }}
          >
            <Text>Archive all</Text>
          </Button>
        </View>
      </BottomSheet>
    </View>
  );
}

// Loading state — 6 row-shaped Skeletons matching InboxRow's layout
// (avatar circle + two text lines). Perceived perf wins over a centered
// spinner because the eye immediately sees the list-like structure.
function InboxLoading() {
  const c = useThemeColors();
  const s = styles(c);
  return (
    <View style={s.loadingWrap}>
      {Array.from({ length: 6 }).map((_, i) => (
        <View key={i} style={s.loadingRow}>
          <Skeleton style={s.loadingAvatar} />
          <View style={s.loadingColumn}>
            <Skeleton style={s.loadingLine1} />
            <Skeleton style={s.loadingLine2} />
          </View>
        </View>
      ))}
    </View>
  );
}

function InboxEmpty({ iconColor }: { iconColor: string }) {
  const c = useThemeColors();
  const s = styles(c);
  return (
    <View style={s.emptyWrap}>
      <Icon name="mail-open-outline" size={42} color={iconColor} />
      <Text style={s.emptyTitle}>Inbox zero</Text>
      <Text style={s.emptyMessage}>
        Mentions, assignments, and agent updates appear here.
      </Text>
    </View>
  );
}

/** One tappable action-sheet row. */
function SheetAction({
  label,
  onPress,
  destructive,
  bold,
}: {
  label: string;
  onPress: () => void;
  destructive?: boolean;
  bold?: boolean;
}) {
  const c = useThemeColors();
  return (
    <Pressable
      accessibilityRole="button"
      accessibilityLabel={label}
      style={({ pressed }) => [
        sheetStyles.action,
        { backgroundColor: pressed ? c.accent : c.card },
      ]}
      onPress={onPress}
    >
      <Text
        style={[
          sheetStyles.actionLabel,
          {
            color: destructive ? c.destructive : c.foreground,
            fontWeight: bold ? "600" : "400",
          },
        ]}
      >
        {label}
      </Text>
    </Pressable>
  );
}

const styles = (c: ThemeColors) =>
  StyleSheet.create({
    // flex-1 bg-background
    screen: { flex: 1, backgroundColor: c.background },
    // px-4 gap-3 pt-4
    errorWrap: { paddingHorizontal: 16, gap: 12, paddingTop: 16 },
    // text-sm text-destructive
    errorText: { fontSize: 14, color: c.destructive },
    // h-px bg-border ml-16
    separator: { height: 1, backgroundColor: c.border, marginLeft: 64 },
    // pb-6
    listContent: { paddingBottom: 24 },
    // px-4 pt-4 gap-4
    loadingWrap: { paddingHorizontal: 16, paddingTop: 16, gap: 16 },
    // flex-row gap-3
    loadingRow: { flexDirection: "row", gap: 12 },
    // size-9 rounded-full
    loadingAvatar: { width: 36, height: 36, borderRadius: 18 },
    // flex-1 gap-2 pt-1
    loadingColumn: { flex: 1, gap: 8, paddingTop: 4 },
    // h-3.5 w-3/4
    loadingLine1: { height: 14, width: "75%" },
    // h-3 w-1/2
    loadingLine2: { height: 12, width: "50%" },
    // flex-1 items-center justify-center px-8 gap-3
    emptyWrap: {
      flex: 1,
      alignItems: "center",
      justifyContent: "center",
      paddingHorizontal: 32,
      gap: 12,
    },
    // text-base font-medium text-foreground text-center
    emptyTitle: {
      fontSize: 16,
      fontWeight: "500",
      color: c.foreground,
      textAlign: "center",
    },
    // text-sm text-muted-foreground text-center
    emptyMessage: {
      fontSize: 14,
      color: c.mutedForeground,
      textAlign: "center",
    },
    sheetTitle: {
      fontSize: 13,
      color: c.mutedForeground,
      textAlign: "center",
      paddingVertical: 10,
    },
    sheetSeparator: { height: 1, backgroundColor: c.border },
    // text-lg font-semibold text-foreground
    confirmTitle: {
      fontSize: 18,
      fontWeight: "600",
      color: c.foreground,
      textAlign: "center",
      marginTop: 8,
    },
    // text-sm leading-5 text-muted-foreground
    confirmMessage: {
      fontSize: 14,
      lineHeight: 20,
      color: c.mutedForeground,
      textAlign: "center",
      marginTop: 8,
      paddingHorizontal: 16,
    },
    confirmActions: {
      flexDirection: "row",
      gap: 12,
      paddingHorizontal: 16,
      paddingTop: 20,
      paddingBottom: 8,
    },
    confirmButton: { flex: 1 },
  });

const sheetStyles = StyleSheet.create({
  // Action-sheet rows: full-width, centered label, comfortable height.
  action: {
    height: 50,
    alignItems: "center",
    justifyContent: "center",
  },
  actionLabel: { fontSize: 16 },
});
