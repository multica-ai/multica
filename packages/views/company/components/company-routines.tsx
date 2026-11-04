"use client";

import { useState, type ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { clientErrorMessage } from "@multica/core/api";
import {
  CHARTER_OUTPUT_KINDS,
  ROUTINE_CADENCES,
  squadRoutinesOptions,
  useCreateSquadRoutine,
  useRunSquadRoutine,
  useSetSquadRoutinePaused,
  useUnlinkSquadRoutine,
  type CreateSquadRoutineRequest,
  type RoutineCadence,
  type RoutineState,
  type SquadRoutine,
} from "@multica/core/company";
import { useWorkspaceId } from "@multica/core/hooks";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { formatInTimeZone } from "../../common/format-in-time-zone";
import { useLocale, useT, useTimeAgo } from "../../i18n";
import { useCharterKindLabels } from "./company-charter-tab";
import { cadenceOfCron } from "../lib/routine-schedule";

const ROUTINE_PURPOSE_MAX = 200;

/**
 * A squad's routines: its autopilots that the charter holds to a schedule, each
 * with the state the platform derived for it. Managers (the squad's creator and
 * workspace admins) can run, pause, remove and add routines; members see them.
 */
export function CompanyRoutines({ squadId, canManage }: { squadId: string; canManage: boolean }) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const query = useQuery(squadRoutinesOptions(wsId, squadId));
  const routines = query.data ?? [];
  const [adding, setAdding] = useState(false);

  return (
    <section aria-label={t(($) => $.company.routines_title)} className="flex flex-col gap-1">
      <div className="flex items-center justify-between gap-2">
        <h3 className="text-caption font-medium text-muted-foreground">{t(($) => $.company.routines_title)}</h3>
        {canManage && !adding ? (
          <Button type="button" variant="outline" size="sm" onClick={() => setAdding(true)}>
            {t(($) => $.company.routines_add)}
          </Button>
        ) : null}
      </div>
      {adding ? <AddRoutineForm squadId={squadId} onDone={() => setAdding(false)} /> : null}
      {query.isError ? (
        <p className="px-2 py-1 text-body text-muted-foreground">{t(($) => $.company.routines_load_failed)}</p>
      ) : query.isPending ? null : routines.length === 0 ? (
        <p className="px-2 py-1 text-body text-muted-foreground">{t(($) => $.company.routines_empty)}</p>
      ) : (
        <ul className="flex flex-col">
          {routines.map((routine) => (
            <RoutineRow key={routine.autopilot_id} routine={routine} squadId={squadId} canManage={canManage} />
          ))}
        </ul>
      )}
    </section>
  );
}

function RoutineRow({
  routine,
  squadId,
  canManage,
}: {
  routine: SquadRoutine;
  squadId: string;
  canManage: boolean;
}) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const locale = useLocale();
  const timeAgo = useTimeAgo();
  const stateLabels = useRoutineStateLabels();
  const cadenceLabels = useCadenceLabels();
  const run = useRunSquadRoutine(wsId, squadId);
  const setPaused = useSetSquadRoutinePaused(wsId, squadId);
  const unlink = useUnlinkSquadRoutine(wsId, squadId);

  const paused = routine.state === "paused";
  const busy = run.isPending || setPaused.isPending || unlink.isPending;

  const schedule = routine.schedule;
  const preset = schedule ? cadenceOfCron(schedule.cron_expression) : null;
  const scheduleText = !schedule
    ? t(($) => $.company.routine_schedule_none)
    : preset
      ? cadenceLabels[preset]
      : schedule.cron_expression;
  const lastRunText = routine.last_run
    ? t(($) => $.company.routine_last_run, { time: timeAgo(routine.last_run.triggered_at) })
    : t(($) => $.company.routine_last_run_none);
  const nextRunText =
    schedule && routine.next_run_at
      ? t(($) => $.company.routine_next_run, {
          time: formatInTimeZone(routine.next_run_at, schedule.timezone, locale),
        })
      : null;
  const meta = [scheduleText, lastRunText, nextRunText].filter(Boolean).join(" · ");

  return (
    <li className="flex flex-col gap-1 border-b py-2 last:border-b-0">
      <div className="flex min-w-0 items-center gap-2">
        <StateChip state={routine.state} label={stateLabels[routine.state]} />
        <span className="min-w-0 flex-1 truncate text-body">{routine.purpose || routine.autopilot_title}</span>
      </div>
      <p className="truncate text-caption text-muted-foreground">{meta}</p>
      {canManage ? (
        <div className="flex flex-wrap gap-1">
          <Button
            type="button"
            variant="ghost"
            size="sm"
            disabled={paused || busy}
            aria-busy={run.isPending}
            onClick={() =>
              run.mutate(routine.autopilot_id, {
                onSuccess: () => toast.success(t(($) => $.company.routine_run_started)),
                onError: (err) => toast.error(clientErrorMessage(err) ?? t(($) => $.company.routine_run_failed)),
              })
            }
          >
            {t(($) => $.company.routine_run_now)}
          </Button>
          <Button
            type="button"
            variant="ghost"
            size="sm"
            disabled={busy}
            aria-busy={setPaused.isPending}
            onClick={() =>
              setPaused.mutate(
                { autopilotId: routine.autopilot_id, paused: !paused },
                {
                  onSuccess: () =>
                    toast.success(
                      paused
                        ? t(($) => $.company.routine_resumed)
                        : t(($) => $.company.routine_paused),
                    ),
                  onError: (err) =>
                    toast.error(clientErrorMessage(err) ?? t(($) => $.company.routine_status_failed)),
                },
              )
            }
          >
            {paused ? t(($) => $.company.routine_resume) : t(($) => $.company.routine_pause)}
          </Button>
          <Button
            type="button"
            variant="ghost"
            size="sm"
            disabled={busy}
            onClick={() =>
              unlink.mutate(
                { autopilotId: routine.autopilot_id, pause: true },
                {
                  onSuccess: () => toast.success(t(($) => $.company.routine_removed)),
                  onError: (err) =>
                    toast.error(clientErrorMessage(err) ?? t(($) => $.company.routine_remove_failed)),
                },
              )
            }
          >
            {t(($) => $.company.routine_remove)}
          </Button>
        </div>
      ) : null}
    </li>
  );
}

function StateChip({ state, label }: { state: RoutineState; label: string }) {
  const style = STATE_CHIP[state];
  return (
    <Badge variant={style.variant} className={style.className} data-state={state}>
      {label}
    </Badge>
  );
}

const STATE_CHIP: Record<RoutineState, { variant: "outline" | "destructive" | "secondary"; className: string }> = {
  on_time: { variant: "outline", className: "border-success/40 text-success" },
  no_output: { variant: "outline", className: "border-warning/40 text-warning" },
  late: { variant: "outline", className: "border-warning/40 text-warning" },
  failed: { variant: "destructive", className: "" },
  paused: { variant: "secondary", className: "" },
  never_ran: { variant: "outline", className: "text-muted-foreground" },
  unknown: { variant: "outline", className: "text-muted-foreground" },
};

function AddRoutineForm({ squadId, onDone }: { squadId: string; onDone: () => void }) {
  const { t } = useT("issues");
  const { t: tCommon } = useT("common");
  const wsId = useWorkspaceId();
  const create = useCreateSquadRoutine(wsId, squadId);
  const cadenceLabels = useCadenceLabels();
  const kindLabels = useCharterKindLabels();
  const [purpose, setPurpose] = useState("");
  const [cadence, setCadence] = useState<RoutineCadence | "cron">("weekly");
  const [cron, setCron] = useState("");
  const [expectedOutput, setExpectedOutput] = useState<NonNullable<CreateSquadRoutineRequest["expected_output"]>>("none");
  const [description, setDescription] = useState("");

  const cadenceItems = [
    ...ROUTINE_CADENCES.map((value) => ({ value, label: cadenceLabels[value] })),
    { value: "cron", label: t(($) => $.company.routine_cadence_cron) },
  ];
  const outputItems = [
    { value: "none", label: t(($) => $.company.routine_output_none) },
    ...CHARTER_OUTPUT_KINDS.map((kind) => ({ value: kind, label: kindLabels[kind] })),
  ];
  const canSubmit = purpose.trim() !== "" && (cadence !== "cron" || cron.trim() !== "") && !create.isPending;

  const submit = () => {
    const desc = description.trim();
    create.mutate(
      {
        cadence: cadence === "cron" ? cron.trim() : cadence,
        purpose: purpose.trim(),
        expected_output: expectedOutput,
        description: desc === "" ? undefined : desc,
      },
      {
        onSuccess: () => {
          toast.success(t(($) => $.company.routine_added));
          onDone();
        },
        onError: (err) => toast.error(clientErrorMessage(err) ?? t(($) => $.company.routine_add_failed)),
      },
    );
  };

  return (
    <form
      className="flex flex-col gap-3 rounded-md border p-3"
      onSubmit={(event) => {
        event.preventDefault();
        if (canSubmit) submit();
      }}
    >
      <FormField id="routine-purpose" label={t(($) => $.company.routine_purpose)}>
        <Input
          id="routine-purpose"
          maxLength={ROUTINE_PURPOSE_MAX}
          placeholder={t(($) => $.company.routine_purpose_placeholder)}
          value={purpose}
          onChange={(event) => setPurpose(event.target.value)}
        />
      </FormField>
      <div className="grid grid-cols-2 gap-2">
        <FormField id="routine-cadence" label={t(($) => $.company.routine_cadence)}>
          <Select
            items={cadenceItems}
            value={cadence}
            onValueChange={(next) => {
              if (next) setCadence(next as RoutineCadence | "cron");
            }}
          >
            <SelectTrigger id="routine-cadence" size="sm" className="w-full">
              <SelectValue>{cadenceItems.find((item) => item.value === cadence)?.label}</SelectValue>
            </SelectTrigger>
            <SelectContent>
              {cadenceItems.map((item) => (
                <SelectItem key={item.value} value={item.value}>
                  {item.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </FormField>
        <FormField id="routine-output" label={t(($) => $.company.routine_expected_output)}>
          <Select
            items={outputItems}
            value={expectedOutput}
            onValueChange={(next) => {
              if (next) setExpectedOutput(next as NonNullable<CreateSquadRoutineRequest["expected_output"]>);
            }}
          >
            <SelectTrigger id="routine-output" size="sm" className="w-full">
              <SelectValue>{outputItems.find((item) => item.value === expectedOutput)?.label}</SelectValue>
            </SelectTrigger>
            <SelectContent>
              {outputItems.map((item) => (
                <SelectItem key={item.value} value={item.value}>
                  {item.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </FormField>
      </div>
      {cadence === "cron" ? (
        <FormField id="routine-cron" label={t(($) => $.company.routine_cadence_cron)}>
          <Input
            id="routine-cron"
            placeholder={t(($) => $.company.routine_cron_placeholder)}
            value={cron}
            onChange={(event) => setCron(event.target.value)}
          />
        </FormField>
      ) : null}
      <FormField id="routine-description" label={t(($) => $.company.routine_description)}>
        <Textarea
          id="routine-description"
          rows={2}
          value={description}
          onChange={(event) => setDescription(event.target.value)}
        />
      </FormField>
      <div className="flex justify-end gap-2">
        <Button type="button" variant="outline" onClick={onDone}>
          {tCommon(($) => $.cancel)}
        </Button>
        <Button type="submit" disabled={!canSubmit} aria-busy={create.isPending}>
          {t(($) => $.company.routines_add)}
        </Button>
      </div>
    </form>
  );
}

function FormField({ id, label, children }: { id: string; label: string; children: ReactNode }) {
  return (
    <div className="flex min-w-0 flex-col gap-1.5">
      <Label htmlFor={id} className="text-caption font-medium text-muted-foreground">
        {label}
      </Label>
      {children}
    </div>
  );
}

function useRoutineStateLabels(): Record<RoutineState, string> {
  const { t } = useT("issues");
  return {
    on_time: t(($) => $.company.routine_state_on_time),
    no_output: t(($) => $.company.routine_state_no_output),
    late: t(($) => $.company.routine_state_late),
    failed: t(($) => $.company.routine_state_failed),
    paused: t(($) => $.company.routine_state_paused),
    never_ran: t(($) => $.company.routine_state_never_ran),
    unknown: t(($) => $.company.routine_state_unknown),
  };
}

function useCadenceLabels(): Record<RoutineCadence, string> {
  const { t } = useT("issues");
  return {
    daily: t(($) => $.company.routine_cadence_daily),
    weekdays: t(($) => $.company.routine_cadence_weekdays),
    weekly: t(($) => $.company.routine_cadence_weekly),
    biweekly: t(($) => $.company.routine_cadence_biweekly),
    monthly: t(($) => $.company.routine_cadence_monthly),
  };
}
