"use client";

import { useState, type FormEvent } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { ApiError } from "@multica/core/api";
import {
  BUDGET_HOLDABLE_CLASSES,
  BUDGET_MODES,
  BUDGET_PERIODS,
  useCreateBudget,
  type BudgetHoldableClass,
  type BudgetMode,
  type BudgetPeriod,
  type BudgetScopeType,
  type CreateBudgetRequest,
} from "@multica/core/governance";
import { useWorkspaceId } from "@multica/core/hooks";
import { companyFloorsOptions } from "@multica/core/company";
import { initiativeListOptions } from "@multica/core/pipeline";
import { squadListOptions } from "@multica/core/workspace/queries";
import { Button } from "@multica/ui/components/ui/button";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
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
import { useT } from "../i18n";

const SCOPE_CHOICES: readonly BudgetScopeType[] = ["company", "floor", "squad", "initiative"];

/**
 * Creates a budget for a scope and period (spec 4.2). A new budget counts the
 * scope's spend already made in the current period. Owners and admins reach it;
 * the server takes the scope target for every scope but the company's.
 */
export function GovernanceBudgetCreateDialog({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const { t } = useT("governance");
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="flex max-h-[min(88dvh,48rem)] flex-col sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{t(($) => $.budgets.create.title)}</DialogTitle>
        </DialogHeader>
        <CreateBudgetForm onCreated={() => onOpenChange(false)} />
      </DialogContent>
    </Dialog>
  );
}

// The form lives inside the dialog content, so each opening starts from the
// defaults. The dialog closes only once the server has kept the budget.
function CreateBudgetForm({ onCreated }: { onCreated: () => void }) {
  const { t } = useT("governance");
  const wsId = useWorkspaceId();
  const create = useCreateBudget(wsId);
  const floors = useQuery(companyFloorsOptions(wsId));
  const squads = useQuery(squadListOptions(wsId));
  const initiatives = useQuery(initiativeListOptions(wsId));

  const [scope, setScope] = useState<BudgetScopeType>("company");
  const [scopeId, setScopeId] = useState("");
  const [period, setPeriod] = useState<BudgetPeriod>("month");
  const [amount, setAmount] = useState("");
  const [softPercent, setSoftPercent] = useState("80");
  // A budget holds by default (spec 4.2); warn is an explicit choice.
  const [mode, setMode] = useState<BudgetMode>("hold");
  const [classes, setClasses] = useState<BudgetHoldableClass[]>([...BUDGET_HOLDABLE_CLASSES]);
  const [submitted, setSubmitted] = useState(false);
  const [serverError, setServerError] = useState<string | null>(null);

  const scopeLabel = (value: BudgetScopeType) => t(($) => $.budgets.scope[value]);
  const targets: { value: string; label: string }[] =
    scope === "floor"
      ? (floors.data ?? []).map((floor) => ({ value: floor.id, label: floor.name }))
      : scope === "squad"
        ? (squads.data ?? []).filter((squad) => !squad.archived_at).map((squad) => ({ value: squad.id, label: squad.name }))
        : scope === "initiative"
          ? (initiatives.data ?? []).map((item) => ({ value: item.id, label: `${item.identifier} ${item.title}` }))
          : [];

  const dollars = Number(amount);
  const soft = Number(softPercent);
  const errors = {
    target: scope !== "company" && !scopeId ? t(($) => $.budgets.create.target_required, { scope: scopeLabel(scope) }) : null,
    amount: amount.trim() === "" || !Number.isFinite(dollars) || dollars < 0 ? t(($) => $.budgets.create.amount_required) : null,
    soft: !Number.isInteger(soft) || soft < 1 || soft > 100 ? t(($) => $.budgets.create.soft_invalid) : null,
    classes: classes.length === 0 ? t(($) => $.budgets.create.classes_required) : null,
  };
  const invalid = Boolean(errors.target || errors.amount || errors.soft || errors.classes);

  const toggleClass = (value: BudgetHoldableClass, checked: boolean) =>
    setClasses((current) =>
      checked ? [...current.filter((item) => item !== value), value] : current.filter((item) => item !== value),
    );

  const submit = (event: FormEvent) => {
    event.preventDefault();
    setSubmitted(true);
    if (invalid) return;
    setServerError(null);
    const request: CreateBudgetRequest = {
      scope_type: scope,
      scope_id: scope === "company" ? undefined : scopeId,
      period,
      amount_micros: Math.round(dollars * 1_000_000),
      soft_percent: soft,
      mode,
      classes,
    };
    create.mutate(request, {
      onSuccess: () => {
        toast.success(t(($) => $.budgets.create.created));
        onCreated();
      },
      onError: (error) =>
        setServerError(
          error instanceof ApiError && error.status === 409
            ? t(($) => $.budgets.create.exists)
            : t(($) => $.budgets.create.failed),
        ),
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
        <div className="flex flex-col gap-1.5 text-body">
          <span>{t(($) => $.budgets.create.scope)}</span>
          <Select
            items={SCOPE_CHOICES.map((value) => ({ value, label: scopeLabel(value) }))}
            value={scope}
            onValueChange={(value) => {
              setScope((value as BudgetScopeType | null) ?? "company");
              setScopeId("");
            }}
          >
            <SelectTrigger aria-label={t(($) => $.budgets.create.scope)} className="w-full">
              <SelectValue>{scopeLabel(scope)}</SelectValue>
            </SelectTrigger>
            <SelectContent>
              {SCOPE_CHOICES.map((value) => (
                <SelectItem key={value} value={value}>
                  {scopeLabel(value)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>

        {scope !== "company" ? (
          <div className="flex flex-col gap-1.5 text-body">
            <span>{scopeLabel(scope)}</span>
            <Select items={targets} value={scopeId} onValueChange={(value) => setScopeId(value ?? "")}>
              <SelectTrigger
                aria-label={scopeLabel(scope)}
                aria-invalid={submitted && errors.target ? true : undefined}
                className="w-full"
              >
                <SelectValue>{targets.find((item) => item.value === scopeId)?.label ?? ""}</SelectValue>
              </SelectTrigger>
              <SelectContent>
                {targets.map((item) => (
                  <SelectItem key={item.value} value={item.value}>
                    {item.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
        ) : null}
        {shownError(errors.target)}

        <div className="grid gap-4 sm:grid-cols-2">
          <div className="flex flex-col gap-1.5 text-body">
            <span>{t(($) => $.budgets.create.period)}</span>
            <Select
              items={BUDGET_PERIODS.map((value) => ({ value, label: t(($) => $.budgets.period[value]) }))}
              value={period}
              onValueChange={(value) => setPeriod((value as BudgetPeriod | null) ?? "month")}
            >
              <SelectTrigger aria-label={t(($) => $.budgets.create.period)} className="w-full">
                <SelectValue>{t(($) => $.budgets.period[period])}</SelectValue>
              </SelectTrigger>
              <SelectContent>
                {BUDGET_PERIODS.map((value) => (
                  <SelectItem key={value} value={value}>
                    {t(($) => $.budgets.period[value])}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <label className="flex flex-col gap-1.5 text-body">
            {t(($) => $.budgets.create.amount)}
            <Input
              inputMode="decimal"
              value={amount}
              aria-invalid={submitted && errors.amount ? true : undefined}
              onChange={(event) => setAmount(event.target.value)}
            />
          </label>
        </div>
        {shownError(errors.amount)}

        <div className="grid gap-4 sm:grid-cols-2">
          <label className="flex flex-col gap-1.5 text-body">
            {t(($) => $.budgets.create.soft_percent)}
            <Input
              type="number"
              inputMode="numeric"
              min={1}
              max={100}
              value={softPercent}
              aria-invalid={submitted && errors.soft ? true : undefined}
              onChange={(event) => setSoftPercent(event.target.value)}
            />
          </label>
          <div className="flex flex-col gap-1.5 text-body">
            <span>{t(($) => $.budgets.create.mode)}</span>
            <Select
              items={BUDGET_MODES.map((value) => ({ value, label: t(($) => $.budgets.mode[value]) }))}
              value={mode}
              onValueChange={(value) => setMode((value as BudgetMode | null) ?? "hold")}
            >
              <SelectTrigger aria-label={t(($) => $.budgets.create.mode)} className="w-full">
                <SelectValue>{t(($) => $.budgets.mode[mode])}</SelectValue>
              </SelectTrigger>
              <SelectContent>
                {BUDGET_MODES.map((value) => (
                  <SelectItem key={value} value={value}>
                    {t(($) => $.budgets.mode[value])}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
        </div>
        {shownError(errors.soft)}

        <fieldset className="flex flex-col gap-2 text-body">
          <legend className="text-body">{t(($) => $.budgets.create.classes)}</legend>
          {BUDGET_HOLDABLE_CLASSES.map((value) => (
            <label key={value} className="flex items-center gap-2">
              <Checkbox
                checked={classes.includes(value)}
                onCheckedChange={(checked) => toggleClass(value, checked)}
              />
              {t(($) => $.budgets.create.class[value])}
            </label>
          ))}
        </fieldset>
        {shownError(errors.classes)}

        {serverError ? (
          <p role="alert" className="text-caption text-destructive">
            {serverError}
          </p>
        ) : null}
      </div>

      <DialogFooter>
        <DialogClose render={<Button type="button" variant="outline" />}>{t(($) => $.budgets.create.cancel)}</DialogClose>
        <Button type="submit" disabled={create.isPending} aria-busy={create.isPending}>
          {t(($) => $.budgets.create.submit)}
        </Button>
      </DialogFooter>
    </form>
  );
}
