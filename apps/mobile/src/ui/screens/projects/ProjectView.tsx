import { RefreshControl, ScrollView, StyleSheet, View } from "react-native";
import type { ProjectPriority, ProjectStatus } from "@multica/core/types";
import { Avatar, type AvatarActor } from "../../components/Avatar";
import { Chip, ChipGroup } from "../../components/Chip";
import { Group, GroupRow } from "../../components/Group";
import { PushHeader } from "../../components/Header";
import { IssueGroups, type IssueGroupModel } from "../../components/IssueGroups";
import type { IssueRowModel } from "../../components/IssueRow";
import { Ring, SegmentBar } from "../../components/Progress";
import { Screen } from "../../components/Screen";
import { SectionTitle } from "../../components/SectionTitle";
import { Failure, Loading } from "../../components/States";
import { PriorityIcon } from "../../components/StatusIcon";
import { Txt } from "../../components/Text";
import { Tile } from "../../components/Tile";
import { Icon, type IconName } from "../../icons/Icon";
import { projectStatusColor } from "../../model/projects";
import { useStrings } from "../../strings";
import { useTheme } from "../../theme";

export interface ProjectModel {
  icon: string | null;
  title: string;
  description: string | null;
  status: ProjectStatus;
  priority: ProjectPriority;
  lead: AvatarActor | null;
  dates: string | null;
}

export interface ProjectResourceModel {
  id: string;
  icon: IconName;
  label: string;
  detail: string;
  url: string | null;
}

export interface ProjectProgressModel {
  done: number;
  started: number;
  unstarted: number;
}

interface ProjectViewProps {
  state: "loading" | "error" | "missing" | "ready";
  project: ProjectModel | null;
  progress: ProjectProgressModel;
  resources: readonly ProjectResourceModel[];
  issues: { state: "loading" | "error" | "ready"; groups: readonly IssueGroupModel[] };
  refreshing?: boolean;
  onRefresh?: () => void;
  onBack: () => void;
  onRetry: () => void;
  onMore: () => void;
  onStatus?: () => void;
  onPriority?: () => void;
  onLead?: () => void;
  onDates?: () => void;
  onOpenResource: (resource: ProjectResourceModel) => void;
  onOpenIssue: (issue: IssueRowModel) => void;
}

export function ProjectView(props: ProjectViewProps) {
  const { state, project, progress, resources, issues, refreshing = false, onRefresh, onBack, onRetry, onMore } = props;
  const strings = useStrings();
  const palette = useTheme();
  const total = progress.done + progress.started + progress.unstarted;
  return (
    <Screen>
      <PushHeader
        onBack={onBack}
        backLabel={strings.common.back}
        actions={state === "ready" ? [{ icon: "MoreHorizontal", label: strings.common.more, onPress: onMore }] : []}
      />
      {state === "loading" ? (
        <Loading />
      ) : state === "error" || state === "missing" || !project ? (
        <Failure title={strings.common.loadFailed} retryLabel={strings.common.retry} onRetry={onRetry} />
      ) : (
        <ScrollView contentInsetAdjustmentBehavior="automatic"
          contentContainerStyle={styles.content}
          refreshControl={onRefresh ? <RefreshControl refreshing={refreshing} onRefresh={onRefresh} tintColor={palette.label3} /> : undefined}
        >
          <Tile emoji={project.icon} icon="FolderKanban" size={52} />
          <Txt variant="heading" style={styles.title}>
            {project.title}
          </Txt>
          <ChipGroup>
            <Chip
              icon={<Ring progress={total > 0 ? progress.done / total : 0} color={projectStatusColor(palette, project.status)} size={14} />}
              label={strings.project.status[project.status] ?? project.status}
              onPress={props.onStatus}
            />
            <Chip icon={<PriorityIcon priority={project.priority} size={14} />} label={project.priority === "none" ? undefined : strings.priority[project.priority]} onPress={props.onPriority} />
            <Chip
              icon={project.lead ? <Avatar actor={project.lead} size={18} /> : <Icon name="CircleUser" size={15} color={palette.label2} />}
              label={project.lead?.name ?? strings.project.noLead}
              onPress={props.onLead}
            />
            <Chip icon={<Icon name="Calendar" size={15} color={palette.label2} />} label={project.dates ?? strings.project.due} onPress={props.onDates} />
          </ChipGroup>
          {project.description ? (
            <Txt variant="body" style={styles.description}>
              {project.description}
            </Txt>
          ) : null}

          {total > 0 ? (
            <View style={styles.block}>
              <SectionTitle right={`${progress.done} / ${total}`}>{strings.project.progress}</SectionTitle>
              <SegmentBar
                segments={[
                  { value: progress.done, color: palette.info },
                  { value: progress.started, color: palette.warning },
                  { value: progress.unstarted, color: "transparent" },
                ]}
              />
              <Txt variant="meta" tone="secondary" style={styles.legend}>
                {strings.project.legend(progress.done, progress.started, progress.unstarted)}
              </Txt>
            </View>
          ) : null}

          {resources.length > 0 ? (
            <View style={styles.block}>
              <SectionTitle>{strings.project.resources}</SectionTitle>
              <Group>
                {resources.map((resource, index) => (
                  <GroupRow
                    key={resource.id}
                    icon={resource.icon}
                    label={resource.label}
                    detail={resource.detail}
                    chevron={resource.url !== null}
                    last={index === resources.length - 1}
                    onPress={resource.url ? () => props.onOpenResource(resource) : undefined}
                  />
                ))}
              </Group>
            </View>
          ) : null}

          <View style={styles.block}>
            <SectionTitle right={total > 0 ? String(total) : undefined}>{strings.project.issues}</SectionTitle>
            <View style={styles.issues}>
              {issues.state === "loading" ? (
                <Loading />
              ) : issues.state === "error" ? (
                <Failure title={strings.common.loadFailed} retryLabel={strings.common.retry} onRetry={onRetry} />
              ) : issues.groups.length === 0 ? (
                <Txt variant="callout" tone="tertiary" style={styles.none}>
                  {strings.project.noIssues}
                </Txt>
              ) : (
                <IssueGroups groups={issues.groups} finishedLabel={strings.project.finished} onOpen={props.onOpenIssue} />
              )}
            </View>
          </View>
        </ScrollView>
      )}
    </Screen>
  );
}

const styles = StyleSheet.create({
  content: { paddingHorizontal: 20, paddingTop: 4, paddingBottom: 60 },
  title: { marginTop: 12, marginBottom: 14 },
  description: { marginTop: 16 },
  block: { marginTop: 28 },
  legend: { marginTop: 8 },
  issues: { marginHorizontal: -20 },
  none: { paddingHorizontal: 20 },
});
