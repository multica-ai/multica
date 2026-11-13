import { KeyboardAvoidingView, Platform, Pressable, ScrollView, StyleSheet, View } from "react-native";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import type { IssuePriority } from "@multica/core/types";
import { Avatar, type AvatarActor } from "../../components/Avatar";
import { Chip, ChipGroup, Pill } from "../../components/Chip";
import { BottomFade } from "../../components/BottomFade";
import { Composer } from "../../components/Composer";
import { Capsule } from "../../components/Floating";
import { PushHeader } from "../../components/Header";
import { RunSteps } from "../../components/RunSteps";
import { Screen } from "../../components/Screen";
import { Failure, Loading } from "../../components/States";
import { PriorityIcon, StatusIcon } from "../../components/StatusIcon";
import { Txt } from "../../components/Text";
import type { StatusPresentation } from "../../model/status";
import type { RunStep } from "../../model/steps";
import type { RunPhase } from "../../model/tasks";
import { Icon } from "../../icons/Icon";
import { Markdown } from "../../markdown/Markdown";
import { useStrings } from "../../strings";
import { radius, useTheme } from "../../theme";

export interface IssueModel {
  identifier: string;
  title: string;
  description: string;
  status: StatusPresentation;
  priority: IssuePriority;
  assignee: AvatarActor | null;
  project: string | null;
}

export interface AgentRunModel {
  id: string;
  agent: AvatarActor;
  phase: RunPhase;
  time: string;
  steps: readonly RunStep[];
  hiddenSteps: number;
}

export interface PullRequestModel {
  id: string;
  title: string;
  state: "open" | "draft" | "merged" | "closed";
  additions: number;
  deletions: number;
  files: number;
  url: string;
}

export type TimelineModel =
  | { kind: "activity"; id: string; actor: AvatarActor; text: string; time: string }
  | { kind: "comment"; id: string; actor: AvatarActor; body: string; time: string; deleted: boolean };

interface IssueViewProps {
  state: "loading" | "error" | "missing" | "ready";
  issue: IssueModel | null;
  run: AgentRunModel | null;
  pullRequests: readonly PullRequestModel[];
  timeline: readonly TimelineModel[];
  onBack: () => void;
  onRetry: () => void;
  onShare?: () => void;
  onStatusPress?: () => void;
  onPriorityPress?: () => void;
  onStop?: () => void;
  onOpenUrl: (url: string) => void;
  onOpenIssue: (issueId: string) => void;
  onComment: (text: string) => Promise<unknown>;
}

function SectionTitle({ children }: { children: string }) {
  return (
    <Txt variant="metaStrong" tone="secondary" style={styles.sectionTitle}>
      {children}
    </Txt>
  );
}

function RunBlock({ run, onStop }: { run: AgentRunModel; onStop?: () => void }) {
  const strings = useStrings();
  const palette = useTheme();
  const active = run.phase === "running" || run.phase === "queued";
  const phaseText = { running: strings.issue.running, queued: strings.issue.queued, failed: strings.issue.failed, done: strings.issue.finished }[run.phase];
  return (
    <View style={styles.section}>
      <SectionTitle>{strings.issue.agent}</SectionTitle>
      <View style={styles.runHead}>
        <Avatar actor={run.agent} size={24} running={run.phase === "running"} />
        <Txt variant="calloutStrong">{run.agent.name}</Txt>
        <Txt variant="meta" tone={run.phase === "failed" ? "destructive" : "secondary"} style={styles.flex} numberOfLines={1}>
          {phaseText} · {run.time}
        </Txt>
        {active && onStop ? (
          <Pill icon={<Icon name="Square" size={11} color={palette.label} strokeWidth={3} />} onPress={onStop}>
            {strings.issue.stop}
          </Pill>
        ) : null}
      </View>
      {run.steps.length ? (
        <View style={styles.steps}>
          <RunSteps steps={run.steps} more={run.hiddenSteps > 0 ? strings.chat.steps(run.hiddenSteps) : undefined} />
        </View>
      ) : null}
    </View>
  );
}

function PullRequestCard({ pr, onOpen }: { pr: PullRequestModel; onOpen: () => void }) {
  const strings = useStrings();
  const palette = useTheme();
  const icon = pr.state === "merged" ? "GitMerge" : pr.state === "closed" ? "GitPullRequestClosed" : pr.state === "draft" ? "GitPullRequestDraft" : "GitPullRequest";
  return (
    <Pressable accessibilityRole="link" onPress={onOpen} style={({ pressed }) => [styles.pr, { backgroundColor: palette.grouped, opacity: pressed ? 0.7 : 1 }]}>
      <View style={styles.prRow}>
        <Icon name={icon} size={16} color={palette.label2} />
        <Txt variant="callout" numberOfLines={1} style={styles.flex}>
          {pr.title}
        </Txt>
        <Txt variant="meta" tone="secondary">
          {strings.issue.pr[pr.state]}
        </Txt>
      </View>
      {pr.files > 0 ? (
        <View style={[styles.prRow, styles.prStats, { borderTopColor: palette.separator }]}>
          <Icon name="Layers" size={16} color={palette.label2} />
          <Txt variant="callout" style={styles.flex}>
            {strings.issue.files(pr.files)}
          </Txt>
          <Txt variant="meta" tone="add">
            +{pr.additions.toLocaleString()}
          </Txt>
          <Txt variant="meta" tone="del">
            −{pr.deletions.toLocaleString()}
          </Txt>
        </View>
      ) : null}
    </Pressable>
  );
}

function TimelineItem({ item, onOpenIssue }: { item: TimelineModel; onOpenIssue: (id: string) => void }) {
  const palette = useTheme();
  const strings = useStrings();
  if (item.kind === "activity") {
    return (
      <View style={styles.activity}>
        <View style={[styles.activityDot, { borderColor: palette.label3 }]} />
        <Txt variant="meta" tone="secondary" numberOfLines={2} style={styles.flex}>
          <Txt variant="metaStrong">{item.actor.name}</Txt> {item.text} · {item.time}
        </Txt>
      </View>
    );
  }
  return (
    <View style={[styles.comment, { backgroundColor: palette.grouped }]}>
      <View style={styles.commentHead}>
        <Avatar actor={item.actor} size={20} />
        <Txt variant="metaStrong">{item.actor.name}</Txt>
        <Txt variant="meta" tone="tertiary">
          {item.time}
        </Txt>
      </View>
      {item.deleted ? (
        <Txt variant="callout" tone="tertiary">
          {strings.issue.deleted}
        </Txt>
      ) : (
        <Markdown source={item.body} size="callout" onOpenIssue={onOpenIssue} />
      )}
    </View>
  );
}

export function IssueView(props: IssueViewProps) {
  const { state, issue, run, pullRequests, timeline, onBack, onRetry, onShare, onStatusPress, onPriorityPress, onStop, onOpenUrl, onOpenIssue, onComment } = props;
  const strings = useStrings();
  const palette = useTheme();
  const insets = useSafeAreaInsets();
  const header = (
    <PushHeader onBack={onBack} backLabel={strings.common.back} right={onShare && issue ? <Capsule actions={[{ icon: "MoreHorizontal", label: strings.common.more, onPress: onShare }]} /> : undefined} />
  );

  if (state !== "ready" || !issue) {
    return (
      <Screen>
        {header}
        {state === "loading" ? <Loading /> : <Failure title={state === "missing" ? strings.issue.notFound : strings.common.loadFailed} retryLabel={strings.common.retry} onRetry={onRetry} />}
      </Screen>
    );
  }

  return (
    <Screen>
      <KeyboardAvoidingView style={styles.flex} behavior={Platform.OS === "ios" ? "padding" : undefined}>
        {header}
        <View style={styles.flex}>
          <ScrollView contentContainerStyle={styles.content} keyboardDismissMode="interactive">
            <Txt variant="meta" tone="secondary">
              {issue.identifier}
            </Txt>
            <Txt variant="heading" style={styles.title} selectable>
              {issue.title}
            </Txt>
            <ChipGroup>
              <Chip icon={<StatusIcon status={issue.status.key} color={issue.status.color} icon={issue.status.icon} size={14} />} label={issue.status.label} onPress={onStatusPress} />
              <Chip icon={<PriorityIcon priority={issue.priority} size={14} />} label={strings.priority[issue.priority]} onPress={onPriorityPress} />
              <Chip icon={issue.assignee ? <Avatar actor={issue.assignee} size={18} /> : <Icon name="CircleUser" size={16} color={palette.label2} />} label={issue.assignee?.name ?? strings.issue.unassigned} />
              {issue.project ? <Chip icon={<Icon name="FolderKanban" size={15} color={palette.label2} />} label={issue.project} /> : null}
            </ChipGroup>
            <View style={styles.description}>
              {issue.description.trim() ? (
                <Markdown source={issue.description} onOpenIssue={onOpenIssue} />
              ) : (
                <Txt variant="body" tone="tertiary">
                  {strings.issue.noDescription}
                </Txt>
              )}
            </View>
            {run ? <RunBlock run={run} onStop={onStop} /> : null}
            {pullRequests.length ? (
              <View style={[styles.section, styles.prs]}>
                {pullRequests.map((pr) => (
                  <PullRequestCard key={pr.id} pr={pr} onOpen={() => onOpenUrl(pr.url)} />
                ))}
              </View>
            ) : null}
            {timeline.length ? (
              <View style={styles.section}>
                <SectionTitle>{strings.issue.activity}</SectionTitle>
                <View style={styles.timeline}>
                  {timeline.map((item) => (
                    <TimelineItem key={item.id} item={item} onOpenIssue={onOpenIssue} />
                  ))}
                </View>
              </View>
            ) : null}
          </ScrollView>
          <BottomFade height={insets.bottom + 80} />
          <View style={[styles.composer, { paddingBottom: Math.max(insets.bottom - 8, 12) }]}>
            <Composer placeholder={strings.issue.comment} sendLabel={strings.common.send} onSend={onComment} />
          </View>
        </View>
      </KeyboardAvoidingView>
    </Screen>
  );
}

const styles = StyleSheet.create({
  flex: { flex: 1 },
  content: { paddingHorizontal: 20, paddingTop: 4, paddingBottom: 120 },
  title: { marginTop: 4, marginBottom: 14 },
  description: { marginTop: 18 },
  section: { marginTop: 28 },
  sectionTitle: { marginBottom: 10 },
  runHead: { flexDirection: "row", alignItems: "center", gap: 8 },
  steps: { marginTop: 10, marginLeft: 4 },
  prs: { gap: 8, marginTop: 16 },
  pr: { borderRadius: radius.card, paddingHorizontal: 14 },
  prRow: { minHeight: 46, flexDirection: "row", alignItems: "center", gap: 10 },
  prStats: { borderTopWidth: StyleSheet.hairlineWidth },
  timeline: { gap: 12 },
  activity: { flexDirection: "row", alignItems: "center", gap: 10, paddingLeft: 2 },
  activityDot: { width: 9, height: 9, borderRadius: 5, borderWidth: 1.5 },
  comment: { borderRadius: radius.card, padding: 14, gap: 8 },
  commentHead: { flexDirection: "row", alignItems: "center", gap: 8 },
  composer: { position: "absolute", left: 16, right: 16, bottom: 0, flexDirection: "row" },
});
