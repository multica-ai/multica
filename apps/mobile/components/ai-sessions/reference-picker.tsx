import { useEffect, useMemo, useState } from "react";
import { ActivityIndicator, FlatList, Pressable, ScrollView, useWindowDimensions, View } from "react-native";
import { KeyboardStickyView } from "react-native-keyboard-controller";
import { Ionicons } from "@expo/vector-icons";
import { router, useLocalSearchParams } from "expo-router";
import { useInfiniteQuery, useQueryClient } from "@tanstack/react-query";
import { SafeAreaView, useSafeAreaInsets } from "react-native-safe-area-context";
import { Text } from "@/components/ui/text";
import { Button } from "@/components/ui/button";
import { TextField } from "@/components/ui/text-field";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { ActorAvatar } from "@/components/ui/actor-avatar";
import { useT } from "@/lib/i18n";
import { useColorScheme } from "@/lib/use-color-scheme";
import { THEME } from "@/lib/theme";
import { cn } from "@/lib/utils";
import { useAIConnection, useAISessionScope } from "@/data/ai-sessions/hooks";
import { aiKeys } from "@/data/ai-sessions/client";
import { aiDraftKey, useAIDrafts } from "@/data/ai-sessions/drafts";
import { EMPTY_REFERENCES, MAX_REFERENCES, REFERENCE_KINDS, referenceKey, toggleReference, type ReferenceKind } from "@/data/ai-sessions/references";
import { referencePageOptions, type ReferencePage } from "@/data/ai-sessions/reference-queries";

export function AIReferencePicker() {
  const { session = "", name, query: initialQuery = "", tokenStart, tokenEnd } = useLocalSearchParams<{ session: string; name?: string; query?: string; tokenStart?: string; tokenEnd?: string }>();
  const scope = useAISessionScope();
  const key = aiDraftKey(scope, session);
  const { client, connected } = useAIConnection();
  const qc = useQueryClient();
  const { t } = useT("aiSessions");
  const { fontScale } = useWindowDimensions();
  const insets = useSafeAreaInsets();
  const { colorScheme } = useColorScheme();
  const theme = THEME[colorScheme];
  const [kind, setKind] = useState<ReferenceKind>("session");
  const [search, setSearch] = useState(initialQuery);
  const [query, setQuery] = useState(initialQuery.trim());
  const [selected, setSelected] = useState(() => useAIDrafts.getState().references[key] ?? EMPTY_REFERENCES);
  const locked = useAIDrafts((state) => !!state.pending[key]);
  useEffect(() => { const timer = setTimeout(() => setQuery(search.trim()), 200); return () => clearTimeout(timer); }, [search]);
  const options = client ? referencePageOptions(client, qc, session, kind, query) : {
    queryKey: [...aiKeys.scope(scope), "references"], initialPageParam: 0,
    queryFn: async (): Promise<ReferencePage> => { throw new Error("device_unreachable"); },
    getNextPageParam: (page: ReferencePage) => page.nextOffset,
  };
  const result = useInfiniteQuery({ ...options, enabled: !!client && !!scope.workspaceId && !!session && connected, staleTime: 30_000 });
  const items = useMemo(() => [...new Map((result.data?.pages.flatMap((page) => page.items) ?? []).map((item) => [referenceKey(item), item])).values()], [result.data]);
  const searching = query !== search.trim();
  const done = () => {
    const state = useAIDrafts.getState();
    if (!session || !scope.workspaceId || state.pending[key]) return;
    state.setReferences(key, selected.map(({ kind, id, name }) => ({ kind, id, name })));
    // Cancel/swipe leaves the unfinished token intact; Done consumes it only
    // if the original text still occupies the same cursor range.
    const start = Number(tokenStart), end = Number(tokenEnd), draft = state.drafts[key] ?? "";
    if (tokenStart !== undefined && tokenEnd !== undefined && Number.isInteger(start) && start >= 0 && end > start && draft.slice(start, end) === `@${initialQuery}`) state.setDraft(key, draft.slice(0, start) + draft.slice(end));
    router.back();
  };
  return <SafeAreaView edges={["bottom"]} className="flex-1 bg-background">
    <View className="flex-row items-center justify-between px-2 pt-3 pb-2">
      <Button style={{ height: "auto", minHeight: 36 }} variant="ghost" size="sm" hitSlop={4} onPress={() => router.back()}><Text style={{ lineHeight: undefined }}>{t("cancel")}</Text></Button>
      {fontScale <= 1.4 && <Text style={{ lineHeight: undefined }} accessibilityRole="header" className="text-base font-semibold">{t("references.title")}</Text>}
      <Button style={{ height: "auto", minHeight: 36 }} variant="ghost" size="sm" hitSlop={4} disabled={locked || !session || !scope.workspaceId} onPress={done}><Text style={{ lineHeight: undefined }}>{t("references.done", { count: selected.length })}</Text></Button>
    </View>
    {fontScale > 1.4 && <Text style={{ lineHeight: undefined }} accessibilityRole="header" className="px-4 pb-2 text-base font-semibold">{t("references.title")}</Text>}
    <View className="px-4 gap-3 pb-2">
      <TextField className="h-auto min-h-10 py-2" value={search} onChangeText={setSearch} placeholder={t("references.search")} accessibilityLabel={t("references.search")} autoCapitalize="none" autoCorrect={false} clearButtonMode="while-editing" />
      <Tabs value={kind} onValueChange={(value) => { const next = REFERENCE_KINDS.find((kind) => kind === value); if (next) setKind(next); }}><ScrollView horizontal className="grow-0" contentContainerStyle={{ flexGrow: 1 }} showsHorizontalScrollIndicator={false}><TabsList className="min-w-full mr-0 h-auto min-h-9">{REFERENCE_KINDS.map((value) => <TabsTrigger key={value} value={value} className="grow shrink-0 h-auto" hitSlop={{ top: 4, bottom: 4 }}><Text style={{ lineHeight: undefined }} numberOfLines={1}>{t(`references.${value}`)}</Text></TabsTrigger>)}</TabsList></ScrollView></Tabs>
      {!connected && <Text style={{ lineHeight: undefined }} accessibilityRole="alert" className="text-sm text-muted-foreground">{t("offline_help")}</Text>}
    </View>
    <FlatList key={kind + query} data={searching ? [] : items} keyExtractor={referenceKey} className="flex-1" keyboardShouldPersistTaps="handled" keyboardDismissMode="interactive" automaticallyAdjustKeyboardInsets contentInsetAdjustmentBehavior="automatic"
      ListHeaderComponent={<Text style={{ lineHeight: undefined }} className="px-4 py-2 text-xs text-muted-foreground">{t(kind === "session" ? "references.session_heading" : kind === "issue" ? "references.issue_heading" : "references.agent_heading", { device: name || t("device_name") })}</Text>}
      renderItem={({ item }) => {
        const checked = selected.some((entry) => referenceKey(entry) === referenceKey(item));
        const disabled = locked || (!checked && (!!item.unavailable || selected.length >= MAX_REFERENCES));
        const detail = item.kind === "agent" ? t(item.unavailable ? `references.${item.unavailable}` : "references.available") : item.detail;
        return <Pressable accessibilityRole="checkbox" accessibilityLabel={`${item.name}, ${detail}`} accessibilityState={{ checked, disabled }} disabled={disabled} onPress={() => setSelected((current) => toggleReference(current, item))} className={cn("min-h-16 flex-row items-center gap-3 px-4 py-3 active:bg-secondary", disabled && "opacity-50")}>
          {item.kind === "agent" && <View accessibilityElementsHidden importantForAccessibility="no-hide-descendants"><ActorAvatar type="agent" id={item.id} name={item.name} size={36} /></View>}
          <View className="flex-1 gap-1"><Text style={{ lineHeight: undefined }} className="text-base font-medium" numberOfLines={1}>{item.name}</Text><Text style={{ lineHeight: undefined }} className="text-xs text-muted-foreground" numberOfLines={1}>{detail}</Text></View>
          <View className="w-5">{checked && <Ionicons name="checkmark" size={20} color={theme.primary} />}</View>
        </Pressable>;
      }}
      ListEmptyComponent={searching || (result.isPending && connected) ? <ActivityIndicator className="py-6" /> : !result.isError && connected ? <Text style={{ lineHeight: undefined }} className="p-6 text-center text-muted-foreground">{t("references.empty")}</Text> : null}
      ListFooterComponent={<View className="px-4 py-2">
        {result.isError ? <><Text style={{ lineHeight: undefined }} accessibilityRole="alert" className="text-sm text-destructive">{t("references.load_failed")}</Text><Button style={{ height: "auto", minHeight: 40 }} hitSlop={2} variant="ghost" disabled={!connected || result.isFetching} onPress={() => void (result.isFetchNextPageError ? result.fetchNextPage() : result.refetch())}><Text style={{ lineHeight: undefined }}>{t("retry")}</Text></Button></>
          : !searching && result.hasNextPage ? <Button style={{ height: "auto", minHeight: 40 }} hitSlop={2} variant="ghost" disabled={!connected || result.isFetching} onPress={() => void result.fetchNextPage()}>{result.isFetchingNextPage ? <ActivityIndicator /> : null}<Text style={{ lineHeight: undefined }}>{t(result.isFetchingNextPage ? "references.loading" : "references.load_more")}</Text></Button>
          : items.length > 0 && !result.isPending && !searching ? <Text style={{ lineHeight: undefined }} className="py-3 text-center text-xs text-muted-foreground">{t("references.loaded")}</Text> : null}
      </View>}
    />
    <KeyboardStickyView offset={{ closed: 0, opened: insets.bottom }}><View className="bg-background px-4 py-3"><Text style={{ lineHeight: undefined }} accessibilityLiveRegion="polite" className="text-center text-xs text-muted-foreground">{t(selected.length === MAX_REFERENCES ? "references.limit" : "references.count", { count: selected.length, max: MAX_REFERENCES })}</Text></View></KeyboardStickyView>
  </SafeAreaView>;
}
