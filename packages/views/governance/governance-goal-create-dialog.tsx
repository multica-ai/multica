"use client";

import { useRef, useState, type FormEvent } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  metricsOptions,
  useCreateGoal,
  type CreateGoalRequest,
  type GoalScopeType,
  type KeyResultInput,
} from "@multica/core/governance";
import { useWorkspaceId } from "@multica/core/hooks";
import { companyFloorsOptions } from "@multica/core/company";
import { initiativeListOptions } from "@multica/core/pipeline";
import { agentListOptions, memberListOptions, squadListOptions } from "@multica/core/workspace/queries";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Input } from "@multica/ui/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { useT } from "../i18n";

// The server's limits: a title in characters, a goal's why in bytes-ish characters.
const TITLE_MAX_CHARS = 200;
const KEY_RESULT_MAX = 10;

type OwnerType = CreateGoalRequest["owner_type"];
type ScopeChoice = Exclude<GoalScopeType, "squad">;
type KeyResultDraft = { key: number; title: string; metric: string; start: string; target: string };

/**
 * Creates a goal as a draft (spec 4.1). Only owners and admins reach it. The
 * server needs a scope target for a floor or an initiative, an owner and a
 * period; key results are optional, and the dialog asks for those, plus the why.
 */
export function GovernanceGoalCreateDialog({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const { t } = useT("governance");
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="flex max-h-[min(88dvh,48rem)] flex-col sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>{t(($) => $.goals.create.title)}</DialogTitle>
        </DialogHeader>
        <CreateGoalForm onCreated={() => onOpenChange(false)} />
      </DialogContent>
    </Dialog>
  );
}

// The form lives inside the dialog content, so each opening starts empty. The
// dialog closes only once the server has accepted the goal.
function CreateGoalForm({ onCreated }: { onCreated: () => void }) {
  const { t } = useT("governance");
  const wsId = useWorkspaceId();
  const create = useCreateGoal(wsId);
  const floors = useQuery(companyFloorsOptions(wsId));
  const initiatives = useQuery(initiativeListOptions(wsId));
  const squads = useQuery(squadListOptions(wsId));
  const agents = useQuery(agentListOptions(wsId));
  const members = useQuery(memberListOptions(wsId));
  const metrics = useQuery(metricsOptions(wsId));

  const [scope, setScope] = useState<ScopeChoice>("company");
  const [scopeId, setScopeId] = useState("");
  const [title, setTitle] = useState("");
  const [why, setWhy] = useState("");
  const [periodStart, setPeriodStart] = useState("");
  const [periodEnd, setPeriodEnd] = useState("");
  const [ownerType, setOwnerType] = useState<OwnerType | "">("");
  const [ownerId, setOwnerId] = useState("");
  const nextKey = useRef(1);
  const [keyResults, setKeyResults] = useState<KeyResultDraft[]>([]);
  const [submitted, setSubmitted] = useState(false);
  const [serverError, setServerError] = useState<string | null>(null);

  const scopeOptions = [
    { value: "company", label: t(($) => $.goals.scope.company) },
    { value: "floor", label: t(($) => $.goals.scope.floor) },
    { value: "initiative", label: t(($) => $.goals.scope.initiative) },
  ];
  const targets: { value: string; label: string }[] =
    scope === "floor"
      ? (floors.data ?? []).map((floor) => ({ value: floor.id, label: floor.name }))
      : scope === "initiative"
        ? (initiatives.data ?? []).map((item) => ({ value: item.id, label: `${item.identifier} ${item.title}` }))
        : [];
  const ownerOptions: { value: string; label: string }[] =
    ownerType === "squad"
      ? (squads.data ?? []).filter((s) => !s.archived_at).map((s) => ({ value: s.id, label: s.name }))
      : ownerType === "agent"
        ? (agents.data ?? []).filter((a) => !a.archived_at).map((a) => ({ value: a.id, label: a.name }))
        : ownerType === "member"
          ? (members.data ?? []).map((m) => ({ value: m.user_id, label: m.name }))
          : [];
  const metricOptions = (metrics.data ?? []).map((metric) => ({ value: metric.key, label: metric.name }));

  const trimmedTitle = title.trim();
  const errors = {
    title: !trimmedTitle
      ? t(($) => $.goals.create.title_required)
      : Array.from(trimmedTitle).length > TITLE_MAX_CHARS
        ? t(($) => $.goals.create.title_too_long)
        : null,
    target: scope !== "company" && !scopeId ? t(($) => $.goals.create.target_required) : null,
    period: !periodStart || !periodEnd
      ? t(($) => $.goals.create.period_required)
      : periodStart >= periodEnd
        ? t(($) => $.goals.create.period_order)
        : null,
    owner: !ownerType || !ownerId ? t(($) => $.goals.create.owner_required) : null,
  };
  const keyResultErrors = keyResults.map((kr) => {
    if (!kr.title.trim()) return t(($) => $.goals.create.kr_title_required);
    if (!kr.metric) return t(($) => $.goals.create.kr_metric_required);
    const start = Number(kr.start);
    const target = Number(kr.target);
    if (kr.start.trim() === "" || kr.target.trim() === "" || !Number.isFinite(start) || !Number.isFinite(target)) {
      return t(($) => $.goals.create.kr_values_required);
    }
    if (start === target) return t(($) => $.goals.create.kr_values_equal);
    return null;
  });
  const invalid =
    Boolean(errors.title || errors.target || errors.period || errors.owner || keyResultErrors.some(Boolean));

  const updateKeyResult = (key: number, patch: Partial<KeyResultDraft>) =>
    setKeyResults((rows) => rows.map((row) => (row.key === key ? { ...row, ...patch } : row)));

  const submit = (event: FormEvent) => {
    event.preventDefault();
    setSubmitted(true);
    if (invalid || !ownerType) return;
    setServerError(null);
    const request: CreateGoalRequest = {
      scope_type: scope,
      scope_id: scope === "company" ? undefined : scopeId,
      title: trimmedTitle,
      why: why.trim(),
      period_start: periodStart,
      period_end: periodEnd,
      owner_type: ownerType,
      owner_id: ownerId,
      key_results: keyResults.map(
        (kr): KeyResultInput => ({
          title: kr.title.trim(),
          metric_key: kr.metric,
          start_value: Number(kr.start),
          target_value: Number(kr.target),
        }),
      ),
    };
    create.mutate(request, {
      onSuccess: () => {
        toast.success(t(($) => $.goals.create.created));
        onCreated();
      },
      onError: () => setServerError(t(($) => $.goals.create.failed)),
    });
  };

  const shownError = (message: string | null) =>
    submitted && message ? (
      <p role="alert" className="-mt-2 text-caption text-destructive">
        {message}
      </p>
    ) : null;

  return (
    <form onSubmit={submit} noValidate className="flex min-h-0 flex-1 flex-col gap-4">
      <div className="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto pr-1">
        <div className="grid gap-4 sm:grid-cols-2">
          <div className="flex flex-col gap-1.5 text-body">
            <span>{t(($) => $.goals.create.scope)}</span>
            <Select
              items={scopeOptions}
              value={scope}
              onValueChange={(value) => {
                setScope((value as ScopeChoice | null) ?? "company");
                setScopeId("");
              }}
            >
              <SelectTrigger aria-label={t(($) => $.goals.create.scope)} className="w-full">
                <SelectValue>{scopeOptions.find((o) => o.value === scope)?.label}</SelectValue>
              </SelectTrigger>
              <SelectContent>
                {scopeOptions.map((option) => (
                  <SelectItem key={option.value} value={option.value}>
                    {option.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          {scope !== "company" ? (
            <div className="flex flex-col gap-1.5 text-body">
              <span>
                {scope === "floor"
                  ? t(($) => $.goals.create.target_floor)
                  : t(($) => $.goals.create.target_initiative)}
              </span>
              <Select items={targets} value={scopeId} onValueChange={(value) => setScopeId(value ?? "")}>
                <SelectTrigger
                  aria-label={scope === "floor" ? t(($) => $.goals.create.target_floor) : t(($) => $.goals.create.target_initiative)}
                  aria-invalid={submitted && errors.target ? true : undefined}
                  className="w-full"
                >
                  <SelectValue>{targets.find((o) => o.value === scopeId)?.label ?? ""}</SelectValue>
                </SelectTrigger>
                <SelectContent>
                  {targets.map((option) => (
                    <SelectItem key={option.value} value={option.value}>
                      {option.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          ) : null}
        </div>
        {shownError(errors.target)}

        <label className="flex flex-col gap-1.5 text-body">
          {t(($) => $.goals.create.title_label)}
          <Input
            value={title}
            aria-invalid={submitted && errors.title ? true : undefined}
            onChange={(event) => setTitle(event.target.value)}
          />
        </label>
        {shownError(errors.title)}

        <label className="flex flex-col gap-1.5 text-body">
          {t(($) => $.goals.create.why_label)}
          <Textarea value={why} rows={2} onChange={(event) => setWhy(event.target.value)} className="text-body text-foreground" />
        </label>

        <div className="grid gap-4 sm:grid-cols-2">
          <label className="flex flex-col gap-1.5 text-body">
            {t(($) => $.goals.create.period_start)}
            <Input
              type="date"
              value={periodStart}
              aria-invalid={submitted && errors.period ? true : undefined}
              onChange={(event) => setPeriodStart(event.target.value)}
            />
          </label>
          <label className="flex flex-col gap-1.5 text-body">
            {t(($) => $.goals.create.period_end)}
            <Input
              type="date"
              value={periodEnd}
              aria-invalid={submitted && errors.period ? true : undefined}
              onChange={(event) => setPeriodEnd(event.target.value)}
            />
          </label>
        </div>
        {shownError(errors.period)}

        <div className="grid gap-4 sm:grid-cols-2">
          <div className="flex flex-col gap-1.5 text-body">
            <span>{t(($) => $.goals.create.owner_type)}</span>
            <Select
              items={OWNER_TYPES.map((type) => ({ value: type, label: t(($) => $.goals.create.owner_types[type]) }))}
              value={ownerType}
              onValueChange={(value) => {
                setOwnerType((value as OwnerType | null) ?? "");
                setOwnerId("");
              }}
            >
              <SelectTrigger aria-label={t(($) => $.goals.create.owner_type)} className="w-full">
                <SelectValue>{ownerType ? t(($) => $.goals.create.owner_types[ownerType]) : ""}</SelectValue>
              </SelectTrigger>
              <SelectContent>
                {OWNER_TYPES.map((type) => (
                  <SelectItem key={type} value={type}>
                    {t(($) => $.goals.create.owner_types[type])}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <div className="flex flex-col gap-1.5 text-body">
            <span>{t(($) => $.goals.create.owner)}</span>
            <Select items={ownerOptions} value={ownerId} onValueChange={(value) => setOwnerId(value ?? "")} disabled={!ownerType}>
              <SelectTrigger aria-label={t(($) => $.goals.create.owner)} aria-invalid={submitted && errors.owner ? true : undefined} className="w-full">
                <SelectValue>{ownerOptions.find((o) => o.value === ownerId)?.label ?? ""}</SelectValue>
              </SelectTrigger>
              <SelectContent>
                {ownerOptions.map((option) => (
                  <SelectItem key={option.value} value={option.value}>
                    {option.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
        </div>
        {shownError(errors.owner)}

        <section className="flex flex-col gap-3" aria-label={t(($) => $.goals.create.key_results)}>
          <h3 className="text-caption font-medium text-muted-foreground">{t(($) => $.goals.create.key_results)}</h3>
          {metrics.isSuccess && metricOptions.length === 0 ? (
            <p role="alert" className="text-caption text-destructive">
              {t(($) => $.reading.no_metrics)}
            </p>
          ) : null}
          {keyResults.map((kr, index) => (
            <div key={kr.key} className="flex flex-col gap-2 rounded-md border p-3">
              <label className="flex flex-col gap-1.5 text-body">
                {t(($) => $.goals.create.kr_title)} {index + 1}
                <Input value={kr.title} onChange={(event) => updateKeyResult(kr.key, { title: event.target.value })} />
              </label>
              <div className="grid gap-3 sm:grid-cols-3">
                <div className="flex flex-col gap-1.5 text-body">
                  <span>{t(($) => $.goals.create.kr_metric)}</span>
                  <Select
                    items={metricOptions}
                    value={kr.metric}
                    onValueChange={(value) => updateKeyResult(kr.key, { metric: value ?? "" })}
                  >
                    <SelectTrigger aria-label={`${t(($) => $.goals.create.kr_metric)} ${index + 1}`} className="w-full">
                      <SelectValue>{metricOptions.find((o) => o.value === kr.metric)?.label ?? ""}</SelectValue>
                    </SelectTrigger>
                    <SelectContent>
                      {metricOptions.map((option) => (
                        <SelectItem key={option.value} value={option.value}>
                          {option.label}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
                <label className="flex flex-col gap-1.5 text-body">
                  {t(($) => $.goals.create.kr_start)}
                  <Input
                    inputMode="decimal"
                    value={kr.start}
                    onChange={(event) => updateKeyResult(kr.key, { start: event.target.value })}
                  />
                </label>
                <label className="flex flex-col gap-1.5 text-body">
                  {t(($) => $.goals.create.kr_target)}
                  <Input
                    inputMode="decimal"
                    value={kr.target}
                    onChange={(event) => updateKeyResult(kr.key, { target: event.target.value })}
                  />
                </label>
              </div>
              {submitted && keyResultErrors[index] ? (
                <p role="alert" className="text-caption text-destructive">
                  {keyResultErrors[index]}
                </p>
              ) : null}
              <div className="flex justify-end">
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  onClick={() => setKeyResults((rows) => rows.filter((row) => row.key !== kr.key))}
                >
                  {t(($) => $.goals.create.kr_remove)}
                </Button>
              </div>
            </div>
          ))}
          {keyResults.length < KEY_RESULT_MAX ? (
            <div>
              <Button
                type="button"
                variant="outline"
                size="sm"
                onClick={() => setKeyResults((rows) => [...rows, newKeyResult(nextKey.current++)])}
              >
                {t(($) => $.goals.create.kr_add)}
              </Button>
            </div>
          ) : null}
        </section>

        {serverError ? (
          <p role="alert" className="text-caption text-destructive">
            {serverError}
          </p>
        ) : null}
      </div>

      <DialogFooter>
        <DialogClose render={<Button type="button" variant="outline" />}>{t(($) => $.goals.create.cancel)}</DialogClose>
        <Button type="submit" disabled={create.isPending} aria-busy={create.isPending}>
          {t(($) => $.goals.create.submit)}
        </Button>
      </DialogFooter>
    </form>
  );
}

const OWNER_TYPES: readonly OwnerType[] = ["squad", "member", "agent"];

function newKeyResult(key: number): KeyResultDraft {
  return { key, title: "", metric: "", start: "", target: "" };
}
