import { Pressable, RefreshControl, SectionList, StyleSheet, View } from "react-native";
import { Capsule } from "../../components/Floating";
import { TitleHeader } from "../../components/Header";
import { BOTTOM_CLEARANCE, Screen } from "../../components/Screen";
import { Empty, Failure, Loading } from "../../components/States";
import { Txt } from "../../components/Text";
import { useStrings } from "../../strings";
import { useTheme } from "../../theme";

export interface AgentListRow {
  id: string;
  title: string;
  /** Coloured lead of the second line, e.g. "Running". */
  state?: { text: string; tone: "warning" | "secondary" };
  meta: string;
  time: string;
  unread: boolean;
  target: { kind: "issue"; issueId: string } | { kind: "chat"; sessionId: string };
}

interface AgentsViewProps {
  state: "loading" | "error" | "ready";
  running: readonly AgentListRow[];
  conversations: readonly AgentListRow[];
  refreshing?: boolean;
  onRefresh?: () => void;
  onRetry: () => void;
  onOpen: (row: AgentListRow) => void;
  onNewChat: () => void;
}

function Row({ row, onOpen }: { row: AgentListRow; onOpen: (row: AgentListRow) => void }) {
  const palette = useTheme();
  return (
    <Pressable accessibilityRole="button" onPress={() => onOpen(row)} style={({ pressed }) => [styles.row, pressed && { backgroundColor: palette.fill }]}>
      {row.unread ? <View style={[styles.dot, { backgroundColor: palette.accent }]} /> : null}
      <View style={styles.body}>
        <View style={styles.line}>
          <Txt variant="row" numberOfLines={1} style={styles.flex}>
            {row.title}
          </Txt>
          <Txt variant="meta" tone="tertiary">
            {row.time}
          </Txt>
        </View>
        <Txt variant="meta" tone="secondary" numberOfLines={1} style={styles.meta}>
          {row.state ? (
            <Txt variant="meta" tone={row.state.tone}>
              {row.state.text}
              {row.meta ? " · " : ""}
            </Txt>
          ) : null}
          {row.meta}
        </Txt>
      </View>
    </Pressable>
  );
}

export function AgentsView({ state, running, conversations, refreshing = false, onRefresh, onRetry, onOpen, onNewChat }: AgentsViewProps) {
  const strings = useStrings();
  const palette = useTheme();
  const sections = [
    { key: "running", title: strings.agents.running, data: running },
    { key: "conversations", title: strings.agents.conversations, data: conversations },
  ].filter((section) => section.data.length > 0);

  return (
    <Screen>
      <TitleHeader title={strings.agents.title} right={<Capsule actions={[{ icon: "SquarePen", label: strings.agents.newChat, onPress: onNewChat }]} />} />
      {state === "loading" ? (
        <Loading />
      ) : state === "error" ? (
        <Failure title={strings.common.loadFailed} retryLabel={strings.common.retry} onRetry={onRetry} />
      ) : (
        <SectionList
          sections={sections}
          keyExtractor={(row) => row.id}
          renderItem={({ item }) => <Row row={item} onOpen={onOpen} />}
          renderSectionHeader={({ section }) => (
            <Txt variant="metaStrong" tone="secondary" style={[styles.section, { backgroundColor: palette.background }]}>
              {section.title}
            </Txt>
          )}
          stickySectionHeadersEnabled={false}
          contentContainerStyle={[styles.list, sections.length === 0 && styles.fill]}
          ListEmptyComponent={<Empty title={strings.agents.empty} detail={strings.agents.emptyDetail} />}
          refreshControl={onRefresh ? <RefreshControl refreshing={refreshing} onRefresh={onRefresh} tintColor={palette.label3} /> : undefined}
        />
      )}
    </Screen>
  );
}

const styles = StyleSheet.create({
  list: { paddingBottom: BOTTOM_CLEARANCE },
  fill: { flexGrow: 1 },
  section: { paddingHorizontal: 20, paddingTop: 16, paddingBottom: 4 },
  row: { paddingVertical: 10, paddingLeft: 20, paddingRight: 20 },
  dot: { position: "absolute", left: 8, top: 18, width: 7, height: 7, borderRadius: 4 },
  body: { minWidth: 0 },
  line: { flexDirection: "row", alignItems: "center", gap: 12 },
  flex: { flex: 1 },
  meta: { marginTop: 2 },
});
