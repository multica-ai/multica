import { useState } from "react";
import {
  KeyboardAvoidingView,
  Platform,
  ScrollView,
  Text,
  View,
} from "react-native";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { useAuthStore } from "@multica/core/auth";
import { issueDetailOptions } from "@multica/core/issues/queries";
import { useToggleCommentReaction } from "@multica/core/issues/mutations";
import { STATUS_CONFIG } from "@multica/core/issues/config/status";
import { PRIORITY_CONFIG } from "@multica/core/issues/config/priority";
import { useActorName } from "@multica/core/workspace/hooks";
import type { TimelineEntry } from "@multica/core/types";

import { ActorAvatar } from "@/components/ui/actor-avatar";
import { StatusIcon } from "@/components/ui/status-icon";
import { PriorityIcon } from "@/components/ui/priority-icon";
import { Markdown } from "@/components/ui/markdown";
import { CommentList } from "@/components/issue/comment-list";
import { CommentComposer } from "@/components/issue/comment-composer";
import { IssueDetailHeader } from "@/components/issue/issue-detail-header";
import { ReactionList } from "@/components/issue/reaction-list";
import { EmojiQuickPicker } from "@/components/issue/emoji-quick-picker";

interface Props {
  issueId: string;
}

export function IssueDetailView({ issueId }: Props) {
  const wsId = useWorkspaceId();
  const currentUserId = useAuthStore((s) => s.user?.id);
  const { data: issue, isLoading, error } = useQuery(
    issueDetailOptions(wsId, issueId),
  );
  const { getActorName } = useActorName();

  // Reply-to state lives at this level so the composer (sibling) can show the
  // quote bar while CommentList only needs to fire `setReplyTo`.
  const [replyTo, setReplyTo] = useState<TimelineEntry | null>(null);
  // Reaction picker target: comment id for which the picker is open. Mutation
  // looks up "did the user already react with this emoji" by reading the live
  // entry from the timeline cache via useToggleCommentReaction.
  const [reactPickerFor, setReactPickerFor] = useState<string | null>(null);
  const toggleReaction = useToggleCommentReaction(issueId);

  if (isLoading) {
    return (
      <View className="flex-1 bg-background items-center justify-center">
        <Text className="text-muted-foreground">Loading…</Text>
      </View>
    );
  }

  if (error || !issue) {
    return (
      <View className="flex-1 bg-background items-center justify-center px-8">
        <Text className="text-destructive text-center">
          {error instanceof Error
            ? error.message
            : "Issue not found or unavailable."}
        </Text>
      </View>
    );
  }

  const assigneeName =
    issue.assignee_type && issue.assignee_id
      ? getActorName(issue.assignee_type, issue.assignee_id)
      : null;

  return (
    <KeyboardAvoidingView
      style={{ flex: 1, backgroundColor: "white" }}
      behavior={Platform.OS === "ios" ? "padding" : undefined}
    >
      <IssueDetailHeader
        identifier={issue.identifier}
        title={issue.title}
      />
      <ScrollView
        className="flex-1 bg-background"
        keyboardShouldPersistTaps="handled"
        contentContainerStyle={{ paddingBottom: 100 }}
      >
        <View className="px-4 pt-4 pb-6 gap-4">
          <View className="flex-row flex-wrap gap-2">
            <Chip>
              <StatusIcon status={issue.status} size={14} />
              <Text className="text-foreground text-sm">
                {STATUS_CONFIG[issue.status].label}
              </Text>
            </Chip>

            <Chip>
              <PriorityIcon priority={issue.priority} size={14} />
              <Text className="text-foreground text-sm">
                {PRIORITY_CONFIG[issue.priority].label}
              </Text>
            </Chip>

            {assigneeName && issue.assignee_type && issue.assignee_id ? (
              <Chip>
                <ActorAvatar
                  type={issue.assignee_type}
                  id={issue.assignee_id}
                  size={18}
                />
                <Text className="text-foreground text-sm">{assigneeName}</Text>
              </Chip>
            ) : (
              <Chip>
                <Text className="text-muted-foreground text-sm">Unassigned</Text>
              </Chip>
            )}
          </View>

          {issue.description ? (
            <View className="mt-2">
              <Markdown content={issue.description} />
            </View>
          ) : (
            <Text className="text-muted-foreground text-sm italic mt-2">
              No description.
            </Text>
          )}

          {issue.reactions && issue.reactions.length > 0 && (
            <ReactionList reactions={issue.reactions} />
          )}
        </View>

        <View className="h-px bg-border mx-4" />

        <CommentList
          issueId={issueId}
          currentUserId={currentUserId}
          onReplyPress={setReplyTo}
          onReactPress={setReactPickerFor}
        />
      </ScrollView>

      <CommentComposer
        issueId={issueId}
        replyTo={replyTo}
        onCancelReply={() => setReplyTo(null)}
      />

      <EmojiQuickPicker
        visible={reactPickerFor !== null}
        onClose={() => setReactPickerFor(null)}
        onSelect={(emoji) => {
          if (!reactPickerFor) return;
          // We don't have direct access to the existing reactions array here,
          // but useToggleCommentReaction expects an `existing` Reaction to
          // know whether to remove vs add. The "+" button means the user is
          // adding a new emoji — when they tap an emoji they don't yet own
          // it, so existing=undefined. Toggling an already-owned emoji from
          // the picker is a degenerate case; the chip-tap path handles toggle
          // off correctly.
          toggleReaction.mutate({
            commentId: reactPickerFor,
            emoji,
            existing: undefined,
          });
        }}
      />
    </KeyboardAvoidingView>
  );
}

function Chip({ children }: { children: React.ReactNode }) {
  return (
    <View className="flex-row items-center gap-1.5 px-3 py-1 rounded-full bg-muted">
      {children}
    </View>
  );
}
