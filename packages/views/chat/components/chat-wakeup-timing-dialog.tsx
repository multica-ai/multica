"use client";

import { useState, type FormEvent } from "react";
import { ApiError } from "@multica/core/api";
import { useRescheduleChatWakeup } from "@multica/core/chat/mutations";
import type { ChatWakeup, ChatWakeupTiming } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Dialog, DialogClose, DialogContent, DialogFooter, DialogHeader, DialogTitle, DialogTrigger } from "@multica/ui/components/ui/dialog";
import { Input } from "@multica/ui/components/ui/input";
import { Pencil } from "@multica/ui/icons";
import { useT } from "../../i18n";

function localDateTime(iso: string | null): string {
  const time = iso ? Date.parse(iso) : NaN;
  if (!Number.isFinite(time)) return "";
  const localTime = time - new Date(time).getTimezoneOffset() * 60_000;
  return new Date(localTime).toISOString().slice(0, 16);
}

export function ChatWakeupTimingDialog({ wakeup, sessionId }: { wakeup: ChatWakeup; sessionId: string }) {
  const { t } = useT("chat");
  const { t: common } = useT("common");
  const reschedule = useRescheduleChatWakeup();
  const [open, setOpen] = useState(false);
  const [error, setError] = useState("");
  const isReminder = wakeup.kind === "at";
  const isCheck = wakeup.kind === "check";
  const defaultTime = localDateTime(isReminder ? wakeup.fire_at : wakeup.expires_at);
  const defaultInterval = String((wakeup.interval_seconds ?? 600) / 60);
  const title = t(($) => $.wakeup.edit_timing);
  const pending = reschedule.isPending;

  async function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (pending) return;
    setError("");
    const form = new FormData(event.currentTarget);
    const value = String(form.get("time"));
    const interval = form.get("interval");
    const timing: ChatWakeupTiming = {};
    if (value !== defaultTime) {
      const date = new Date(value);
      if (!Number.isFinite(date.getTime())) {
        setError(t(($) => $.wakeup.edit_failed));
        return;
      }
      timing[isReminder ? "at" : "expires_at"] = date.toISOString();
    }
    if (interval !== null && interval !== defaultInterval) {
      timing.interval_seconds = Math.round(Number(interval) * 60);
    }
    if (Object.keys(timing).length === 0) {
      setOpen(false);
      return;
    }
    try {
      await reschedule.mutateAsync({ sessionId, wakeupId: wakeup.id, timing });
      setOpen(false);
    } catch (err) {
      setError(err instanceof ApiError && err.status === 400 ? err.message : t(($) => $.wakeup.edit_failed));
    }
  }

  return (
    <Dialog open={open} onOpenChange={(next) => {
      if (pending) return;
      setError("");
      setOpen(next);
    }}>
      <DialogTrigger render={<Button variant="ghost" size="icon-xs" aria-label={title} className="shrink-0 text-muted-foreground" />}>
        <Pencil aria-hidden />
      </DialogTrigger>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
        </DialogHeader>
        <form className="grid gap-4" onSubmit={save}>
          <label className="grid gap-1.5 text-caption">
            {isReminder ? t(($) => $.wakeup.edit_reminder) : t(($) => $.wakeup.edit_deadline)}
            <Input name="time" type="datetime-local" defaultValue={defaultTime} required disabled={pending} />
          </label>
          {isCheck && (
            <label className="grid gap-1.5 text-caption">
              {t(($) => $.wakeup.edit_interval)}
              <Input name="interval" type="number" min={10} step="any" defaultValue={defaultInterval} required disabled={pending} />
            </label>
          )}
          {error && <p role="alert" className="text-caption text-destructive">{error}</p>}
          <DialogFooter>
            <DialogClose render={<Button type="button" variant="outline" disabled={pending} />}>
              {common(($) => $.cancel)}
            </DialogClose>
            <Button type="submit" disabled={pending} aria-busy={pending}>{common(($) => $.save)}</Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
