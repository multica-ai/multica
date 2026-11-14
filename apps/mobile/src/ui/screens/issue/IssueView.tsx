import { useState } from "react";
import { KeyboardAvoidingView, Platform, Pressable, ScrollView, StyleSheet, View } from "react-native";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import type { IssuePriority } from "@multica/core/types";
import { Avatar, type AvatarActor } from "../../components/Avatar";
import { Chip, ChipGroup } from "../../components/Chip";
import { BottomFade } from "../../components/BottomFade";
import { Composer } from "../../components/Composer";
import { PushHeader } from "../../components/Header";
import { Screen } from "../../components/Screen";
import { Failure, Loading } from "../../components/States";
import { PriorityIcon, StatusIcon } from "../../components/StatusIcon";
import { Txt } from "../../components/Text";
import { editableMentions, type MentionCandidate, type MentionTarget } from "../../model/mentions";
import type { StatusPresentation } from "../../model/status";
import { Icon } from "../../icons/Icon";
import { Markdown } from "../../markdown/Markdown";
import { useStrings } from "../../strings";
import { radius, useTheme } from "../../theme";
import { RunBlock, Thread, type CommentHandlers, type CommentModel, type TimelineModel } from "./IssueThread";

export type { AgentRunModel, CommentModel, TimelineModel } from "./IssueThread";

export interface IssueLinkModel {
  id: string;
  identifier: string;
  title: string;
  status: StatusPresentation;
  done: boolean;
}

export interface IssueModel {
  identifier: string;
  title: string;
  description: string;
  status: StatusPresentation;
  priority: IssuePriority;
  assignee: AvatarActor | null;
  project: string | null;
  labels: readonly { id: string; name: string; color: string }[];
  /** Formatted for display. */
  start: string | null;
  due: string | null;
  overdue: boolean;
  parent: IssueLinkModel | null;
  children: readonly IssueLinkModel[];
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

export interface PropertyHandlers {
  onStatus: () => void;
  onPriority: () => void;
  onAssignee: () => void;
  onProject: () => void;
  onLabels: () => void;
  onStart: () => void;
  onDue: () => void;
  /** Offers the properties that have no value yet. */
  onAdd?: () => void;
}

interface IssueViewProps {
  state: "loading" | "error" | "missing" | "ready";
  issue: IssueModel | null;
  pullRequests: readonly PullRequestModel[];
  timeline: readonly TimelineModel[];
  onBack: () => void;
  onRetry: () => void;
  onMore?: () => void;
  properties?: PropertyHandlers;
  onOpenUrl: (url: string) => void;
  onOpenIssue: (issueId: string) => void;
  comments: Omit<CommentHandlers, "onReply" | "onEdit" | "onOpenIssue">;
  onComment: (text: string, parentId: string | null) => Promise<unknown>;
  onEditComment: (commentId: string, text: string) => Promise<unknown>;
  mentions?: { candidates: readonly MentionCandidate[]; onQuery?: (query: string | null) => void };
}

function SectionTitle({ children, trailing }: { children: string; trailing?: string }) {
  return (
    <View style={styles.sectionHead}>
      <Txt variant="metaStrong" tone="secondary" style={styles.flex}>
        {children}
      </Txt>
      {trailing ? (
        <Txt variant="meta" tone="tertiary">
          {trailing}
        </Txt>
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

function IssueLink({ link, onOpen, last }: { link: IssueLinkModel; onOpen: () => void; last: boolean }) {
  const palette = useTheme();
  return (
    <Pressable
      accessibilityRole="link"
      onPress={onOpen}
      style={({ pressed }) => [styles.child, !last && { borderBottomWidth: StyleSheet.hairlineWidth, borderBottomColor: palette.separator }, pressed && { opacity: 0.6 }]}
    >
      <StatusIcon status={link.status.key} color={link.status.color} icon={link.status.icon} size={15} />
      <Txt variant="meta" tone="secondary">
        {link.identifier}
      </Txt>
      <Txt variant="callout" numberOfLines={1} style={styles.flex}>
        {link.title}
      </Txt>
    </Pressable>
  );
}

function Properties({ issue, handlers }: { issue: IssueModel; handlers?: PropertyHandlers }) {
  const strings = useStrings();
  const palette = useTheme();
  return (
    <ChipGroup>
      <Chip
        icon={<StatusIcon status={issue.status.key} color={issue.status.color} icon={issue.status.icon} size={14} />}
        label={issue.status.label}
        onPress={handlers?.onStatus}
        accessibilityLabel={`${strings.issue.statusTitle}: ${issue.status.label}`}
      />
      <Chip
        icon={<PriorityIcon priority={issue.priority} size={14} />}
        label={strings.priority[issue.priority]}
        onPress={handlers?.onPriority}
        accessibilityLabel={`${strings.issue.priorityTitle}: ${strings.priority[issue.priority]}`}
      />
      <Chip
        icon={issue.assignee ? <Avatar actor={issue.assignee} size={18} /> : <Icon name="CircleUser" size={16} color={palette.label2} />}
        label={issue.assignee?.name ?? strings.issue.unassigned}
        muted={!issue.assignee}
        onPress={handlers?.onAssignee}
        accessibilityLabel={`${strings.issue.assignee}: ${issue.assignee?.name ?? strings.issue.unassigned}`}
      />
      <Chip
        icon={<Icon name="FolderKanban" size={15} color={palette.label2} />}
        label={issue.project ?? strings.issue.noProject}
        muted={!issue.project}
        onPress={handlers?.onProject}
        accessibilityLabel={`${strings.issue.project}: ${issue.project ?? strings.issue.noProject}`}
      />
      {issue.labels.map((label) => (
        <Chip key={label.id} icon={<View style={[styles.labelDot, { backgroundColor: label.color }]} />} label={label.name} onPress={handlers?.onLabels} />
      ))}
      {issue.start ? <Chip icon={<Icon name="Calendar" size={15} color={palette.label2} />} label={issue.start} onPress={handlers?.onStart} accessibilityLabel={`${strings.issue.start}: ${issue.start}`} /> : null}
      {issue.due ? (
        <Chip
          icon={<Icon name="CalendarClock" size={15} color={issue.overdue ? palette.destructive : palette.label2} />}
          label={issue.due}
          onPress={handlers?.onDue}
          accessibilityLabel={`${strings.issue.due}: ${issue.due}`}
        />
      ) : null}
      {handlers?.onAdd ? <Chip icon={<Icon name="Plus" size={15} color={palette.label2} />} onPress={handlers.onAdd} accessibilityLabel={strings.issue.addProperty} /> : null}
    </ChipGroup>
  );
}

type ComposerTarget = { kind: "reply"; comment: CommentModel } | { kind: "edit"; comment: CommentModel; text: string; targets: MentionTarget[] };

export function IssueView(props: IssueViewProps) {
  const { state, issue, pullRequests, timeline, onBack, onRetry, onMore, properties, onOpenUrl, onOpenIssue, comments, onComment, onEditComment, mentions } = props;
  const strings = useStrings();
  const palette = useTheme();
  const insets = useSafeAreaInsets();
  const [target, setTarget] = useState<ComposerTarget | null>(null);
  const header = <PushHeader onBack={onBack} backLabel={strings.common.back} actions={onMore && issue ? [{ icon: "MoreHorizontal", label: strings.common.more, onPress: onMore }] : []} />;

  if (state !== "ready" || !issue) {
    return (
      <Screen>
        {header}
        {state === "loading" ? <Loading /> : <Failure title={state === "missing" ? strings.issue.notFound : strings.common.loadFailed} retryLabel={strings.common.retry} onRetry={onRetry} />}
      </Screen>
    );
  }

  const handlers: CommentHandlers = {
    ...comments,
    onOpenIssue,
    onReply: (comment) => setTarget({ kind: "reply", comment }),
    onEdit: (comment) => setTarget({ kind: "edit", comment, ...editableMentions(comment.body) }),
  };
  const done = issue.children.filter((child) => child.done).length;

  return (
    <Screen>
      <KeyboardAvoidingView style={styles.flex} behavior={Platform.OS === "ios" ? "padding" : undefined}>
        {header}
        <View style={styles.flex}>
          <ScrollView contentInsetAdjustmentBehavior="automatic" contentContainerStyle={styles.content} keyboardDismissMode="interactive" keyboardShouldPersistTaps="handled">
            {issue.parent ? (
              <Pressable accessibilityRole="link" onPress={() => onOpenIssue(issue.parent!.id)} style={({ pressed }) => [styles.parent, pressed && { opacity: 0.6 }]}>
                <Icon name="ListTree" size={14} color={palette.label2} />
                <Txt variant="meta" tone="secondary" numberOfLines={1} style={styles.flexShrink}>
                  {issue.parent.identifier} {issue.parent.title}
                </Txt>
                <Icon name="ChevronRight" size={14} color={palette.label3} />
              </Pressable>
            ) : null}
            <Txt variant="meta" tone="secondary">
              {issue.identifier}
            </Txt>
            <Txt variant="heading" style={styles.title} selectable>
              {issue.title}
            </Txt>
            <Properties issue={issue} handlers={properties} />
            <View style={styles.description}>
              {issue.description.trim() ? (
                <Markdown source={issue.description} onOpenIssue={onOpenIssue} />
              ) : (
                <Txt variant="body" tone="tertiary">
                  {strings.issue.noDescription}
                </Txt>
              )}
            </View>
            {issue.children.length ? (
              <View style={styles.section}>
                <SectionTitle trailing={`${done}/${issue.children.length}`}>{strings.issue.subIssues}</SectionTitle>
                <View style={[styles.children, { backgroundColor: palette.grouped }]}>
                  {issue.children.map((child, index) => (
                    <IssueLink key={child.id} link={child} onOpen={() => onOpenIssue(child.id)} last={index === issue.children.length - 1} />
                  ))}
                </View>
              </View>
            ) : null}
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
                  {timeline.map((item) =>
                    item.kind === "thread" ? (
                      <Thread key={item.id} thread={item} handlers={handlers} />
                    ) : item.kind === "run" ? (
                      <RunBlock key={item.id} run={item.run} onStop={comments.onStop} />
                    ) : (
                      <View key={item.id} style={styles.activity}>
                        <View style={[styles.activityDot, { borderColor: palette.label3 }]} />
                        <Txt variant="meta" tone="secondary" numberOfLines={2} style={styles.flex}>
                          <Txt variant="metaStrong">{item.actor.name}</Txt> {item.text} · {item.time}
                        </Txt>
                      </View>
                    ),
                  )}
                </View>
              </View>
            ) : null}
          </ScrollView>
          <BottomFade height={insets.bottom + 80} />
          <View style={[styles.composer, { paddingBottom: Math.max(insets.bottom - 8, 12) }]}>
            <Composer
              placeholder={target?.kind === "reply" ? strings.issue.replyPlaceholder : strings.issue.comment}
              sendLabel={strings.common.send}
              mentions={mentions}
              focusKey={target?.kind === "reply" ? target.comment.id : null}
              draft={target?.kind === "edit" ? { key: target.comment.id, text: target.text, targets: target.targets } : null}
              context={
                target
                  ? {
                      label: target.kind === "reply" ? strings.issue.replyingTo(target.comment.actor.name) : strings.issue.editing,
                      cancelLabel: strings.common.cancel,
                      onCancel: () => setTarget(null),
                    }
                  : undefined
              }
              onSend={async (text) => {
                if (target?.kind === "edit") await onEditComment(target.comment.id, text);
                else await onComment(text, target?.kind === "reply" ? target.comment.id : null);
                setTarget(null);
              }}
            />
          </View>
        </View>
      </KeyboardAvoidingView>
    </Screen>
  );
}

const styles = StyleSheet.create({
  flex: { flex: 1 },
  flexShrink: { flexShrink: 1 },
  content: { paddingHorizontal: 20, paddingTop: 4, paddingBottom: 140 },
  parent: { flexDirection: "row", alignItems: "center", gap: 6, alignSelf: "flex-start", marginBottom: 8, maxWidth: "100%" },
  title: { marginTop: 4, marginBottom: 14 },
  description: { marginTop: 18 },
  section: { marginTop: 28 },
  sectionHead: { flexDirection: "row", alignItems: "center", marginBottom: 10 },
  labelDot: { width: 9, height: 9, borderRadius: 5, marginHorizontal: 2 },
  children: { borderRadius: radius.card, paddingHorizontal: 14 },
  child: { minHeight: 44, flexDirection: "row", alignItems: "center", gap: 8 },
  prs: { gap: 8, marginTop: 16 },
  pr: { borderRadius: radius.card, paddingHorizontal: 14 },
  prRow: { minHeight: 46, flexDirection: "row", alignItems: "center", gap: 10 },
  prStats: { borderTopWidth: StyleSheet.hairlineWidth },
  timeline: { gap: 12 },
  activity: { flexDirection: "row", alignItems: "center", gap: 10, paddingLeft: 2 },
  activityDot: { width: 9, height: 9, borderRadius: 5, borderWidth: 1.5 },
  composer: { position: "absolute", left: 16, right: 16, bottom: 0, flexDirection: "row" },
});
