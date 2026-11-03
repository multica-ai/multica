import { useRef } from "react";
import { ActivityIndicator, ScrollView, View } from "react-native";
import { useLocalSearchParams } from "expo-router";
import { useQuery } from "@tanstack/react-query";
import { useAIConnection, useAISessionScope } from "@/data/ai-sessions/hooks";
import { aiKeys } from "@/data/ai-sessions/client";
import { remainingUsage, usageSchema } from "@/data/ai-sessions/schemas";
import { Text } from "@/components/ui/text";
import { Button } from "@/components/ui/button";
import { IconButton } from "@/components/ui/icon-button";
import { useT } from "@/lib/i18n";
import { AISheet } from "@/components/ai-sessions/sheet";

export default function AIUsage() {
  const { t } = useT("aiSessions");
  const { session, name } = useLocalSearchParams<{ session: string; name?: string }>();
  const { client, connected } = useAIConnection();
  const scope = useAISessionScope();
  const snapshot = useQuery({ queryKey: aiKeys.snapshot(scope, session), enabled: !!client && connected, queryFn: ({ signal }) => client!.snapshot(session, signal), staleTime: Infinity });
  const provider = snapshot.data?.conversation.provider;
  const force = useRef(false);
  const query = useQuery({ queryKey: [...aiKeys.scope(scope), "usage", session, provider], enabled: !!client && connected && !!provider,
    queryFn: ({ signal }) => { const refresh = force.current; force.current = false; return client!.request("GET", `/usage?${new URLSearchParams({ provider: provider!, conversation_id: session, force: String(refresh) })}`, usageSchema, undefined, signal); }, refetchInterval: connected ? 60_000 : false });
  const refresh = () => { force.current = true; void query.refetch(); };
  const data = query.data;
  const fmt = (value: string) => Number.isFinite(Date.parse(value)) ? new Date(value).toLocaleString() : t("awaiting_refresh");
  const context = data?.context;
  const percent = context?.used_tokens != null && context.limit_tokens ? Math.max(0, Math.min(100, context.used_tokens / context.limit_tokens * 100)) : null;
  return <AISheet title={t("usage")} trailing={<IconButton name="refresh" hitSlop={4} accessibilityLabel={t("refresh_usage")} disabled={!connected || !provider || query.isFetching} onPress={refresh} />}>
    <Text style={{ lineHeight: undefined }} className="text-sm text-muted-foreground">{provider === "claude" ? "Claude Code" : provider === "codex" ? "Codex" : ""} · {name || t("device_name")}</Text>
    <ScrollView contentContainerStyle={{ gap: 16, paddingBottom: 24 }}>
      <View className="flex-row"><Text style={{ lineHeight: undefined }} className="flex-1 font-semibold">{t("account_remaining")}</Text><Text style={{ lineHeight: undefined }} className="text-muted-foreground">{data?.account.plan}</Text></View>
      {query.isPending && connected && <ActivityIndicator />}
      {data?.account.windows.map((window) => { const remaining = remainingUsage(window.used_percent, window.resets_at); return <View key={window.id} className="gap-2">
        <View className="flex-row justify-between"><Text style={{ lineHeight: undefined }}>{window.label === "5h" ? t("five_hour") : window.label === "7d" ? t("weekly") : window.label}</Text><Text style={{ lineHeight: undefined }}>{remaining == null ? t("awaiting_refresh") : t("remaining", { count: Math.floor(remaining) })}</Text></View>
        {remaining != null && <Meter percent={remaining} />}
        {window.resets_at && <Text style={{ lineHeight: undefined }} className="text-xs text-muted-foreground">{t("resets", { time: fmt(window.resets_at) })}</Text>}
      </View>; })}
      {(query.isError || !connected || data?.account.status === "error") && <Text style={{ lineHeight: undefined }} accessibilityRole="alert" className="text-sm text-muted-foreground">{t(data?.account.windows.length ? "usage_stale" : "usage_error")}</Text>}
      {!query.isPending && !query.isError && connected && data?.account.status !== "error" && !data?.account.windows.length && <Text style={{ lineHeight: undefined }} className="text-muted-foreground">{t("usage_unavailable")}</Text>}
      {(query.isError || !connected || data?.account.status === "error") && <Button style={{ height: "auto", minHeight: 40 }} hitSlop={2} variant="outline" disabled={!connected || !provider || query.isFetching} onPress={refresh}><Text style={{ lineHeight: undefined }}>{t("retry")}</Text></Button>}
      <View className="pt-4 border-t border-border gap-2"><View className="flex-row justify-between"><Text style={{ lineHeight: undefined }}>{t("context_usage")}</Text>{percent != null && <Text style={{ lineHeight: undefined }}>{t("used_percent", { count: Math.round(percent) })}</Text>}</View>
        {context?.used_tokens != null ? <Text style={{ lineHeight: undefined }} className="text-xs text-muted-foreground">{context.used_tokens.toLocaleString()}{context.limit_tokens ? ` / ${context.limit_tokens.toLocaleString()}` : ""} tokens</Text> : <Text style={{ lineHeight: undefined }} className="text-muted-foreground">{t("context_unavailable")}</Text>}
      </View>
      {data?.account.updated_at && <Text style={{ lineHeight: undefined }} className="text-xs text-muted-foreground">{t("updated_at", { time: fmt(data.account.updated_at) })}</Text>}
      <Text style={{ lineHeight: undefined }} className="text-xs text-muted-foreground">{t("usage_shared")}</Text>
    </ScrollView>
  </AISheet>;
}

function Meter({ percent }: { percent: number }) { return <View className="h-1 rounded-full bg-secondary overflow-hidden" accessibilityRole="progressbar" accessibilityValue={{ min: 0, max: 100, now: percent }}><View className="h-full bg-foreground" style={{ width: `${percent}%` }} /></View>; }
