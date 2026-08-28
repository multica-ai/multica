/**
 * Comments directory modal (RUYI-28) — Android-first navigation aid.
 *
 * Presents the issue's root comments as an ASC (oldest-first) list with a
 * local author/text filter. Selecting an entry:
 *   1. writes a focus intent {issueId, rootId, nonce} into the per-issue
 *      comment-focus store (the timeline beneath consumes it: expands the
 *      target root, then bounded-locates its row with viewability
 *      confirmation — see timeline-list.tsx and lib/comment-locate.ts),
 *   2. stays open while the intent's status is `pending`,
 *   3. closes (`router.back()`) ONLY once the status turns `located` —
 *      i.e. the target row is confirmed on screen, not merely scrolled
 *      at. A `failed` status keeps the modal open with an inline error +
 *      Retry (a fresh nonce), so a wrong jump never strands the user.
 *
 * System back / header back still close the modal directly — that path
 * never waits on the locate run.
 *
 * Reads the existing TanStack timeline cache (`issueTimelineOptions`) —
 * no server state is copied or refetched. If the cache is empty the modal
 * shows the same loading state as the timeline.
 *
 * Route params: deliberately does NOT reuse the inbox deep-link's
 * `highlight` / `h` params (approved scope) — those drive the timeline's
 * flash-and-scroll-to-bottom path, which is a different interaction from
 * the directory's expand-and-locate.
 *
 * `presentation: "modal"` (registered in the workspace _layout): full-page
 * slide-up with its own header; Android system back closes it.
 */
import { useEffect, useMemo, useRef, useState } from "react";
import {
  ActivityIndicator,
  Pressable,
  ScrollView,
  TextInput,
  View,
} from "react-native";
import { router, useLocalSearchParams } from "expo-router";
import { useQuery } from "@tanstack/react-query";
import { Ionicons } from "@expo/vector-icons";
import { Text } from "@/components/ui/text";
import { ActorAvatar } from "@/components/ui/actor-avatar";
import { issueTimelineOptions } from "@/data/queries/issues";
import {
  useCommentFocusStore,
  type CommentFocusStatus,
} from "@/data/stores/comment-focus-store";
import { useActorLookup } from "@/data/use-actor-name";
import { useWorkspaceStore } from "@/data/workspace-store";
import { buildTimelineRows } from "@/lib/timeline-thread";
import { coalesceTimeline } from "@/lib/timeline-coalesce";
import {
  buildCommentDirectory,
  filterCommentDirectory,
} from "@/lib/comment-directory";
import { useColorScheme } from "@/lib/use-color-scheme";
import { THEME } from "@/lib/theme";
import { useT } from "@/lib/use-t";

export default function IssueCommentsDirectoryRoute() {
  const { t } = useT("issues");
  const { id } = useLocalSearchParams<{ id: string }>();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const { data: entries, isLoading } = useQuery(
    issueTimelineOptions(wsId, id),
  );
  const { getName } = useActorLookup();
  const requestFocus = useCommentFocusStore((s) => s.requestFocus);
  const focus = useCommentFocusStore((s) => s.focus);
  const status = useCommentFocusStore((s) => s.status);
  const { colorScheme } = useColorScheme();
  const mutedFg = THEME[colorScheme].mutedForeground;
  const destructive = THEME[colorScheme].destructive;
  const [query, setQuery] = useState("");

  const items = useMemo(() => {
    if (!entries) return [];
    return buildCommentDirectory(buildTimelineRows(coalesceTimeline(entries)));
  }, [entries]);

  // Resolve author display names once; the filter matches on them too.
  const authorNames = useMemo(() => {
    const out: Record<string, string> = {};
    for (const it of items) {
      out[it.rootId] = getName(
        it.authorType as "member" | "agent" | null | undefined,
        it.authorId,
      );
    }
    return out;
  }, [items, getName]);

  const filtered = useMemo(
    () => filterCommentDirectory(items, query, authorNames),
    [items, query, authorNames],
  );

  // ── Locate-status handshake with the timeline ─────────────────────────
  // The timeline's controller publishes pending/located/failed into the
  // store. We close only on `located` for OUR issue, and only for a
  // request minted while THIS modal mount is open. The mount-time nonce
  // snapshot stops a re-opened modal from replaying the previous visit's
  // already-`located` intent into an unrequested router.back().
  const mountedNonceRef = useRef<number>(-1);
  if (
    mountedNonceRef.current === -1 &&
    focus &&
    focus.issueId === id &&
    focus.nonce > 0
  ) {
    mountedNonceRef.current = focus.nonce;
  }
  const closedRef = useRef(false);
  useEffect(() => {
    if (!focus || focus.issueId !== id || !status) return;
    if (status.nonce !== focus.nonce) return;
    if (
      status.phase === "located" &&
      !closedRef.current &&
      status.nonce > mountedNonceRef.current
    ) {
      closedRef.current = true;
      router.back();
    }
  }, [focus, status, id]);

  // Status relevant to THIS modal's latest request (failed → inline retry).
  const ownStatus: CommentFocusStatus | null =
    focus && focus.issueId === id && status && status.nonce === focus.nonce
      ? status
      : null;
  const failedReason = ownStatus?.phase === "failed" ? ownStatus.reason : null;
  // Rows stay interactive while a locate is pending — the user may change
  // their mind and pick a different thread (a new tap mints a new nonce
  // and supersedes the in-flight run). Dim only the row whose locate is
  // pending so the feedback is local.
  const pendingRootId =
    ownStatus?.phase === "pending" ? focus?.rootId : null;

  const onSelect = (rootId: string) => {
    requestFocus(id, rootId);
  };

  const onRetry = () => {
    if (!focus || focus.issueId !== id) return;
    requestFocus(id, focus.rootId);
  };

  return (
    <View className="flex-1 bg-background">
      {/* Local filter — plain TextInput keeps this self-contained; the
          native search-bar pattern (useNativeSearchBar) belongs to the
          formSheet pickers, this is a full-page modal with a body-drawn
          header per the modal container selection table. */}
      <View className="px-4 pt-2 pb-3 gap-3 border-b border-border">
        <Text className="text-base font-semibold text-foreground">
          {t("mobile.detail.comments_title", "Comments")}
        </Text>
        <View className="flex-row items-center gap-2 px-3 py-2 rounded-lg bg-secondary">
          <Ionicons name="search" size={14} color={mutedFg} />
          <TextInput
            value={query}
            onChangeText={setQuery}
            placeholder={t(
              "mobile.detail.comments_search_placeholder",
              "Filter by author or text…",
            )}
            placeholderTextColor={mutedFg}
            autoCapitalize="none"
            autoCorrect={false}
            className="flex-1 text-sm text-foreground"
          />
        </View>
        {failedReason ? (
          <View className="flex-row items-center gap-2">
            <Ionicons name="alert-circle" size={14} color={destructive} />
            <Text className="flex-1 text-xs text-destructive">
              {t(
                "mobile.detail.comments_locate_failed",
                "Couldn't jump to that comment.",
              )}
            </Text>
            <Pressable
              onPress={onRetry}
              hitSlop={6}
              accessibilityRole="button"
              accessibilityLabel={t(
                "mobile.detail.comments_locate_retry_a11y",
                "Retry jumping to comment",
              )}
            >
              <Text className="text-xs text-primary font-medium">
                {t("mobile.comment.retry", "Retry")}
              </Text>
            </Pressable>
          </View>
        ) : null}
      </View>
      {isLoading ? (
        <View className="py-8 items-center">
          <ActivityIndicator color={mutedFg} />
        </View>
      ) : filtered.length === 0 ? (
        <View className="py-8 items-center">
          <Text className="text-sm text-muted-foreground">
            {items.length === 0
              ? t("mobile.detail.comments_empty", "No comments yet.")
              : t("mobile.detail.comments_no_match", "No matching comments.")}
          </Text>
        </View>
      ) : (
        <ScrollView showsVerticalScrollIndicator={false}>
          {filtered.map((it) => (
            <Pressable
              key={it.rootId}
              onPress={() => onSelect(it.rootId)}
              className="px-4 py-3 gap-1 active:bg-secondary border-b border-border/40"
              accessibilityRole="button"
              accessibilityLabel={
                authorNames[it.rootId] ?? it.rootId
              }
            >
              <View className="flex-row items-center gap-2">
                <ActorAvatar
                  type={it.authorType as "member" | "agent"}
                  id={it.authorId}
                  size={20}
                />
                <Text
                  className="text-sm font-medium text-foreground flex-1"
                  numberOfLines={1}
                >
                  {authorNames[it.rootId] ?? ""}
                </Text>
                {pendingRootId === it.rootId ? (
                  <ActivityIndicator size="small" color={mutedFg} />
                ) : null}
                {it.replyCount > 0 ? (
                  <Text className="text-xs text-muted-foreground">
                    {t("mobile.comment.reply_count", "{{count}} replies", {
                      count: it.replyCount,
                    })}
                  </Text>
                ) : null}
                {it.resolved ? (
                  <Ionicons
                    name="checkmark-circle"
                    size={14}
                    color={mutedFg}
                  />
                ) : null}
              </View>
              {it.summary ? (
                <Text
                  className="text-sm text-muted-foreground"
                  numberOfLines={2}
                >
                  {it.summary}
                </Text>
              ) : null}
            </Pressable>
          ))}
        </ScrollView>
      )}
    </View>
  );
}
