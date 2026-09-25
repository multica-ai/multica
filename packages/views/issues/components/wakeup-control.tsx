"use client";

import { useId, useState } from "react";
import { toast } from "sonner";
import type { AgentTask, IssueWakeup } from "@multica/core/types";
import { Switch } from "@multica/ui/components/ui/switch";
import { Input } from "@multica/ui/components/ui/input";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Button } from "@multica/ui/components/ui/button";
import { useT } from "../../i18n";
import { isActiveWakeupRun, useWakeupText } from "./wakeup-presentation";

export function WakeupControl({
  wakeup,
  task,
  pending,
  closed,
  onDisable,
  onEnable,
}: {
  wakeup: Omit<IssueWakeup, "instruction">;
  task?: AgentTask;
  pending: boolean;
  closed: boolean;
  onDisable: () => void;
  onEnable: (input?: { at?: string; rearm?: boolean }) => Promise<void>;
}) {
  const { t } = useT("issues");
  const text = useWakeupText();
  const status = task?.status ?? wakeup.last_task_status;
  const canDisable =
    !wakeup.disabled_at &&
    (wakeup.enabled ||
      (["queued", "deferred"].includes(status ?? "") && !task?.started_at));
  const activeRun = isActiveWakeupRun(status);
  const consumed =
    !wakeup.enabled &&
    wakeup.mode === "once" &&
    (!wakeup.disabled_at || !!wakeup.last_task_id);
  const expired =
    wakeup.kind === "at" &&
    (!wakeup.next_fire_at ||
      new Date(wakeup.next_fire_at).getTime() <= Date.now());
  const needsRearm = !wakeup.enabled && (consumed || expired);
  const canEnable = !closed && (!activeRun || wakeup.mode === "continuous");
  return (
    <div className="flex min-h-11 shrink-0 items-center px-2">
      {wakeup.enabled || (!needsRearm && !canDisable) ? (
        <Switch
          checked={wakeup.enabled}
          disabled={pending || (!wakeup.enabled && !canEnable)}
          aria-label={t(($) => $.wakeups.toggle, {
            agent: wakeup.agent_name,
          })}
          onCheckedChange={(checked) => {
            if (checked)
              void onEnable().catch((err) =>
                toast.error(
                  text.error(
                    err,
                    t(($) => $.wakeups.enable_error),
                  ),
                ),
              );
            else onDisable();
          }}
        />
      ) : canDisable ? (
        <Button
          variant="ghost"
          size="sm"
          disabled={pending}
          onClick={onDisable}
        >
          {t(($) => $.wakeups.withdraw)}
        </Button>
      ) : wakeup.kind === "at" ? (
        <RescheduleWakeup
          disabled={pending || !canEnable}
          pending={pending}
          onSubmit={(at) => onEnable({ at, rearm: true })}
        />
      ) : (
        <Button
          variant="ghost"
          size="sm"
          disabled={pending || !canEnable}
          onClick={() =>
            void onEnable({ rearm: true }).catch((err) =>
              toast.error(
                text.error(
                  err,
                  t(($) => $.wakeups.enable_error),
                ),
              ),
            )
          }
        >
          {t(($) => $.wakeups.resubscribe)}
        </Button>
      )}
    </div>
  );
}

function RescheduleWakeup({
  disabled,
  pending,
  onSubmit,
}: {
  disabled: boolean;
  pending: boolean;
  onSubmit: (at: string) => Promise<void>;
}) {
  const { t } = useT("issues");
  const text = useWakeupText();
  const id = useId();
  const [open, setOpen] = useState(false);
  const [value, setValue] = useState("");
  const [error, setError] = useState("");
  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <Button
        variant="ghost"
        size="sm"
        disabled={disabled}
        onClick={() => {
          setError("");
          setValue("");
          setOpen(true);
        }}
      >
        {t(($) => $.wakeups.reschedule)}
      </Button>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t(($) => $.wakeups.reschedule)}</DialogTitle>
        </DialogHeader>
        <form
          className="space-y-3"
          onSubmit={async (event) => {
            event.preventDefault();
            if (pending) return;
            const date = new Date(value);
            if (
              !Number.isFinite(date.getTime()) ||
              date.getTime() <= Date.now()
            ) {
              setError(t(($) => $.wakeups.future_time));
              return;
            }
            try {
              await onSubmit(date.toISOString());
              setOpen(false);
            } catch (err) {
              setError(
                text.error(
                  err,
                  t(($) => $.wakeups.enable_error),
                ),
              );
            }
          }}
        >
          <label htmlFor={id} className="text-caption">
            {t(($) => $.wakeups.local_time, {
              timezone: Intl.DateTimeFormat().resolvedOptions().timeZone,
            })}
          </label>
          <Input
            id={id}
            type="datetime-local"
            required
            value={value}
            disabled={pending}
            aria-invalid={!!error}
            aria-describedby={error ? `${id}-error` : undefined}
            onChange={(event) => {
              setValue(event.target.value);
              setError("");
            }}
          />
          {error && (
            <p
              id={`${id}-error`}
              role="alert"
              className="text-caption text-destructive"
            >
              {error}
            </p>
          )}
          <Button type="submit" disabled={pending}>
            {t(($) => $.wakeups.reschedule)}
          </Button>
        </form>
      </DialogContent>
    </Dialog>
  );
}
