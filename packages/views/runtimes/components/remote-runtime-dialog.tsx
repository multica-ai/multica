"use client";

import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
} from "@multica/ui/components/ui/dialog";
import { useT } from "../../i18n";
import { QoderConfiguration } from "./qoder-configuration";

/**
 * Connects the workspace's Qoder Cloud Agent. An existing connection is
 * managed on its runtime page instead.
 */
export function RemoteRuntimeDialog({
  onClose,
  onConnected,
}: {
  onClose: () => void;
  /** Called after a first-time connection succeeds, before the dialog closes. */
  onConnected?: () => void;
}) {
  const { t } = useT("runtimes");
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      <DialogContent className="max-h-[85vh] overflow-y-auto sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>{t(($) => $.qoder.connect_title)}</DialogTitle>
          <DialogDescription>
            {t(($) => $.qoder.connect_description)}
          </DialogDescription>
        </DialogHeader>
        <QoderConfiguration
          onConnected={() => {
            onConnected?.();
            onClose();
          }}
        />
      </DialogContent>
    </Dialog>
  );
}
