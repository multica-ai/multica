"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  briefPushOptions,
  useSaveBriefPush,
  type BriefPushPreference,
  type UpdateBriefPushRequest,
} from "@multica/core/office";
import { useWorkspaceId } from "@multica/core/hooks";
import { Button } from "@multica/ui/components/ui/button";
import { Switch } from "@multica/ui/components/ui/switch";
import { Loader2 } from "@multica/ui/icons";
import { useT } from "../../i18n";

const HOURS = Array.from({ length: 24 }, (_, hour) => hour);
const DEFAULT_QUIET_HOURS = { start: 22, end: 7 };

/**
 * The caller's chat delivery of the daily brief (spec 2.7): whether a brief is
 * pushed to their linked chat, and the local hours it waits through. A chat must
 * be linked first; the switch stays off until one is.
 */
export function OfficeBriefDelivery() {
  const wsId = useWorkspaceId();
  const { data, isError } = useQuery(briefPushOptions(wsId));
  const { t } = useT("issues");

  if (isError) {
    return (
      <p role="alert" className="text-caption text-destructive">
        {t(($) => $.office.delivery_load_failed)}
      </p>
    );
  }
  if (!data) return null;
  // Keyed on the last read, so the form starts again from what the server stored.
  return <DeliveryForm key={data.quiet_hours ? "quiet" : "open"} saved={data} />;
}

function DeliveryForm({ saved }: { saved: BriefPushPreference }) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const save = useSaveBriefPush(wsId);
  const hasChat = saved.binding_id !== null;

  const [push, setPush] = useState(saved.brief_push);
  const [quietOn, setQuietOn] = useState(saved.quiet_hours !== null);
  const [start, setStart] = useState(saved.quiet_hours?.start ?? DEFAULT_QUIET_HOURS.start);
  const [end, setEnd] = useState(saved.quiet_hours?.end ?? DEFAULT_QUIET_HOURS.end);

  // A save always sends all three fields: the server clears any it does not get.
  const request: UpdateBriefPushRequest = {
    brief_push: push && hasChat,
    binding_id: saved.binding_id,
    quiet_hours: quietOn ? { start, end } : null,
  };
  const stored: UpdateBriefPushRequest = {
    brief_push: saved.brief_push,
    binding_id: saved.binding_id,
    quiet_hours: saved.quiet_hours,
  };
  const dirty = JSON.stringify(request) !== JSON.stringify(stored);
  const sameHours = quietOn && start === end;

  const onSave = () =>
    save.mutate(request, {
      onSuccess: () => toast.success(t(($) => $.office.delivery_saved)),
      onError: (error) =>
        toast.error(error instanceof Error ? error.message : t(($) => $.office.delivery_save_failed)),
    });

  return (
    <section aria-labelledby="office-brief-delivery-title" className="flex flex-col gap-3 rounded-lg border px-4 py-3">
      <h3 id="office-brief-delivery-title" className="text-body font-medium">
        {t(($) => $.office.delivery_title)}
      </h3>
      {!hasChat ? <p className="text-caption text-muted-foreground">{t(($) => $.office.delivery_no_chat)}</p> : null}
      <div className="flex items-center justify-between gap-3">
        <label htmlFor="office-brief-push" className="text-body">
          {t(($) => $.office.delivery_push)}
        </label>
        <Switch
          id="office-brief-push"
          checked={push}
          disabled={!hasChat || save.isPending}
          onCheckedChange={(checked) => setPush(checked)}
        />
      </div>
      <div className="flex items-center justify-between gap-3">
        <label htmlFor="office-brief-quiet" className="text-body">
          {t(($) => $.office.delivery_quiet)}
        </label>
        <Switch
          id="office-brief-quiet"
          checked={quietOn}
          disabled={save.isPending}
          onCheckedChange={(checked) => setQuietOn(checked)}
        />
      </div>
      {quietOn ? (
        <div className="flex flex-wrap items-end gap-3">
          <HourField id="office-brief-quiet-from" label={t(($) => $.office.delivery_quiet_from)} value={start} onChange={setStart} />
          <HourField id="office-brief-quiet-until" label={t(($) => $.office.delivery_quiet_to)} value={end} onChange={setEnd} />
          <p className="w-full text-caption text-muted-foreground">{t(($) => $.office.delivery_quiet_hint)}</p>
        </div>
      ) : null}
      {sameHours ? (
        <p role="alert" className="text-caption text-destructive">
          {t(($) => $.office.delivery_same_hours)}
        </p>
      ) : null}
      <div className="flex justify-end">
        <Button
          type="button"
          disabled={!dirty || sameHours || save.isPending}
          aria-busy={save.isPending}
          onClick={onSave}
        >
          {save.isPending ? <Loader2 className="size-4 animate-spin" /> : null}
          {t(($) => $.office.delivery_save)}
        </Button>
      </div>
    </section>
  );
}

function HourField({
  id,
  label,
  value,
  onChange,
}: {
  id: string;
  label: string;
  value: number;
  onChange: (hour: number) => void;
}) {
  const { t } = useT("issues");
  return (
    <div className="flex flex-col gap-1">
      <label htmlFor={id} className="text-caption text-muted-foreground">
        {label}
      </label>
      <select
        id={id}
        value={value}
        onChange={(event) => onChange(Number(event.target.value))}
        className="h-9 rounded-lg border border-input bg-background px-3 text-body text-foreground focus-visible:outline-2 focus-visible:outline-ring"
      >
        {HOURS.map((hour) => (
          <option key={hour} value={hour}>
            {t(($) => $.office.delivery_hour, { hour: String(hour).padStart(2, "0") })}
          </option>
        ))}
      </select>
    </div>
  );
}
