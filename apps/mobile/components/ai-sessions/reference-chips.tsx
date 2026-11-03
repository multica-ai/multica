import { ScrollView, View } from "react-native";
import { Text } from "@/components/ui/text";
import { IconButton } from "@/components/ui/icon-button";
import { useT } from "@/lib/i18n";
import { cn } from "@/lib/utils";
import { readReferenceMessage, referenceKey, type AIReference } from "@/data/ai-sessions/references";

export function ReferenceChips({ references, onRemove, disabled = false }: { references: AIReference[]; onRemove?: (reference: AIReference) => void; disabled?: boolean }) {
  const { t } = useT("aiSessions");
  if (!references.length) return null;
  const chips = references.map((reference) => <View key={referenceKey(reference)} className={cn("max-w-full min-h-7 flex-row items-center rounded-full bg-primary/10 pl-2", onRemove ? "max-w-64" : "pr-2")}>
    <Text style={{ lineHeight: undefined }} className="shrink py-1 text-xs font-medium" numberOfLines={1}>{t(reference.kind === "agent" ? "references.invoke" : `references.${reference.kind}`)} · {reference.name}</Text>
    {onRemove && <IconButton name="close" iconSize={12} hitSlop={8} accessibilityLabel={t("references.remove", { name: reference.name })} disabled={disabled} className="h-7 w-7 rounded-full" onPress={() => onRemove(reference)} />}
  </View>);
  return onRemove ? <ScrollView horizontal keyboardShouldPersistTaps="handled" contentContainerStyle={{ gap: 6, paddingVertical: 8 }} showsHorizontalScrollIndicator>{chips}</ScrollView> : <View className="flex-row flex-wrap gap-1.5">{chips}</View>;
}

export function AIMessageContent({ content }: { content: string }) {
  const message = readReferenceMessage(content);
  return <View className="gap-2"><ReferenceChips references={message.references} />{!!message.text && <Text style={{ lineHeight: undefined }}>{message.text}</Text>}</View>;
}
