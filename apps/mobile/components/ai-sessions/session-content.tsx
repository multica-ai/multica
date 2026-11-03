import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { ActionSheetIOS, ActivityIndicator, Alert, KeyboardAvoidingView, Platform, Pressable, TextInput, View } from "react-native";
import { FlashList, type FlashListRef } from "@shopify/flash-list";
import { Header } from "@/components/ui/header";
import { SafeAreaView } from "react-native-safe-area-context";
import { Stack, router, useFocusEffect, useLocalSearchParams } from "expo-router";
import { useInfiniteQuery, useQuery, useQueryClient } from "@tanstack/react-query";
import { localV2SummarySchema } from "@multica/core/local-conversation/v2";
import * as DocumentPicker from "expo-document-picker";
import { Text } from "@/components/ui/text";
import { Button } from "@/components/ui/button";
import { IconButton } from "@/components/ui/icon-button";
import { MulticaLogo } from "@/components/brand/multica-logo";
import { SessionMarkdown } from "@/components/ai-sessions/session-markdown";
import { useT } from "@/lib/i18n";
import { createRequestId } from "@/lib/request-id";
import { THEME } from "@/lib/theme";
import { useColorScheme } from "@/lib/use-color-scheme";
import { aiKeys } from "@/data/ai-sessions/client";
import { useAIConnection, useAISessionScope, useAISubscriptionRejected } from "@/data/ai-sessions/hooks";
import { useAIDrafts, aiDraftKey, type DraftAttachment } from "@/data/ai-sessions/drafts";
import { draftUploads } from "@/data/ai-sessions/uploads";
import { useStoredSnapshot } from "@/data/ai-sessions/snapshot-store";
import { commandResultSchema, historySchema, providersSchema, queueSchema } from "@/data/ai-sessions/schemas";
import { transcript } from "@/data/ai-sessions/projection";
import { ConnectionNotice } from "@/components/ai-sessions/connection-notice";
import { SessionAttachment } from "@/components/ai-sessions/session-attachment";
import { openDeviceFile, remoteWebURL } from "@/data/ai-sessions/files";
import { SessionQuestions } from "@/components/ai-sessions/questions";

const NO_ATTACHMENTS: DraftAttachment[] = [];

export function AISessionContent({ sessionId, embedded = false, active = true, onOpenSource }: { sessionId?: string; embedded?: boolean; active?: boolean; onOpenSource?: () => void }) {
  const { t } = useT("aiSessions");
  const { workspace, device, session: routeSession, name, turn } = useLocalSearchParams<{ workspace: string; device: string; session: string; name?: string; turn?: string }>();
  const session = sessionId ?? routeSession;
  const [focused, setFocused] = useState(false);
  useFocusEffect(useCallback(() => { setFocused(true); return () => setFocused(false); }, []));
  const scope = useAISessionScope();
  const { client, connected } = useAIConnection();
  const subscriptionRejected = useAISubscriptionRejected(session);
  const qc = useQueryClient();
  const key = aiKeys.snapshot(scope, session);
  // The conversation as last read shows at once; the daemon sends what changed since.
  const restored = useStoredSnapshot(scope, session, key);
  const query = useQuery({ queryKey: key, enabled: !!client && restored, retry: false, staleTime: Infinity, queryFn: ({ signal }) => client!.snapshot(session, signal) });
  const snapshot = query.data;
  const { colorScheme } = useColorScheme();
  const theme = THEME[colorScheme];
  const draftKey = aiDraftKey(scope, session);
  const draft = useAIDrafts((s) => s.drafts[draftKey] ?? "");
  const setDraft = useAIDrafts((s) => s.setDraft);
  const pending = useAIDrafts((s) => s.pending[draftKey]);
  const setPending = useAIDrafts((s) => s.setPending);
  const attachments = useAIDrafts((s) => s.attachments[draftKey] ?? NO_ATTACHMENTS);
  const setAttachments = useAIDrafts((s) => s.setAttachments);
  const uploading = attachments.some((file) => file.status === "uploading");
  const [busy, setBusy] = useState(false);
  const [sendFailed, setSendFailed] = useState(false);
  const [expanded, setExpanded] = useState<Set<string>>(new Set());
  const list = useRef<FlashListRef<ReturnType<typeof transcript>[number]>>(null);
  const nearBottom = useRef(true);
  const rows = useMemo(() => snapshot ? transcript(snapshot) : [], [snapshot]);
  const history = useInfiniteQuery({ queryKey: [...aiKeys.scope(scope), "history", session, snapshot?.store_generation, snapshot?.turns[0]?.turn_index], enabled: false,
    initialPageParam: snapshot?.turns[0]?.turn_index ?? 0,
    queryFn: ({ signal, pageParam }) => client!.request("GET", `/sessions/${encodeURIComponent(session)}/history?before_index=${pageParam}`, historySchema, undefined, signal),
    getNextPageParam: (page) => page.has_older_turns && page.turns.length ? page.turns[0].turn_index : undefined,
  });
  const historyTurns = useMemo(() => history.data?.pages.flatMap((page) => page.store_generation === snapshot?.store_generation ? page.turns : []) ?? [], [history.data, snapshot?.store_generation]);
  const allRows = useMemo(() => snapshot ? transcript({ ...snapshot, turns: [...new Map([...historyTurns, ...snapshot.turns].map((turn) => [turn.id, turn])).values()].sort((a, b) => a.turn_index - b.turn_index) }) : [], [snapshot, historyTurns]);
  const hasOlder = history.data ? history.hasNextPage : snapshot?.has_older_turns;
  const [readingHistory, setReadingHistory] = useState(false);
  const running = snapshot?.turns.some((turn) => turn.status === "running") ?? false;
  // Selecting subscribes from the cached position, so the stored one must be in place first.
  useEffect(() => { if (restored && connected && focused && active) client?.select(session); }, [restored, client, connected, session, focused, active]);
  useEffect(() => (client && focused && active ? client.focus(session) : undefined), [client, session, focused, active]);
  useEffect(() => { if (nearBottom.current && rows.length) list.current?.scrollToEnd({ animated: false }); }, [rows]);
  useEffect(() => { if (snapshot) client?.committed(session); }, [client, session, snapshot]);
  const pendingRequest = pending?.requestId;
  useEffect(() => { if (pendingRequest) client?.pendingShown(session, pendingRequest); }, [client, session, pendingRequest]);
  useEffect(() => {
    if (!pending || !(snapshot?.queue.items.some((item) => item.requestId === pending.requestId) || snapshot?.turns.some((turn) => turn.request_id === pending.requestId))) return;
    if (draft === pending.text) setDraft(draftKey, "");
    setAttachments(draftKey, (files) => files.filter((file) => !pending.attachments.includes(file.id)));
    draftUploads.forget(pending.attachments);
    setPending(draftKey, null);
  }, [snapshot, pending, draft, draftKey, setDraft, setPending, setAttachments]);

  const showError = () => Alert.alert(t("error_title"), t("operation_failed"));
  const chooseProvider = async () => {
    if (!client || !connected || running || busy || uploading || pending) return;
    const available = (await client.request("GET", "/providers", providersSchema)).providers.filter((p) => p.available);
    ActionSheetIOS.showActionSheetWithOptions({ title: t("provider"), options: [t("cancel"), ...available.map((p) => p.name)], cancelButtonIndex: 0 }, (index) => {
      const provider = available[index - 1];
      if (!provider || provider.id === snapshot?.conversation.provider) return;
      setBusy(true);
      void client.request("POST", "/sessions", localV2SummarySchema, { id: `ios-${createRequestId()}`, provider: provider.id, title: t("new"), directory_source_id: session })
        .then((created) => {
          setDraft(aiDraftKey(scope, created.conversation_id), draft);
          void qc.invalidateQueries({ queryKey: aiKeys.list(scope) });
          router.replace({ pathname: "/[workspace]/ai-sessions/[device]/[session]", params: { workspace, device, session: created.conversation_id, name } });
        }).catch(showError).finally(() => setBusy(false));
    });
  };
  const send = async () => {
    if (!client || !connected || !snapshot || busy || snapshot.conversation.archived || !(draft.trim() || attachments.length || pending)) return;
    if (pending && pending.text !== draft) { Alert.alert(t("error_title"), t("send_failed")); return; }
    let request = pending ?? { requestId: createRequestId(), text: draft, attachments: attachments.map((file) => file.id), queued: running || snapshot.queue.items.length > 0 || snapshot.queue.paused, queueContent: draft || t("attached_files"), createdAt: Date.now() };
    setPending(draftKey, request); setBusy(true); setSendFailed(false);
    try {
      // The message stays pending while its attachments finish; a failed one gets another attempt.
      const stored = await draftUploads.ensure(request.attachments);
      draftUploads.forget(request.attachments);
      if (stored.some((id, index) => id !== request.attachments[index])) { request = { ...request, attachments: stored }; setPending(draftKey, request); }
      if (request.queued) {
        await client.submit(session, { type: "queue_enqueue", paused: snapshot.queue.paused, item: { requestId: request.requestId, content: request.queueContent ?? request.text, attachmentIds: request.attachments, display: { id: request.requestId, clientSubmitId: request.requestId, createdAtUnixMs: request.createdAt, content: [{ type: "text", text: request.text }] } } }, queueSchema, request.createdAt);
      } else await client.submit(session, { type: "send", request_id: request.requestId, text: request.text, attachment_ids: request.attachments }, commandResultSchema, request.createdAt);
      setDraft(draftKey, ""); setPending(draftKey, null); setAttachments(draftKey, () => []);
      void query.refetch();
    } catch (error) {
      const rejected = error instanceof Error && ["invalid_request", "not_found", "turn_running", "archived", "unknown_provider", "invalid_settings"].includes(error.message);
      if (rejected) { setPending(draftKey, null); showError(); }
      else setSendFailed(true);
    }
    finally { setBusy(false); }
  };
  const upload = async () => {
    if (!client || !connected || busy || pending) return;
    const result = await DocumentPicker.getDocumentAsync({ multiple: true, copyToCacheDirectory: true });
    if (result.canceled) return;
    for (const asset of result.assets) draftUploads.start(client, draftKey, { uri: asset.uri, name: asset.name, mimeType: asset.mimeType });
  };
  const removeAttachment = (id: string) => {
    setAttachments(draftKey, (files) => files.filter((file) => file.id !== id));
    if (client) draftUploads.remove(client, id);
  };
  const metadata = (body: Record<string, unknown>) => client!.command(session, { type: "update", ...body }, localV2SummarySchema).then(() => { void query.refetch(); void qc.invalidateQueries({ queryKey: aiKeys.list(scope) }); });
  const actions = () => {
    if (!snapshot) return;
    ActionSheetIOS.showActionSheetWithOptions({ options: [t("cancel"), t("rename"), t(snapshot.conversation.pinned ? "unpin" : "pin"), t("usage"), t(snapshot.conversation.archived ? "unarchive" : "archive")], cancelButtonIndex: 0 }, (index) => {
      if (index === 1) Alert.prompt(t("rename"), undefined, [{ text: t("cancel"), style: "cancel" }, { text: t("save"), onPress: (value?: string) => { if (value?.trim()) void metadata({ title: value.trim() }).catch(showError); } }], "plain-text", snapshot.conversation.title);
      if (index === 2) void metadata({ pinned: !snapshot.conversation.pinned }).catch(showError);
      if (index === 3) router.push({ pathname: "/[workspace]/ai-sessions/[device]/usage", params: { workspace, device, session, name } });
      if (index === 4) void metadata({ archived: !snapshot.conversation.archived }).catch(showError);
    });
  };
  const located = useRef("");
  useEffect(() => {
    if (!focused) { located.current = ""; return; }
    if (!turn || located.current === turn || !active) return;
    const index = allRows.findIndex((row) => row.turnId === turn);
    if (index >= 0) { located.current = turn; nearBottom.current = false; list.current?.scrollToIndex({ index, animated: true }); }
    else if (hasOlder && !history.isFetching && connected) void history.fetchNextPage();
  }, [turn, allRows, focused, active, hasOlder, history, connected]);
  const sideRequest = useRef<{ key: string; requestId: string } | null>(null);
  const createSide = async (quote: string, turnId?: string) => {
    if (!client || !connected || busy || !quote.trim()) return;
    const boundary = JSON.stringify([session, turnId, quote]);
    if (sideRequest.current?.key !== boundary) sideRequest.current = { key: boundary, requestId: createRequestId() };
    setBusy(true);
    try {
      const created = await client.command(session, { type: "create_side", quote, turn_id: turnId, request_id: sideRequest.current.requestId }, localV2SummarySchema);
      sideRequest.current = null;
      void qc.invalidateQueries({ queryKey: aiKeys.list(scope) });
      router.push({ pathname: "/[workspace]/ai-sessions/[device]/side", params: { workspace, device, session, sideId: created.conversation_id, tab: "side", name } });
    } catch { showError(); } finally { setBusy(false); }
  };
  const resource = (value: string): boolean => {
    if (/^mc:\/\/file\//.test(value)) { if (client) void openDeviceFile(client, `/attachments/${encodeURIComponent(value.slice(10))}`).catch(showError); return true; }
    if (/^https?:/i.test(value)) {
      const url = remoteWebURL(value);
      if (!url) Alert.alert(t("web"), t("local_web"));
      else router.push({ pathname: "/[workspace]/ai-sessions/[device]/side", params: { workspace, device, session, tab: "web", url, name } });
      return true;
    }
    if (/^(\/|[A-Za-z]:[\\/]|sandbox:)/.test(value)) {
      router.push({ pathname: "/[workspace]/ai-sessions/[device]/side", params: { workspace, device, session, tab: "files", path: value.replace(/^sandbox:/, ""), name } });
      return true;
    }
    return false;
  };
  const params = { workspace, device, session };
  return <SafeAreaView edges={embedded ? [] : ["bottom"]} className="flex-1 bg-background">
    {!embedded && <Stack.Screen options={{ headerShown: false, title: snapshot?.conversation.title || t("new") }} />}
    {!embedded && <Header title={snapshot?.conversation.title || t("new")} left={<IconButton name="chevron-back" accessibilityLabel={t("sessions")} onPress={() => router.back()} />} right={<View className="flex-row">
      <IconButton name="folder-open-outline" accessibilityLabel={t("select_project")} disabled={!connected || !snapshot || snapshot.conversation.turn_count > 0} onPress={() => router.push({ pathname: "/[workspace]/ai-sessions/[device]/folders", params })} />
      <IconButton name="document-text-outline" accessibilityLabel={t("files")} onPress={() => router.push({ pathname: "/[workspace]/ai-sessions/[device]/side", params: { ...params, name } })} />
      <IconButton name="ellipsis-horizontal" accessibilityLabel={t("sessions")} disabled={!connected} onPress={actions} />
    </View>} />}
    {!embedded && <View className="items-center py-1"><Text className="text-xs text-muted-foreground">{name || t("device_name")} · {t(connected ? "online" : "offline")}</Text></View>}
    {snapshot?.conversation.side_source_id && <Pressable className="mx-4 my-2 p-3 rounded-lg bg-secondary" accessibilityRole="button" onPress={() => onOpenSource ? onOpenSource() : router.push({ pathname: "/[workspace]/ai-sessions/[device]/[session]", params: { workspace, device, session: snapshot.conversation.side_source_id!, turn: snapshot.conversation.side_source_turn_id, name } })}><Text className="text-xs text-muted-foreground">{t("source")} ›</Text><Text numberOfLines={3}>{snapshot.conversation.side_quote}</Text></Pressable>}
    <ConnectionNotice connected={connected} subscriptionRejected={subscriptionRejected} onRetrySubscription={() => client?.retrySubscription(session)} />
    <KeyboardAvoidingView behavior={Platform.OS === "ios" ? "padding" : undefined} className="flex-1">
      {query.isPending ? <ActivityIndicator className="mt-8" /> : <FlashList ref={list} data={allRows} keyExtractor={(item) => item.id}
        maintainVisibleContentPosition={{ startRenderingFromBottom: true }}
        onScroll={({ nativeEvent }) => { nearBottom.current = nativeEvent.contentSize.height - nativeEvent.contentOffset.y - nativeEvent.layoutMeasurement.height < 100; setReadingHistory(!nearBottom.current); }}
        contentContainerStyle={{ padding: 16 }} keyboardShouldPersistTaps="handled"
        ListHeaderComponent={hasOlder || history.isError ? <Button variant="ghost" disabled={!connected || history.isFetchingNextPage} onPress={() => { nearBottom.current = false; void history.fetchNextPage(); }}><Text>{t(history.isError ? "retry" : "load_more")}</Text>{history.isFetchingNextPage && <ActivityIndicator />}</Button> : null}
        ListEmptyComponent={<View className="items-center justify-center py-20 gap-4"><MulticaLogo size={36} /><Text className="text-muted-foreground">{t("empty_conversation")}</Text>{query.isError && <Button onPress={() => void query.refetch()}><Text>{t("retry")}</Text></Button>}</View>}
        renderItem={({ item }) => item.role === "user" ? <View className="self-end max-w-[90%] rounded-2xl bg-secondary px-4 py-3 mb-5"><Text>{item.content}</Text>{item.attachmentIds?.map((id) => <SessionAttachment key={id} id={id} />)}</View>
          : item.role === "assistant" ? <View className="mb-5 gap-2"><Text className="text-xs text-muted-foreground">{snapshot?.conversation.provider === "claude" ? "Claude Code" : "Codex"}</Text><SessionMarkdown content={item.content} session={session} onResourcePress={resource} selectionAction={{ label: t("side_continue"), onSelect: (quote) => { void createSide(quote, item.turnId); } }} /></View>
          : item.role === "reasoning" ? <View className="mb-3 gap-1"><Pressable accessibilityRole="button" disabled={item.running} accessibilityState={item.running ? undefined : { expanded: expanded.has(item.id) }} className="flex-row items-center gap-2" onPress={() => setExpanded((old) => { const next = new Set(old); if (next.has(item.id)) next.delete(item.id); else next.add(item.id); return next; })}>{item.running && <ActivityIndicator size="small" />}<Text className="text-xs text-muted-foreground">{item.running ? t("thinking") : `${expanded.has(item.id) ? "▾" : "▸"} ${t("thought")}`}</Text></Pressable>{!!item.content && (item.running || expanded.has(item.id)) && <Text selectable className="text-xs text-muted-foreground">{item.content}</Text>}</View>
          : <View className="mb-3 rounded-lg bg-secondary p-3"><Pressable accessibilityRole="button" accessibilityState={{ expanded: expanded.has(item.id) }} onPress={() => setExpanded((old) => { const next = new Set(old); if (next.has(item.id)) next.delete(item.id); else next.add(item.id); return next; })}><Text className="text-xs text-muted-foreground">{expanded.has(item.id) ? "▾" : "▸"} {t("view_activity")}</Text></Pressable>{expanded.has(item.id) && <Text selectable className="text-xs mt-2">{item.content}</Text>}</View>}
        ListFooterComponent={<>{snapshot?.pending_interactions.map((interaction) => <SessionQuestions key={interaction.interaction_id} interaction={interaction} disabled={!connected || busy} onAnswer={async (answer) => { await client!.command(session, { type: "answer", interaction_id: interaction.interaction_id, answer }, commandResultSchema); void query.refetch(); }} />)}{running && <View className="flex-row items-center gap-2 py-3"><ActivityIndicator size="small" /><Text className="text-sm text-muted-foreground flex-1">{t("working")}</Text><IconButton name="stop" accessibilityLabel={t("stop")} disabled={!connected || busy} onPress={() => { void client!.command(session, { type: "interrupt" }, commandResultSchema).then(() => query.refetch()).catch(showError); }} /></View>}</>}
      />}
      {readingHistory && <Button variant="ghost" onPress={() => { nearBottom.current = true; setReadingHistory(false); list.current?.scrollToEnd({ animated: true }); }}><Text>↓ {t("latest")}</Text></Button>}
      {!!snapshot && Math.max(snapshot.queue.items.length, snapshot.conversation.queue_count) > 0 && <Button variant="outline" className="mx-3 mb-2" onPress={() => router.push({ pathname: "/[workspace]/ai-sessions/[device]/queue", params })}><Text>{t(snapshot.queue.paused ? "queued_paused" : "queued_count", { count: Math.max(snapshot.queue.items.length, snapshot.conversation.queue_count) })} ›</Text></Button>}
      {sendFailed && pending && <View className="px-4 py-2"><Text accessibilityRole="alert" className="text-sm text-destructive">{t("send_failed")}</Text></View>}
      <View className="mx-3 mb-2 p-3 rounded-xl border border-border bg-background gap-2">
        {!!attachments.length && <View className="gap-1">{attachments.map((file) => <Button key={file.id} variant="ghost" size="sm" disabled={!!pending} onPress={() => removeAttachment(file.id)}>{file.status === "uploading" && <ActivityIndicator />}<Text numberOfLines={1} className={file.status === "failed" ? "text-destructive" : undefined}>{file.name} ×</Text></Button>)}{attachments.some((file) => file.status === "failed") && <Text accessibilityRole="alert" className="text-sm text-destructive">{t("attachment_failed")}</Text>}</View>}
        <TextInput multiline value={draft} onChangeText={(value) => setDraft(draftKey, value)} editable={!busy && !pending && !snapshot?.conversation.archived} placeholder={t(rows.length ? "continue" : "message")} placeholderTextColor={theme.mutedForeground} accessibilityLabel={t("message")} style={{ color: theme.foreground, fontSize: 16, minHeight: 48, maxHeight: 160, padding: 0 }} />
        <View className="flex-row items-center gap-1">
          <IconButton name="add" accessibilityLabel={t("attachment")} disabled={!connected || !!pending} onPress={() => void upload().catch(showError)} />
          <Button variant="ghost" size="sm" disabled={!connected || running || busy} onPress={() => router.push({ pathname: "/[workspace]/ai-sessions/[device]/models", params: { ...params, provider: snapshot?.conversation.provider } })}><Text className="max-w-24" numberOfLines={1}>{snapshot?.settings.model || t("default_model")}⌄</Text></Button>
          <Button variant="ghost" size="sm" disabled={!connected || running || busy || uploading || !!pending} accessibilityLabel={t("provider")} onPress={() => void chooseProvider().catch(showError)}><Text className="text-xs" numberOfLines={1}>{snapshot?.conversation.provider === "claude" ? "Claude Code" : "Codex"}⌄</Text></Button>
          <View className="flex-1" />
          {busy ? <ActivityIndicator /> : <Button size={running ? "sm" : "icon"} disabled={!connected || !snapshot || !(draft.trim() || attachments.length || pending) || snapshot.conversation.archived} accessibilityLabel={t(sendFailed ? "retry" : "send")} onPress={() => void send()}><Text>{running ? t("enqueue") : "↑"}</Text></Button>}
        </View>
      </View>
    </KeyboardAvoidingView>
  </SafeAreaView>;
}
