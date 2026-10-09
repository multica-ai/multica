"use client";

import { useId, useState } from "react";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
} from "@multica/ui/components/ui/dialog";
import {
  Select,
  SelectTrigger,
  SelectValue,
  SelectContent,
  SelectItem,
} from "@multica/ui/components/ui/select";
import { useT } from "../../i18n";
import { QoderConfiguration } from "./qoder-configuration";

// Each managed provider owns its configuration and API integration.
const remoteRuntimeTypes = [
  {
    value: "qoder_cloud",
    label: "Qoder Cloud Agent",
    Configuration: QoderConfiguration,
  },
];

export function RemoteRuntimeDialog({
  onClose,
  configure = false,
  onAddAgent,
}: {
  onClose: () => void;
  configure?: boolean;
  onAddAgent?: () => void;
}) {
  const { t } = useT("runtimes");
  const labelId = useId();
  const [type, setType] = useState<string | null>(() =>
    remoteRuntimeTypes.length === 1
      ? (remoteRuntimeTypes[0]?.value ?? null)
      : null,
  );
  const provider = remoteRuntimeTypes.find((item) => item.value === type);
  const Configuration = provider?.Configuration;
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      <DialogContent className="max-h-[85vh] overflow-y-auto sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>
            {configure
              ? t(($) => $.qoder.credentials_title)
              : t(($) => $.remote_runtime.action)}
          </DialogTitle>
          <DialogDescription>
            {configure
              ? t(($) => $.qoder.credentials_description)
              : t(($) => $.remote_runtime.description)}
          </DialogDescription>
        </DialogHeader>
        {!configure && (
          <div className="space-y-2">
            <p id={labelId} className="text-caption font-medium">
              {t(($) => $.remote_runtime.type)}
            </p>
            <Select
              items={remoteRuntimeTypes.map(({ value, label }) => ({
                value,
                label,
              }))}
              value={type}
              onValueChange={setType}
            >
              <SelectTrigger className="w-full" aria-labelledby={labelId}>
                <SelectValue
                  placeholder={t(($) => $.remote_runtime.select_type)}
                />
              </SelectTrigger>
              <SelectContent alignItemWithTrigger={false} align="start">
                {remoteRuntimeTypes.map((item) => (
                  <SelectItem key={item.value} value={item.value}>
                    {item.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
        )}
        {Configuration && <Configuration key={type} onAddAgent={onAddAgent} />}
      </DialogContent>
    </Dialog>
  );
}
