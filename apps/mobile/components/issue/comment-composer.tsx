import { useEffect, useRef, useState } from "react";
import {
  ActivityIndicator,
  Pressable,
  Text,
  TextInput,
  View,
} from "react-native";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import * as Haptics from "expo-haptics";
import { useCreateComment } from "@multica/core/issues/mutations";
import { useActorName } from "@multica/core/workspace/hooks";
import type { TimelineEntry } from "@multica/core/types";

import { IconSymbol } from "@/components/ui/icon-symbol";
import {
  MentionPicker,
  type MentionSelection,
} from "@/components/issue/mention-picker";

interface Props {
  issueId: string;
  /** Comment being replied to. When set, send fires with parentId. */
  replyTo: TimelineEntry | null;
  onCancelReply: () => void;
}

// Sticky-bottom composer for issue detail.
//
// v2 (this revision) adds:
//   - reply-to quote bar (iMessage-style) above the input when `replyTo` set;
//     send goes out with parentId so the backend threads it.
//   - delegates the mutation to @multica/core/issues/mutations.useCreateComment
//     so optimistic insertion + WS invalidation are unified with web.
//
// Existing v1 features kept: multiline TextInput (autogrows ~5 lines), @ pill
// → MentionPicker → inserts `[@Name](mention://type/id) ` token.
export function CommentComposer({ issueId, replyTo, onCancelReply }: Props) {
  const [text, setText] = useState("");
  const [pickerVisible, setPickerVisible] = useState(false);
  const inputRef = useRef<TextInput>(null);
  const insets = useSafeAreaInsets();
  const { getActorName } = useActorName();

  const create = useCreateComment(issueId);

  // Focus the input as soon as the user picks a comment to reply to so the
  // keyboard pops without an extra tap.
  useEffect(() => {
    if (replyTo) {
      setTimeout(() => inputRef.current?.focus(), 100);
    }
  }, [replyTo]);

  const trimmed = text.trim();
  const canSend = trimmed.length > 0 && !create.isPending;

  const handleSend = () => {
    if (!canSend) return;
    Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light).catch(() => {});
    create.mutate(
      { content: trimmed, parentId: replyTo?.id },
      {
        onSuccess: () => {
          setText("");
          if (replyTo) onCancelReply();
        },
      },
    );
  };

  const handleMentionSelect = (s: MentionSelection) => {
    setPickerVisible(false);
    const insert = `[@${s.name}](mention://${s.type}/${s.id}) `;
    setText((prev) => {
      const sep = prev.length > 0 && !/\s$/.test(prev) ? " " : "";
      return prev + sep + insert;
    });
    setTimeout(() => inputRef.current?.focus(), 100);
  };

  const replyAuthor = replyTo
    ? getActorName(replyTo.actor_type, replyTo.actor_id)
    : "";
  const replyPreview = replyTo
    ? (replyTo.content ?? "").replace(/\n/g, " ").slice(0, 60)
    : "";

  return (
    <>
      <View
        className="border-t border-border bg-background"
        style={{ paddingBottom: Math.max(insets.bottom, 12) }}
      >
        {replyTo && (
          <View className="flex-row items-center gap-2 px-3 pt-2 pb-1">
            <View className="w-0.5 self-stretch bg-brand/40 rounded-full" />
            <View className="flex-1 min-w-0">
              <Text className="text-xs text-muted-foreground">
                Replying to{" "}
                <Text className="text-brand font-medium">@{replyAuthor}</Text>
              </Text>
              <Text
                className="text-xs text-muted-foreground"
                numberOfLines={1}
              >
                {replyPreview}
              </Text>
            </View>
            <Pressable
              onPress={onCancelReply}
              hitSlop={8}
              className="rounded-full bg-muted items-center justify-center"
              style={{ width: 24, height: 24 }}
            >
              {/* eslint-disable-next-line @typescript-eslint/no-explicit-any */}
              <IconSymbol
                name={"xmark" as any}
                size={12}
                color="hsl(240 4% 46%)"
              />
            </Pressable>
          </View>
        )}

        <View className="flex-row items-end gap-2 px-3 py-2">
          <Pressable
            onPress={() => setPickerVisible(true)}
            hitSlop={8}
            className="rounded-full bg-muted items-center justify-center"
            style={{ width: 36, height: 36 }}
          >
            <Text className="text-muted-foreground text-base font-semibold">
              @
            </Text>
          </Pressable>

          <TextInput
            ref={inputRef}
            value={text}
            onChangeText={setText}
            placeholder={replyTo ? "Reply…" : "Comment"}
            placeholderTextColor="hsl(240 4% 46%)"
            multiline
            editable={!create.isPending}
            style={{
              flex: 1,
              minHeight: 36,
              maxHeight: 120,
              paddingHorizontal: 12,
              paddingTop: 8,
              paddingBottom: 8,
              fontSize: 16,
              color: "hsl(240 10% 4%)",
              backgroundColor: "hsl(240 5% 96%)",
              borderRadius: 18,
            }}
          />

          <Pressable
            onPress={handleSend}
            disabled={!canSend}
            hitSlop={8}
            style={{
              width: 36,
              height: 36,
              borderRadius: 18,
              alignItems: "center",
              justifyContent: "center",
              backgroundColor: canSend ? "hsl(240 6% 10%)" : "hsl(240 5% 96%)",
            }}
          >
            {create.isPending ? (
              <ActivityIndicator color="white" size="small" />
            ) : (
              // eslint-disable-next-line @typescript-eslint/no-explicit-any
              <IconSymbol
                name={"arrow.up" as any}
                size={18}
                color={canSend ? "white" : "hsl(240 4% 46%)"}
              />
            )}
          </Pressable>
        </View>
      </View>

      <MentionPicker
        visible={pickerVisible}
        onClose={() => setPickerVisible(false)}
        onSelect={handleMentionSelect}
      />
    </>
  );
}
