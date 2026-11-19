import { Pressable, RefreshControl, SectionList, StyleSheet, View } from "react-native";
import { Avatar } from "../../components/Avatar";
import { TitleHeader } from "../../components/Header";
import { Ring } from "../../components/Progress";
import { BOTTOM_CLEARANCE, Screen } from "../../components/Screen";
import { Empty, Failure, Loading } from "../../components/States";
import { Txt } from "../../components/Text";
import { Tile } from "../../components/Tile";
import { projectStatusColor, type ProjectRowModel, type ProjectSection } from "../../model/projects";
import { useStrings } from "../../strings";
import { useTheme } from "../../theme";

function ProjectRow({ project, onPress }: { project: ProjectRowModel; onPress: () => void }) {
  const palette = useTheme();
  const strings = useStrings();
  const progress = project.total > 0 ? project.done / project.total : 0;
  const meta = [project.lead?.name, `${project.done}/${project.total}`, project.due ? strings.project.dueOn(project.due) : null].filter(Boolean).join(" · ");
  return (
    <Pressable accessibilityRole="button" onPress={onPress} style={({ pressed }) => [styles.row, pressed && { backgroundColor: palette.fill }]}>
      <Tile emoji={project.icon} icon="FolderKanban" />
      <View style={styles.flex}>
        <Txt variant="row" numberOfLines={1}>
          {project.title}
        </Txt>
        <View style={styles.meta}>
          {project.lead ? <Avatar actor={project.lead} size={16} /> : null}
          <Txt variant="meta" tone="secondary" numberOfLines={1} style={styles.flex}>
            {meta}
          </Txt>
        </View>
      </View>
      <Txt variant="meta" tone="tertiary">
        {Math.round(progress * 100)}%
      </Txt>
      <Ring progress={progress} color={projectStatusColor(palette, project.status)} size={20} />
    </Pressable>
  );
}

interface ProjectsViewProps {
  state: "loading" | "error" | "ready";
  sections: readonly ProjectSection[];
  refreshing?: boolean;
  onRefresh?: () => void;
  onRetry: () => void;
  onOpen: (projectId: string) => void;
  onCreate: () => void;
}

export function ProjectsView({ state, sections, refreshing = false, onRefresh, onRetry, onOpen, onCreate }: ProjectsViewProps) {
  const strings = useStrings();
  const palette = useTheme();
  return (
    <Screen>
      <TitleHeader title={strings.project.title} actions={[{ icon: "Plus", label: strings.project.new, onPress: onCreate }]} />
      {state === "loading" ? (
        <Loading />
      ) : state === "error" ? (
        <Failure title={strings.common.loadFailed} retryLabel={strings.common.retry} onRetry={onRetry} />
      ) : (
        <SectionList contentInsetAdjustmentBehavior="automatic"
          sections={sections.map((section) => ({ key: section.status, title: strings.project.status[section.status] ?? section.status, data: section.projects }))}
          keyExtractor={(project) => project.id}
          renderSectionHeader={({ section }) => (
            <Txt variant="metaStrong" tone="secondary" style={[styles.section, { backgroundColor: palette.background }]}>
              {section.title}
            </Txt>
          )}
          renderItem={({ item }) => <ProjectRow project={item} onPress={() => onOpen(item.id)} />}
          contentContainerStyle={[styles.list, sections.length === 0 && styles.fill]}
          ListEmptyComponent={<Empty title={strings.project.empty} detail={strings.project.emptyDetail} />}
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
  meta: { flexDirection: "row", alignItems: "center", gap: 6, marginTop: 3 },
});
