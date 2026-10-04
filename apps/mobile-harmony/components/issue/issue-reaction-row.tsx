/**
 * HarmonyOS port of apps/mobile/components/issue/issue-reaction-row.tsx.
 * Issue-level reaction row — sits right under the description, mirroring
 * web's issue-detail.tsx:785 placement.
 *
 * Reads issue.reactions from the detail cache passed by the parent. No
 * separate query — single source of truth on the detail object. Renders
 * nothing when there are no reactions.
 */
import React from "react";
import { StyleSheet, View } from "react-native";
import type { Issue, IssueReaction } from "@multica/core/types";
import { ReactionBar } from "./reaction-bar";
import { useToggleIssueReaction } from "@/data/mutations/issues";
import { useAuthStore } from "@/data/auth-store";

export function IssueReactionRow({ issue }: { issue: Issue }) {
  const userId = useAuthStore((s) => s.user?.id);
  const reactions: IssueReaction[] = issue.reactions ?? [];
  const toggle = useToggleIssueReaction(issue.id);

  const onToggle = React.useCallback(
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
    <View style={styles.wrap}>
      <ReactionBar
        reactions={reactions}
        currentUserId={userId}
        onToggle={onToggle}
      />
    </View>
  );
}

// px-4 pb-3
const styles = StyleSheet.create({
  wrap: { paddingHorizontal: 16, paddingBottom: 12 },
});
