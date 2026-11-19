import { Pressable, RefreshControl, SectionList, StyleSheet, View } from "react-native";
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
  target: AgentTarget;
}

type AgentTarget = { kind: "issue"; issueId: string } | { kind: "chat"; sessionId: string };

function isSelected(row: AgentListRow, selected: AgentTarget | null): boolean {
  if (!selected) return false;
  return row.target.kind === "issue" ? selected.kind === "issue" && selected.issueId === row.target.issueId : selected.kind === "chat" && selected.sessionId === row.target.sessionId;
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
  /** The conversation or issue open beside the list. */
  selected?: AgentTarget | null;
}

function Row({ row, selected, onOpen }: { row: AgentListRow; selected: boolean; onOpen: (row: AgentListRow) => void }) {
  const palette = useTheme();
  return (
    <Pressable
      accessibilityRole="button"
      accessibilityState={{ selected }}
      onPress={() => onOpen(row)}
      style={({ pressed }) => [styles.row, selected && styles.selected, (selected || pressed) && { backgroundColor: palette.fill }]}
    >
      {row.unread ? <View style={[styles.dot, selected && styles.selectedDot, { backgroundColor: palette.accent }]} /> : null}
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

export function AgentsView({ state, running, conversations, refreshing = false, onRefresh, onRetry, onOpen, onNewChat, selected = null }: AgentsViewProps) {
  const strings = useStrings();
  const palette = useTheme();
  const sections = [
    { key: "running", title: strings.agents.running, data: running },
    { key: "conversations", title: strings.agents.conversations, data: conversations },
  ].filter((section) => section.data.length > 0);

  return (
    <Screen>
      <TitleHeader title={strings.agents.title} actions={[{ icon: "SquarePen", label: strings.agents.newChat, onPress: onNewChat }]} />
      {state === "loading" ? (
        <Loading />
      ) : state === "error" ? (
        <Failure title={strings.common.loadFailed} retryLabel={strings.common.retry} onRetry={onRetry} />
      ) : (
        <SectionList contentInsetAdjustmentBehavior="automatic"
          sections={sections}
          keyExtractor={(row) => row.id}
          extraData={selected}
          renderItem={({ item }) => <Row row={item} selected={isSelected(item, selected)} onOpen={onOpen} />}
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
  selected: { marginHorizontal: 8, borderRadius: 14, paddingLeft: 12, paddingRight: 12 },
  selectedDot: { left: 0 },
  body: { minWidth: 0 },
  line: { flexDirection: "row", alignItems: "center", gap: 12 },
  flex: { flex: 1 },
  meta: { marginTop: 2 },
});
