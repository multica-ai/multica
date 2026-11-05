import {
  submissionOutbox,
  useSubmissionOutbox,
  EMPTY_SUBMISSIONS,
} from "@/data/ai-sessions/submission-outbox";
import { submissionPresentation } from "@/data/ai-sessions/submission-presentation";
import type { LocalV2Snapshot } from "@multica/core/local-conversation/v2";
import { useCallback, useMemo, useState } from "react";
import { ActionSheetIOS, ActivityIndicator, Alert, Animated, PanResponder, Pressable, ScrollView, StyleSheet, useAnimatedValue, useWindowDimensions, View } from "react-native";
import { router, useFocusEffect, useLocalSearchParams } from "expo-router";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { localMessageDisplayContent } from "@multica/core/local-conversation/protocol";
import { useAIConnection, useAISessionScope } from "@/data/ai-sessions/hooks";
import { aiKeys } from "@/data/ai-sessions/client";
import { queueItemSchema, queueSchema, type AIQueue } from "@/data/ai-sessions/schemas";
import { Text } from "@/components/ui/text";
import { Button } from "@/components/ui/button";
import { useT } from "@/lib/i18n";
import { AIMessageContent } from "@/components/ai-sessions/reference-chips";
import { AISheet } from "@/components/ai-sessions/sheet";
import { SessionAttachment } from "@/components/ai-sessions/session-attachment";
import { IconButton } from "@/components/ui/icon-button";
import { cn } from "@/lib/utils";

export default function AIQueueScreen() {
  const { t } = useT("aiSessions");
  const { height } = useWindowDimensions();
  const offset = useAnimatedValue(0);
  const drag = useMemo(() => {
    const reset = () => Animated.spring(offset, { toValue: 0, useNativeDriver: true }).start();
    return PanResponder.create({
      onMoveShouldSetPanResponder: (_, gesture) => gesture.dy > 8 && gesture.dy > Math.abs(gesture.dx),
      onPanResponderMove: (_, gesture) => offset.setValue(Math.max(0, gesture.dy)),
      onPanResponderRelease: (_, gesture) => {
        if (gesture.dy > 80 || gesture.vy > 0.7) {
          Animated.timing(offset, { toValue: height, duration: 180, useNativeDriver: true }).start(({ finished }) => { if (finished) router.back(); });
        } else reset();
      },
      onPanResponderTerminate: reset,
    });
  }, [height, offset]);
  const { session } = useLocalSearchParams<{ session: string }>();
  const { client, connected } = useAIConnection();
  const scope = useAISessionScope();
  const qc = useQueryClient();
  const outboxKey = JSON.stringify([
    process.env.EXPO_PUBLIC_API_URL,
    scope.userId,
    scope.workspaceId,
    scope.deviceId,
    session,
  ]);
  const records = useSubmissionOutbox(
    (state) => state.records[outboxKey] ?? EMPTY_SUBMISSIONS,
  );
  const snapshot = qc.getQueryData<LocalV2Snapshot>(
    aiKeys.snapshot(scope, session),
  );
  const [active, setActive] = useState(false);
  useFocusEffect(useCallback(() => { setActive(true); return () => setActive(false); }, []));
  const key = [...aiKeys.scope(scope), "queue", session];
  const query = useQuery({ queryKey: key, enabled: !!client && connected && active,
    queryFn: async ({ signal }) => { const next = await client!.request("GET", `/sessions/${encodeURIComponent(session)}/queue`, queueSchema, undefined, signal); const current = qc.getQueryData<AIQueue>(key); return current && current.revision > next.revision ? current : next; }, refetchInterval: active && connected ? 2000 : false });
  const update = useMutation({
    mutationFn: ({
      action,
      requestId,
    }: {
      action: "pause" | "resume" | "remove" | "send_now";
      requestId?: string;
    }) =>
      client!.command(
        session,
        { type: "queue_update", action, request_id: requestId },
        queueSchema,
      ),
    onSuccess: (queue, variables) => {
      if (variables.action === "remove" && variables.requestId)
        void submissionOutbox
          .remove(outboxKey, variables.requestId)
          .catch(() => Alert.alert(t("error_title"), t("operation_failed")));
      qc.setQueryData(key, queue);
      void qc.invalidateQueries({ queryKey: aiKeys.snapshot(scope, session) });
    },
    onError: () => {
      Alert.alert(t("error_title"), t("operation_failed"));
      void query.refetch();
    },
  });
  const local = submissionPresentation(records, snapshot).queued;
  const items = [
    ...(query.data?.items ?? []).filter(
      (item) =>
        snapshot?.submissions.find(
          (value) => value.request_id === item.requestId,
        )?.state !== "starting",
    ),
  ];
  for (const record of local)
    if (!items.some((item) => item.requestId === record.request.requestId))
      items.push({
        requestId: record.request.requestId,
        content: record.request.prepared?.text ?? record.request.text,
        attachmentIds: record.request.attachments,
      });
  const choose = (requestId: string) =>
    ActionSheetIOS.showActionSheetWithOptions(
      {
        options: [t("cancel"), t("send_now_interrupt"), t("remove")],
        cancelButtonIndex: 0,
        destructiveButtonIndex: 2,
      },
      (index) => {
        if (index === 1) update.mutate({ action: "send_now", requestId });
        if (index === 2) update.mutate({ action: "remove", requestId });
      },
    );
  return (
    <View className="flex-1 justify-end" accessibilityViewIsModal>
      <Pressable
        style={StyleSheet.absoluteFill}
        className="bg-black/40"
        accessibilityRole="button"
        accessibilityLabel={t("cancel")}
        onPress={() => router.back()}
      />
      <Animated.View
        {...drag.panHandlers}
        style={{ transform: [{ translateY: offset }] }}
        className="shrink overflow-hidden rounded-t-3xl bg-background"
      >
        <AISheet title={t("queue")} maxHeight={height * 0.8}>
          <Text
            style={{ lineHeight: undefined }}
            className="text-sm text-muted-foreground"
          >
            {t("queue_help")}
          </Text>
          <Button
            style={{ height: "auto", minHeight: 40 }}
            hitSlop={2}
            variant="outline"
            disabled={!connected || !query.data || update.isPending}
            onPress={() =>
              update.mutate({ action: query.data?.paused ? "resume" : "pause" })
            }
          >
            <Text style={{ lineHeight: undefined }}>
              {t(query.data?.paused ? "resume_queue" : "pause_queue")}
            </Text>
          </Button>
          {!!query.data?.error && (
            <Text
              style={{ lineHeight: undefined }}
              accessibilityRole="alert"
              className="text-sm text-destructive"
            >
              {query.data.error}
            </Text>
          )}
          {(query.isPending || update.isPending) && connected && (
            <ActivityIndicator />
          )}
          {(query.isError || !connected) && (
            <Button
              style={{ height: "auto", minHeight: 40 }}
              hitSlop={2}
              variant="outline"
              disabled={!connected}
              onPress={() => void query.refetch()}
            >
              <Text style={{ lineHeight: undefined }}>{t("retry")}</Text>
            </Button>
          )}
          <ScrollView
            style={{ flexGrow: 0, flexShrink: 1 }}
            contentInsetAdjustmentBehavior="never"
            keyboardShouldPersistTaps="handled"
          >
            {!items.length && !query.isPending && (
              <Text
                style={{ lineHeight: undefined }}
                className="py-6 text-muted-foreground"
              >
                {t("queue_empty")}
              </Text>
            )}
            {items.map((item, index, items) => {
              const parsed = queueItemSchema.safeParse(item);
              return (
                <View
                  key={parsed.success ? parsed.data.requestId : index}
                  className={cn(
                    "py-4 flex-row gap-3",
                    index < items.length - 1 && "border-b border-border",
                  )}
                >
                  <Text
                    style={{ lineHeight: undefined }}
                    className="text-muted-foreground"
                  >
                    {index + 1}
                  </Text>
                  <View className="flex-1">
                    {parsed.success ? (
                      <>
                        <AIMessageContent
                          content={localMessageDisplayContent({
                            role: "user",
                            content: parsed.data.content,
                            attachmentIds: parsed.data.attachmentIds,
                          })}
                        />
                        {parsed.data.attachmentIds.map((id) => (
                          <SessionAttachment key={id} id={id} />
                        ))}
                      </>
                    ) : (
                      <Text style={{ lineHeight: undefined }}>
                        {t("operation_failed")}
                      </Text>
                    )}
                  </View>
                  <IconButton
                    name="ellipsis-horizontal"
                    hitSlop={4}
                    accessibilityLabel={t("more_actions")}
                    disabled={!parsed.success || !connected || update.isPending}
                    onPress={() =>
                      parsed.success && choose(parsed.data.requestId)
                    }
                  />
                </View>
              );
            })}
          </ScrollView>
        </AISheet>
        <View
          pointerEvents="none"
          className="absolute top-1 self-center h-1 w-10 rounded-full bg-muted-foreground/40"
        />
      </Animated.View>
    </View>
  );
}
