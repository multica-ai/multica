import { Pressable, ScrollView, StyleSheet, View } from "react-native";
import { PushHeader } from "../../components/Header";
import { Screen } from "../../components/Screen";
import { Failure, Loading } from "../../components/States";
import { Txt } from "../../components/Text";
import { Tile } from "../../components/Tile";
import { Icon, type IconName } from "../../icons/Icon";
import type { ViewRowModel } from "../../model/views";
import { useStrings } from "../../strings";
import { useTheme } from "../../theme";

const BUILTIN_ICONS: Record<string, IconName> = {
  "builtin:my_issues": "CircleUser",
  "builtin:my_agents": "Bot",
  "builtin:all_issues": "ListTodo",
  "builtin:unread": "Inbox",
};

function ViewRow({ view, onOpen, onToggleStar }: { view: ViewRowModel; onOpen: () => void; onToggleStar: () => void }) {
  const palette = useTheme();
  const strings = useStrings();
  return (
    <Pressable accessibilityRole="button" onPress={onOpen} style={({ pressed }) => [styles.row, pressed && { backgroundColor: palette.fill }]}>
      <Tile icon={BUILTIN_ICONS[view.id] ?? "Layers"} size={36} />
      <View style={styles.flex}>
        <Txt variant="row" numberOfLines={1}>
          {view.label}
        </Txt>
        <Txt variant="meta" tone="secondary" numberOfLines={1} style={styles.meta}>
          {view.detail}
        </Txt>
      </View>
      <Pressable
        accessibilityRole="button"
        accessibilityLabel={view.starred ? strings.views.unstar : strings.views.star}
        accessibilityState={{ selected: view.starred }}
        hitSlop={12}
        onPress={onToggleStar}
        style={({ pressed }) => ({ opacity: pressed ? 0.5 : 1 })}
      >
        <Icon name="Star" size={19} color={view.starred ? palette.warning : palette.label3} />
      </Pressable>
    </Pressable>
  );
}

interface ViewsViewProps {
  state: "loading" | "error" | "ready";
  starred: readonly ViewRowModel[];
  others: readonly ViewRowModel[];
  onBack: () => void;
  onRetry: () => void;
  onOpen: (id: string) => void;
  onToggleStar: (view: ViewRowModel) => void;
}

export function ViewsView({ state, starred, others, onBack, onRetry, onOpen, onToggleStar }: ViewsViewProps) {
  const strings = useStrings();
  const section = (title: string, rows: readonly ViewRowModel[]) =>
    rows.length > 0 ? (
      <View>
        <Txt variant="metaStrong" tone="secondary" style={styles.section}>
          {title}
        </Txt>
        {rows.map((view) => (
          <ViewRow key={view.id} view={view} onOpen={() => onOpen(view.id)} onToggleStar={() => onToggleStar(view)} />
        ))}
      </View>
    ) : null;
  return (
    <Screen>
      <PushHeader title={strings.views.title} onBack={onBack} backLabel={strings.common.back} />
      {state === "loading" ? (
        <Loading />
      ) : state === "error" ? (
        <Failure title={strings.common.loadFailed} retryLabel={strings.common.retry} onRetry={onRetry} />
      ) : (
        <ScrollView contentContainerStyle={styles.list}>
          {section(strings.views.starred, starred)}
          {section(strings.views.others, others)}
          <Txt variant="meta" tone="tertiary" style={styles.note}>
            {strings.views.note}
          </Txt>
        </ScrollView>
      )}
    </Screen>
  );
}

const styles = StyleSheet.create({
  flex: { flex: 1, minWidth: 0 },
  list: { paddingBottom: 60 },
  section: { paddingHorizontal: 20, paddingTop: 16, paddingBottom: 4 },
  row: { flexDirection: "row", alignItems: "center", gap: 12, paddingHorizontal: 20, paddingVertical: 9 },
  meta: { marginTop: 2 },
  note: { paddingHorizontal: 20, paddingTop: 20 },
});
