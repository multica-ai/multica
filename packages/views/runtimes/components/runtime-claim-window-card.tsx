"use client";

import { useEffect, useState } from "react";
import type { AgentRuntime } from "@multica/core/types";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  RUNTIME_CLAIM_WINDOW_DURATION_MINUTES,
  addMinutesToHHMM,
} from "@multica/core/runtimes";
import { useUpdateRuntime } from "@multica/core/runtimes/mutations";
import { Button } from "@multica/ui/components/ui/button";
import { Switch } from "@multica/ui/components/ui/switch";
import { TimeInput } from "@multica/ui/components/ui/time-input";
import { toast } from "sonner";
import { TimezoneSelect } from "../../common/timezone-select";
import { useViewingTimezone } from "../../common/use-viewing-timezone";
import { useT } from "../../i18n";

export function RuntimeClaimWindowCard({
  runtime,
  canEdit,
}: {
  runtime: AgentRuntime;
  canEdit: boolean;
}) {
  const { t } = useT("runtimes");
  const wsId = useWorkspaceId();
  const viewingTimezone = useViewingTimezone();
  const updateRuntime = useUpdateRuntime(wsId);
  const savedEnabled = Boolean(
    runtime.claim_window_start && runtime.claim_window_timezone,
  );
  const savedStart = runtime.claim_window_start ?? "02:00";
  const savedTimezone = runtime.claim_window_timezone ?? viewingTimezone;
  const [enabled, setEnabled] = useState(savedEnabled);
  const [startTime, setStartTime] = useState(savedStart);
  const [timezone, setTimezone] = useState(savedTimezone);

  useEffect(() => {
    setEnabled(savedEnabled);
    setStartTime(savedStart);
    setTimezone(savedTimezone);
  }, [runtime.id, savedEnabled, savedStart, savedTimezone]);

  const endTime = addMinutesToHHMM(
    startTime,
    RUNTIME_CLAIM_WINDOW_DURATION_MINUTES,
  );
  const savedEndTime = addMinutesToHHMM(
    savedStart,
    RUNTIME_CLAIM_WINDOW_DURATION_MINUTES,
  );
  const dirty = enabled !== savedEnabled || (
    enabled && (startTime !== savedStart || timezone !== savedTimezone)
  );

  const status = !savedEnabled
    ? t(($) => $.detail.claim_window.always)
    : runtime.claim_window_open === true
      ? t(($) => $.detail.claim_window.open, { end: savedEndTime })
      : t(($) => $.detail.claim_window.closed, { start: savedStart });

  const save = () => {
    updateRuntime.mutate(
      {
        runtimeId: runtime.id,
        patch: {
          claim_window: enabled
            ? { start_time: startTime, timezone }
            : null,
        },
      },
      {
        onSuccess: () => toast.success(t(($) => $.detail.claim_window.saved)),
        onError: () => toast.error(t(($) => $.detail.claim_window.failed)),
      },
    );
  };

  return (
    <div className="rounded-lg border">
      <div className="border-b px-4 py-2.5">
        <div className="text-xs font-semibold">
          {t(($) => $.detail.claim_window.title)}
        </div>
        <p className="mt-1 text-xs leading-5 text-muted-foreground">
          {t(($) => $.detail.claim_window.description)}
        </p>
      </div>

      <div className="space-y-3 p-4">
        <div>
          <p className="text-xs font-medium">{status}</p>
          {enabled && endTime ? (
            <p className="mt-1 font-mono text-[11px] text-muted-foreground">
              <span>
                {t(($) => $.detail.claim_window.preview, {
                  start: startTime,
                  end: endTime,
                })}
              </span>
              <span className="mx-1.5">/</span>
              <span>{timezone}</span>
            </p>
          ) : null}
        </div>

        {canEdit ? (
          <>
            <div className="flex items-center justify-between border-t pt-3">
              <span className="text-xs">
                {t(($) => $.detail.claim_window.enabled)}
              </span>
              <Switch
                size="sm"
                checked={enabled}
                disabled={updateRuntime.isPending}
                aria-label={t(($) => $.detail.claim_window.enabled)}
                onCheckedChange={(checked) => setEnabled(checked)}
              />
            </div>

            {enabled ? (
              <div className="grid grid-cols-1 gap-3 border-t pt-3">
                <div>
                  <div className="mb-1.5 text-[11px] text-muted-foreground">
                    {t(($) => $.detail.claim_window.start)}
                  </div>
                  <TimeInput
                    value={startTime}
                    onChange={setStartTime}
                    disabled={updateRuntime.isPending}
                  />
                </div>
                <div>
                  <div className="mb-1.5 text-[11px] text-muted-foreground">
                    {t(($) => $.detail.claim_window.timezone)}
                  </div>
                  <TimezoneSelect
                    value={timezone}
                    onValueChange={setTimezone}
                    browserSuffix={t(($) => $.detail.claim_window.browser_suffix)}
                    disabled={updateRuntime.isPending}
                  />
                </div>
              </div>
            ) : null}

            <div className="flex justify-end border-t pt-3">
              <Button
                size="sm"
                className="h-8"
                disabled={!dirty || updateRuntime.isPending}
                onClick={save}
              >
                {updateRuntime.isPending
                  ? t(($) => $.detail.claim_window.saving)
                  : t(($) => $.detail.claim_window.save)}
              </Button>
            </div>
          </>
        ) : null}
      </div>
    </div>
  );
}
