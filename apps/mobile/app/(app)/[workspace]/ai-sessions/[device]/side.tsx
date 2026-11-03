import { invalidateAISessionLists } from "@/data/ai-sessions/list-queries";
import { useCallback, useEffect, useRef, useState } from "react";
import { ActivityIndicator, Alert, Keyboard, ScrollView, Share, View } from "react-native";
import { Ionicons } from "@expo/vector-icons";
import { SafeAreaView } from "react-native-safe-area-context";
import { router, Stack, useFocusEffect, useLocalSearchParams } from "expo-router";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import WebView from "react-native-webview";
import { localV2SummarySchema, type LocalV2Snapshot } from "@multica/core/local-conversation/v2";
import { z } from "zod";
import { aiKeys } from "@/data/ai-sessions/client";
import { aiDraftKey } from "@/data/ai-sessions/drafts";
import { useAIConnection, useAISessionScope } from "@/data/ai-sessions/hooks";
import { remoteWebURL } from "@/data/ai-sessions/files";
import { useAIViewPreferences } from "@/data/ai-sessions/preferences";
import { AISessionContent } from "@/components/ai-sessions/session-content";
import { AIFilesContent } from "@/components/ai-sessions/files-content";
import { Text } from "@/components/ui/text";
import { TextField } from "@/components/ui/text-field";
import { Button } from "@/components/ui/button";
import { IconButton } from "@/components/ui/icon-button";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { useT } from "@/lib/i18n";
import { createRequestId } from "@/lib/request-id";
import { useColorScheme } from "@/lib/use-color-scheme";
import { THEME } from "@/lib/theme";

const workspaceStateSchema = z.object({ workspace: z.object({ revision: z.number().int() }).nullable() });
const revisionSchema = z.object({ revision: z.number().int() });
export default function AISideWorkspace() {
  const { t } = useT("aiSessions");
  const { colorScheme } = useColorScheme();
  const theme = THEME[colorScheme];
  const { workspace, device, session, sideId, tab, url, path, name } = useLocalSearchParams<{ workspace: string; device: string; session: string; sideId?: string; tab?: string; url?: string; path?: string; name?: string }>();
  const scope = useAISessionScope();
  const { client, connected } = useAIConnection();
  const qc = useQueryClient();
  const key = aiDraftKey(scope, session);
  const layout = useAIViewPreferences((s) => s.layouts[key]);
  const setLayout = useAIViewPreferences((s) => s.setLayout);
  const selected = layout?.tab ?? "files";
  const address = layout?.url;
  const activeSide = layout?.sideId;
  const [input, setInput] = useState<string | null>(null);
  const entered = input ?? (url && remoteWebURL(url) ? url : address) ?? "";
  const [active, setActive] = useState(false);
  useFocusEffect(useCallback(() => { setActive(true); return () => setActive(false); }, []));
  useEffect(() => {
    if (tab === "files" || tab === "web" || tab === "side") setLayout(key, { tab });
    if (sideId) setLayout(key, { sideId });
    if (url && remoteWebURL(url)) setLayout(key, { url: remoteWebURL(url)! });
  }, [key, sideId, tab, url, setLayout]);
  const source = useQuery({ queryKey: aiKeys.snapshot(scope, session), enabled: !!client && connected, staleTime: Infinity, queryFn: ({ signal }) => client!.snapshot(session, signal) });
  const state = useQuery({ queryKey: [...aiKeys.scope(scope), "side-workspace", session], enabled: !!client && connected && active, queryFn: ({ signal }) => client!.request("GET", `/sessions/${encodeURIComponent(session)}/side-workspace`, workspaceStateSchema, undefined, signal) });
  const revision = useRef(0);
  const lastWorkspace = useRef<Record<string, unknown> | null>(null);
  const [location, setLocation] = useState<{ root: string; path: string; kind: "file" | "directory" } | null>(null);
  const onLocation = useCallback((root: string, path: string, kind: "file" | "directory") => setLocation((old) => old?.root === root && old.path === path && old.kind === kind ? old : { root, path, kind }), []);
  useEffect(() => {
    if (!client || !connected || !state.isSuccess || !source.data) return;
    const root = location?.root ?? source.data.conversation.cwd;
    const tabs: Record<string, unknown>[] = root ? [{ id: "ios-files", kind: "file", root, path: location?.path ?? root, fileKind: location?.kind ?? "directory" }] : [];
    if (address) tabs.push({ id: "ios-web", kind: "web", url: address });
    if (activeSide) tabs.push({ id: "ios-side", kind: "side", sessionId: activeSide });
    const id = `ios-${selected}`;
    const selectedTabId = tabs.some((entry) => entry.id === id) ? id : null;
    const timer = setTimeout(() => {
      revision.current = Math.max(Date.now(), revision.current + 1, (state.data.workspace?.revision ?? 0) + 1);
      lastWorkspace.current = { conversationId: session, revision: revision.current, panelOpen: active, visible: active, selectedTabId, tabs };
      void client.command(session, { type: "side_workspace_sync", workspace: lastWorkspace.current }, revisionSchema)
        .then((result) => { revision.current = Math.max(revision.current, result.revision); }).catch(() => {});
    }, 150);
    return () => clearTimeout(timer);
  }, [client, connected, session, source.data, state.isSuccess, state.data, selected, address, activeSide, active, location]);
  useEffect(() => () => {
    if (!client || !lastWorkspace.current) return;
    revision.current = Math.max(Date.now(), revision.current + 1);
    void client.command(session, { type: "side_workspace_sync", workspace: { ...lastWorkspace.current, revision: revision.current, visible: false, panelOpen: false } }, revisionSchema).catch(() => {});
  }, [client, session]);
  const web = useRef<WebView>(null);
  const [webLoading, setWebLoading] = useState(false);
  const [webError, setWebError] = useState(false);
  const [webNavigation, setWebNavigation] = useState({ canGoBack: false, canGoForward: false });
  const newSideRequest = useRef(createRequestId());
  const create = useMutation({ mutationFn: () => client!.command(session, { type: "create_side", request_id: newSideRequest.current }, localV2SummarySchema), onSuccess: (created) => { newSideRequest.current = createRequestId(); setLayout(key, { sideId: created.conversation_id, tab: "side" }); void invalidateAISessionLists(qc, scope); }, onError: () => Alert.alert(t("error_title"), t("operation_failed")) });
  const chooseSide = () => router.push({ pathname: "/[workspace]/ai-sessions/[device]/side-sessions", params: { workspace, device, session } });
  const openWeb = () => {
    const value = remoteWebURL(entered.includes("://") ? entered : `https://${entered}`);
    if (!value) { Alert.alert(t("web"), t("local_web")); return; }
    Keyboard.dismiss();
    setLayout(key, { url: value, tab: "web" });
  };
  return <SafeAreaView edges={["bottom"]} className="flex-1 bg-background">
    <Stack.Screen options={{ title: t("side"), headerBackTitle: t("sessions") }} />
    <Stack.Toolbar placement="left"><Stack.Toolbar.View hidesSharedBackground><IconButton name="chevron-back" hitSlop={4} accessibilityLabel={t("sessions")} onPress={() => router.back()} /></Stack.Toolbar.View></Stack.Toolbar>
    <Text style={{ lineHeight: undefined }} className="text-xs text-muted-foreground text-center py-1">{name || t("device_name")} · {t(connected ? "online" : "offline")}</Text>
    <View className="px-4 py-2"><Tabs value={selected} onValueChange={(value) => { if (value === "files" || value === "web" || value === "side") setLayout(key, { tab: value }); }}><ScrollView horizontal className="grow-0" contentContainerStyle={{ flexGrow: 1 }} showsHorizontalScrollIndicator={false}><TabsList className="min-w-full mr-0 h-auto min-h-9">{(["files", "web", "side"] as const).map((value) => <TabsTrigger key={value} value={value} className="grow shrink-0 h-auto" hitSlop={{ top: 4, bottom: 4 }}><Text style={{ lineHeight: undefined }} numberOfLines={1}>{t(value === "side" ? "side_chat" : value)}</Text></TabsTrigger>)}</TabsList></ScrollView></Tabs></View>
    <View className="flex-1" style={{ display: selected === "files" ? "flex" : "none" }}><AIFilesContent key={path ?? "root"} initialPath={path} onLocation={onLocation} /></View>
    <View className="flex-1" style={{ display: selected === "web" ? "flex" : "none" }}>
      <View className="flex-row items-center gap-2 px-3 py-2"><View className="flex-1"><TextField className="h-auto min-h-10 py-2" value={entered} onChangeText={setInput} autoCapitalize="none" autoCorrect={false} placeholder={t("web_address")} accessibilityLabel={t("web_address")} onSubmitEditing={openWeb} /></View><IconButton name="arrow-forward" accessibilityLabel={t("open_web")} onPress={openWeb} /></View>
      {webLoading && <ActivityIndicator accessibilityLabel={t("web_loading")} className="py-2" />}
      {webError && <View accessibilityRole="alert" className="px-4 py-3 gap-2"><Text style={{ lineHeight: undefined }} className="text-sm text-destructive">{t("web_failed")}</Text><Button style={{ height: "auto", minHeight: 40 }} hitSlop={2} variant="outline" onPress={() => web.current?.reload()}><Text style={{ lineHeight: undefined }}>{t("retry")}</Text></Button></View>}
      {address ? <WebView ref={web} style={{ flex: 1, backgroundColor: theme.background }} source={{ uri: address }}
        renderError={() => <View className="flex-1 bg-background" />}
        originWhitelist={["http://*", "https://*"]}
        onShouldStartLoadWithRequest={(request) => !!remoteWebURL(request.url)}
        onLoadStart={() => { setWebLoading(true); setWebError(false); }}
        onLoadEnd={() => setWebLoading(false)}
        onError={() => { setWebError(true); setWebLoading(false); }}
        onHttpError={() => { setWebError(true); setWebLoading(false); }}
        onContentProcessDidTerminate={() => { setWebError(true); setWebLoading(false); }}
        onNavigationStateChange={(state) => {
          setWebNavigation({ canGoBack: state.canGoBack, canGoForward: state.canGoForward });
          const safe = remoteWebURL(state.url);
          if (safe && safe !== address) { setLayout(key, { url: safe }); setInput(null); }
        }}
      /> : <View className="flex-1 items-center justify-center px-6"><Text style={{ lineHeight: undefined }} className="text-sm text-muted-foreground text-center">{t("web_empty")}</Text></View>}
      <View className="flex-row justify-between p-2 border-t border-border"><IconButton name="chevron-back" accessibilityLabel={t("web_back")} disabled={!webNavigation.canGoBack} onPress={() => web.current?.goBack()} /><IconButton name="chevron-forward" accessibilityLabel={t("web_forward")} disabled={!webNavigation.canGoForward} onPress={() => web.current?.goForward()} /><IconButton name="refresh" accessibilityLabel={t("retry")} disabled={!address || webLoading} onPress={() => web.current?.reload()} /><IconButton name="share-outline" accessibilityLabel={t("share")} disabled={!address} onPress={() => { if (address) void Share.share({ url: address }).catch(() => {}); }} /></View>
    </View>
    <View className="flex-1" style={{ display: selected === "side" ? "flex" : "none" }}>
      <Button style={{ height: "auto", minHeight: 40 }} hitSlop={2} variant="ghost" disabled={!connected || create.isPending} onPress={chooseSide}><Text style={{ lineHeight: undefined }}>{t("side_chat")}</Text><Ionicons name="chevron-down" size={14} color={theme.mutedForeground} /></Button>
      {activeSide ? <AISessionContent key={activeSide} sessionId={activeSide} embedded active={active && selected === "side"} onOpenSource={() => router.dismissTo({ pathname: "/[workspace]/ai-sessions/[device]/[session]", params: { workspace, device, session, name, turn: qc.getQueryData<LocalV2Snapshot>(aiKeys.snapshot(scope, activeSide))?.conversation.side_source_turn_id } })} /> : <Button style={{ height: "auto", minHeight: 40 }} hitSlop={2} variant="outline" className="m-4" disabled={!connected || create.isPending} onPress={() => create.mutate()}><Text style={{ lineHeight: undefined }}>{t("new")}</Text></Button>}
    </View>
  </SafeAreaView>;
}
