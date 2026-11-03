import { ActivityIndicator, Alert, Image, Pressable, View } from "react-native";
import { useQuery } from "@tanstack/react-query";
import { useAIConnection, useAISessionScope } from "@/data/ai-sessions/hooks";
import { aiKeys } from "@/data/ai-sessions/client";
import { openDeviceFile } from "@/data/ai-sessions/files";
import { attachmentMetadataSchema } from "@/data/ai-sessions/schemas";
import { Text } from "@/components/ui/text";
import { IconButton } from "@/components/ui/icon-button";
import { useT } from "@/lib/i18n";

export function SessionAttachment({ id }: { id: string }) {
  const { t } = useT("aiSessions");
  const { client, connected } = useAIConnection();
  const scope = useAISessionScope();
  const route = `/attachments/${encodeURIComponent(id)}`;
  const metadata = useQuery({ queryKey: [...aiKeys.scope(scope), "attachment", id], enabled: !!client && connected, queryFn: ({ signal }) => client!.request("GET", `${route}/metadata`, attachmentMetadataSchema, undefined, signal), staleTime: Infinity });
  const open = (share = false) => { if (client) void openDeviceFile(client, route, metadata.data?.name, share).catch(() => Alert.alert(t("error_title"), t("preview_error"))); };
  return <View className="gap-2 mt-2">
    {metadata.data?.mime_type.startsWith("image/") && <Pressable accessibilityRole="button" accessibilityLabel={metadata.data.name} disabled={!connected} onPress={() => open()}><Image source={{ uri: client?.url(route), headers: client?.headers() }} resizeMode="contain" style={{ width: "100%", maxWidth: 240, aspectRatio: 1.5 }} /></Pressable>}
    <View className="flex-row items-center border border-border rounded-lg px-2"><Pressable accessibilityRole="button" className="flex-1 p-2" disabled={!connected} onPress={() => metadata.isError ? void metadata.refetch() : open()}><Text style={{ lineHeight: undefined }} numberOfLines={2}>{metadata.data?.name ?? t(metadata.isError ? "retry" : "open_file")}</Text>{metadata.data && <Text style={{ lineHeight: undefined }} className="text-xs text-muted-foreground">{Math.ceil(metadata.data.size / 1024)} KB</Text>}{metadata.isFetching && <ActivityIndicator />}</Pressable><IconButton name="share-outline" accessibilityLabel={t("share")} disabled={!connected || !metadata.data} onPress={() => open(true)} /></View>
  </View>;
}
