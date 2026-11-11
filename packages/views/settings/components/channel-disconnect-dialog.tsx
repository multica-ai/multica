"use client";

import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { api } from "@multica/core/api";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@multica/ui/components/ui/alert-dialog";
import type { ChannelConnection } from "./use-messaging-channels";
import { useT } from "../../i18n";

export function ChannelDisconnectDialog({
  connection,
  open,
  onOpenChange,
}: {
  connection: ChannelConnection;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const { t } = useT("settings");
  const qc = useQueryClient();
  const { channel, installation } = connection;
  const [disconnecting, setDisconnecting] = useState(false);
  const disconnect = async () => {
    if (disconnecting) return;
    setDisconnecting(true);
    try {
      const revoke = {
        lark: api.deleteLarkInstallation.bind(api),
        slack: api.deleteSlackInstallation.bind(api),
        dingtalk: api.deleteDingTalkInstallation.bind(api),
        wecom: api.deleteWecomInstallation.bind(api),
        telegram: api.deleteTelegramInstallation.bind(api),
      }[channel];
      await revoke(installation.workspace_id, installation.id);
      await qc.invalidateQueries({
        queryKey: [channel, installation.workspace_id],
      });
      onOpenChange(false);
    } catch (error) {
      toast.error(
        error instanceof Error
          ? error.message
          : t(($) => $.channels.disconnect_failed),
      );
    } finally {
      setDisconnecting(false);
    }
  };
  return (
    <AlertDialog
      open={open}
      onOpenChange={(next) => {
        if (!disconnecting) onOpenChange(next);
      }}
    >
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>
            {t(($) => $.channels.disconnect_title)}
          </AlertDialogTitle>
          <AlertDialogDescription>
            {t(($) => $.channels.disconnect_description, {
              platform: t(($) => $[channel].section_title),
            })}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel disabled={disconnecting}>
            {t(($) => $.lark.install_close)}
          </AlertDialogCancel>
          <AlertDialogAction disabled={disconnecting} onClick={disconnect}>
            {t(($) => $.channels.disconnect)}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
