"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  canGovern,
  companyStateOptions,
  timezoneOptions,
  useSetCompanyState,
  useSetQuietHours,
  type CompanyState,
  type QuietHours,
  type SelectableCompanyState,
  DIGEST_MODES,
  digestModeOptions,
  useSetDigestMode,
} from "@multica/core/governance";
import { useWorkspaceId } from "@multica/core/hooks";
import { useCurrentMember } from "@multica/core/permissions";
import { Button } from "@multica/ui/components/ui/button";
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
import { Label } from "@multica/ui/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { Switch } from "@multica/ui/components/ui/switch";
import { TimeInput } from "@multica/ui/components/ui/time-input";
import { cn } from "@multica/ui/lib/utils";
import { PAGE_GUTTER } from "../layout/page-header";
import { useT } from "../i18n";
import { GovernanceHealthSection } from "./governance-health-section";

const SELECTABLE_STATES: readonly SelectableCompanyState[] = ["running", "quiet", "paused"];

/**
 * The company's controls: its state, the daily quiet window and the stop
 * action. Every change waits for the server and then shows what it kept; the
 * segmented control and the quiet hours never move ahead of the response.
 */
export function GovernanceControlsTab() {
  const { t } = useT("governance");
  const wsId = useWorkspaceId();
  const { role } = useCurrentMember(wsId);
  const editable = canGovern(role);
  const company = useQuery(companyStateOptions(wsId));
  const setState = useSetCompanyState(wsId);
  const [confirmingStop, setConfirmingStop] = useState(false);

  const info = company.data;
  // The segmented control shows the boss's setting; `state` is what is in force
  // now, which a quiet window can change without touching the setting.
  const setting: CompanyState = info?.setting ?? "unknown";
  const state: CompanyState = info?.state ?? "unknown";
  const busy = setState.isPending;

  const choose = (next: SelectableCompanyState) => {
    if (!editable || busy || next === setting) return;
    setState.mutate({ state: next }, { onError: () => toast.error(t(($) => $.controls.state_save_failed)) });
  };

  const stopRunningWork = () => {
    setState.mutate(
      { state: "paused", stop_running: true },
      {
        onSuccess: () => setConfirmingStop(false),
        onError: () => toast.error(t(($) => $.controls.state_save_failed)),
      },
    );
  };

  return (
    <div className={cn(PAGE_GUTTER, "flex flex-col gap-8 py-4")}>
      <section className="flex flex-col gap-3" aria-labelledby="governance-state-heading">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <h2 id="governance-state-heading" className="text-title-sm font-medium">
            {t(($) => $.controls.state_heading)}
          </h2>
          {info && editable ? (
            <Button
              type="button"
              size="sm"
              variant={state === "paused" ? "brand" : "outline"}
              disabled={busy}
              aria-busy={busy}
              onClick={() => choose(state === "paused" ? "running" : "paused")}
            >
              {state === "paused" ? t(($) => $.controls.resume) : t(($) => $.controls.pause)}
            </Button>
          ) : null}
        </div>

        {company.isError ? (
          <p role="alert" className="text-caption text-destructive">
            {t(($) => $.controls.state_load_failed)}
          </p>
        ) : null}

        {info ? (
          <div role="group" aria-label={t(($) => $.controls.state_heading)} className="flex flex-wrap gap-1 rounded-lg bg-muted p-[3px] w-fit">
            {SELECTABLE_STATES.map((option) => {
              const selected = setting === option;
              return (
                <Button
                  key={option}
                  type="button"
                  size="sm"
                  variant={selected ? "brand" : "ghost"}
                  aria-pressed={selected}
                  disabled={!editable || busy}
                  onClick={() => choose(option)}
                >
                  {t(($) => $.controls.state[option])}
                </Button>
              );
            })}
          </div>
        ) : null}

        {info && state !== setting ? (
          <p className="text-caption text-muted-foreground">
            {t(($) => $.controls.in_force, { state: t(($) => $.controls.state[state]) })}
          </p>
        ) : null}

        {state === "paused" ? (
          <p className="text-caption text-muted-foreground">{t(($) => $.controls.pause_hint)}</p>
        ) : null}

        <div>
          <Button
            type="button"
            size="sm"
            variant="outline"
            className="text-destructive"
            disabled={!editable || busy || !info}
            onClick={() => setConfirmingStop(true)}
          >
            {t(($) => $.controls.stop_work)}
          </Button>
        </div>

        {!editable ? <p className="text-caption text-muted-foreground">{t(($) => $.controls.read_only)}</p> : null}
      </section>

      {info ? (
        <section className="flex flex-col gap-3" aria-labelledby="governance-quiet-heading">
          <h2 id="governance-quiet-heading" className="text-title-sm font-medium">
            {t(($) => $.controls.quiet_heading)}
          </h2>
          <QuietHoursForm key={JSON.stringify(info.quiet_hours)} wsId={wsId} initial={info.quiet_hours} editable={editable} />
        </section>
      ) : null}

      <DigestModeSection wsId={wsId} editable={editable} />

      <GovernanceHealthSection />

      <AlertDialog
        open={confirmingStop}
        onOpenChange={(open) => {
          // The dialog stays up while the pause is in flight; it closes once the server has answered.
          if (!open && busy) return;
          setConfirmingStop(open);
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t(($) => $.controls.stop_confirm_title)}</AlertDialogTitle>
            <AlertDialogDescription>{t(($) => $.controls.stop_confirm_description)}</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t(($) => $.controls.cancel)}</AlertDialogCancel>
            <AlertDialogAction
              variant="destructive"
              disabled={busy}
              aria-busy={busy}
              onClick={stopRunningWork}
            >
              {t(($) => $.controls.stop_confirm_action)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

/**
 * The quiet window as a draft. It is keyed by the saved value, so a saved
 * change (or a refetch that brings one) starts the form again from the server.
 */
function QuietHoursForm({
  wsId,
  initial,
  editable,
}: {
  wsId: string;
  initial: QuietHours;
  editable: boolean;
}) {
  const { t } = useT("governance");
  const save = useSetQuietHours(wsId);
  const [draft, setDraft] = useState<QuietHours>(initial);
  const dirty = draft.enabled !== initial.enabled
    || draft.start !== initial.start
    || draft.end !== initial.end
    || draft.timezone !== initial.timezone;
  const zones = timezoneOptions(draft.timezone);
  const busy = save.isPending;

  return (
    <form
      className="flex max-w-xl flex-col gap-4"
      onSubmit={(event) => {
        event.preventDefault();
        if (!editable || !dirty || busy) return;
        save.mutate(draft, {
          onSuccess: () => toast.success(t(($) => $.controls.quiet_saved)),
          onError: () => toast.error(t(($) => $.controls.quiet_save_failed)),
        });
      }}
    >
      <div className="flex items-center gap-3">
        <Switch
          id="governance-quiet-enabled"
          checked={draft.enabled}
          disabled={!editable || busy}
          onCheckedChange={(enabled) => setDraft({ ...draft, enabled })}
        />
        <Label htmlFor="governance-quiet-enabled">{t(($) => $.controls.quiet_enabled)}</Label>
      </div>

      <div className="flex flex-wrap items-end gap-4">
        <div className="flex flex-col gap-1.5">
          <span className="text-caption font-medium">{t(($) => $.controls.quiet_from)}</span>
          <TimeInput
            value={draft.start}
            disabled={!editable || busy}
            hourLabel={t(($) => $.controls.quiet_hour)}
            minuteLabel={t(($) => $.controls.quiet_minute)}
            onChange={(start) => setDraft({ ...draft, start })}
          />
        </div>
        <div className="flex flex-col gap-1.5">
          <span className="text-caption font-medium">{t(($) => $.controls.quiet_until)}</span>
          <TimeInput
            value={draft.end}
            disabled={!editable || busy}
            hourLabel={t(($) => $.controls.quiet_hour)}
            minuteLabel={t(($) => $.controls.quiet_minute)}
            onChange={(end) => setDraft({ ...draft, end })}
          />
        </div>
      </div>

      <div className="flex flex-col gap-1.5">
        <span className="text-caption font-medium">{t(($) => $.controls.quiet_timezone)}</span>
        <Select
          items={zones.map((zone) => ({ value: zone, label: zone }))}
          value={draft.timezone}
          disabled={!editable || busy}
          onValueChange={(timezone) => {
            if (timezone) setDraft({ ...draft, timezone });
          }}
        >
          <SelectTrigger aria-label={t(($) => $.controls.quiet_timezone)} className="w-72">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {zones.map((zone) => (
              <SelectItem key={zone} value={zone}>
                {zone}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>

      <p className="text-caption text-muted-foreground">{t(($) => $.controls.quiet_hint)}</p>

      <div>
        <Button type="submit" size="sm" disabled={!editable || !dirty || busy} aria-busy={busy}>
          {t(($) => $.controls.quiet_save)}
        </Button>
      </div>
    </form>
  );
}

/**
 * The Secretary's digest source (spec 4.8): automatic, squads or floors, with a
 * note of what automatic resolves to at the current squad count.
 */
function DigestModeSection({ wsId, editable }: { wsId: string; editable: boolean }) {
  const { t } = useT("governance");
  const { data } = useQuery(digestModeOptions(wsId));
  const save = useSetDigestMode(wsId);
  if (!data) return null;
  const label = t(($) => $.controls.digest_heading);
  return (
    <section className="flex flex-col gap-3" aria-labelledby="governance-digest-heading">
      <h2 id="governance-digest-heading" className="text-title-sm font-medium">
        {label}
      </h2>
      <div role="group" aria-label={label} className="flex flex-wrap gap-2">
        {DIGEST_MODES.map((mode) => (
          <Button
            key={mode}
            type="button"
            size="sm"
            variant={data.setting === mode ? "default" : "outline"}
            aria-pressed={data.setting === mode}
            disabled={!editable || save.isPending}
            onClick={() =>
              save.mutate(mode, {
                onError: () => toast.error(t(($) => $.controls.digest_save_failed)),
              })
            }
          >
            {t(($) => $.controls.digest_mode[mode])}
          </Button>
        ))}
      </div>
      <p className="max-w-prose text-caption text-muted-foreground">
        {t(($) => $.controls.digest_note, {
          resolved: t(($) => $.controls.digest_mode[data.resolved]),
          count: data.active_squads,
          threshold: data.auto_floor_threshold,
        })}
      </p>
    </section>
  );
}
