import { useEffect, useRef, useState } from "react";
import { ActivityIndicator, FlatList, Pressable, View } from "react-native";
import { router, useLocalSearchParams } from "expo-router";
import { useInfiniteQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { invalidateAISessionLists, sessionPageSummaries } from "@/data/ai-sessions/list-queries";
import { localV2SummarySchema } from "@multica/core/local-conversation/v2";
import { useAIConnection, useAISessionScope } from "@/data/ai-sessions/hooks";
import { aiKeys } from "@/data/ai-sessions/client";
import { aiDraftKey } from "@/data/ai-sessions/drafts";
import { useAIViewPreferences } from "@/data/ai-sessions/preferences";
import { AISheet } from "@/components/ai-sessions/sheet";
import { Button } from "@/components/ui/button";
import { Text } from "@/components/ui/text";
import { TextField } from "@/components/ui/text-field";
import { useT } from "@/lib/i18n";
import { createRequestId } from "@/lib/request-id";
import { aiSessionPreview } from "@/data/ai-sessions/preview";

export default function AISideSessions() {
  const { session } = useLocalSearchParams<{ session: string }>();
  const { t } = useT("aiSessions");
  const scope = useAISessionScope();
  const { client, connected } = useAIConnection();
  const qc = useQueryClient();
  const key = aiDraftKey(scope, session);
  const [search, setSearch] = useState("");
  const [debouncedSearch, setDebouncedSearch] = useState("");
  useEffect(() => { const timer = setTimeout(() => setDebouncedSearch(search.trim()), 200); return () => clearTimeout(timer); }, [search]);
  const selected = useAIViewPreferences((state) => state.layouts[key]?.sideId);
  const setLayout = useAIViewPreferences((state) => state.setLayout);
  const query = useInfiniteQuery({ queryKey: [...aiKeys.browse(scope), "side", session, debouncedSearch], enabled: !!client && connected, retry: false, initialPageParam: "",
    queryFn: ({ pageParam, signal }) => client!.sessionPage({ sourceId: session, search: debouncedSearch, cursor: pageParam, limit: 8 }, signal),
    getNextPageParam: (page) => page.next_cursor ?? undefined,
  });
  const select = (id: string) => { setLayout(key, { sideId: id, tab: "side" }); router.back(); };
  const requestId = useRef(createRequestId());
  const create = useMutation({ mutationFn: () => client!.command(session, { type: "create_side", request_id: requestId.current }, localV2SummarySchema),
    onSuccess: (created) => { void invalidateAISessionLists(qc, scope); select(created.conversation_id); },
  });
  const sessions = sessionPageSummaries(query.data?.pages ?? []);
  return <AISheet title={t("side_chat")}>
    <TextField className="h-auto min-h-10 py-2" value={search} onChangeText={setSearch} placeholder={t("search")} accessibilityLabel={t("search")} />
    <Button style={{ height: "auto", minHeight: 40 }} hitSlop={2} variant="outline" disabled={!connected || create.isPending} onPress={() => create.mutate()}>{create.isPending && <ActivityIndicator />}<Text style={{ lineHeight: undefined }}>{t("new")}</Text></Button>
    {create.isError && <Text style={{ lineHeight: undefined }} accessibilityRole="alert" className="text-sm text-destructive">{t("operation_failed")}</Text>}
    {query.isError && <Button style={{ height: "auto", minHeight: 40 }} hitSlop={2} variant="outline" disabled={!connected || query.isFetching} onPress={() => void (query.isFetchNextPageError ? query.fetchNextPage() : query.refetch())}><Text style={{ lineHeight: undefined }}>{t("retry")}</Text></Button>}
    <FlatList data={sessions} keyExtractor={(entry) => entry.conversation_id} keyboardShouldPersistTaps="handled"
      ListEmptyComponent={query.isPending && connected ? <ActivityIndicator className="py-6" /> : query.isSuccess ? <Text style={{ lineHeight: undefined }} className="py-6 text-center text-muted-foreground">{t(search.trim() ? "empty_search" : "empty_sessions")}</Text> : null}
      ListFooterComponent={query.isFetchingNextPage ? <ActivityIndicator className="py-3" /> : query.hasNextPage && !query.isError ? <Button style={{ height: "auto", minHeight: 40 }} hitSlop={2} variant="ghost" disabled={!connected || query.isFetching} onPress={() => void query.fetchNextPage()}><Text style={{ lineHeight: undefined }}>{t("load_more_sessions")}</Text></Button> : null}
      renderItem={({ item }) => <Pressable accessibilityRole="button" accessibilityState={{ selected: selected === item.conversation_id }} disabled={create.isPending} className="min-h-14 py-4 border-b border-border flex-row items-center gap-3 active:bg-secondary" onPress={() => select(item.conversation_id)}><View className="flex-1 gap-1"><Text style={{ lineHeight: undefined }} className="font-medium" numberOfLines={1}>{item.title}</Text><Text style={{ lineHeight: undefined }} className="text-xs text-muted-foreground" numberOfLines={1}>{aiSessionPreview(item.last_message_preview)}</Text></View>{selected === item.conversation_id && <Text style={{ lineHeight: undefined }}>✓</Text>}</Pressable>}
    />
  </AISheet>;
}
