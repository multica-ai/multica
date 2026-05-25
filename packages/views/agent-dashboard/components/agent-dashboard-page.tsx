"use client";

import { useEffect, useMemo, useState } from "react";
import {
  Activity,
  ArrowDown,
  ArrowUp,
  Bot,
  CalendarDays,
  CheckCircle2,
  Clock3,
  Eye,
  Filter,
  ListFilter,
  RefreshCw,
  UserRound,
  Users,
  XCircle,
} from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import {
  Bar,
  BarChart,
  CartesianGrid,
  Cell,
  Line,
  LineChart,
  Pie,
  PieChart,
  ReferenceLine,
  XAxis,
  YAxis,
} from "recharts";
import { agentListOptions, memberListOptions } from "@multica/core/workspace/queries";
import {
  agentRunDashboardOptions,
  agentRunDashboardRunDetailOptions,
} from "@multica/core/agent-dashboard";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import type {
  Agent,
  AgentRunDashboardAgent,
  AgentRunDashboardDaily,
  AgentRunDashboardFailureReason,
  AgentRunDashboardHeatmapCell,
  AgentRunDashboardRun,
  MemberWithUser,
} from "@multica/core/types";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import {
  ChartContainer,
  ChartTooltip,
  ChartTooltipContent,
  type ChartConfig,
} from "@multica/ui/components/ui/chart";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@multica/ui/components/ui/popover";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@multica/ui/components/ui/table";
import { ActorAvatar } from "../../common/actor-avatar";
import { PageHeader } from "../../layout/page-header";
import { AppLink, useNavigation } from "../../navigation";
import { KpiCard } from "../../runtimes/components/shared";
import { useT } from "../../i18n";

const TIME_RANGES = [7, 14, 30] as const;
type TimeRange = (typeof TIME_RANGES)[number];
type AgentSortKey =
  | "agent"
  | "project"
  | "runs"
  | "success"
  | "duration"
  | "lastRun"
  | "status";

const SUCCESS_WARNING_THRESHOLD = 0.8;
const PAGE_SIZE = 20;

const EMPTY_AGENTS: Agent[] = [];
const EMPTY_MEMBERS: MemberWithUser[] = [];
const EMPTY_DAILY: AgentRunDashboardDaily[] = [];
const EMPTY_HEATMAP: AgentRunDashboardHeatmapCell[] = [];
const EMPTY_REASONS: AgentRunDashboardFailureReason[] = [];
const EMPTY_RUNS: AgentRunDashboardRun[] = [];

const runTrendConfig = {
  total_runs: { label: "Runs", color: "var(--chart-1)" },
} satisfies ChartConfig;

const successTrendConfig = {
  success: { label: "Success", color: "var(--chart-2)" },
} satisfies ChartConfig;

const workdayConfig = {
  runs: { label: "Runs", color: "var(--chart-1)" },
} satisfies ChartConfig;

const retryConfig = {
  count: { label: "Runs", color: "var(--chart-3)" },
} satisfies ChartConfig;

const PIE_COLORS = [
  "var(--chart-5)",
  "var(--chart-4)",
  "var(--chart-3)",
  "var(--chart-2)",
  "var(--chart-1)",
];

function defaultTimezone(): string {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC";
  } catch {
    return "UTC";
  }
}

function parseTimeRange(raw: string | null): TimeRange {
  const parsed = Number(raw);
  return TIME_RANGES.includes(parsed as TimeRange) ? (parsed as TimeRange) : 30;
}

function parseHour(raw: string | null, fallback: number): number {
  const parsed = Number(raw);
  return Number.isInteger(parsed) && parsed >= 0 && parsed <= 23 ? parsed : fallback;
}

function parseSortKey(raw: string | null): AgentSortKey {
  const keys: AgentSortKey[] = [
    "agent",
    "project",
    "runs",
    "success",
    "duration",
    "lastRun",
    "status",
  ];
  return keys.includes(raw as AgentSortKey) ? (raw as AgentSortKey) : "runs";
}

function parseSelectedAgents(raw: string | null): string[] {
  if (!raw) return [];
  return raw.split(",").map((v) => v.trim()).filter(Boolean);
}

function formatPercent(value: number): string {
  return `${Math.round(value * 100)}%`;
}

function formatDuration(seconds: number): string {
  if (!Number.isFinite(seconds) || seconds <= 0) return "-";
  if (seconds < 60) return `${Math.round(seconds)}s`;
  const minutes = Math.floor(seconds / 60);
  const hours = Math.floor(minutes / 60);
  if (hours <= 0) return `${minutes}m`;
  const mins = minutes % 60;
  return mins > 0 ? `${hours}h ${mins}m` : `${hours}h`;
}

function formatDateTime(value: string | null): string {
  if (!value) return "-";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "-";
  return date.toLocaleString(undefined, {
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  });
}

function projectLabel(row: AgentRunDashboardAgent): string {
  if (row.project_count > 1) return `${row.project_count} projects`;
  return row.project_title ?? "No project";
}

function runIssueLabel(run: AgentRunDashboardRun): string {
  if (run.issue_number) return `#${run.issue_number} ${run.issue_title ?? ""}`.trim();
  return run.issue_title ?? "No issue";
}

function failureReasonLabel(reason: string): string {
  switch (reason) {
    case "http_503":
      return "503";
    case "timeout":
      return "Timeout";
    case "permission":
      return "Permission";
    case "invalid_request":
    case "api_invalid_request":
      return "Invalid request";
    case "runtime_offline":
      return "Runtime offline";
    case "runtime_recovery":
      return "Runtime recovery";
    case "manual":
      return "Manual";
    default:
      return reason ? reason.replaceAll("_", " ") : "Agent error";
  }
}

function statusVariant(status: string): "default" | "secondary" | "outline" | "destructive" {
  if (status === "failed") return "destructive";
  if (status === "completed") return "secondary";
  if (status === "running" || status === "dispatched") return "default";
  return "outline";
}

function statusLabel(status: string): string {
  switch (status) {
    case "completed":
      return "Completed";
    case "failed":
      return "Failed";
    case "running":
      return "Running";
    case "dispatched":
      return "Dispatched";
    case "queued":
      return "Queued";
    case "cancelled":
      return "Cancelled";
    case "working":
      return "Working";
    case "idle":
      return "Idle";
    case "offline":
      return "Offline";
    default:
      return status;
  }
}

function dateLabel(date: string): string {
  const parsed = new Date(`${date}T00:00:00Z`);
  if (Number.isNaN(parsed.getTime())) return date;
  return `${parsed.getMonth() + 1}/${parsed.getDate()}`;
}

function compareMaybeString(a: string | null | undefined, b: string | null | undefined) {
  return (a ?? "").localeCompare(b ?? "");
}

function successSortValue(row: AgentRunDashboardAgent): number {
  return row.successful_runs + row.failed_runs > 0 ? row.success_rate : -1;
}

function sortAgents(
  rows: AgentRunDashboardAgent[],
  sortKey: AgentSortKey,
  sortDir: "asc" | "desc",
) {
  const sorted = [...rows].sort((a, b) => {
    switch (sortKey) {
      case "agent":
        return a.agent_name.localeCompare(b.agent_name);
      case "project":
        return projectLabel(a).localeCompare(projectLabel(b));
      case "success":
        return successSortValue(a) - successSortValue(b);
      case "duration":
        return a.average_duration_seconds - b.average_duration_seconds;
      case "lastRun":
        return compareMaybeString(a.last_run_at, b.last_run_at);
      case "status":
        return a.agent_status.localeCompare(b.agent_status);
      case "runs":
      default:
        return a.total_runs - b.total_runs;
    }
  });
  return sortDir === "asc" ? sorted : sorted.reverse();
}

function workdayWeekendData(daily: AgentRunDashboardDaily[]) {
  let workday = 0;
  let weekend = 0;
  for (const row of daily) {
    const date = new Date(`${row.date}T00:00:00Z`);
    const day = date.getDay();
    if (day === 0 || day === 6) weekend += row.total_runs;
    else workday += row.total_runs;
  }
  return [
    { label: "Workday", runs: workday },
    { label: "Weekend", runs: weekend },
  ];
}

function buildTrendRows(daily: AgentRunDashboardDaily[]) {
  return daily.map((row) => ({
    ...row,
    label: dateLabel(row.date),
    success: Math.round(row.success_rate * 100),
  }));
}

export function ownerFilterDisplayLabel({
  selectedOwnerId,
  selectedMember,
  allOwnersLabel,
  selectedOwnerFallback,
}: {
  selectedOwnerId: string | null;
  selectedMember: Pick<MemberWithUser, "name" | "email"> | null;
  allOwnersLabel: string;
  selectedOwnerFallback: string;
}): string {
  if (!selectedOwnerId) return allOwnersLabel;
  return selectedMember?.name || selectedMember?.email || selectedOwnerFallback;
}

function useDashboardUrlState() {
  const navigation = useNavigation();
  const initial = navigation.searchParams;
  const pathname = navigation.pathname;
  const currentSearch = navigation.searchParams.toString();
  const replace = navigation.replace;
  const [days, setDays] = useState<TimeRange>(() => parseTimeRange(initial.get("days")));
  const [selectedAgentIds, setSelectedAgentIds] = useState<string[]>(() =>
    parseSelectedAgents(initial.get("agents")),
  );
  const [selectedOwnerId, setSelectedOwnerId] = useState<string | null>(() =>
    initial.get("owner")?.trim() || null,
  );
  const [startHour, setStartHour] = useState(() => parseHour(initial.get("start_hour"), 0));
  const [endHour, setEndHour] = useState(() => parseHour(initial.get("end_hour"), 23));
  const [sortKey, setSortKey] = useState<AgentSortKey>(() => parseSortKey(initial.get("sort")));
  const [sortDir, setSortDir] = useState<"asc" | "desc">(() =>
    initial.get("dir") === "asc" ? "asc" : "desc",
  );
  const [page, setPage] = useState(() => Math.max(1, Number(initial.get("page") ?? "1") || 1));

  useEffect(() => {
    const params = new URLSearchParams();
    if (days !== 30) params.set("days", String(days));
    if (selectedOwnerId) params.set("owner", selectedOwnerId);
    if (selectedAgentIds.length > 0) params.set("agents", selectedAgentIds.join(","));
    if (startHour !== 0) params.set("start_hour", String(startHour));
    if (endHour !== 23) params.set("end_hour", String(endHour));
    if (sortKey !== "runs") params.set("sort", sortKey);
    if (sortDir !== "desc") params.set("dir", sortDir);
    if (page > 1) params.set("page", String(page));
    const next = params.toString() ? `${pathname}?${params.toString()}` : pathname;
    const current = currentSearch ? `${pathname}?${currentSearch}` : pathname;
    if (next !== current) replace(next);
  }, [
    currentSearch,
    days,
    endHour,
    page,
    pathname,
    replace,
    selectedAgentIds,
    selectedOwnerId,
    sortDir,
    sortKey,
    startHour,
  ]);

  return {
    days,
    setDays: (next: TimeRange) => {
      setDays(next);
      setPage(1);
    },
    selectedAgentIds,
    setSelectedAgentIds: (next: string[]) => {
      setSelectedAgentIds(next);
      setPage(1);
    },
    selectedOwnerId,
    setSelectedOwnerId: (next: string | null) => {
      setSelectedOwnerId(next);
      setSelectedAgentIds([]);
      setPage(1);
    },
    startHour,
    setStartHour: (next: number) => {
      setStartHour(next);
      setPage(1);
    },
    endHour,
    setEndHour: (next: number) => {
      setEndHour(next);
      setPage(1);
    },
    sortKey,
    setSortKey,
    sortDir,
    setSortDir,
    page,
    setPage,
  };
}

export function AgentDashboardPage() {
  const { t } = useT("agent-dashboard");
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const currentUserId = useAuthStore((s) => s.user?.id ?? null);
  const timezone = useMemo(defaultTimezone, []);
  const [selectedRunId, setSelectedRunId] = useState<string | null>(null);
  const {
    days,
    setDays,
    selectedAgentIds,
    setSelectedAgentIds,
    selectedOwnerId,
    setSelectedOwnerId,
    startHour,
    setStartHour,
    endHour,
    setEndHour,
    sortKey,
    setSortKey,
    sortDir,
    setSortDir,
    page,
    setPage,
  } = useDashboardUrlState();

  const { data: agents = EMPTY_AGENTS } = useQuery(agentListOptions(wsId));
  const { data: members = EMPTY_MEMBERS } = useQuery(memberListOptions(wsId));
  const ownerAgents = useMemo(
    () => agents.filter((agent) => !selectedOwnerId || agent.owner_id === selectedOwnerId),
    [agents, selectedOwnerId],
  );
  const dashboardQuery = useQuery(
    agentRunDashboardOptions(wsId, {
      days,
      agentIds: selectedAgentIds,
      ownerId: selectedOwnerId,
      startHour,
      endHour,
      timezone,
      limit: 50,
    }),
  );
  const detailQuery = useQuery(
    agentRunDashboardRunDetailOptions(wsId, selectedRunId),
  );

  const dashboard = dashboardQuery.data;
  const summary = dashboard?.summary;
  const daily = dashboard?.daily ?? EMPTY_DAILY;
  const heatmap = dashboard?.heatmap ?? EMPTY_HEATMAP;
  const failureReasons = dashboard?.failure_reasons ?? EMPTY_REASONS;
  const recentFailures = dashboard?.recent_failures ?? EMPTY_RUNS;
  const recentRuns = dashboard?.recent_runs ?? EMPTY_RUNS;
  const retryRows = dashboard?.retry_distribution ?? [];
  const agentRows = dashboard?.agents ?? [];
  const trendRows = useMemo(() => buildTrendRows(daily), [daily]);
  const weekdayData = useMemo(() => workdayWeekendData(daily), [daily]);
  const sortedAgents = useMemo(
    () => sortAgents(agentRows, sortKey, sortDir),
    [agentRows, sortDir, sortKey],
  );
  const totalPages = Math.max(1, Math.ceil(sortedAgents.length / PAGE_SIZE));
  const pageRows = sortedAgents.slice((page - 1) * PAGE_SIZE, page * PAGE_SIZE);

  useEffect(() => {
    if (page > totalPages) setPage(totalPages);
  }, [page, setPage, totalPages]);

  const selectSort = (key: AgentSortKey) => {
    if (sortKey === key) {
      setSortDir(sortDir === "asc" ? "desc" : "asc");
      return;
    }
    setSortKey(key);
    setSortDir(key === "agent" || key === "project" || key === "status" ? "asc" : "desc");
  };

  const hasNoRuns = !dashboardQuery.isLoading && (summary?.total_runs ?? 0) === 0;

  return (
    <div className="flex h-full flex-col">
      <PageHeader className="h-auto min-h-12 flex-wrap justify-between gap-y-1.5 px-5 py-1.5 sm:py-0">
        <div className="flex min-w-0 items-center gap-2">
          <Activity className="h-4 w-4 shrink-0 text-muted-foreground" />
          <h1 className="truncate text-sm font-medium">{t(($) => $.title)}</h1>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <OwnerFilter
            members={members}
            currentUserId={currentUserId}
            selectedOwnerId={selectedOwnerId}
            onChange={setSelectedOwnerId}
          />
          <AgentFilter
            agents={ownerAgents}
            selectedAgentIds={selectedAgentIds}
            onChange={setSelectedAgentIds}
          />
          <HourFilter
            startHour={startHour}
            endHour={endHour}
            onStartChange={setStartHour}
            onEndChange={setEndHour}
          />
          <Segmented
            value={days}
            onChange={setDays}
            options={TIME_RANGES.map((value) => ({ label: `${value}D`, value }))}
          />
        </div>
      </PageHeader>

      <div className="flex-1 overflow-y-auto">
        <div className="mx-auto max-w-7xl space-y-5 p-6">
          <p className="text-xs text-muted-foreground">{t(($) => $.subtitle)}</p>

          {dashboardQuery.isLoading ? (
            <DashboardSkeleton />
          ) : hasNoRuns ? (
            <DashboardEmpty />
          ) : (
            <>
              <div className="grid grid-cols-1 divide-y rounded-lg border bg-card sm:grid-cols-2 sm:divide-x sm:divide-y-0 lg:grid-cols-4">
                <KpiCard
                  label={t(($) => $.kpi.total_runs, { days })}
                  value={String(summary?.total_runs ?? 0)}
                />
                <KpiCard
                  label={t(($) => $.kpi.success_rate, { days })}
                  value={formatPercent(summary?.success_rate ?? 0)}
                  hint={t(($) => $.kpi.success_hint, {
                    success: summary?.successful_runs ?? 0,
                    failed: summary?.failed_runs ?? 0,
                  })}
                />
                <KpiCard
                  label={t(($) => $.kpi.active_agents, { days })}
                  value={String(summary?.active_agent_count ?? 0)}
                />
                <KpiCard
                  label={t(($) => $.kpi.avg_duration, { days })}
                  value={formatDuration(summary?.average_duration_seconds ?? 0)}
                />
              </div>

              <div className="grid gap-5 xl:grid-cols-2">
                <RunTrendChart data={trendRows} />
                <SuccessTrendChart data={trendRows} />
              </div>

              <div className="grid gap-5 xl:grid-cols-[minmax(0,1.35fr)_minmax(320px,0.65fr)]">
                <Heatmap data={heatmap} />
                <FailureReasonChart data={failureReasons} />
              </div>

              <div className="grid gap-5 xl:grid-cols-2">
                <WorkdayWeekendChart data={weekdayData} />
                <RetryDistributionChart rows={retryRows} />
              </div>

              <AgentTable
                rows={pageRows}
                totalRows={sortedAgents.length}
                sortKey={sortKey}
                sortDir={sortDir}
                page={page}
                totalPages={totalPages}
                onSort={selectSort}
                onPageChange={setPage}
                onOpenRun={setSelectedRunId}
                agentDetailPath={(id) => paths.agentDetail(id)}
              />

              <div className="grid gap-5 xl:grid-cols-2">
                <RecentFailureList runs={recentFailures} onOpenRun={setSelectedRunId} />
                <RecentRunList runs={recentRuns} onOpenRun={setSelectedRunId} />
              </div>
            </>
          )}
        </div>
      </div>

      <RunDetailDialog
        open={!!selectedRunId}
        onOpenChange={(open) => {
          if (!open) setSelectedRunId(null);
        }}
        isLoading={detailQuery.isLoading}
        detail={detailQuery.data}
      />
    </div>
  );
}

function Segmented<T extends string | number>({
  value,
  onChange,
  options,
}: {
  value: T;
  onChange: (value: T) => void;
  options: readonly { label: string; value: T }[];
}) {
  return (
    <div className="inline-flex items-center gap-0.5 rounded-md bg-muted p-0.5">
      {options.map((option) => (
        <button
          key={String(option.value)}
          type="button"
          onClick={() => onChange(option.value)}
          className={`rounded-sm px-2.5 py-1 text-xs font-medium transition-colors ${
            value === option.value
              ? "bg-background text-foreground shadow-sm"
              : "text-muted-foreground hover:text-foreground"
          }`}
        >
          {option.label}
        </button>
      ))}
    </div>
  );
}

function OwnerFilter({
  members,
  currentUserId,
  selectedOwnerId,
  onChange,
}: {
  members: MemberWithUser[];
  currentUserId: string | null;
  selectedOwnerId: string | null;
  onChange: (id: string | null) => void;
}) {
  const { t } = useT("agent-dashboard");
  const selectedMember = selectedOwnerId
    ? members.find((member) => member.user_id === selectedOwnerId) ?? null
    : null;
  const selectedOwnerLabel = ownerFilterDisplayLabel({
    selectedOwnerId,
    selectedMember,
    allOwnersLabel: t(($) => $.filter.all_owners),
    selectedOwnerFallback: t(($) => $.filter.selected_owner),
  });
  const mineActive = !!currentUserId && selectedOwnerId === currentUserId;

  return (
    <div className="flex items-center gap-1">
      <Button
        variant={mineActive ? "secondary" : "outline"}
        size="sm"
        disabled={!currentUserId}
        onClick={() => {
          if (currentUserId) onChange(currentUserId);
        }}
      >
        <UserRound />
        <span>{t(($) => $.filter.mine)}</span>
      </Button>
      <Select
        value={selectedOwnerId ?? "all"}
        onValueChange={(value) => onChange(value === "all" ? null : value)}
      >
        <SelectTrigger size="sm" className="w-44 max-w-[46vw]">
          <Users className="h-3.5 w-3.5 text-muted-foreground" />
          <SelectValue>{selectedOwnerLabel}</SelectValue>
        </SelectTrigger>
        <SelectContent alignItemWithTrigger={false}>
          <SelectItem value="all">{t(($) => $.filter.all_owners)}</SelectItem>
          {selectedOwnerId && !selectedMember && (
            <SelectItem value={selectedOwnerId}>
              {t(($) => $.filter.selected_owner)}
            </SelectItem>
          )}
          {members.map((member) => (
            <SelectItem key={member.user_id} value={member.user_id}>
              <span className="truncate">{member.name || member.email}</span>
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </div>
  );
}

function AgentFilter({
  agents,
  selectedAgentIds,
  onChange,
}: {
  agents: Agent[];
  selectedAgentIds: string[];
  onChange: (ids: string[]) => void;
}) {
  const { t } = useT("agent-dashboard");
  const selected = new Set(selectedAgentIds);
  const selectedLabel =
    selectedAgentIds.length === 0
      ? t(($) => $.filter.all_agents)
      : t(($) => $.filter.agent_count, { count: selectedAgentIds.length });

  const toggle = (id: string, checked: boolean) => {
    const next = new Set(selected);
    if (checked) next.add(id);
    else next.delete(id);
    onChange([...next]);
  };

  return (
    <Popover>
      <PopoverTrigger
        render={
          <Button variant="outline" size="sm">
            <ListFilter />
            <span>{selectedLabel}</span>
          </Button>
        }
      />
      <PopoverContent align="end" className="max-h-80 overflow-y-auto">
        <button
          type="button"
          className="flex items-center gap-2 rounded-md px-2 py-1.5 text-left text-sm hover:bg-muted"
          onClick={() => onChange([])}
        >
          <Checkbox checked={selectedAgentIds.length === 0} />
          <span>{t(($) => $.filter.all_agents)}</span>
        </button>
        <div className="h-px bg-border" />
        {agents.map((agent) => (
          <label
            key={agent.id}
            className="flex cursor-pointer items-center gap-2 rounded-md px-2 py-1.5 text-sm hover:bg-muted"
          >
            <Checkbox
              checked={selected.has(agent.id)}
              onCheckedChange={(checked) => toggle(agent.id, checked === true)}
            />
            <span className="truncate">{agent.name}</span>
          </label>
        ))}
      </PopoverContent>
    </Popover>
  );
}

function HourFilter({
  startHour,
  endHour,
  onStartChange,
  onEndChange,
}: {
  startHour: number;
  endHour: number;
  onStartChange: (hour: number) => void;
  onEndChange: (hour: number) => void;
}) {
  const { t } = useT("agent-dashboard");
  return (
    <div className="flex items-center gap-1 rounded-md border bg-background px-2 py-1">
      <Filter className="h-3.5 w-3.5 text-muted-foreground" />
      <Select value={String(startHour)} onValueChange={(value) => onStartChange(Number(value))}>
        <SelectTrigger size="sm" className="h-6 w-[72px] border-0 px-1 shadow-none">
          <SelectValue />
        </SelectTrigger>
        <SelectContent alignItemWithTrigger={false}>
          {Array.from({ length: 24 }, (_, hour) => (
            <SelectItem key={hour} value={String(hour)}>
              {hour.toString().padStart(2, "0")}:00
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
      <span className="text-xs text-muted-foreground">{t(($) => $.filter.to)}</span>
      <Select value={String(endHour)} onValueChange={(value) => onEndChange(Number(value))}>
        <SelectTrigger size="sm" className="h-6 w-[72px] border-0 px-1 shadow-none">
          <SelectValue />
        </SelectTrigger>
        <SelectContent alignItemWithTrigger={false}>
          {Array.from({ length: 24 }, (_, hour) => (
            <SelectItem key={hour} value={String(hour)}>
              {hour.toString().padStart(2, "0")}:59
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </div>
  );
}

function RunTrendChart({ data }: { data: ReturnType<typeof buildTrendRows> }) {
  const { t } = useT("agent-dashboard");
  return (
    <div className="rounded-lg border bg-card p-4">
      <div className="mb-3 flex items-center justify-between">
        <h2 className="text-sm font-semibold">{t(($) => $.charts.run_trend)}</h2>
        <CalendarDays className="h-4 w-4 text-muted-foreground" />
      </div>
      <ChartContainer config={runTrendConfig} className="aspect-[3/1] w-full">
        <LineChart data={data} margin={{ top: 6, right: 8, bottom: 0, left: 0 }}>
          <CartesianGrid vertical={false} />
          <XAxis dataKey="label" tickLine={false} axisLine={false} tickMargin={8} />
          <YAxis tickLine={false} axisLine={false} tickMargin={8} allowDecimals={false} width={36} />
          <ChartTooltip content={<ChartTooltipContent />} />
          <Line
            type="monotone"
            dataKey="total_runs"
            stroke="var(--color-total_runs)"
            strokeWidth={2}
            dot={false}
          />
        </LineChart>
      </ChartContainer>
    </div>
  );
}

function SuccessTrendChart({ data }: { data: ReturnType<typeof buildTrendRows> }) {
  const { t } = useT("agent-dashboard");
  return (
    <div className="rounded-lg border bg-card p-4">
      <div className="mb-3 flex items-center justify-between">
        <h2 className="text-sm font-semibold">{t(($) => $.charts.success_trend)}</h2>
        <CheckCircle2 className="h-4 w-4 text-muted-foreground" />
      </div>
      <ChartContainer config={successTrendConfig} className="aspect-[3/1] w-full">
        <LineChart data={data} margin={{ top: 6, right: 8, bottom: 0, left: 0 }}>
          <CartesianGrid vertical={false} />
          <XAxis dataKey="label" tickLine={false} axisLine={false} tickMargin={8} />
          <YAxis
            tickLine={false}
            axisLine={false}
            tickMargin={8}
            width={40}
            domain={[0, 100]}
            tickFormatter={(value) => `${value}%`}
          />
          <ReferenceLine y={SUCCESS_WARNING_THRESHOLD * 100} stroke="var(--destructive)" strokeDasharray="4 4" />
          <ChartTooltip
            content={<ChartTooltipContent formatter={(value) => `${value}%`} />}
          />
          <Line
            type="monotone"
            dataKey="success"
            stroke="var(--color-success)"
            strokeWidth={2}
            dot={false}
          />
        </LineChart>
      </ChartContainer>
    </div>
  );
}

function Heatmap({ data }: { data: AgentRunDashboardHeatmapCell[] }) {
  const { t } = useT("agent-dashboard");
  const max = Math.max(1, ...data.map((cell) => cell.run_count));
  const map = new Map(data.map((cell) => [`${cell.weekday}:${cell.hour}`, cell.run_count]));
  const weekdays = [
    t(($) => $.weekday.sun),
    t(($) => $.weekday.mon),
    t(($) => $.weekday.tue),
    t(($) => $.weekday.wed),
    t(($) => $.weekday.thu),
    t(($) => $.weekday.fri),
    t(($) => $.weekday.sat),
  ];

  return (
    <div className="rounded-lg border bg-card p-4">
      <div className="mb-3 flex items-center justify-between">
        <h2 className="text-sm font-semibold">{t(($) => $.charts.heatmap)}</h2>
        <Clock3 className="h-4 w-4 text-muted-foreground" />
      </div>
      <div className="overflow-x-auto">
        <div className="grid min-w-[760px] grid-cols-[44px_repeat(24,minmax(0,1fr))] gap-1">
          <div />
          {Array.from({ length: 24 }, (_, hour) => (
            <div key={hour} className="text-center text-[10px] text-muted-foreground">
              {hour}
            </div>
          ))}
          {weekdays.map((label, weekday) => (
            <div key={label} className="contents">
              <div className="flex h-6 items-center text-xs text-muted-foreground">{label}</div>
              {Array.from({ length: 24 }, (_, hour) => {
                const count = map.get(`${weekday}:${hour}`) ?? 0;
                const intensity = count === 0 ? 0 : Math.max(0.18, count / max);
                return (
                  <div
                    key={`${weekday}-${hour}`}
                    title={`${label} ${hour}:00 · ${count}`}
                    className="h-6 rounded-sm border border-background"
                    style={{
                      background:
                        count === 0
                          ? "var(--muted)"
                          : `color-mix(in oklch, var(--chart-1) ${Math.round(intensity * 100)}%, transparent)`,
                    }}
                  />
                );
              })}
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}

function FailureReasonChart({ data }: { data: AgentRunDashboardFailureReason[] }) {
  const { t } = useT("agent-dashboard");
  const chartData = data.map((row) => ({
    ...row,
    label: failureReasonLabel(row.reason),
  }));
  const total = chartData.reduce((sum, row) => sum + row.count, 0);
  return (
    <div className="rounded-lg border bg-card p-4">
      <div className="mb-3 flex items-center justify-between">
        <h2 className="text-sm font-semibold">{t(($) => $.charts.failure_reasons)}</h2>
        <XCircle className="h-4 w-4 text-muted-foreground" />
      </div>
      {total === 0 ? (
        <NoChartData />
      ) : (
        <div className="grid gap-3 sm:grid-cols-[minmax(180px,0.9fr)_minmax(0,1fr)]">
          <ChartContainer config={{ count: { label: "Failures" } }} className="aspect-square">
            <PieChart>
              <Pie data={chartData} dataKey="count" nameKey="label" innerRadius={42} outerRadius={72} paddingAngle={2}>
                {chartData.map((row, index) => (
                  <Cell key={row.reason} fill={PIE_COLORS[index % PIE_COLORS.length]} />
                ))}
              </Pie>
              <ChartTooltip content={<ChartTooltipContent />} />
            </PieChart>
          </ChartContainer>
          <div className="space-y-2 self-center">
            {chartData.map((row, index) => (
              <div key={row.reason} className="flex items-center justify-between gap-3 text-sm">
                <span className="flex min-w-0 items-center gap-2">
                  <span
                    className="h-2.5 w-2.5 rounded-sm"
                    style={{ background: PIE_COLORS[index % PIE_COLORS.length] }}
                  />
                  <span className="truncate">{row.label}</span>
                </span>
                <span className="font-mono text-xs tabular-nums text-muted-foreground">{row.count}</span>
              </div>
            ))}
          </div>
        </div>
      )}
    </div>
  );
}

function WorkdayWeekendChart({ data }: { data: { label: string; runs: number }[] }) {
  const { t } = useT("agent-dashboard");
  return (
    <div className="rounded-lg border bg-card p-4">
      <div className="mb-3 flex items-center justify-between">
        <h2 className="text-sm font-semibold">{t(($) => $.charts.workday_weekend)}</h2>
        <BarIcon />
      </div>
      <ChartContainer config={workdayConfig} className="aspect-[3/1] w-full">
        <BarChart data={data} margin={{ top: 6, right: 8, bottom: 0, left: 0 }}>
          <CartesianGrid vertical={false} />
          <XAxis dataKey="label" tickLine={false} axisLine={false} tickMargin={8} />
          <YAxis tickLine={false} axisLine={false} tickMargin={8} allowDecimals={false} width={36} />
          <ChartTooltip content={<ChartTooltipContent />} />
          <Bar dataKey="runs" fill="var(--color-runs)" radius={[4, 4, 0, 0]} />
        </BarChart>
      </ChartContainer>
    </div>
  );
}

function RetryDistributionChart({
  rows,
}: {
  rows: { attempt: number; count: number }[];
}) {
  const { t } = useT("agent-dashboard");
  const data = rows.map((row) => ({
    attempt: `${row.attempt}`,
    count: row.count,
  }));
  return (
    <div className="rounded-lg border bg-card p-4">
      <div className="mb-3 flex items-center justify-between">
        <h2 className="text-sm font-semibold">{t(($) => $.charts.retry_distribution)}</h2>
        <RefreshCw className="h-4 w-4 text-muted-foreground" />
      </div>
      {data.length === 0 ? (
        <NoChartData />
      ) : (
        <ChartContainer config={retryConfig} className="aspect-[3/1] w-full">
          <BarChart data={data} margin={{ top: 6, right: 8, bottom: 0, left: 0 }}>
            <CartesianGrid vertical={false} />
            <XAxis dataKey="attempt" tickLine={false} axisLine={false} tickMargin={8} />
            <YAxis tickLine={false} axisLine={false} tickMargin={8} allowDecimals={false} width={36} />
            <ChartTooltip content={<ChartTooltipContent />} />
            <Bar dataKey="count" fill="var(--color-count)" radius={[4, 4, 0, 0]} />
          </BarChart>
        </ChartContainer>
      )}
    </div>
  );
}

function AgentTable({
  rows,
  totalRows,
  sortKey,
  sortDir,
  page,
  totalPages,
  onSort,
  onPageChange,
  onOpenRun,
  agentDetailPath,
}: {
  rows: AgentRunDashboardAgent[];
  totalRows: number;
  sortKey: AgentSortKey;
  sortDir: "asc" | "desc";
  page: number;
  totalPages: number;
  onSort: (key: AgentSortKey) => void;
  onPageChange: (page: number) => void;
  onOpenRun: (id: string) => void;
  agentDetailPath: (id: string) => string;
}) {
  const { t } = useT("agent-dashboard");
  const Header = ({ label, column }: { label: string; column: AgentSortKey }) => (
    <button
      type="button"
      onClick={() => onSort(column)}
      className="inline-flex items-center gap-1 text-left"
    >
      {label}
      {sortKey === column ? (
        sortDir === "asc" ? <ArrowUp className="h-3 w-3" /> : <ArrowDown className="h-3 w-3" />
      ) : null}
    </button>
  );

  return (
    <div className="rounded-lg border bg-card">
      <div className="flex flex-wrap items-center justify-between gap-3 border-b px-4 py-3">
        <h2 className="text-sm font-semibold">{t(($) => $.agents.title)}</h2>
        <span className="text-xs text-muted-foreground">
          {t(($) => $.agents.count, { count: totalRows })}
        </span>
      </div>
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead><Header label={t(($) => $.agents.agent)} column="agent" /></TableHead>
            <TableHead><Header label={t(($) => $.agents.project)} column="project" /></TableHead>
            <TableHead className="text-right"><Header label={t(($) => $.agents.runs)} column="runs" /></TableHead>
            <TableHead className="text-right"><Header label={t(($) => $.agents.success)} column="success" /></TableHead>
            <TableHead className="text-right"><Header label={t(($) => $.agents.duration)} column="duration" /></TableHead>
            <TableHead><Header label={t(($) => $.agents.last_run)} column="lastRun" /></TableHead>
            <TableHead><Header label={t(($) => $.agents.status)} column="status" /></TableHead>
            <TableHead className="text-right">{t(($) => $.agents.actions)}</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {rows.map((row) => (
            <TableRow key={row.agent_id}>
              <TableCell>
                <AppLink
                  href={agentDetailPath(row.agent_id)}
                  className="flex min-w-0 items-center gap-2 text-sm font-medium hover:underline"
                >
                  <ActorAvatar actorType="agent" actorId={row.agent_id} size={22} enableHoverCard />
                  <span className="truncate">{row.agent_name}</span>
                </AppLink>
              </TableCell>
              <TableCell className="max-w-[180px] truncate text-muted-foreground">
                {projectLabel(row)}
              </TableCell>
              <TableCell className="text-right font-mono tabular-nums">{row.total_runs}</TableCell>
              <TableCell className="text-right font-mono tabular-nums">
                {row.successful_runs + row.failed_runs > 0 ? formatPercent(row.success_rate) : "-"}
              </TableCell>
              <TableCell className="text-right font-mono tabular-nums">
                {formatDuration(row.average_duration_seconds)}
              </TableCell>
              <TableCell className="text-muted-foreground">{formatDateTime(row.last_run_at)}</TableCell>
              <TableCell>
                <Badge variant={statusVariant(row.agent_status)}>{statusLabel(row.agent_status)}</Badge>
              </TableCell>
              <TableCell className="text-right">
                <Button
                  variant="ghost"
                  size="icon-sm"
                  disabled={!row.last_task_id}
                  onClick={() => row.last_task_id && onOpenRun(row.last_task_id)}
                  aria-label={t(($) => $.agents.view_run)}
                >
                  <Eye />
                </Button>
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
      <div className="flex items-center justify-between border-t px-4 py-3">
        <span className="text-xs text-muted-foreground">
          {t(($) => $.agents.page, { page, total: totalPages })}
        </span>
        <div className="flex items-center gap-2">
          <Button
            variant="outline"
            size="sm"
            disabled={page <= 1}
            onClick={() => onPageChange(page - 1)}
          >
            {t(($) => $.agents.prev)}
          </Button>
          <Button
            variant="outline"
            size="sm"
            disabled={page >= totalPages}
            onClick={() => onPageChange(page + 1)}
          >
            {t(($) => $.agents.next)}
          </Button>
        </div>
      </div>
    </div>
  );
}

function RecentFailureList({
  runs,
  onOpenRun,
}: {
  runs: AgentRunDashboardRun[];
  onOpenRun: (id: string) => void;
}) {
  const { t } = useT("agent-dashboard");
  return (
    <RunList
      title={t(($) => $.failures.title)}
      empty={t(($) => $.failures.empty)}
      runs={runs}
      onOpenRun={onOpenRun}
      showError
    />
  );
}

function RecentRunList({
  runs,
  onOpenRun,
}: {
  runs: AgentRunDashboardRun[];
  onOpenRun: (id: string) => void;
}) {
  const { t } = useT("agent-dashboard");
  return (
    <RunList
      title={t(($) => $.recent.title)}
      empty={t(($) => $.recent.empty)}
      runs={runs}
      onOpenRun={onOpenRun}
    />
  );
}

function RunList({
  title,
  empty,
  runs,
  onOpenRun,
  showError = false,
}: {
  title: string;
  empty: string;
  runs: AgentRunDashboardRun[];
  onOpenRun: (id: string) => void;
  showError?: boolean;
}) {
  return (
    <div className="rounded-lg border bg-card">
      <div className="border-b px-4 py-3">
        <h2 className="text-sm font-semibold">{title}</h2>
      </div>
      {runs.length === 0 ? (
        <p className="px-4 py-8 text-center text-xs text-muted-foreground">{empty}</p>
      ) : (
        <div className="divide-y">
          {runs.map((run) => (
            <button
              key={run.id}
              type="button"
              onClick={() => onOpenRun(run.id)}
              className="grid w-full grid-cols-[minmax(0,1fr)_auto] gap-3 px-4 py-3 text-left hover:bg-muted/50"
            >
              <span className="min-w-0">
                <span className="flex items-center gap-2">
                  <Badge variant={statusVariant(run.status)}>{statusLabel(run.status)}</Badge>
                  <span className="truncate text-sm font-medium">{run.agent_name}</span>
                </span>
                <span className="mt-1 block truncate text-xs text-muted-foreground">
                  {runIssueLabel(run)}
                </span>
                {showError && run.error ? (
                  <span className="mt-1 block truncate font-mono text-xs text-destructive">
                    {run.error}
                  </span>
                ) : null}
              </span>
              <span className="text-right text-xs text-muted-foreground">
                {formatDateTime(run.run_at)}
              </span>
            </button>
          ))}
        </div>
      )}
    </div>
  );
}

function RunDetailDialog({
  open,
  onOpenChange,
  isLoading,
  detail,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  isLoading: boolean;
  detail:
    | import("@multica/core/types").AgentRunDashboardRunDetail
    | undefined;
}) {
  const { t } = useT("agent-dashboard");
  const run = detail?.run;
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[86vh] overflow-y-auto sm:max-w-4xl">
        <DialogHeader>
          <DialogTitle>{run ? `${run.agent_name} · ${statusLabel(run.status)}` : t(($) => $.detail.title)}</DialogTitle>
          <DialogDescription>
            {run ? `${runIssueLabel(run)} · ${formatDateTime(run.run_at)}` : t(($) => $.detail.loading)}
          </DialogDescription>
        </DialogHeader>
        {isLoading || !detail ? (
          <div className="space-y-3">
            <Skeleton className="h-24 w-full" />
            <Skeleton className="h-36 w-full" />
          </div>
        ) : (
          <div className="grid gap-5 lg:grid-cols-[minmax(0,0.9fr)_minmax(0,1.1fr)]">
            <div className="space-y-5">
              <div>
                <h3 className="mb-2 text-sm font-semibold">{t(($) => $.detail.timeline)}</h3>
                <div className="space-y-2">
                  {detail.timeline.map((event) => (
                    <div key={`${event.key}-${event.timestamp}`} className="flex items-center gap-3 text-sm">
                      <span className="h-2 w-2 rounded-full bg-primary" />
                      <span className="min-w-24 text-muted-foreground">{event.label}</span>
                      <span className="font-mono text-xs tabular-nums">{formatDateTime(event.timestamp)}</span>
                    </div>
                  ))}
                </div>
              </div>
              <div>
                <h3 className="mb-2 text-sm font-semibold">{t(($) => $.detail.breakdown)}</h3>
                <BreakdownBars breakdown={detail.duration_breakdown} />
              </div>
              {detail.run.error ? (
                <div>
                  <h3 className="mb-2 text-sm font-semibold">{t(($) => $.detail.error)}</h3>
                  <pre className="max-h-48 overflow-auto rounded-md bg-muted p-3 text-xs whitespace-pre-wrap text-destructive">
                    {detail.run.error}
                  </pre>
                </div>
              ) : null}
            </div>
            <div>
              <h3 className="mb-2 text-sm font-semibold">{t(($) => $.detail.messages)}</h3>
              {detail.messages.length === 0 ? (
                <p className="rounded-md border border-dashed p-6 text-center text-xs text-muted-foreground">
                  {t(($) => $.detail.no_messages)}
                </p>
              ) : (
                <div className="max-h-[520px] space-y-2 overflow-auto pr-1">
                  {detail.messages.map((message) => (
                    <div key={message.seq} className="rounded-md border bg-background p-3">
                      <div className="mb-1 flex items-center justify-between gap-2 text-xs text-muted-foreground">
                        <span>{message.type}{message.tool ? ` · ${message.tool}` : ""}</span>
                        <span className="font-mono tabular-nums">{formatDateTime(message.created_at)}</span>
                      </div>
                      <pre className="overflow-auto whitespace-pre-wrap text-xs">
                        {message.content || message.output || JSON.stringify(message.input ?? {}, null, 2)}
                      </pre>
                    </div>
                  ))}
                </div>
              )}
            </div>
          </div>
        )}
      </DialogContent>
    </Dialog>
  );
}

function BreakdownBars({
  breakdown,
}: {
  breakdown: import("@multica/core/types").AgentRunDurationBreakdown;
}) {
  const { t } = useT("agent-dashboard");
  const rows = [
    { label: t(($) => $.detail.llm), value: breakdown.llm_seconds, color: "var(--chart-1)" },
    { label: t(($) => $.detail.tools), value: breakdown.tool_call_seconds, color: "var(--chart-3)" },
    { label: t(($) => $.detail.network), value: breakdown.network_wait_seconds, color: "var(--chart-4)" },
  ];
  const total = Math.max(1, breakdown.total_seconds);
  return (
    <div className="space-y-2">
      {rows.map((row) => (
        <div key={row.label} className="space-y-1">
          <div className="flex items-center justify-between text-xs">
            <span className="text-muted-foreground">{row.label}</span>
            <span className="font-mono tabular-nums">{formatDuration(row.value)}</span>
          </div>
          <div className="h-2 overflow-hidden rounded-full bg-muted">
            <div
              className="h-full rounded-full"
              style={{ width: `${Math.min(100, (row.value / total) * 100)}%`, background: row.color }}
            />
          </div>
        </div>
      ))}
    </div>
  );
}

function DashboardSkeleton() {
  return (
    <div className="space-y-5">
      <Skeleton className="h-24 w-full" />
      <div className="grid gap-5 xl:grid-cols-2">
        <Skeleton className="h-72 w-full" />
        <Skeleton className="h-72 w-full" />
      </div>
      <Skeleton className="h-96 w-full" />
    </div>
  );
}

function DashboardEmpty() {
  const { t } = useT("agent-dashboard");
  return (
    <div className="flex min-h-[360px] flex-col items-center justify-center gap-3 rounded-lg border border-dashed bg-card p-8 text-center">
      <Bot className="h-8 w-8 text-muted-foreground" />
      <div>
        <h2 className="text-sm font-semibold">{t(($) => $.empty.title)}</h2>
        <p className="mt-1 text-xs text-muted-foreground">{t(($) => $.empty.body)}</p>
      </div>
    </div>
  );
}

function NoChartData() {
  const { t } = useT("agent-dashboard");
  return (
    <div className="flex aspect-[3/1] items-center justify-center rounded-md border border-dashed bg-muted/20 p-6 text-center text-xs text-muted-foreground">
      {t(($) => $.charts.no_data)}
    </div>
  );
}

function BarIcon() {
  return <Activity className="h-4 w-4 text-muted-foreground" />;
}
