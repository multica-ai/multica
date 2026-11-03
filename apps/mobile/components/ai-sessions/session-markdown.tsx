import { useMemo, useState } from "react";
import { ActionSheetIOS, Alert, Image, Modal, Pressable, View } from "react-native";
import { Markdown } from "@/lib/markdown";
import { splitMarkdown } from "@/lib/markdown/split-markdown";
import { Text } from "@/components/ui/text";
import { Button } from "@/components/ui/button";
import { useT } from "@/lib/i18n";
import { openDeviceFile } from "@/data/ai-sessions/files";
import { useAIConnection } from "@/data/ai-sessions/hooks";

/** Remote computer images keep their scope headers, including in the viewer. */
export function SessionImage({ path, session, label }: { path: string; session: string; label?: string }) {
  const { client } = useAIConnection();
  const { t } = useT("aiSessions");
  const [failed, setFailed] = useState(false);
  const [open, setOpen] = useState(false);
  const external = /^(https?:\/\/|data:image\/)/i.test(path);
  const attachment = /^mc:\/\/file\/([\w-]+)$/.exec(path);
  const route = attachment ? `/attachments/${encodeURIComponent(attachment[1])}` : `/sessions/${encodeURIComponent(session)}/image?${new URLSearchParams({ path: path.replace(/^sandbox:/, "") })}`;
  const source = external ? { uri: path } : client ? { uri: client.url(route), headers: client.headers() } : undefined;
  const openFile = (share = false) => { if (client) void openDeviceFile(client, route, "", share).catch(() => Alert.alert(t("error_title"), t("preview_error"))); };
  if (failed || !source) return <Button style={{ height: "auto", minHeight: 40 }} hitSlop={2} variant="outline" disabled={!client} onPress={() => openFile()}><Text style={{ lineHeight: undefined }} numberOfLines={1}>{label || t("open_file")}</Text></Button>;
  return <>
    <Pressable accessibilityRole="button" accessibilityLabel={label || t("open_file")} onPress={() => setOpen(true)} onLongPress={() => !external && ActionSheetIOS.showActionSheetWithOptions({ options: [t("cancel"), t("share")], cancelButtonIndex: 0 }, (index) => { if (index === 1) openFile(true); })}>
      <Image source={source} resizeMode="contain" style={{ width: "100%", height: 200 }} onError={() => setFailed(true)} />
    </Pressable>
    <Modal visible={open} animationType="fade" onRequestClose={() => setOpen(false)}>
      <View className="flex-1 bg-background pt-16 px-4 pb-8"><Button style={{ height: "auto", minHeight: 40 }} hitSlop={2} variant="ghost" onPress={() => setOpen(false)}><Text style={{ lineHeight: undefined }}>{t("cancel")}</Text></Button><Image source={source} resizeMode="contain" style={{ flex: 1, width: "100%" }} /></View>
    </Modal>
  </>;
}

export function SessionMarkdown({ content, session, onResourcePress, selectionAction }: { content: string; session: string; onResourcePress?: (url: string) => boolean; selectionAction?: { label: string; onSelect(text: string): void } }) {
  const segments = useMemo(() => splitMarkdown(content), [content]);
  return <View className="gap-3">{segments.map((segment, index) => segment.type === "image" ? <SessionImage key={`${index}:${segment.uri}`} session={session} path={segment.uri} label={segment.alt} />
    : <Markdown key={index} onResourcePress={onResourcePress} selectionAction={selectionAction} content={segment.type === "prose" ? segment.content : `\`\`\`${segment.lang ?? ""}\n${segment.code}\n\`\`\``} />)}</View>;
}
