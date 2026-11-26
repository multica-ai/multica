"use client";

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { CartesianGrid, Line, LineChart, XAxis, YAxis } from "recharts";
import { Plus, X } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import {
  ChartContainer,
  ChartTooltip,
  ChartTooltipContent,
  type ChartConfig,
} from "@multica/ui/components/ui/chart";
import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@multica/ui/components/ui/table";
import { useWorkspaceId } from "@multica/core/hooks";
import { contributionEventsOptions, type ContributionScope } from "@multica/core/dashboard";
import { issueRouteRef, useWorkspacePaths } from "@multica/core/paths";
import { projectListOptions } from "@multica/core/projects/queries";
import { useActorName } from "@multica/core/workspace/hooks";
import type { ContributionActorStats, ContributionSummary } from "@multica/core/types";
import { AppLink } from "../../../navigation";
import { useLocale, useT } from "../../../i18n";
import { Segmented } from "../dashboard-shared";
import { formatShortDate } from "./contribution-overview";
import { ActorLabel, Card, EmptyRow, LegendDot, Pager, StackBar } from "./contribution-shared";
import {
  STATUS_COLOR,
  actorKey,
  isStatusGroup,
  splitActors,
} from "./contribution-utils";

type MemberMetric = "participated" | "collaborated" | "delivered";
const TREND_COLORS = ["var(--chart-1)", "var(--warning)", "var(--chart-3)", "var(--success)", "var(--chart-5)"];
const MAX_TREND_MEMBERS = TREND_COLORS.length;
const EVENTS_PAGE = 20;

function MemberRows({ rows }: { rows: ContributionActorStats[] }) {
  return (
    <>
      {rows.map((m) => (
        <TableRow key={actorKey(m.actor_type, m.actor_id)}>
          <TableCell className="max-w-56">
            <ActorLabel actorType={m.actor_type} actorId={m.actor_id} />
          </TableCell>
          <TableCell className="min-w-40">
            <StackBar
              segments={[
                { value: m.created, color: "var(--chart-1)" },
                { value: m.commented, color: "var(--chart-3)" },
                { value: m.status_changed, color: "var(--warning)" },
              ]}
            />
          </TableCell>
          <TableCell className="text-right tabular-nums">{m.created}</TableCell>
          <TableCell className="text-right tabular-nums">{m.commented}</TableCell>
          <TableCell className="text-right tabular-nums">{m.status_changed}</TableCell>
          <TableCell className="text-right tabular-nums">{m.completed}</TableCell>
          <TableCell className="text-right font-medium tabular-nums">{m.participated}</TableCell>
        </TableRow>
      ))}
    </>
  );
}

function MemberTable({ summary }: { summary: ContributionSummary }) {
  const { t } = useT("usage");
  const { members, agents } = useMemo(() => splitActors(summary.actors), [summary.actors]);
  const groupRow = (label: string) => (
    <TableRow className="bg-muted/60 hover:bg-muted/60">
      <TableCell colSpan={7} className="py-1 text-caption font-medium text-muted-foreground">
        {label}
      </TableCell>
    </TableRow>
  );
  return (
    <Card
      flush
      title={t(($) => $.contribution.members.title)}
      aside={
        <>
          <LegendDot color="var(--chart-1)" label={t(($) => $.contribution.members.created)} />
          <LegendDot color="var(--chart-3)" label={t(($) => $.contribution.members.commented)} />
          <LegendDot color="var(--warning)" label={t(($) => $.contribution.members.status_changed)} />
        </>
      }
    >
      {members.length + agents.length === 0 ? (
        <EmptyRow>{t(($) => $.contribution.board.empty)}</EmptyRow>
      ) : (
        <>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t(($) => $.contribution.members.member)}</TableHead>
                <TableHead>{t(($) => $.contribution.members.composition)}</TableHead>
                <TableHead className="text-right">{t(($) => $.contribution.members.created)}</TableHead>
                <TableHead className="text-right">{t(($) => $.contribution.members.commented)}</TableHead>
                <TableHead className="text-right">{t(($) => $.contribution.members.status_changed)}</TableHead>
                <TableHead className="text-right">{t(($) => $.contribution.members.completed)}</TableHead>
                <TableHead className="text-right">{t(($) => $.contribution.members.participated)}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {members.length > 0 && groupRow(t(($) => $.contribution.members.people_group, { count: members.length }))}
              <MemberRows rows={members} />
              {agents.length > 0 && groupRow(t(($) => $.contribution.members.agents_group, { count: agents.length }))}
              <MemberRows rows={agents} />
            </TableBody>
          </Table>
          <p className="border-t px-4 py-2.5 text-caption text-muted-foreground">
            {t(($) => $.contribution.members.note)}
          </p>
        </>
      )}
    </Card>
  );
}

function MemberTrendCard({
  summary,
  selected,
  onSelectedChange,
}: {
  summary: ContributionSummary;
  selected: string[];
  onSelectedChange: (keys: string[]) => void;
}) {
  const { t } = useT("usage");
  const locale = useLocale();
  const { getActorName } = useActorName();
  const [metric, setMetric] = useState<MemberMetric>("participated");

  const nameOf = (key: string) => {
    const [type = "", id = ""] = key.split(":");
    return getActorName(type, id);
  };
  const series = selected.map((key, i) => ({ key, slot: `s${i}`, color: TREND_COLORS[i]! }));
  const config: ChartConfig = Object.fromEntries(series.map((s) => [s.slot, { label: nameOf(s.key), color: s.color }]));
  const data = summary.weekly.map((w) => {
    const point: Record<string, string | number> = { label: formatShortDate(w.date, locale) };
    for (const s of series) {
      const row = summary.actor_weekly.find((b) => b.week === w.date && actorKey(b.actor_type, b.actor_id) === s.key);
      point[s.slot] = row ? row[metric] : 0;
    }
    return point;
  });
  const candidates = summary.actors.filter((a) => a.actor_type === "member" || a.actor_type === "agent");

  return (
    <Card
      title={
        <span className="flex flex-wrap items-center gap-2">
          {t(($) => $.contribution.members.trend_title)}
          {series.map((s) => (
            <span key={s.key} className="inline-flex h-6 items-center gap-1.5 rounded-md bg-muted pr-0.5 pl-1.5 text-caption font-normal">
              <span aria-hidden className="size-2 rounded-full" style={{ background: s.color }} />
              {nameOf(s.key)}
              <button
                type="button"
                aria-label={t(($) => $.contribution.members.remove, { name: nameOf(s.key) })}
                className="inline-flex size-[18px] items-center justify-center rounded-sm text-muted-foreground hover:bg-background"
                onClick={() => onSelectedChange(selected.filter((k) => k !== s.key))}
              >
                <X className="size-3" />
              </button>
            </span>
          ))}
          <DropdownMenu>
            <DropdownMenuTrigger
              render={
                <Button variant="ghost" size="sm" className="h-6 gap-1 px-1.5 text-caption font-normal text-muted-foreground">
                  <Plus className="size-3.5" />
                  {t(($) => $.contribution.members.add_member)}
                </Button>
              }
            />
            <DropdownMenuContent align="start" className="max-h-72 min-w-52">
              {candidates.map((a) => {
                const key = actorKey(a.actor_type, a.actor_id);
                const checked = selected.includes(key);
                return (
                  <DropdownMenuCheckboxItem
                    key={key}
                    checked={checked}
                    disabled={!checked && selected.length >= MAX_TREND_MEMBERS}
                    onCheckedChange={(next) =>
                      onSelectedChange(next ? [...selected, key] : selected.filter((k) => k !== key))
                    }
                  >
                    <span className="truncate">{getActorName(a.actor_type, a.actor_id)}</span>
                  </DropdownMenuCheckboxItem>
                );
              })}
            </DropdownMenuContent>
          </DropdownMenu>
        </span>
      }
      aside={
        <Segmented
          label={t(($) => $.contribution.members.metric_label)}
          value={metric}
          onChange={setMetric}
          options={[
            { label: t(($) => $.contribution.members.metric_participated), value: "participated" },
            { label: t(($) => $.contribution.members.metric_collaborated), value: "collaborated" },
            { label: t(($) => $.contribution.members.metric_delivered), value: "delivered" },
          ]}
        />
      }
    >
      {series.length === 0 ? (
        <p className="flex aspect-[4/1] items-center justify-center rounded-md border border-dashed text-caption text-muted-foreground">
          {t(($) => $.contribution.members.pick_hint)}
        </p>
      ) : (
        <ChartContainer config={config} className="aspect-[4/1] w-full">
          <LineChart data={data} margin={{ left: 0, right: 8, top: 4, bottom: 0 }}>
            <CartesianGrid vertical={false} />
            <XAxis dataKey="label" tickLine={false} axisLine={false} tickMargin={8} interval="preserveStartEnd" />
            <YAxis tickLine={false} axisLine={false} tickMargin={8} allowDecimals={false} width="auto" />
            <ChartTooltip content={<ChartTooltipContent />} />
            {series.map((s) => (
              <Line key={s.slot} dataKey={s.slot} stroke={`var(--color-${s.slot})`} strokeWidth={2} dot={false} />
            ))}
          </LineChart>
        </ChartContainer>
      )}
    </Card>
  );
}

function ActivityTable({ scope, actors }: { scope: ContributionScope; actors: string[] }) {
  const { t } = useT("usage");
  const locale = useLocale();
  const wsId = useWorkspaceId();
  const wsPaths = useWorkspacePaths();
  const [offset, setOffset] = useState(0);
  const { data: projects = [] } = useQuery(projectListOptions(wsId));
  const projectName = (id: string) => projects.find((p) => p.id === id)?.title ?? t(($) => $.contribution.activity.no_project);
  const actorsKey = actors.join(",");
  // A changed filter starts the list over; derived during render so the
  // query never fires once with the stale offset.
  const [pageFor, setPageFor] = useState(actorsKey);
  if (pageFor !== actorsKey) {
    setPageFor(actorsKey);
    setOffset(0);
  }
  const { data } = useQuery(contributionEventsOptions(wsId, scope, { actors, limit: EVENTS_PAGE, offset }));
  const events = data?.events ?? [];
  const total = data?.total ?? 0;
  const timeFmt = new Intl.DateTimeFormat(locale, { timeZone: scope.tz, month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit" });

  return (
    <Card
      flush
      title={t(($) => $.contribution.activity.title)}
      aside={<span className="text-caption text-muted-foreground">{t(($) => $.contribution.activity.caption, { count: total })}</span>}
    >
      {events.length === 0 ? (
        <EmptyRow>{t(($) => $.contribution.activity.empty)}</EmptyRow>
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{t(($) => $.contribution.activity.date)}</TableHead>
              <TableHead>{t(($) => $.contribution.activity.actor)}</TableHead>
              <TableHead>{t(($) => $.contribution.activity.event)}</TableHead>
              <TableHead>{t(($) => $.contribution.activity.issue)}</TableHead>
              <TableHead>{t(($) => $.contribution.activity.issue_title)}</TableHead>
              <TableHead>{t(($) => $.contribution.activity.project)}</TableHead>
              <TableHead>{t(($) => $.contribution.activity.status)}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {events.map((e, i) => (
              <TableRow key={`${e.issue_id}-${e.at}-${e.kind}-${i}`}>
                <TableCell className="text-caption tabular-nums text-muted-foreground">{timeFmt.format(new Date(e.at))}</TableCell>
                <TableCell className="max-w-44">
                  <ActorLabel actorType={e.actor_type} actorId={e.actor_id} />
                </TableCell>
                <TableCell>
                  {e.kind === "created" || e.kind === "commented" || e.kind === "status_changed"
                    ? t(($) => $.contribution.kind[e.kind as "created" | "commented" | "status_changed"])
                    : e.kind}
                </TableCell>
                <TableCell className="text-caption tabular-nums">
                  <AppLink href={wsPaths.issueDetail(issueRouteRef({ id: e.issue_id, identifier: e.identifier || null }))} className="text-muted-foreground hover:text-foreground hover:underline">
                    {e.identifier}
                  </AppLink>
                </TableCell>
                <TableCell className="max-w-72 truncate">{e.title}</TableCell>
                <TableCell className="max-w-40 truncate">{e.project_id ? projectName(e.project_id) : t(($) => $.contribution.activity.no_project)}</TableCell>
                <TableCell>
                  <StatusLabel group={e.status_group} />
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}
      <Pager offset={offset} limit={EVENTS_PAGE} total={total} onChange={setOffset} />
    </Card>
  );
}

export function StatusLabel({ group }: { group: string }) {
  const { t } = useT("usage");
  if (!isStatusGroup(group)) return null;
  return (
    <span className="inline-flex items-center gap-1.5 whitespace-nowrap">
      <span aria-hidden className="size-2 rounded-full" style={{ background: STATUS_COLOR[group] }} />
      {t(($) => $.contribution.status[group])}
    </span>
  );
}

export function ContributionMembers({
  summary,
  scope,
}: {
  summary: ContributionSummary;
  scope: ContributionScope;
}) {
  // The two most active people and agents start selected so the trend and the
  // activity list are populated on first view.
  const defaults = useMemo(() => {
    const { members, agents } = splitActors(summary.actors);
    return [members[0], agents[0]]
      .filter((a): a is ContributionActorStats => !!a)
      .map((a) => actorKey(a.actor_type, a.actor_id));
  }, [summary.actors]);
  const [picked, setPicked] = useState<string[] | null>(null);
  const selected = picked ?? defaults;

  return (
    <div className="space-y-5">
      <MemberTable summary={summary} />
      <MemberTrendCard summary={summary} selected={selected} onSelectedChange={setPicked} />
      <ActivityTable scope={scope} actors={selected} />
    </div>
  );
}
