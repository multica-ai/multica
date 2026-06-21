"use client";

import { useState } from "react";
import { Repeat } from "lucide-react";
import {
  Dialog,
  DialogContent,
} from "@multica/ui/components/ui/dialog";
import { Button } from "@multica/ui/components/ui/button";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { useT } from "../../i18n";

function toLocalDateTimeValue(date: Date) {
  const offsetMs = date.getTimezoneOffset() * 60_000;
  return new Date(date.getTime() - offsetMs).toISOString().slice(0, 16);
}

const PRESET_INTERVALS = [
  { value: "5", label: "5 min" },
  { value: "15", label: "15 min" },
  { value: "30", label: "30 min" },
  { value: "60", label: "1 hour" },
  { value: "360", label: "6 hours" },
  { value: "1440", label: "Daily" },
  { value: "custom", label: "Custom" },
] as const;

interface PollingSetupDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onConfirm: (pollIntervalMinutes: number, pollStartAt?: string | null) => void;
  defaultInterval?: number | null;
  defaultStartAt?: string | null;
}

export function PollingSetupDialog({
  open,
  onOpenChange,
  onConfirm,
  defaultInterval,
  defaultStartAt,
}: PollingSetupDialogProps) {
  const { t } = useT("issues");

  const defaultPreset = defaultInterval
    ? PRESET_INTERVALS.find((p) => p.value === String(defaultInterval))
      ? String(defaultInterval)
      : "custom"
    : "30";

  const [preset, setPreset] = useState<string>(defaultPreset);
  const [customMinutes, setCustomMinutes] = useState<string>(
    defaultInterval && !PRESET_INTERVALS.find((p) => p.value === String(defaultInterval))
      ? String(defaultInterval)
      : ""
  );
  const [startAt, setStartAt] = useState<string>(() => {
    if (defaultStartAt) {
      return toLocalDateTimeValue(new Date(defaultStartAt));
    }
    return "";
  });

  const getIntervalMinutes = (): number | null => {
    if (preset === "custom") {
      const val = parseInt(customMinutes, 10);
      return val > 0 ? val : null;
    }
    return parseInt(preset, 10);
  };

  const handleConfirm = () => {
    const minutes = getIntervalMinutes();
    if (minutes && minutes > 0) {
      const startAtISO = startAt ? new Date(startAt).toISOString() : null;
      onConfirm(minutes, startAtISO);
      onOpenChange(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="w-[calc(100vw-2rem)] !max-w-[420px] gap-0 overflow-hidden rounded-lg p-0">
        <div className="px-5 pb-4 pt-5">
          <div className="flex items-start gap-3">
            <div className="mt-0.5 flex size-10 shrink-0 items-center justify-center rounded-lg border bg-purple-500/10 text-purple-500">
              <Repeat className="size-4" />
            </div>
            <div className="min-w-0">
              <h2 className="text-base font-semibold">
                {t(($) => $.polling_setup.title)}
              </h2>
              <p className="mt-1 text-sm leading-5 text-muted-foreground">
                {t(($) => $.polling_setup.description)}
              </p>
            </div>
          </div>

          <div className="mt-4 grid gap-3">
            <div className="grid gap-1.5">
              <label className="text-sm font-medium">
                {t(($) => $.polling_setup.interval_label)}
              </label>
              <Select value={preset} onValueChange={(v) => v != null && setPreset(v)}>
                <SelectTrigger className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {PRESET_INTERVALS.map((opt) => (
                    <SelectItem key={opt.value} value={opt.value}>
                      {opt.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>

            {preset === "custom" && (
              <div className="grid gap-1.5">
                <label className="text-sm font-medium">
                  {t(($) => $.polling_setup.custom_label)}
                </label>
                <input
                  type="number"
                  min={1}
                  value={customMinutes}
                  onChange={(e) => setCustomMinutes(e.target.value)}
                  placeholder="30"
                  className="flex h-9 w-full rounded-md border border-input bg-transparent px-3 py-1 text-sm shadow-sm transition-colors placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
                />
              </div>
            )}

            <div className="grid gap-1.5">
              <label className="text-sm font-medium">
                {t(($) => $.polling_setup.start_at_label)}
              </label>
              <input
                type="datetime-local"
                min={toLocalDateTimeValue(new Date())}
                value={startAt}
                onChange={(e) => setStartAt(e.target.value)}
                className="flex h-9 w-full rounded-md border border-input bg-transparent px-3 py-1 text-sm shadow-sm transition-colors focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
              />
              <p className="text-xs text-muted-foreground">
                {t(($) => $.polling_setup.start_at_hint)}
              </p>
            </div>
          </div>
        </div>

        <div className="border-t bg-muted/25 px-5 py-4">
          <div className="flex justify-end gap-2">
            <Button
              type="button"
              variant="outline"
              onClick={() => onOpenChange(false)}
            >
              {t(($) => $.polling_setup.cancel)}
            </Button>
            <Button
              type="button"
              className="bg-purple-600 hover:bg-purple-700"
              disabled={!getIntervalMinutes()}
              onClick={handleConfirm}
            >
              {t(($) => $.polling_setup.confirm)}
            </Button>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  );
}
