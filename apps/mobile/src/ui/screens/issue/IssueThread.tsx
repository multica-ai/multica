import { useState } from "react";
import { Pressable, StyleSheet, View } from "react-native";
import { Avatar, type AvatarActor } from "../../components/Avatar";
import { Pill } from "../../components/Chip";
import { RunSteps } from "../../components/RunSteps";
import { Txt } from "../../components/Text";
import { pick } from "../../components/action-sheet";
import { Icon } from "../../icons/Icon";
import { Markdown } from "../../markdown/Markdown";
import { MarkdownImage } from "../../markdown/MarkdownImage";
import { useMarkdownHost } from "../../markdown/host";
import { markdownPreview } from "../../markdown/parse";
import type { RunStep } from "../../model/steps";
import type { RunPhase } from "../../model/tasks";
import { useStrings } from "../../strings";
import { radius, useTheme } from "../../theme";

export interface AgentRunModel {
  id: string;
  agent: AvatarActor;
  phase: RunPhase;
  time: string;
  steps: readonly RunStep[];
  hiddenSteps: number;
}

export interface ReactionModel {
  emoji: string;
  count: number;
  mine: boolean;
}

/** A file on a comment that its body does not already show. */
export interface FileModel {
  id: string;
  name: string;
  url: string;
  image: boolean;
}

export interface CommentModel {
  id: string;
  actor: AvatarActor;
  body: string;
  time: string;
  edited: boolean;
  deleted: boolean;
  mine: boolean;
  reactions: readonly ReactionModel[];
  files: readonly FileModel[];
}

export type TimelineModel =
  | { kind: "activity"; id: string; actor: AvatarActor; text: string; time: string }
  | { kind: "thread"; id: string; root: CommentModel; replies: readonly CommentModel[]; run: AgentRunModel | null; resolved: boolean }
  | { kind: "run"; id: string; run: AgentRunModel };

export const QUICK_REACTIONS = ["👍", "👀", "🎉", "❤️", "😄", "🚀", "👎"] as const;

export interface CommentHandlers {
  onReply: (comment: CommentModel) => void;
  onEdit: (comment: CommentModel) => void;
  onReact: (commentId: string, emoji: string) => void;
  onCopy: (text: string) => void;
  onCopyLink: (commentId: string) => void;
  onResolve: (commentId: string, resolved: boolean) => void;
  onDelete: (commentId: string) => void;
  onOpenIssue: (issueId: string) => void;
  onStop?: (runId: string) => void;
}

type MenuAction = "reply" | "react" | "copy" | "link" | "resolve" | "edit" | "delete";

function useCommentMenu(handlers: CommentHandlers) {
  const strings = useStrings();
  return async (comment: CommentModel, thread: { rootId: string; resolved: boolean }) => {
    if (comment.deleted) return;
    const options: { value: MenuAction; label: string; destructive?: boolean }[] = [
      { value: "reply", label: strings.issue.reply },
      { value: "react", label: strings.issue.react },
      { value: "copy", label: strings.issue.copyText },
      { value: "link", label: strings.issue.copyLink },
      { value: "resolve", label: thread.resolved ? strings.issue.unresolve : strings.issue.resolve },
    ];
    if (comment.mine) options.push({ value: "edit", label: strings.issue.edit }, { value: "delete", label: strings.issue.delete, destructive: true });
    const action = await pick(undefined, options, strings.common.cancel);
    if (action === "reply") handlers.onReply(comment);
    else if (action === "edit") handlers.onEdit(comment);
    else if (action === "copy") handlers.onCopy(comment.body);
    else if (action === "link") handlers.onCopyLink(comment.id);
    else if (action === "resolve") handlers.onResolve(thread.rootId, !thread.resolved);
    else if (action === "delete") handlers.onDelete(comment.id);
    else if (action === "react") {
      const emoji = await pick(strings.issue.react, QUICK_REACTIONS.map((value) => ({ value, label: value })), strings.common.cancel);
      if (emoji) handlers.onReact(comment.id, emoji);
    }
  };
}

function Reactions({ comment, onReact }: { comment: CommentModel; onReact: (commentId: string, emoji: string) => void }) {
  const palette = useTheme();
  if (!comment.reactions.length) return null;
  return (
    <View style={styles.reactions}>
      {comment.reactions.map((reaction) => (
        <Pressable
          key={reaction.emoji}
          accessibilityRole="button"
          accessibilityState={{ selected: reaction.mine }}
          accessibilityLabel={`${reaction.emoji} ${reaction.count}`}
          onPress={() => onReact(comment.id, reaction.emoji)}
          style={({ pressed }) => [
            styles.reaction,
            { backgroundColor: reaction.mine ? palette.fillStrong : palette.fill, borderColor: reaction.mine ? palette.label3 : "transparent", opacity: pressed ? 0.6 : 1 },
          ]}
        >
          <Txt variant="meta">{reaction.emoji}</Txt>
          <Txt variant="metaStrong" tone="secondary">
            {reaction.count}
          </Txt>
        </Pressable>
      ))}
    </View>
  );
}

function Files({ files }: { files: readonly FileModel[] }) {
  const palette = useTheme();
  const host = useMarkdownHost();
  if (!files.length) return null;
  return (
    <View style={styles.files}>
      {files.map((file) =>
        file.image ? (
          <MarkdownImage key={file.id} url={file.url} alt={file.name} />
        ) : (
          <Pressable key={file.id} accessibilityRole="link" onPress={() => host.openLink(file.url)} style={({ pressed }) => [styles.file, { backgroundColor: palette.fill, opacity: pressed ? 0.6 : 1 }]}>
            <Icon name="File" size={16} color={palette.label2} />
            <Txt variant="callout" numberOfLines={1} style={styles.flex}>
              {file.name}
            </Txt>
          </Pressable>
        ),
      )}
    </View>
  );
}

function Comment({ comment, onMenu, handlers }: { comment: CommentModel; onMenu: () => void; handlers: CommentHandlers }) {
  const strings = useStrings();
  const palette = useTheme();
  return (
    <Pressable accessible={false} onLongPress={onMenu} delayLongPress={300} style={styles.commentBody}>
      <View style={styles.commentHead}>
        <Avatar actor={comment.actor} size={20} />
        <Txt variant="metaStrong" numberOfLines={1} style={styles.flexShrink}>
          {comment.actor.name}
        </Txt>
        <Txt variant="meta" tone="tertiary" style={styles.flex} numberOfLines={1}>
          {comment.time}
          {comment.edited ? ` · ${strings.issue.edited}` : ""}
        </Txt>
        {comment.deleted ? null : (
          <Pressable accessibilityRole="button" accessibilityLabel={strings.common.more} onPress={onMenu} hitSlop={10}>
            <Icon name="MoreHorizontal" size={18} color={palette.label3} />
          </Pressable>
        )}
      </View>
      {comment.deleted ? (
        <Txt variant="callout" tone="tertiary">
          {strings.issue.deleted}
        </Txt>
      ) : (
        <>
          {comment.body.trim() ? <Markdown source={comment.body} size="callout" onOpenIssue={handlers.onOpenIssue} /> : null}
          <Files files={comment.files} />
          <Reactions comment={comment} onReact={handlers.onReact} />
        </>
      )}
    </Pressable>
  );
}

/** A run's agent, how it is going, and its latest steps. */
export function RunBlock({ run, onStop, inline = false }: { run: AgentRunModel; onStop?: (runId: string) => void; inline?: boolean }) {
  const strings = useStrings();
  const palette = useTheme();
  const active = run.phase === "running" || run.phase === "queued";
  const phaseText = { running: strings.issue.running, queued: strings.issue.queued, failed: strings.issue.failed, done: strings.issue.finished }[run.phase];
  return (
    <View style={[styles.run, inline ? [styles.divider, styles.runInline, { borderTopColor: palette.separator }] : [styles.runCard, { backgroundColor: palette.grouped }]]}>
      <View style={styles.runHead}>
        <Avatar actor={run.agent} size={20} running={run.phase === "running"} />
        <Txt variant="metaStrong" numberOfLines={1} style={styles.flexShrink}>
          {run.agent.name}
        </Txt>
        <Txt variant="meta" tone={run.phase === "failed" ? "destructive" : "secondary"} style={styles.flex} numberOfLines={1}>
          {phaseText} · {run.time}
        </Txt>
        {active && onStop ? (
          <Pill icon={<Icon name="Square" size={11} color={palette.label} strokeWidth={3} />} onPress={() => onStop(run.id)}>
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

/** A comment with its replies, and the run it started while that run is live or failed. */
export function Thread({ thread, handlers }: { thread: Extract<TimelineModel, { kind: "thread" }>; handlers: CommentHandlers }) {
  const strings = useStrings();
  const palette = useTheme();
  const menu = useCommentMenu(handlers);
  const [expanded, setExpanded] = useState(false);
  const context = { rootId: thread.root.id, resolved: thread.resolved };

  if (thread.resolved && !expanded) {
    return (
      <Pressable
        accessibilityRole="button"
        accessibilityHint={strings.issue.showThread}
        onPress={() => setExpanded(true)}
        style={({ pressed }) => [styles.card, styles.collapsed, { backgroundColor: palette.grouped, opacity: pressed ? 0.7 : 1 }]}
      >
        <Icon name="CheckCircle2" size={16} color={palette.label2} />
        <Txt variant="meta" tone="secondary" numberOfLines={1} style={styles.flex}>
          <Txt variant="metaStrong" tone="secondary">
            {strings.issue.resolvedThread}
          </Txt>
          {" · "}
          {thread.root.actor.name}: {markdownPreview(thread.root.body)}
        </Txt>
        <Txt variant="meta" tone="tertiary">
          {thread.replies.length ? strings.issue.replies(thread.replies.length) : ""}
        </Txt>
      </Pressable>
    );
  }

  return (
    <View style={[styles.card, { backgroundColor: palette.grouped }]}>
      <Comment comment={thread.root} handlers={handlers} onMenu={() => void menu(thread.root, context)} />
      {thread.replies.map((reply) => (
        <View key={reply.id} style={[styles.divider, { borderTopColor: palette.separator }]}>
          <Comment comment={reply} handlers={handlers} onMenu={() => void menu(reply, context)} />
        </View>
      ))}
      {thread.run ? <RunBlock run={thread.run} onStop={handlers.onStop} inline /> : null}
      <View style={styles.footer}>
        <Pressable accessibilityRole="button" onPress={() => handlers.onReply(thread.root)} hitSlop={8} style={({ pressed }) => [styles.replyButton, { opacity: pressed ? 0.5 : 1 }]}>
          <Icon name="CornerDownRight" size={14} color={palette.label2} />
          <Txt variant="metaStrong" tone="secondary">
            {strings.issue.reply}
          </Txt>
        </Pressable>
        {thread.resolved ? (
          <Pressable accessibilityRole="button" onPress={() => setExpanded(false)} hitSlop={8}>
            <Txt variant="meta" tone="tertiary">
              {strings.issue.resolvedThread}
            </Txt>
          </Pressable>
        ) : null}
      </View>
    </View>
  );
}

const styles = StyleSheet.create({
  flex: { flex: 1 },
  flexShrink: { flexShrink: 1 },
  card: { borderRadius: radius.card, paddingHorizontal: 14, paddingTop: 12, paddingBottom: 6 },
  collapsed: { flexDirection: "row", alignItems: "center", gap: 8, paddingBottom: 12 },
  commentBody: { gap: 8, paddingBottom: 8 },
  runInline: { paddingBottom: 8 },
  divider: { borderTopWidth: StyleSheet.hairlineWidth, paddingTop: 10 },
  commentHead: { flexDirection: "row", alignItems: "center", gap: 8 },
  reactions: { flexDirection: "row", flexWrap: "wrap", gap: 6 },
  reaction: { height: 28, borderRadius: 14, borderWidth: 1, flexDirection: "row", alignItems: "center", gap: 4, paddingHorizontal: 9 },
  files: { gap: 8 },
  file: { minHeight: 40, borderRadius: 10, flexDirection: "row", alignItems: "center", gap: 8, paddingHorizontal: 12 },
  footer: { flexDirection: "row", alignItems: "center", justifyContent: "space-between", paddingVertical: 6 },
  replyButton: { flexDirection: "row", alignItems: "center", gap: 6 },
  run: { gap: 8 },
  runCard: { borderRadius: radius.card, padding: 14 },
  runHead: { flexDirection: "row", alignItems: "center", gap: 8 },
  steps: { marginLeft: 4 },
});
