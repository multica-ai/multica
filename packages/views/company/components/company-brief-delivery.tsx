"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  briefPushBindingsOptions,
  briefPushOptions,
  useSaveBriefPush,
  type BriefPushBinding,
  type BriefPushPreference,
  type UpdateBriefPushRequest,
} from "@multica/core/company";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { Button } from "@multica/ui/components/ui/button";
import { Switch } from "@multica/ui/components/ui/switch";
import { Loader2 } from "@multica/ui/icons";
import { AppLink } from "../../navigation";
import { useT } from "../../i18n";

const HOURS = Array.from({ length: 24 }, (_, hour) => hour);
const DEFAULT_QUIET_HOURS = { start: 22, end: 7 };
const SELECT_CLASS =
  "h-9 rounded-lg border border-input bg-background px-3 text-body text-foreground focus-visible:outline-2 focus-visible:outline-ring";

/**
 * The caller's chat delivery of the daily brief (spec 2.7): which linked chat a
 * brief is pushed to, whether it is pushed, and the local hours it waits through.
 * The switch stays off until a chat is picked.
 */
export function CompanyBriefDelivery() {
  const wsId = useWorkspaceId();
  const preference = useQuery(briefPushOptions(wsId));
  const bindings = useQuery(briefPushBindingsOptions(wsId));
  const { t } = useT("issues");

  if (preference.isError || bindings.isError) {
    return (
      <p role="alert" className="text-caption text-destructive">
        {t(($) => $.company.delivery_load_failed)}
      </p>
    );
  }
  if (!preference.data || !bindings.data) return null;
  // Keyed on the last read, so the form starts again from what the server stored.
  return (
    <DeliveryForm
      key={preference.data.quiet_hours ? "quiet" : "open"}
      saved={preference.data}
      bindings={bindings.data}
    />
  );
}

// chatName labels a chat with its platform's name from the settings channel
// sections, then the chat's own label. A platform without a name falls back to its
// type, capitalized.
function chatName({ channel, label }: BriefPushBinding, channelNames: Record<string, string>) {
  const name = channelNames[channel] ?? channel.charAt(0).toUpperCase() + channel.slice(1);
  return label ? `${name} · ${label}` : name;
}

function DeliveryForm({ saved, bindings }: { saved: BriefPushPreference; bindings: BriefPushBinding[] }) {
  const { t } = useT("issues");
  const { t: tSettings } = useT("settings");
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const save = useSaveBriefPush(wsId);
  // The chat app's type is what the installation stores; the names are the
  // settings sections' titles for the same apps. Lark's installations store feishu.
  const channelNames: Record<string, string> = {
    telegram: tSettings(($) => $.telegram.section_title),
    slack: tSettings(($) => $.slack.section_title),
    feishu: tSettings(($) => $.lark.section_title),
    dingtalk: tSettings(($) => $.dingtalk.section_title),
    wecom: tSettings(($) => $.wecom.section_title),
  };

  // A saved chat that is no longer listed cannot take briefs, so it reads as none
  // and the next save clears it.
  const storedBinding = bindings.some((binding) => binding.id === saved.binding_id) ? saved.binding_id : null;
  const stored: UpdateBriefPushRequest = {
    brief_push: saved.brief_push && storedBinding !== null,
    binding_id: storedBinding,
    quiet_hours: saved.quiet_hours,
  };

  const [binding, setBinding] = useState<string | null>(storedBinding);
  const [push, setPush] = useState(stored.brief_push);
  const [quietOn, setQuietOn] = useState(saved.quiet_hours !== null);
  const [start, setStart] = useState(saved.quiet_hours?.start ?? DEFAULT_QUIET_HOURS.start);
  const [end, setEnd] = useState(saved.quiet_hours?.end ?? DEFAULT_QUIET_HOURS.end);

  const pushOn = push && binding !== null;
  // A save always sends all three fields: the server clears any it does not get.
  const request: UpdateBriefPushRequest = {
    brief_push: pushOn,
    binding_id: binding,
    quiet_hours: quietOn ? { start, end } : null,
  };
  const dirty = JSON.stringify(request) !== JSON.stringify(stored);
  const sameHours = quietOn && start === end;

  const onSave = () =>
    save.mutate(request, {
      onSuccess: () => toast.success(t(($) => $.company.delivery_saved)),
      onError: (error) =>
        toast.error(error instanceof Error ? error.message : t(($) => $.company.delivery_save_failed)),
    });

  return (
    <section aria-labelledby="company-brief-delivery-title" className="flex flex-col gap-3 rounded-lg border px-4 py-3">
      <h3 id="company-brief-delivery-title" className="text-body font-medium">
        {t(($) => $.company.delivery_title)}
      </h3>
      {bindings.length === 0 ? (
        <p className="text-caption text-muted-foreground">
          <AppLink
            href={`${paths.settings()}?tab=channels&integration=telegram`}
            className="underline-offset-2 hover:text-foreground hover:underline"
          >
            {t(($) => $.company.delivery_no_chat)}
          </AppLink>
        </p>
      ) : (
        <div className="flex items-center justify-between gap-3">
          <label htmlFor="company-brief-chat" className="text-body">
            {t(($) => $.company.delivery_chat)}
          </label>
          <select
            id="company-brief-chat"
            value={binding ?? ""}
            disabled={save.isPending}
            onChange={(event) => setBinding(event.target.value || null)}
            className={SELECT_CLASS}
          >
            {binding === null ? (
              <option value="" disabled>
                {t(($) => $.company.delivery_chat_placeholder)}
              </option>
            ) : null}
            {bindings.map((option) => (
              <option key={option.id} value={option.id}>
                {chatName(option, channelNames)}
              </option>
            ))}
          </select>
        </div>
      )}
      <div className="flex items-center justify-between gap-3">
        <label htmlFor="company-brief-push" className="text-body">
          {t(($) => $.company.delivery_push)}
        </label>
        <Switch
          id="company-brief-push"
          checked={pushOn}
          disabled={binding === null || save.isPending}
          onCheckedChange={(checked) => setPush(checked)}
        />
      </div>
      <div className="flex items-center justify-between gap-3">
        <label htmlFor="company-brief-quiet" className="text-body">
          {t(($) => $.company.delivery_quiet)}
        </label>
        <Switch
          id="company-brief-quiet"
          checked={quietOn}
          disabled={save.isPending}
          onCheckedChange={(checked) => setQuietOn(checked)}
        />
      </div>
      {quietOn ? (
        <div className="flex flex-wrap items-end gap-3">
          <HourField id="company-brief-quiet-from" label={t(($) => $.company.delivery_quiet_from)} value={start} onChange={setStart} />
          <HourField id="company-brief-quiet-until" label={t(($) => $.company.delivery_quiet_to)} value={end} onChange={setEnd} />
          <p className="w-full text-caption text-muted-foreground">{t(($) => $.company.delivery_quiet_hint)}</p>
        </div>
      ) : null}
      {sameHours ? (
        <p role="alert" className="text-caption text-destructive">
          {t(($) => $.company.delivery_same_hours)}
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
          {t(($) => $.company.delivery_save)}
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
        className={SELECT_CLASS}
      >
        {HOURS.map((hour) => (
          <option key={hour} value={hour}>
            {t(($) => $.company.delivery_hour, { hour: String(hour).padStart(2, "0") })}
          </option>
        ))}
      </select>
    </div>
  );
}
