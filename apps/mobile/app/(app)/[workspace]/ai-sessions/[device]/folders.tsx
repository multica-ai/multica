import { useState } from "react";
import { ActivityIndicator, Alert, FlatList, Pressable } from "react-native";
import { Ionicons } from "@expo/vector-icons";
import { router, useLocalSearchParams, useTheme } from "expo-router";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { z } from "zod";
import { localV2SummarySchema } from "@multica/core/local-conversation/v2";
import { useAIConnection, useAISessionScope } from "@/data/ai-sessions/hooks";
import { aiKeys } from "@/data/ai-sessions/client";
import { directoriesSchema } from "@/data/ai-sessions/schemas";
import { Text } from "@/components/ui/text";
import { Button } from "@/components/ui/button";
import { useT } from "@/lib/i18n";
import { AISheet } from "@/components/ai-sessions/sheet";

export default function AIFolders() {
  const { t } = useT("aiSessions");
  const { colors } = useTheme();
  const { session } = useLocalSearchParams<{ session?: string }>();
  const { client, connected } = useAIConnection();
  const scope = useAISessionScope();
  const qc = useQueryClient();
  const [path, setPath] = useState("");
  const query = useQuery({ queryKey: [...aiKeys.scope(scope), "directories", path], enabled: !!client && connected,
    queryFn: ({ signal }) => client!.request("GET", `/directories?${new URLSearchParams({ path })}`, directoriesSchema, undefined, signal) });
  const apply = useMutation({ mutationFn: async () => {
    if (!query.data || !client || !connected) throw new Error("device_unreachable");
    if (session) return client.command(session, { type: "set_directory", cwd: query.data.path }, localV2SummarySchema);
    return client.request("POST", "/folder-projects", z.object({ id: z.string(), title: z.string(), path: z.string() }), { path: query.data.path });
  }, onSuccess: () => { void qc.invalidateQueries({ queryKey: aiKeys.scope(scope) }); router.back(); }, onError: () => Alert.alert(t("error_title"), t("operation_failed")) });
  return <AISheet title={t("choose_folder")}>
    <Text style={{ lineHeight: undefined }} className="text-xs text-muted-foreground" selectable>{query.data?.path}</Text>
    {!!query.data?.parent && <Button style={{ height: "auto", minHeight: 40 }} hitSlop={2} variant="ghost" onPress={() => setPath(query.data!.parent)}><Ionicons name="arrow-up" size={16} color={colors.text} /><Text style={{ lineHeight: undefined }}>{t("parent")}</Text></Button>}
    {query.isPending && <ActivityIndicator />}
    {query.isError && <Button style={{ height: "auto", minHeight: 40 }} hitSlop={2} variant="outline" onPress={() => void query.refetch()}><Text style={{ lineHeight: undefined }}>{t("retry")}</Text></Button>}
    <FlatList data={query.data?.entries} keyExtractor={(entry) => entry.path} ListEmptyComponent={query.isSuccess ? <Text style={{ lineHeight: undefined }} className="py-6 text-center text-muted-foreground">{t("empty_folders")}</Text> : null} renderItem={({ item }) => <Pressable accessibilityRole="button" className="min-h-12 py-3 flex-row items-center gap-3 border-b border-border active:bg-secondary" onPress={() => setPath(item.path)}><Text style={{ lineHeight: undefined }} className="flex-1" numberOfLines={2}>{item.name}</Text><Ionicons name="chevron-forward" size={16} color={colors.text} /></Pressable>} />
    <Button style={{ height: "auto", minHeight: 40 }} hitSlop={2} disabled={!connected || !query.data || apply.isPending} onPress={() => apply.mutate()}><Text style={{ lineHeight: undefined }}>{t("use_folder")}</Text></Button>
  </AISheet>;
}
