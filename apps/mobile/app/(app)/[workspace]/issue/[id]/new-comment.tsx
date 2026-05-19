/**
 * Comment composition modal — full-screen replacement for the old always-on
 * inline composer at the bottom of issue/[id].tsx.
 *
 * Why a modal route instead of inline:
 *   - Always-on inline composer competed with the timeline for vertical
 *     space and the keyboard avoiding logic was clunky (user feedback).
 *   - Modal gives the composer dedicated real estate: bigger TextArea,
 *     MentionSuggestionBar can lay out without colliding with toolbar.
 *   - iOS slide-up sheet is the platform-standard "compose" pattern
 *     (Mail, Linear, Slack thread reply).
 *
 * Reuses the same `useMentionInput` + `useFileAttach` hooks as the chat
 * composer — same `[@name](mention://type/id)` markdown the server parses.
 *
 * Reply mode: route param `parent` carries the parent comment id;
 * `parentName` carries the display name for the header. The composer
 * submits with `parentId` set; backend treats it as a threaded reply.
 *
 * Submit success → router.back() returns to the issue detail screen. The
 * `useCreateComment` optimistic mutation has already patched the timeline
 * cache before the modal closes, so the new comment is visible immediately
 * without a flash.
 */
import { useCallback, useRef, useState } from "react";
import {
  Alert,
  KeyboardAvoidingView,
  Platform,
  Pressable,
  TextInput,
  View,
} from "react-native";
import { Stack, router, useLocalSearchParams } from "expo-router";
import { Image } from "expo-image";
import { Text } from "@/components/ui/text";
import { AutosizeTextArea } from "@/components/ui/autosize-textarea";
import { MentionSuggestionBar } from "@/components/issue/mention-suggestion-bar";
import { useMentionInput } from "@/lib/use-mention-input";
import { useFileAttach } from "@/components/editor/use-file-attach";
import { useColorScheme } from "@/lib/use-color-scheme";
import { THEME } from "@/lib/theme";
import { useCreateComment } from "@/data/mutations/issues";
import { cn } from "@/lib/utils";

const ICON_SIZE = 18;

export default function NewCommentModal() {
  const { id, parent, parentName } = useLocalSearchParams<{
    id: string;
    parent?: string;
    parentName?: string;
  }>();
  const { colorScheme } = useColorScheme();
  const theme = THEME[colorScheme];

  const mention = useMentionInput();
  const fileAttach = useFileAttach();
  const inputRef = useRef<TextInput>(null);
  const [submitting, setSubmitting] = useState(false);
  const createComment = useCreateComment(id);

  const trimmed = mention.text.trim();
  // Same send-gate as the old inline composer: text non-empty, not already
  // mid-submit, no upload in flight (the upload's deferred insertAtCursor
  // would otherwise race a clear and orphan markdown into the next message).
  const canSend = trimmed.length > 0 && !submitting && !fileAttach.uploading;

  const isReply = !!parent;
  const title = isReply ? "Reply" : "New comment";

  const handleSend = useCallback(async () => {
    if (!canSend) return;
    setSubmitting(true);
    const snap = mention.snapshot();
    const content = mention.serialize().trim();
    try {
      await createComment.mutateAsync({ content, parentId: parent });
      router.back();
    } catch (err) {
      // Restore so the user doesn't lose their text. Surface a toast so
      // they know it failed (Alert is the simplest reliable surface here).
      mention.restore(snap);
      Alert.alert(
        "Couldn't send",
        err instanceof Error ? err.message : "Try again in a moment.",
      );
      setSubmitting(false);
    }
  }, [canSend, mention, createComment, parent]);

  const handleAttachImage = useCallback(async () => {
    const result = await fileAttach.pickAndUploadImage({ issueId: id });
    if (result) mention.insertAtCursor(`![](${result.url})`);
  }, [fileAttach, mention, id]);

  const handleAttachFile = useCallback(async () => {
    const result = await fileAttach.pickAndUploadFile({ issueId: id });
    if (result) {
      // Mobile preprocess converts `[📎 name](url)` into the file-card visual,
      // round-tripping identically to web.
      mention.insertAtCursor(`[📎 ${result.filename}](${result.url})`);
    }
  }, [fileAttach, mention, id]);

  return (
    <View className="flex-1 bg-background">
      <Stack.Screen
        options={{
          title,
          headerRight: () => (
            <Pressable
              onPress={handleSend}
              disabled={!canSend}
              hitSlop={8}
              className={cn(
                "h-7 px-3 items-center justify-center rounded-full",
                canSend ? "bg-primary active:opacity-80" : "bg-secondary",
              )}
              accessibilityRole="button"
              accessibilityLabel="Send comment"
              accessibilityState={{ disabled: !canSend }}
            >
              <Text
                className={cn(
                  "text-sm font-medium",
                  canSend ? "text-primary-foreground" : "text-muted-foreground",
                )}
              >
                {submitting ? "Sending…" : "Send"}
              </Text>
            </Pressable>
          ),
        }}
      />

      {isReply ? (
        <View className="flex-row items-center gap-2 px-4 py-2 bg-secondary/40 border-b border-border">
          <Text className="text-xs text-muted-foreground">↩</Text>
          <Text
            className="flex-1 text-xs text-muted-foreground"
            numberOfLines={1}
          >
            Replying to{" "}
            <Text className="text-foreground font-medium">{parentName}</Text>
          </Text>
        </View>
      ) : null}

      <KeyboardAvoidingView
        behavior={Platform.OS === "ios" ? "padding" : undefined}
        className="flex-1"
      >
        <View className="flex-1 px-4 pt-3">
          <AutosizeTextArea
            ref={inputRef}
            value={mention.text}
            onChangeText={mention.handlers.onChangeText}
            selection={mention.selection}
            onSelectionChange={mention.handlers.onSelectionChange}
            placeholder={isReply ? "Write a reply…" : "Add a comment…"}
            className="flex-1 text-base"
            editable={!submitting}
            autoFocus
            multiline
          />
        </View>

        <MentionSuggestionBar {...mention.suggestionBar} />

        {/* Toolbar pinned above the keyboard. SF Symbols via expo-image —
         *  tintColor pulled from THEME so light/dark flip automatically. */}
        <View className="flex-row items-center px-4 py-2 gap-1 border-t border-border bg-background">
          <Pressable
            onPress={mention.handlers.onAtButtonPress}
            disabled={submitting || fileAttach.uploading}
            className="h-9 w-9 items-center justify-center rounded-full active:bg-secondary"
            hitSlop={6}
            accessibilityRole="button"
            accessibilityLabel="Mention"
          >
            <Image
              source="sf:at"
              tintColor={theme.mutedForeground}
              style={{ width: ICON_SIZE, height: ICON_SIZE }}
            />
          </Pressable>
          <Pressable
            onPress={handleAttachImage}
            disabled={submitting || fileAttach.uploading}
            className="h-9 w-9 items-center justify-center rounded-full active:bg-secondary"
            hitSlop={6}
            accessibilityRole="button"
            accessibilityLabel="Attach image"
          >
            <Image
              source="sf:photo"
              tintColor={theme.mutedForeground}
              style={{ width: ICON_SIZE, height: ICON_SIZE }}
            />
          </Pressable>
          <Pressable
            onPress={handleAttachFile}
            disabled={submitting || fileAttach.uploading}
            className="h-9 w-9 items-center justify-center rounded-full active:bg-secondary"
            hitSlop={6}
            accessibilityRole="button"
            accessibilityLabel="Attach file"
          >
            <Image
              source="sf:paperclip"
              tintColor={theme.mutedForeground}
              style={{ width: ICON_SIZE, height: ICON_SIZE }}
            />
          </Pressable>
        </View>
      </KeyboardAvoidingView>
    </View>
  );
}
