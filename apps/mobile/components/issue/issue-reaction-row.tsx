/**
 * Issue-level reaction row. Sits right under the description, mirroring
 * web's `issue-detail.tsx:785` placement.
 *
 * Reads issue.reactions from the detail cache passed by the parent. No
 * separate query — single source of truth on the detail object.
 *
 * Renders nothing when there are no reactions. Adding a reaction is
 * deferred to a long-press affordance on the issue body (TODO).
 */
import { useCallback } from "react";
import { View } from "react-native";
import type { Issue, IssueReaction } from "@multica/core/types";
import { ReactionBar } from "./reaction-bar";
import { useToggleIssueReaction } from "@/data/mutations/issues";
import { useAuthStore } from "@/data/auth-store";

export function IssueReactionRow({ issue }: { issue: Issue }) {
  const userId = useAuthStore((s) => s.user?.id);
  const reactions: IssueReaction[] = issue.reactions ?? [];
  const toggle = useToggleIssueReaction(issue.id);

  const onToggle = useCallback(
    (emoji: string) => {
      const existing = reactions.find(
        (r) =>
          r.emoji === emoji &&
          r.actor_type === "member" &&
          r.actor_id === userId,
      );
      toggle.mutate({ emoji, existing });
    },
    [reactions, userId, toggle],
  );

  if (reactions.length === 0) return null;

  return (
    <View className="px-4 pb-3">
      <ReactionBar
        reactions={reactions}
        currentUserId={userId}
        onToggle={onToggle}
      />
    </View>
  );
}
