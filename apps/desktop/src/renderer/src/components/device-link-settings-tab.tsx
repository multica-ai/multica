import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { z } from "zod";
import { parseWithFallback } from "@multica/core/api/schema";
import { Button } from "@multica/ui/components/ui/button";
import { Switch } from "@multica/ui/components/ui/switch";
import { AlertDialog, AlertDialogCancel, AlertDialogContent, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle } from "@multica/ui/components/ui/alert-dialog";
import { SettingsCard, SettingsRow, SettingsTab } from "@multica/views/settings";
import { useT } from "@multica/views/i18n";
import { localV2Transport } from "../platform/local-v2-transport";
import { ConversationPreheatSettings } from "./conversation-preheat-settings";
import { ServicePreviewSettings } from "./service-preview-settings";

const settingSchema = z.object({ available: z.boolean().default(false), enabled: z.boolean().default(false) });
type DeviceLinkSetting = z.infer<typeof settingSchema>;
const OFF: DeviceLinkSetting = { available: false, enabled: false };

export const deviceLinkSettingKey = ["device-link", "setting"] as const;

/** Whether this device can and does share its conversations. */
export function useDeviceLinkSetting() {
  const qc = useQueryClient();
  useEffect(() => {
    let previous: string | undefined;
    return window.daemonAPI?.onStatusChange((status) => {
      if (status.state === "running" && previous !== "running") {
        void qc.invalidateQueries({ queryKey: deviceLinkSettingKey });
      }
      previous = status.state;
    });
  }, [qc]);
  return useQuery({
    queryKey: deviceLinkSettingKey,
    queryFn: async () => parseWithFallback<DeviceLinkSetting>(await localV2Transport().deviceLink(), settingSchema, OFF, { endpoint: "local.device-link" }),
    retry: false,
  });
}

/** Settings → this device → Device link: let the account's other devices use this one. */
export function DeviceLinkSettingsTab() {
  const { t } = useT("settings");
  const qc = useQueryClient();
  const setting = useDeviceLinkSetting();
  const [confirming, setConfirming] = useState(false);
  const save = useMutation({
    mutationFn: async (enabled: boolean) => parseWithFallback<DeviceLinkSetting>(await localV2Transport().setDeviceLink(enabled), settingSchema, OFF, { endpoint: "local.device-link" }),
    onSuccess: (value) => { qc.setQueryData(deviceLinkSettingKey, value); setConfirming(false); },
  });
  const enabled = setting.data?.enabled ?? false;
  return (
    <SettingsTab title={t(($) => $.desktop.device_link.title)} scope="device">
      <SettingsCard>
        <SettingsRow label={t(($) => $.desktop.device_link.allow)} description={t(($) => $.desktop.device_link.allow_description)}>
          <Switch
            aria-label={t(($) => $.desktop.device_link.allow)}
            checked={enabled}
            disabled={!setting.data?.available || save.isPending}
            onCheckedChange={(checked) => {
              save.reset();
              // Turning it on lets other devices run agents here, so it is confirmed first.
              if (checked) setConfirming(true);
              else save.mutate(false);
            }}
          />
        </SettingsRow>
      </SettingsCard>
      <ConversationPreheatSettings />
      <ServicePreviewSettings />
      <AlertDialog open={confirming} onOpenChange={(open) => { if (!open && !save.isPending) setConfirming(false); }}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t(($) => $.desktop.device_link.confirm_title)}</AlertDialogTitle>
            <AlertDialogDescription>{t(($) => $.desktop.device_link.confirm_description)}</AlertDialogDescription>
          </AlertDialogHeader>
          {save.isError && <p role="alert" className="text-caption text-destructive">{t(($) => $.desktop.device_link.save_failed)}</p>}
          <AlertDialogFooter>
            <AlertDialogCancel disabled={save.isPending}>{t(($) => $.desktop.device_link.cancel)}</AlertDialogCancel>
            <Button disabled={save.isPending} aria-busy={save.isPending} onClick={() => save.mutate(true)}>{t(($) => $.desktop.device_link.confirm)}</Button>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </SettingsTab>
  );
}
