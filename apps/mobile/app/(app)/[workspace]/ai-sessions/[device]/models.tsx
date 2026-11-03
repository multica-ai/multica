import { useState } from "react";
import { ActivityIndicator, Alert, Pressable, ScrollView, View } from "react-native";
import { useLocalSearchParams } from "expo-router";
import { Ionicons } from "@expo/vector-icons";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useAIConnection, useAISessionScope } from "@/data/ai-sessions/hooks";
import { aiKeys } from "@/data/ai-sessions/client";
import { commandResultSchema, modelsSchema } from "@/data/ai-sessions/schemas";
import { Text } from "@/components/ui/text";
import { Button } from "@/components/ui/button";
import { useT } from "@/lib/i18n";
import { AISheet } from "@/components/ai-sessions/sheet";
import { useColorScheme } from "@/lib/use-color-scheme";
import { THEME } from "@/lib/theme";

export default function AIModels() {
  const { t } = useT("aiSessions");
  const { colorScheme } = useColorScheme();
  const theme = THEME[colorScheme];
  const { session } = useLocalSearchParams<{ session: string }>();
  const scope = useAISessionScope();
  const { client, connected } = useAIConnection();
  const qc = useQueryClient();
  const [page, setPage] = useState<"settings" | "model" | "reasoning" | "permissions">("settings");
  const snapshot = useQuery({ queryKey: aiKeys.snapshot(scope, session), enabled: !!client && connected, staleTime: Infinity, queryFn: ({ signal }) => client!.snapshot(session, signal) });
  const provider = snapshot.data?.conversation.provider;
  const query = useQuery({ queryKey: [...aiKeys.scope(scope), "models", provider], enabled: !!client && connected && !!provider,
    queryFn: ({ signal }) => client!.request("GET", `/models?${new URLSearchParams({ provider: provider! })}`, modelsSchema, undefined, signal) });
  const settings = snapshot.data?.settings;
  const selected = query.data?.models.find((model) => model.id === settings?.model);
  const levels = selected?.thinking?.supported_levels ?? [];
  const change = useMutation({ mutationFn: (patch: Record<string, unknown>) => client!.command(session, { type: "update_settings", ...patch }, commandResultSchema),
    onSuccess: async () => { await qc.invalidateQueries({ queryKey: aiKeys.snapshot(scope, session) }); setPage("settings"); },
    onError: () => Alert.alert(t("error_title"), t("operation_failed")),
  });
  const running = snapshot.data?.turns.some((turn) => turn.status === "running");
  const disabled = !connected || !settings || running || change.isPending;
  const permissions: { value: string; label: string; help: string }[] = [{ value: "default", label: t("permission_default"), help: t("permission_default_help") }, { value: "auto", label: t("permission_auto"), help: t("permission_auto_help") }, { value: "full-access", label: t("permission_full"), help: t("permission_full_help") }];
  const row = (label: string, value: string, onPress: () => void, checked?: boolean, help?: string) => <Pressable key={value} disabled={disabled} accessibilityRole={checked === undefined ? "button" : "radio"} accessibilityState={{ disabled, checked }} onPress={onPress} className="min-h-12 rounded-lg px-3 py-3 flex-row items-center gap-3 active:bg-secondary"><View className="flex-1 gap-1"><Text style={{ lineHeight: undefined }}>{label}</Text>{help && <Text style={{ lineHeight: undefined }} className="text-xs text-muted-foreground">{help}</Text>}</View>{checked === undefined ? <View className="max-w-[50%] flex-row items-center gap-2"><Text style={{ lineHeight: undefined }} className="shrink text-sm text-muted-foreground text-right" numberOfLines={2}>{value}</Text><Ionicons name="chevron-forward" size={16} color={theme.mutedForeground} /></View> : <View className="w-5">{checked && <Ionicons name="checkmark" size={20} color={theme.primary} />}</View>}</Pressable>;
  return <AISheet title={t(page === "settings" ? "settings" : page === "model" ? "model" : page === "reasoning" ? "reasoning" : "permissions")} onBack={page === "settings" ? undefined : () => setPage("settings")}>
    <Text style={{ lineHeight: undefined }} className="text-sm text-muted-foreground">{provider === "claude" ? "Claude Code" : "Codex"}</Text>
    {(query.isPending || snapshot.isPending || change.isPending) && <ActivityIndicator />}
    {(query.isError || snapshot.isError) && <Button style={{ height: "auto", minHeight: 40 }} hitSlop={2} variant="outline" onPress={() => { void query.refetch(); void snapshot.refetch(); }}><Text style={{ lineHeight: undefined }}>{t("retry")}</Text></Button>}
    <ScrollView className="-mx-2" contentContainerStyle={{ paddingBottom: 16 }}>
      {page === "settings" && <>{row(t("model"), selected?.label ?? settings?.model ?? t("default_model"), () => setPage("model"))}{row(t("reasoning"), levels.find((level) => level.value === settings?.reasoning_effort)?.label ?? settings?.reasoning_effort ?? t("model_default"), () => setPage("reasoning"))}{row(t("permissions"), permissions.find((p) => p.value === settings?.permission_mode)?.label ?? t("permission_default"), () => setPage("permissions"))}</>}
      {page === "model" && <>{row(t("default_model"), "default", () => change.mutate({ model: null, reasoning_effort: null }), !settings?.model)}{query.data?.models.map((model) => row(model.label, model.id, () => change.mutate({ model: model.id, reasoning_effort: null }), model.id === settings?.model))}</>}
      {page === "reasoning" && <>{row(t("model_default"), "default", () => change.mutate({ reasoning_effort: null }), !settings?.reasoning_effort)}{levels.map((level) => row(level.label, level.value, () => change.mutate({ reasoning_effort: level.value }), level.value === settings?.reasoning_effort))}</>}
      {page === "permissions" && permissions.map((p) => row(p.label, p.value, () => change.mutate({ permission_mode: p.value }), p.value === settings?.permission_mode, p.help))}
      {page === "reasoning" && !settings?.model && <Text style={{ lineHeight: undefined }} className="text-sm text-muted-foreground mt-4">{t("reasoning_choose_model")}</Text>}
      {running && <Text style={{ lineHeight: undefined }} className="text-sm text-muted-foreground mt-4">{t("settings_busy")}</Text>}
    </ScrollView>
  </AISheet>;
}
