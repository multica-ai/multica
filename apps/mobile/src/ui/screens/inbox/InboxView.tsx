import { FlatList, Pressable, RefreshControl, StyleSheet, View } from "react-native";
import { Avatar, type AvatarActor } from "../../components/Avatar";
import { Capsule } from "../../components/Floating";
import { TitleHeader } from "../../components/Header";
import { BOTTOM_CLEARANCE, Screen } from "../../components/Screen";
import { Empty, Failure, Loading } from "../../components/States";
import { StatusIcon } from "../../components/StatusIcon";
import { Txt } from "../../components/Text";
import { Icon, type IconName } from "../../icons/Icon";
import { useStrings } from "../../strings";
import { useTheme } from "../../theme";

export type InboxBadge = { kind: "icon"; icon: IconName } | { kind: "status"; status: string };

export interface InboxRow {
  id: string;
  issueId: string | null;
  identifier: string | null;
  title: string;
  summary: string;
  time: string;
  actor: AvatarActor;
  badge: InboxBadge | null;
  unread: boolean;
}

interface InboxViewProps {
  state: "loading" | "error" | "ready";
  rows: readonly InboxRow[];
  refreshing?: boolean;
  onRefresh?: () => void;
  onRetry: () => void;
  onOpen: (row: InboxRow) => void;
  onMarkAllRead?: () => void;
  onEndReached?: () => void;
}

function Badge({ badge }: { badge: InboxBadge }) {
  const palette = useTheme();
  return badge.kind === "status" ? <StatusIcon status={badge.status} size={12} /> : <Icon name={badge.icon} size={11} color={palette.label2} strokeWidth={2.4} />;
}

function Row({ row, onOpen }: { row: InboxRow; onOpen: (row: InboxRow) => void }) {
  const palette = useTheme();
  return (
    <Pressable
      accessibilityRole="button"
      onPress={() => onOpen(row)}
      style={({ pressed }) => [styles.row, pressed && { backgroundColor: palette.fill }]}
    >
      {row.unread ? <View style={[styles.dot, { backgroundColor: palette.accent }]} /> : null}
      <Avatar actor={row.actor} badge={row.badge ? <Badge badge={row.badge} /> : undefined} />
      <View style={styles.body}>
        <Txt variant="row" numberOfLines={1} tone={row.unread ? "primary" : "secondary"}>
          {row.identifier ? (
            <Txt variant="rowRegular" tone="secondary">
              {row.identifier}{"  "}
            </Txt>
          ) : null}
          {row.title}
        </Txt>
        <Txt variant="meta" tone={row.unread ? "secondary" : "tertiary"} numberOfLines={2} style={styles.summary}>
          {row.summary} · {row.time}
        </Txt>
      </View>
    </Pressable>
  );
}

export function InboxView({ state, rows, refreshing = false, onRefresh, onRetry, onOpen, onMarkAllRead, onEndReached }: InboxViewProps) {
  const strings = useStrings();
  const palette = useTheme();
  return (
    <Screen>
      <TitleHeader
        title={strings.inbox.title}
        right={onMarkAllRead ? <Capsule actions={[{ icon: "CheckCircle2", label: strings.inbox.markAllRead, onPress: onMarkAllRead }]} /> : null}
      />
      {state === "loading" ? (
        <Loading />
      ) : state === "error" ? (
        <Failure title={strings.common.loadFailed} retryLabel={strings.common.retry} onRetry={onRetry} />
      ) : (
        <FlatList
          data={rows}
          keyExtractor={(row) => row.id}
          renderItem={({ item }) => <Row row={item} onOpen={onOpen} />}
          contentContainerStyle={[styles.list, rows.length === 0 && styles.fill]}
          ListEmptyComponent={<Empty title={strings.inbox.empty} detail={strings.inbox.emptyDetail} />}
          onEndReached={onEndReached}
          onEndReachedThreshold={0.5}
          refreshControl={onRefresh ? <RefreshControl refreshing={refreshing} onRefresh={onRefresh} tintColor={palette.label3} /> : undefined}
        />
      )}
    </Screen>
  );
}

const styles = StyleSheet.create({
  list: { paddingTop: 6, paddingBottom: BOTTOM_CLEARANCE },
  fill: { flexGrow: 1 },
  row: { flexDirection: "row", gap: 14, paddingVertical: 11, paddingLeft: 24, paddingRight: 20 },
  dot: { position: "absolute", left: 9, top: 26, width: 7, height: 7, borderRadius: 4 },
  body: { flex: 1, minWidth: 0 },
  summary: { marginTop: 2 },
});
