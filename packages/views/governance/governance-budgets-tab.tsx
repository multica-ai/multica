"use client";

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  BUDGET_SCOPE_TYPES,
  budgetHoldsOptions,
  budgetPace,
  budgetPeriodsOptions,
  budgetsOptions,
  canGovern,
  useExtendBudget,
  useReleaseBudgetHolds,
  useUpdateBudget,
  type Budget,
  type BudgetHold,
  type BudgetScopeType,
} from "@multica/core/governance";
import { useWorkspaceId } from "@multica/core/hooks";
import { useCurrentMember } from "@multica/core/permissions";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Plus } from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";
import { PAGE_GUTTER } from "../layout/page-header";
import { useT } from "../i18n";
import { GovernanceBudgetCreateDialog } from "./governance-budget-create-dialog";
import { GovernancePlaceholder } from "./governance-placeholder";

/** Dollars from millionths, with the currency's own grouping. */
export function formatBudgetUsd(micros: number): string {
  return new Intl.NumberFormat(undefined, { style: "currency", currency: "USD", maximumFractionDigits: 2 }).format(
    micros / 1_000_000,
  );
}

function formatDay(iso: string | null | undefined): string {
  if (!iso) return "";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "";
  return new Intl.DateTimeFormat(undefined, { month: "short", day: "numeric" }).format(d);
}

/**
 * The Budgets tab (spec 4.2): every budget grouped by scope, its spend against
 * the limit with the elapsed share of the period marked and the projected
 * overrun date, the runs held for it, and its history by period. Only an owner
 * or an admin changes a budget; a member reads the same figures.
 */
export function GovernanceBudgetsTab() {
  const { t } = useT("governance");
  const wsId = useWorkspaceId();
  const { role } = useCurrentMember(wsId);
  const editable = canGovern(role);
  const budgets = useQuery(budgetsOptions(wsId));
  const holds = useQuery(budgetHoldsOptions(wsId));
  const [openBudget, setOpenBudget] = useState<string | null>(null);
  const [creating, setCreating] = useState(false);

  const list = useMemo(() => budgets.data?.budgets ?? [], [budgets.data]);
  const grouped = useMemo(
    () => BUDGET_SCOPE_TYPES.map((scope) => ({ scope, items: list.filter((b) => b.scope_type === scope) })),
    [list],
  );
  const holdsByBudget = useMemo(() => {
    const map = new Map<string, BudgetHold[]>();
    for (const h of holds.data ?? []) {
      map.set(h.budget_id, [...(map.get(h.budget_id) ?? []), h]);
    }
    return map;
  }, [holds.data]);

  // Owners and admins create budgets here; a company with none yet still shows the button.
  const createControls = editable ? (
    <>
      <div className="flex justify-end">
        <Button type="button" size="sm" onClick={() => setCreating(true)}>
          <Plus />
          {t(($) => $.budgets.create.button)}
        </Button>
      </div>
      <GovernanceBudgetCreateDialog open={creating} onOpenChange={setCreating} />
    </>
  ) : null;

  if (budgets.isSuccess && list.length === 0) {
    return (
      <div className={cn(PAGE_GUTTER, "flex flex-col gap-4 py-4")}>
        {createControls}
        <GovernancePlaceholder />
      </div>
    );
  }

  return (
    <div className={cn(PAGE_GUTTER, "space-y-6 py-4")}>
      {createControls}
      {!editable && <p className="text-muted-foreground text-sm">{t(($) => $.budgets.read_only)}</p>}
      {grouped.map(({ scope, items }) =>
        items.length === 0 ? null : (
          <section key={scope} className="space-y-2">
            <h2 className="text-sm font-medium">{t(($) => $.budgets.scope[scope])}</h2>
            <ul className="space-y-3">
              {items.map((b) => (
                <BudgetRow
                  key={b.id}
                  budget={b}
                  scope={scope}
                  wsId={wsId}
                  editable={editable}
                  open={openBudget === b.id}
                  onToggleHistory={() => setOpenBudget(openBudget === b.id ? null : b.id)}
                  holdCount={holdsByBudget.get(b.id)?.length ?? 0}
                />
              ))}
            </ul>
          </section>
        ),
      )}
      <HoldsSection wsId={wsId} editable={editable} holds={holds.data ?? []} budgets={list} />
    </div>
  );
}

function BudgetRow({
  budget,
  scope,
  wsId,
  editable,
  open,
  onToggleHistory,
  holdCount,
}: {
  budget: Budget;
  scope: BudgetScopeType;
  wsId: string;
  editable: boolean;
  open: boolean;
  onToggleHistory: () => void;
  holdCount: number;
}) {
  const { t } = useT("governance");
  const pace = budgetPace(budget, new Date());
  const limit = budget.limit_micros;
  const width = Math.min(100, Math.round(pace.used * 100));
  const elapsedAt = Math.round(pace.elapsed * 100);
  const update = useUpdateBudget(wsId);
  const [amount, setAmount] = useState("");
  const scopeLabel = budget.scope_id
    ? `${t(($) => $.budgets.scope[scope])} ${budget.scope_id.slice(0, 8)}`
    : t(($) => $.budgets.scope.company);

  const save = () => {
    const dollars = Number(amount);
    if (!Number.isFinite(dollars) || dollars < 0) return;
    update.mutate(
      { id: budget.id, patch: { amount_micros: Math.round(dollars * 1_000_000) } },
      {
        onSuccess: () => setAmount(""),
        onError: () => toast.error(t(($) => $.budgets.save_failed)),
      },
    );
  };

  return (
    <li className="rounded-md border p-3">
      <div className="flex flex-wrap items-center gap-2">
        <span className="font-medium">{scopeLabel}</span>
        <span className="text-muted-foreground text-xs">
          {t(($) => $.budgets.period[budget.period])} · {t(($) => $.budgets.mode[budget.mode])}
        </span>
        <span className="ml-auto text-xs" data-testid="budget-state">
          {t(($) => $.budgets.state[budget.state])}
        </span>
      </div>
      <div
        className="bg-muted relative mt-2 h-2 w-full overflow-hidden rounded"
        role="progressbar"
        aria-label={t(($) => $.budgets.spent_label)}
        aria-valuemin={0}
        aria-valuemax={100}
        aria-valuenow={Math.min(100, width)}
      >
        <div className="bg-primary h-full" style={{ width: `${width}%` }} />
        <div className="bg-foreground/60 absolute top-0 h-full w-px" style={{ left: `${elapsedAt}%` }} data-testid="budget-elapsed" />
      </div>
      <p className="text-muted-foreground mt-1 text-xs">
        {t(($) => $.budgets.spent, { spent: formatBudgetUsd(budget.spent_micros), limit: formatBudgetUsd(limit) })} ·{" "}
        {t(($) => $.budgets.elapsed, { percent: elapsedAt })}
      </p>
      <p className="text-muted-foreground text-xs">
        {pace.overrunAt
          ? t(($) => $.budgets.overrun, { date: formatDay(pace.overrunAt.toISOString()) })
          : t(($) => $.budgets.no_overrun)}
        {holdCount > 0 ? ` · ${t(($) => $.budgets.holds_count, { count: holdCount })}` : ""}
      </p>
      <div className="mt-2 flex flex-wrap items-center gap-2">
        <Button size="sm" variant="ghost" onClick={onToggleHistory}>
          {t(($) => $.budgets.history)}
        </Button>
        {editable && (
          <>
            <Input
              aria-label={t(($) => $.budgets.amount_label)}
              className="h-8 w-32"
              inputMode="decimal"
              placeholder={t(($) => $.budgets.amount_placeholder)}
              value={amount}
              onChange={(e) => setAmount(e.target.value)}
            />
            <Button size="sm" variant="outline" disabled={!amount || update.isPending} onClick={save}>
              {t(($) => $.budgets.set_amount)}
            </Button>
          </>
        )}
      </div>
      {open && <BudgetHistory wsId={wsId} budget={budget} />}
    </li>
  );
}

function BudgetHistory({ wsId, budget }: { wsId: string; budget: Budget }) {
  const { t } = useT("governance");
  const periods = useQuery(budgetPeriodsOptions(wsId, budget.id));
  const rows = periods.data ?? [];
  if (periods.isSuccess && rows.length === 0) {
    return <p className="text-muted-foreground mt-2 text-xs">{t(($) => $.budgets.history_empty)}</p>;
  }
  return (
    <table className="mt-2 w-full text-xs" aria-label={t(($) => $.budgets.history)}>
      <thead className="text-muted-foreground text-left">
        <tr>
          <th className="py-1 font-normal">{t(($) => $.budgets.columns.period)}</th>
          <th className="py-1 font-normal">{t(($) => $.budgets.columns.spent)}</th>
          <th className="py-1 font-normal">{t(($) => $.budgets.columns.extended)}</th>
          <th className="py-1 font-normal">{t(($) => $.budgets.columns.runs)}</th>
          <th className="py-1 font-normal">{t(($) => $.budgets.columns.state)}</th>
        </tr>
      </thead>
      <tbody>
        {rows.map((p) => (
          <tr key={p.period_start} className="border-t">
            <td className="py-1">
              {formatDay(p.period_start)} – {formatDay(p.period_end)}
            </td>
            <td className="py-1">{formatBudgetUsd(p.spent_micros)}</td>
            <td className="py-1">{formatBudgetUsd(p.extended_micros)}</td>
            <td className="py-1">{p.runs}</td>
            <td className="py-1">{t(($) => $.budgets.state[p.state])}</td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}

function HoldsSection({
  wsId,
  editable,
  holds,
  budgets,
}: {
  wsId: string;
  editable: boolean;
  holds: BudgetHold[];
  budgets: Budget[];
}) {
  const { t } = useT("governance");
  const release = useReleaseBudgetHolds(wsId);
  const extend = useExtendBudget(wsId);
  const [amounts, setAmounts] = useState<Record<string, string>>({});

  const byBudget = new Map<string, BudgetHold[]>();
  for (const h of holds) byBudget.set(h.budget_id, [...(byBudget.get(h.budget_id) ?? []), h]);

  if (byBudget.size === 0) {
    return (
      <section className="space-y-2">
        <h2 className="text-sm font-medium">{t(($) => $.budgets.holds_title)}</h2>
        <p className="text-muted-foreground text-sm">{t(($) => $.budgets.holds_empty)}</p>
      </section>
    );
  }

  return (
    <section className="space-y-2">
      <h2 className="text-sm font-medium">{t(($) => $.budgets.holds_title)}</h2>
      <p className="text-muted-foreground text-xs">{t(($) => $.budgets.release_hint)}</p>
      <ul className="space-y-2">
        {[...byBudget.entries()].map(([budgetId, items]) => {
          const budget = budgets.find((b) => b.id === budgetId);
          const amount = amounts[budgetId] ?? "";
          return (
            <li key={budgetId} className="flex flex-wrap items-center gap-2 rounded-md border p-3 text-sm">
              <span>{t(($) => $.budgets.holds_count, { count: items.length })}</span>
              <span className="text-muted-foreground text-xs">
                {budget ? t(($) => $.budgets.scope[budget.scope_type]) : ""}
              </span>
              {editable && (
                <>
                  <Button
                    size="sm"
                    variant="outline"
                    disabled={release.isPending}
                    onClick={() =>
                      release.mutate(
                        { id: budgetId },
                        {
                          onSuccess: (res) => toast.success(t(($) => $.budgets.released, { count: res?.released ?? 0 })),
                          onError: () => toast.error(t(($) => $.budgets.save_failed)),
                        },
                      )
                    }
                  >
                    {t(($) => $.budgets.release)}
                  </Button>
                  <Input
                    aria-label={t(($) => $.budgets.extend_amount)}
                    className="h-8 w-32"
                    inputMode="decimal"
                    placeholder={t(($) => $.budgets.amount_placeholder)}
                    value={amount}
                    onChange={(e) => setAmounts({ ...amounts, [budgetId]: e.target.value })}
                  />
                  <Button
                    size="sm"
                    variant="outline"
                    disabled={!amount || extend.isPending}
                    onClick={() => {
                      const dollars = Number(amount);
                      if (!Number.isFinite(dollars) || dollars <= 0) return;
                      extend.mutate(
                        { id: budgetId, amount_micros: Math.round(dollars * 1_000_000) },
                        {
                          onSuccess: () => {
                            setAmounts({ ...amounts, [budgetId]: "" });
                            toast.success(t(($) => $.budgets.extended));
                          },
                          onError: () => toast.error(t(($) => $.budgets.save_failed)),
                        },
                      );
                    }}
                  >
                    {t(($) => $.budgets.extend)}
                  </Button>
                </>
              )}
            </li>
          );
        })}
      </ul>
    </section>
  );
}
