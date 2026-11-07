import { useState } from "react";
import { ActionSheetIOS, ActivityIndicator, Alert, Pressable, SectionList, View } from "react-native";
import { Ionicons } from "@expo/vector-icons";
import { Stack, router, useLocalSearchParams } from "expo-router";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { localV2SummarySchema } from "@multica/core/local-conversation/v2";
import { Text } from "@/components/ui/text";
import { TextField } from "@/components/ui/text-field";
import { Button } from "@/components/ui/button";
import { IconButton } from "@/components/ui/icon-button";
import { ConnectionNotice } from "@/components/ai-sessions/connection-notice";
import { NewSessionMenu } from "@/components/ai-sessions/new-session-menu";
import { ProviderLogo } from "@/components/ai-sessions/provider-logo";
import { useAIConnection, useAISessionScope } from "@/data/ai-sessions/hooks";
import { aiKeys } from "@/data/ai-sessions/client";
import { providersSchema } from "@/data/ai-sessions/schemas";
import { useSessionList } from "@/data/ai-sessions/use-session-list";
import { invalidateAISessionLists } from "@/data/ai-sessions/list-queries";
import { useT } from "@/lib/i18n";
import { createRequestId } from "@/lib/request-id";
import { aiSessionPreview } from "@/data/ai-sessions/preview";
import { useColorScheme } from "@/lib/use-color-scheme";
import { THEME } from "@/lib/theme";

export default function AISessionList() {
  const { t } = useT("aiSessions");
  const { colorScheme } = useColorScheme();
  const { workspace, device, name } = useLocalSearchParams<{ workspace: string; device: string; name?: string }>();
  const { client, connected } = useAIConnection();
  const qc = useQueryClient();
  const [search, setSearch] = useState("");
  const [archived, setArchived] = useState(false);
  const scope = useAISessionScope();
  const [expandedModes, setExpandedModes] = useState<Record<string, Record<string, boolean>>>({});
  const mode = String(archived);
  const expanded = expandedModes[mode] ?? {};
  const toggle = (key: string) => setExpandedModes((state) => ({ ...state, [mode]: { ...state[mode], [key]: !state[mode]?.[key] } }));
  const base = aiKeys.scope(scope);
  const { catalog: sessions, sections, refresh, query } = useSessionList({ client, scope, connected, search, archived, expanded });
  const providers = useQuery({ queryKey: [...base, "providers"], enabled: !!client && connected, queryFn: ({ signal }) => client!.request("GET", "/providers", providersSchema, undefined, signal) });
  const create = useMutation({ mutationFn: async ({ provider, project }: { provider: string; project?: string }) => client!.request("POST", "/sessions", localV2SummarySchema, { id: `ios-${createRequestId()}`, provider, title: t("new"), folder_project_id: project }),
    onSuccess: (created) => {
      void invalidateAISessionLists(qc, client!.scope);
      router.push({ pathname: "/[workspace]/ai-sessions/[device]/[session]", params: { workspace, device, session: created.conversation_id, name } });
    }, onError: () => Alert.alert(t("error_title"), t("operation_failed")),
  });
  const listActions = () => {
    ActionSheetIOS.showActionSheetWithOptions({
      options: [t("cancel"), t("new_project"), t(archived ? "history" : "archived")],
      cancelButtonIndex: 0,
      disabledButtonIndices: connected ? [] : [1],
    }, (index) => {
      if (index === 1 && connected) router.push({ pathname: "/[workspace]/ai-sessions/[device]/folders", params: { workspace, device } });
      if (index === 2) setArchived((value) => !value);
    });
  };
  return <View className="flex-1 bg-background">
    <Stack.Screen options={{ title: archived ? t("archived") : name || t("sessions") }} />
    <Stack.Toolbar placement="left">
      <Stack.Toolbar.View hidesSharedBackground>
        <IconButton name="chevron-back" hitSlop={4} accessibilityLabel={t("title")} onPress={() => router.back()} />
      </Stack.Toolbar.View>
    </Stack.Toolbar>
    <Stack.Toolbar placement="right">
      <Stack.Toolbar.View hidesSharedBackground>
        <View className="flex-row">
          <IconButton name="ellipsis-horizontal" hitSlop={4} accessibilityLabel={t("more_actions")} onPress={listActions} />
          <NewSessionMenu providers={providers.data?.providers ?? []} disabled={!connected || create.isPending || providers.isPending} onSelect={(provider) => create.mutate({ provider })} />
        </View>
      </Stack.Toolbar.View>
    </Stack.Toolbar>
    <ConnectionNotice connected={connected} />
    {connected && client && !client.completionReadSupported && <Text className="px-4 py-2 text-caption text-muted-foreground">{t("unread_requires_update")}</Text>}
    <View className="px-4 py-3"><TextField className="h-auto min-h-10 py-2" clearButtonMode="while-editing" value={search} onChangeText={setSearch} placeholder={t("search")} accessibilityLabel={t("search")} /></View>
    {sessions.isError && <View className="px-4 pb-3 gap-2"><Text style={{ lineHeight: undefined }} accessibilityRole="alert" className="text-body text-destructive">{t("connection_failed")}</Text><Button style={{ height: "auto", minHeight: 40 }} hitSlop={2} variant="outline" disabled={!connected || sessions.isFetching} onPress={() => void sessions.refetch()}><Text style={{ lineHeight: undefined }}>{t("retry")}</Text></Button></View>}
    {sessions.isPending ? connected ? <ActivityIndicator /> : null : <SectionList keyboardShouldPersistTaps="handled" keyboardDismissMode="on-drag" automaticallyAdjustKeyboardInsets sections={sections} keyExtractor={(s) => s.conversation_id}
      refreshing={sessions.isRefetching} onRefresh={refresh} stickySectionHeadersEnabled={false}
      ListHeaderComponent={<Text className="px-4 pt-2 text-caption text-muted-foreground">{t(query ? "search_results" : "directories")}</Text>}
      ListFooterComponent={!sessions.isError && sections.length === 0 ? <Text style={{ lineHeight: undefined }} className="p-6 text-center text-muted-foreground">{t(search ? "empty_search" : "empty_sessions")}</Text> : null}
      renderSectionHeader={({ section }) => <View className={`flex-row items-center mx-4 mt-3 pl-3 min-h-14 border border-border bg-surface-1 ${section.collapsed ? "rounded-xl" : "rounded-t-xl"}`}>
        <Pressable className="flex-1 min-h-11 flex-row items-center gap-2 py-2" accessibilityRole={query ? "header" : "button"} accessibilityState={query ? undefined : { expanded: !section.collapsed }}
          accessibilityLabel={`${section.id ? section.title : t("unassigned")}${(client?.completionReadSupported && section.has_unread_completion) ? `, ${t("unread_group")}` : ""}`}
          onLongPress={() => Alert.alert(section.id ? section.title : t("unassigned"))}
          onPress={() => { if (!query) toggle(section.key); }}>
          {!query && <Ionicons accessible={false} name={section.collapsed ? "chevron-forward" : "chevron-down"} size={14} color={THEME[colorScheme].mutedForeground} />}
          <Ionicons accessible={false} name="folder-outline" size={18} color={THEME[colorScheme].mutedForeground} />
          <Text style={{ lineHeight: undefined }} className="flex-1 text-body font-medium" numberOfLines={1}>{section.id ? section.title : t("unassigned")}</Text>
          <View className="w-2 h-2 mr-3">{(client?.completionReadSupported && section.has_unread_completion) === true && <View className="w-2 h-2 rounded-full bg-brand" />}</View>
        </Pressable>
        <NewSessionMenu providers={providers.data?.providers ?? []} disabled={!connected || create.isPending || providers.isPending} accessibilityLabel={t("new_in_group", { name: section.id ? section.title : t("unassigned") })} onSelect={(provider) => create.mutate({ provider, project: section.id || undefined })} />
      </View>}
      renderSectionFooter={({ section }) => section.collapsed ? null : <View className="mx-4 px-3 pb-2 border-x border-b border-border rounded-b-xl bg-surface-1">
        {section.loading ? <ActivityIndicator className="py-3" /> : section.error ? <View className="gap-2"><Text style={{ lineHeight: undefined }} accessibilityRole="alert" className="text-body text-destructive">{t("connection_failed")}</Text><Button style={{ height: "auto", minHeight: 40 }} hitSlop={2} variant="ghost" disabled={!connected} onPress={section.retry}><Text style={{ lineHeight: undefined }}>{t("retry")}</Text></Button></View>
          : section.count === 0 ? <Text className="py-3 text-caption text-muted-foreground">{t("empty_sessions")}</Text> : section.hasMore ? <Button style={{ height: "auto", minHeight: 40 }} hitSlop={2} variant="ghost" disabled={!connected} onPress={section.loadMore}><Text style={{ lineHeight: undefined }}>{t("load_more_sessions")}</Text></Button> : null}
      </View>}
      renderItem={({ item }) => <Pressable className="mx-4 px-3 py-4 flex-row items-center gap-3 border-x border-b border-border bg-surface-1 active:bg-secondary" accessibilityRole="button" accessibilityLabel={`${item.title}${(client?.completionReadSupported && item.has_unread_completion) ? `, ${t("unread_result")}` : ""}`}
        onPress={() => router.push({ pathname: "/[workspace]/ai-sessions/[device]/[session]", params: { workspace, device, session: item.conversation_id, name } })}>
        <View accessibilityElementsHidden importantForAccessibility="no-hide-descendants" className="w-8 h-8 shrink-0 items-center justify-center"><ProviderLogo provider={item.provider} color={THEME[colorScheme].foreground} /></View>
        <View className="flex-1 min-w-0">
          <Text style={{ lineHeight: undefined }} className="font-medium text-title-sm" numberOfLines={1}>{item.pinned ? "📌 " : ""}{item.title}</Text><Text style={{ lineHeight: undefined }} className="text-caption text-muted-foreground mt-1" numberOfLines={1}>{item.provider === "claude" ? "Claude Code" : item.provider === "codex" ? "Codex" : item.provider} · {aiSessionPreview(item.last_message_preview)}</Text>
        </View>
        <View className="w-2 h-2">{(client?.completionReadSupported && item.has_unread_completion) === true && <View className="w-2 h-2 rounded-full bg-brand" />}</View>
      </Pressable>}
    />}
  </View>;
}
