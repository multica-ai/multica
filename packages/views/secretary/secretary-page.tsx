"use client";

import { useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { CalendarCheck, RefreshCw, Search } from "lucide-react";
import { api } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import { secretaryKeys, secretaryOptions } from "@multica/core/secretary/queries";
import {
  arrangeSecretaryItems,
  secretaryDay,
  type ArrangedItem,
} from "@multica/core/secretary/selectors";
import { Button } from "@multica/ui/components/ui/button";
import { useT } from "../i18n";
import { PageHeader } from "../layout/page-header";
import { SecretaryItemCard } from "./secretary-item";

type View = "today" | "matters" | "history";

interface SecretaryPageProps {
  onOriginalRecords: () => void;
  initialView?: View;
  originalLabel?: string;
}

export function SecretaryPage({
  onOriginalRecords,
  initialView = "today",
  originalLabel,
}: SecretaryPageProps) {
  const { t } = useT("issues");
  const workspaceId = useWorkspaceId();
  const query = useQuery(secretaryOptions(workspaceId));
  const data = query.data;
  const [view, setView] = useState<View>(initialView);
  const [search, setSearch] = useState("");
  const [capacityInput, setCapacityInput] = useState("");
  const client = useQueryClient();
  const today = new Intl.DateTimeFormat("sv-SE", { timeZone: "Asia/Shanghai" }).format(
    new Date(),
  );
  const items = useMemo(() => (data ? arrangeSecretaryItems(data) : []), [data]);
  const day = data ? secretaryDay(items, data, today) : null;
  const capacityMutation = useMutation({
    mutationFn: (minutes: number) =>
      api.saveSecretaryInstruction({
        request_id: crypto.randomUUID(),
        expected_revision: data?.revision ?? 0,
        item_key: "day",
        kind: "capacity",
        scheduled_on: today,
        capacity_minutes: minutes,
      }),
    onSuccess: () => client.invalidateQueries({ queryKey: secretaryKeys.all(workspaceId) }),
  });

  function cards(list: ArrangedItem[]) {
    return (
      <div className="space-y-3">
        {list.map((item) => (
          <SecretaryItemCard
            key={item.key}
            item={item}
            revision={data?.revision ?? 0}
            today={today}
          />
        ))}
      </div>
    );
  }

  const matching = (item: ArrangedItem) =>
    !search.trim() ||
    `${data?.projection?.cases.find((candidate) => candidate.id === item.case_id)?.title ?? ""} ${item.title} ${item.situation} ${item.next_step}`
      .toLocaleLowerCase()
      .includes(search.trim().toLocaleLowerCase());
  const inView = (item: ArrangedItem) =>
    view === "history"
      ? item.stage === "history" || item.kind !== "action"
      : item.stage !== "history" && item.kind === "action";
  const readyCount = day?.ready.length ?? 0;
  const active = items.filter((item) => item.kind === "action" && item.stage !== "history");
  const originalRecordsLabel =
    originalLabel ?? t(($) => $.secretary.all_original_records);

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <PageHeader>
        <CalendarCheck className="mr-2 size-4 text-muted-foreground" />
        <h1 className="text-sm font-medium">{t(($) => $.secretary.title)}</h1>
        <Button
          variant="ghost"
          size="sm"
          className="ml-auto"
          onClick={onOriginalRecords}
        >
          {originalRecordsLabel}
        </Button>
      </PageHeader>
      <div className="flex-1 overflow-auto">
        <div className="mx-auto max-w-5xl space-y-6 px-5 py-6 md:px-8">
          <div className="flex flex-wrap items-start justify-between gap-4">
            <div>
              <p className="mb-1 text-sm text-muted-foreground">{today}</p>
              <h2 className="text-2xl font-semibold tracking-tight">
                {t(($) => $.secretary.headline)}
              </h2>
              <p className="mt-2 text-sm text-muted-foreground">
                {t(($) => $.secretary.summary, {
                  ready: readyCount,
                  preparing: active.filter((item) => item.stage === "preparing").length,
                  waiting: active.filter((item) => item.stage === "waiting").length,
                })}
              </p>
            </div>
            <Button
              variant="outline"
              size="sm"
              onClick={() => query.refetch()}
              disabled={query.isFetching}
            >
              <RefreshCw className="mr-2 size-3.5" />
              {t(($) => $.secretary.refresh)}
            </Button>
          </div>

          {query.isPending && <p role="status">{t(($) => $.secretary.loading)}</p>}
          {(query.isError || (!query.isPending && !data)) && (
            <p role="alert" className="rounded-lg border p-4">
              {t(($) => $.secretary.load_error)}
            </p>
          )}
          {data && !data.projection && (
            <p role="status" className="rounded-lg border p-4">
              {t(($) => $.secretary.rebuilding)}
            </p>
          )}
          {!!data?.unmapped_count && (
            <p
              role="status"
              className="rounded-lg border border-amber-300 p-4 text-sm"
            >
              {t(($) => $.secretary.unmapped, { count: data.unmapped_count })}
            </p>
          )}

          {data?.projection && day && (
            <>
              <nav
                aria-label={t(($) => $.secretary.views_aria)}
                className="flex gap-2 border-b pb-3"
              >
                {(
                  [
                    ["today", t(($) => $.secretary.view_today)],
                    ["matters", t(($) => $.secretary.view_matters)],
                    ["history", t(($) => $.secretary.view_history)],
                  ] as const
                ).map(([value, label]) => (
                  <Button
                    key={value}
                    variant={view === value ? "default" : "ghost"}
                    onClick={() => setView(value)}
                    aria-pressed={view === value}
                  >
                    {label}
                  </Button>
                ))}
              </nav>

              {view === "today" ? (
                <>
                  <section
                    className="rounded-xl border bg-muted/20 p-5"
                    aria-label={t(($) => $.secretary.today_schedule_aria)}
                  >
                    <div className="flex flex-wrap items-center justify-between gap-4">
                      <div>
                        <h3 className="font-medium">
                          {t(($) => $.secretary.today_scheduled, {
                            count: day.today.length,
                          })}
                        </h3>
                        <p className="mt-1 text-sm text-muted-foreground">
                          {t(($) => $.secretary.minutes_estimated, {
                            minutes: day.minutes,
                          })}
                          {day.unestimated
                            ? t(($) => $.secretary.unestimated_suffix, {
                                count: day.unestimated,
                              })
                            : ""}
                          {day.capacity == null
                            ? t(($) => $.secretary.capacity_unknown)
                            : t(($) => $.secretary.capacity_known, {
                                minutes: day.capacity,
                              })}
                        </p>
                      </div>
                      <form
                        onSubmit={(event) => {
                          event.preventDefault();
                          capacityMutation.mutate(Number(capacityInput));
                        }}
                        className="flex items-center gap-2"
                      >
                        <label className="text-sm" htmlFor="secretary-capacity">
                          {t(($) => $.secretary.capacity_label)}
                        </label>
                        <input
                          id="secretary-capacity"
                          aria-label={t(($) => $.secretary.capacity_aria)}
                          type="number"
                          min={0}
                          max={1440}
                          required
                          value={capacityInput}
                          onChange={(event) => setCapacityInput(event.target.value)}
                          className="w-20 rounded-md border bg-background p-2 text-sm"
                        />
                        <Button
                          size="sm"
                          type="submit"
                          variant="outline"
                          disabled={capacityMutation.isPending}
                        >
                          {t(($) => $.secretary.confirm)}
                        </Button>
                      </form>
                    </div>
                    {day.overCapacity && (
                      <p
                        role="status"
                        className="mt-3 text-sm text-amber-700 dark:text-amber-400"
                      >
                        {t(($) => $.secretary.over_capacity, {
                          minutes: day.minutes - (day.capacity ?? 0),
                        })}
                      </p>
                    )}
                    {capacityMutation.isError && (
                      <p role="alert" className="mt-2 text-sm text-destructive">
                        {t(($) => $.secretary.capacity_error)}
                      </p>
                    )}
                  </section>

                  {day.completionReports.length > 0 && (
                    <details className="rounded-xl border p-4">
                      <summary className="cursor-pointer font-medium">
                        {t(($) => $.secretary.completion_reports, {
                          count: day.completionReports.length,
                        })}
                      </summary>
                      <div className="mt-4">{cards(day.completionReports)}</div>
                    </details>
                  )}
                  {day.completedToday.length > 0 && (
                    <details className="rounded-xl border p-4">
                      <summary className="cursor-pointer font-medium">
                        {t(($) => $.secretary.completed_today, {
                          count: day.completedToday.length,
                        })}
                      </summary>
                      <div className="mt-4">{cards(day.completedToday)}</div>
                    </details>
                  )}
                  {day.deadlines.length > 0 && (
                    <section className="space-y-3">
                      <h3 className="font-medium">
                        {t(($) => $.secretary.deadlines, { count: day.deadlines.length })}
                      </h3>
                      <p className="text-sm text-muted-foreground">
                        {t(($) => $.secretary.deadlines_hint)}
                      </p>
                      {cards(day.deadlines)}
                    </section>
                  )}
                  {(day.todayDetails.length > 0 || day.today.length === 0) && (
                    <section className="space-y-3">
                      <h3 className="font-medium">{t(($) => $.secretary.today_actions)}</h3>
                      {day.todayDetails.length ? (
                        cards(day.todayDetails)
                      ) : (
                        <p className="rounded-xl border border-dashed p-6 text-sm text-muted-foreground">
                          {day.completionReports.length
                            ? t(($) => $.secretary.today_empty_after_report)
                            : t(($) => $.secretary.today_empty)}
                        </p>
                      )}
                    </section>
                  )}
                  {day.overdueDetails.length > 0 && (
                    <section className="space-y-3">
                      <h3 className="font-medium">
                        {t(($) => $.secretary.overdue, {
                          count: day.overdueDetails.length,
                        })}
                      </h3>
                      <p className="text-sm text-muted-foreground">
                        {t(($) => $.secretary.overdue_hint)}
                      </p>
                      {cards(day.overdueDetails)}
                    </section>
                  )}
                  {day.priorityFollowups.length > 0 && (
                    <section className="rounded-xl border p-4">
                      <h3 className="font-medium">
                        {t(($) => $.secretary.priority_followups)}
                      </h3>
                      <p className="mt-1 text-sm text-muted-foreground">
                        {t(($) => $.secretary.priority_followups_hint)}
                      </p>
                      <ul className="mt-3 space-y-3">
                        {day.priorityFollowups.map((item) => (
                          <li key={item.key} className="text-sm">
                            <p className="font-medium">{item.title}</p>
                            <p className="mt-1 text-muted-foreground">{item.watch_reason}</p>
                          </li>
                        ))}
                      </ul>
                    </section>
                  )}
                  <details className="rounded-xl border p-4">
                    <summary className="cursor-pointer font-medium">
                      {t(($) => $.secretary.unplanned, {
                        count: day.unplannedDetails.length,
                      })}
                    </summary>
                    <div className="mt-4">{cards(day.unplannedDetails)}</div>
                  </details>
                  {day.followups.length > 0 && (
                    <details className="rounded-xl border p-4">
                      <summary className="cursor-pointer font-medium">
                        {t(($) => $.secretary.followups, { count: day.followups.length })}
                      </summary>
                      <div className="mt-4">{cards(day.followups)}</div>
                    </details>
                  )}
                </>
              ) : (
                <>
                  <label className="flex items-center gap-2 rounded-lg border px-3 py-2">
                    <Search className="size-4 text-muted-foreground" />
                    <input
                      aria-label={t(($) => $.secretary.search)}
                      placeholder={t(($) => $.secretary.search)}
                      value={search}
                      onChange={(event) => setSearch(event.target.value)}
                      className="w-full bg-transparent text-sm outline-none"
                    />
                  </label>
                  {data.projection.cases.map((matter) => {
                    const members = items.filter(
                      (item) => item.case_id === matter.id && matching(item) && inView(item),
                    );
                    if (!members.length) return null;
                    return (
                      <details
                        key={matter.id}
                        className="rounded-xl border p-5"
                        open={search ? true : undefined}
                      >
                        <summary className="cursor-pointer">
                          <span className="font-semibold">{matter.title}</span>
                          <span className="ml-3 text-sm text-muted-foreground">
                            {t(($) => $.secretary.item_count, { count: members.length })}
                          </span>
                          <span className="ml-3 text-xs text-muted-foreground">
                            {matter.area}
                          </span>
                        </summary>
                        <div className="mt-4">{cards(members)}</div>
                      </details>
                    );
                  })}
                  {search && !items.some((item) => matching(item) && inView(item)) && (
                    <p className="py-8 text-center text-sm text-muted-foreground">
                      {t(($) => $.secretary.no_search_results)}
                    </p>
                  )}
                </>
              )}
              <p className="border-t pt-4 text-xs text-muted-foreground">
                {t(($) => $.secretary.source_summary, {
                  count: items.length,
                  updated: data.projection.source_as_of.replace("T", " ").slice(0, 16),
                })}
              </p>
            </>
          )}
        </div>
      </div>
    </div>
  );
}
