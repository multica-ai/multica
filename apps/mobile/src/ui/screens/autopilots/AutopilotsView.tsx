import { Pressable, RefreshControl, SectionList, StyleSheet, View } from "react-native";
import { Capsule } from "../../components/Floating";
import { TitleHeader } from "../../components/Header";
import { BOTTOM_CLEARANCE, Screen } from "../../components/Screen";
import { Empty, Failure, Loading } from "../../components/States";
import { Txt } from "../../components/Text";
import { Icon } from "../../icons/Icon";
import type { AutopilotRowModel } from "../../model/autopilots";
import { useStrings } from "../../strings";
import { useTheme } from "../../theme";

export interface AutopilotSection {
  key: string;
  title: string;
  rows: readonly AutopilotRowModel[];
}

function AutopilotRow({ item, onPress }: { item: AutopilotRowModel; onPress: () => void }) {
  const palette = useTheme();
  const strings = useStrings();
  return (
    <Pressable accessibilityRole="button" onPress={onPress} style={({ pressed }) => [styles.row, pressed && { backgroundColor: palette.fill }]}>
      <View style={[styles.mark, { backgroundColor: palette.fill }]}>
        <Icon name={item.paused ? "Pause" : "Zap"} size={18} color={item.paused ? palette.label3 : palette.label} />
      </View>
      <View style={styles.flex}>
        <View style={styles.line}>
          <Txt variant="row" numberOfLines={1} style={styles.flex}>
            {item.title}
          </Txt>
          {item.lastFailed ? (
            <Txt variant="meta" tone="destructive">
              {strings.autopilot.lastFailed}
            </Txt>
          ) : item.next ? (
            <Txt variant="meta" tone="tertiary">
              {item.next}
            </Txt>
          ) : null}
        </View>
        <View style={styles.meta}>
          <Icon name={item.triggerIcon} size={14} color={palette.label2} />
          <Txt variant="meta" tone="secondary" numberOfLines={1} style={styles.flex}>
            {item.triggers} · {item.assignee.name}
          </Txt>
        </View>
      </View>
    </Pressable>
  );
}

interface AutopilotsViewProps {
  state: "loading" | "error" | "ready";
  sections: readonly AutopilotSection[];
  refreshing?: boolean;
  onRefresh?: () => void;
  onRetry: () => void;
  onOpen: (autopilotId: string) => void;
  onCreate: () => void;
}

export function AutopilotsView({ state, sections, refreshing = false, onRefresh, onRetry, onOpen, onCreate }: AutopilotsViewProps) {
  const strings = useStrings();
  const palette = useTheme();
  return (
    <Screen>
      <TitleHeader title={strings.autopilot.title} right={<Capsule actions={[{ icon: "Plus", label: strings.autopilot.new, onPress: onCreate }]} />} />
      {state === "loading" ? (
        <Loading />
      ) : state === "error" ? (
        <Failure title={strings.common.loadFailed} retryLabel={strings.common.retry} onRetry={onRetry} />
      ) : (
        <SectionList
          sections={sections.map((section) => ({ key: section.key, title: section.title, data: section.rows }))}
          keyExtractor={(item) => item.id}
          renderSectionHeader={({ section }) => (
            <Txt variant="metaStrong" tone="secondary" style={[styles.section, { backgroundColor: palette.background }]}>
              {section.title}
            </Txt>
          )}
          renderItem={({ item }) => <AutopilotRow item={item} onPress={() => onOpen(item.id)} />}
          contentContainerStyle={[styles.list, sections.length === 0 && styles.fill]}
          ListEmptyComponent={<Empty title={strings.autopilot.empty} detail={strings.autopilot.emptyDetail} />}
          refreshControl={onRefresh ? <RefreshControl refreshing={refreshing} onRefresh={onRefresh} tintColor={palette.label3} /> : undefined}
        />
      )}
    </Screen>
  );
}

const styles = StyleSheet.create({
  flex: { flex: 1, minWidth: 0 },
  list: { paddingBottom: BOTTOM_CLEARANCE },
  fill: { flexGrow: 1 },
  section: { paddingHorizontal: 20, paddingTop: 16, paddingBottom: 4 },
  row: { flexDirection: "row", alignItems: "center", gap: 12, paddingHorizontal: 20, paddingVertical: 9 },
  mark: { width: 36, height: 36, borderRadius: 18, alignItems: "center", justifyContent: "center" },
  line: { flexDirection: "row", alignItems: "center", gap: 12 },
  meta: { flexDirection: "row", alignItems: "center", gap: 5, marginTop: 3 },
});
