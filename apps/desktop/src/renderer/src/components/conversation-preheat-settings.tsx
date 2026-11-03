import { useEffect } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { z } from "zod";
import { parseWithFallback } from "@multica/core/api/schema";
import { Switch } from "@multica/ui/components/ui/switch";
import { SettingsCard, SettingsRow } from "@multica/views/settings";
import { useT } from "@multica/views/i18n";
import { localV2Transport } from "../platform/local-v2-transport";

const settingSchema = z.object({ old_conversations: z.boolean().default(true) });
const key = ["conversation-preheat", "setting"] as const;

/** Settings → this device: whether opening a conversation may start its agent early. */
export function ConversationPreheatSettings() {
  const { t } = useT("settings");
  const qc = useQueryClient();
  useEffect(() => window.daemonAPI?.onStatusChange((status) => { if (status.state === "running") void qc.invalidateQueries({ queryKey: key }); }), [qc]);
  const setting = useQuery({
    queryKey: key,
    queryFn: async () => parseWithFallback<{ old_conversations: boolean } | null>(await localV2Transport().conversationPreheat(), settingSchema, null, { endpoint: "local.conversation-preheat" }),
    retry: false,
  });
  const save = useMutation({
    mutationFn: async (enabled: boolean) => {
      const value = parseWithFallback<{ old_conversations: boolean } | null>(await localV2Transport().setConversationPreheat(enabled), settingSchema, null, { endpoint: "local.conversation-preheat" });
      if (!value) throw new Error("Invalid conversation preheat response");
      return value;
    },
    onSuccess: (value) => { qc.setQueryData(key, value); },
  });
  const label = t(($) => $.desktop.conversation_preheat.label);
  return (
    <SettingsCard>
      <SettingsRow label={label} description={t(($) => $.desktop.conversation_preheat.description)}>
        <Switch aria-label={label} checked={setting.data?.old_conversations ?? false} disabled={!setting.data || save.isPending} onCheckedChange={(checked) => { save.reset(); save.mutate(checked); }} />
      </SettingsRow>
      {save.isError && <p role="alert" className="px-4 pb-3 text-caption text-destructive">{t(($) => $.desktop.conversation_preheat.failed)}</p>}
    </SettingsCard>
  );
}
