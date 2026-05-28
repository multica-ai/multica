/**
 * Full emoji picker for a comment reaction — opened from the per-comment
 * long-press menu's "+" tapback button. Mirrors web's emoji-mart picker
 * that sits behind QuickEmojiPicker's overflow button: same product
 * semantics (mobile must offer the full emoji set, not only the 8 quick
 * picks).
 *
 * Reads the comment from the timeline cache to detect an already-applied
 * reaction by the current user, then fires `useToggleCommentReaction` with
 * the right `existing` value so re-tapping an active emoji removes it
 * (matches web behaviour and the inline ReactionBar toggle semantics).
 *
 * Library: `rn-emoji-keyboard` (TheWidlarzGroup/rn-emoji-keyboard). We
 * embed the `EmojiKeyboard` component (no built-in modal) inside the
 * Expo Router formSheet route body, so the iOS UISheetPresentationController
 * still owns the chrome (grabber, detents, drag-to-dismiss).
 */
import { useCallback, useMemo } from "react";
import { View } from "react-native";
import { useLocalSearchParams, router } from "expo-router";
import { useQuery } from "@tanstack/react-query";
import { EmojiKeyboard, type EmojiType } from "rn-emoji-keyboard";
import type { Reaction } from "@multica/core/types";
import { Text } from "@/components/ui/text";
import { issueTimelineOptions } from "@/data/queries/issues";
import { useToggleCommentReaction } from "@/data/mutations/issues";
import { useAuthStore } from "@/data/auth-store";
import { useWorkspaceStore } from "@/data/workspace-store";
import { useColorScheme } from "@/lib/use-color-scheme";
import { THEME } from "@/lib/theme";

export default function CommentEmojiPickerRoute() {
  const { id, commentId } = useLocalSearchParams<{
    id: string;
    commentId: string;
  }>();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const userId = useAuthStore((s) => s.user?.id);
  const toggle = useToggleCommentReaction(id);
  const { colorScheme } = useColorScheme();

  const { data: timeline = [] } = useQuery(issueTimelineOptions(wsId, id));
  const entry = useMemo(
    () => timeline.find((e) => e.id === commentId) ?? null,
    [timeline, commentId],
  );

  const reactions = useMemo<Reaction[]>(
    () => (entry?.reactions ?? []) as Reaction[],
    [entry?.reactions],
  );

  const onSelect = useCallback(
    (picked: EmojiType) => {
      const existing = reactions.find(
        (r) =>
          r.emoji === picked.emoji &&
          r.actor_type === "member" &&
          r.actor_id === userId,
      );
      toggle.mutate({ commentId, emoji: picked.emoji, existing });
      router.back();
    },
    [reactions, userId, toggle, commentId],
  );

  const theme = THEME[colorScheme];

  return (
    <View className="flex-1">
      <View className="px-4 pt-3 pb-2">
        <Text className="text-lg font-semibold text-foreground">
          Add Reaction
        </Text>
      </View>
      <View className="flex-1">
        <EmojiKeyboard
          onEmojiSelected={onSelect}
          enableSearchBar
          enableRecentlyUsed
          categoryPosition="top"
          theme={{
            backdrop: theme.background,
            knob: theme.mutedForeground,
            container: theme.popover,
            header: theme.foreground,
            skinTonesContainer: theme.secondary,
            category: {
              icon: theme.mutedForeground,
              iconActive: theme.foreground,
              container: theme.popover,
              containerActive: theme.secondary,
            },
            search: {
              background: theme.secondary,
              text: theme.foreground,
              placeholder: theme.mutedForeground,
              icon: theme.mutedForeground,
            },
            customButton: {
              icon: theme.mutedForeground,
              iconPressed: theme.foreground,
              background: theme.secondary,
              backgroundPressed: theme.muted,
            },
            emoji: {
              selected: theme.secondary,
            },
          }}
        />
      </View>
    </View>
  );
}
