"use client";

import { useQuery } from "@tanstack/react-query";
import {
  Activity,
  AlertTriangle,
  CheckCircle2,
  ExternalLink,
  XCircle,
} from "lucide-react";
import {
  Area,
  AreaChart,
  CartesianGrid,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts";

import { useWorkspaceId } from "@multica/core/hooks";
import {
  seInfraHealthOptions,
  type SEAgentFleet,
  type SEInfraHealthSnapshot,
} from "@multica/core/se";

import { useT } from "../i18n/use-t";
import { Badge } from "@multica/ui/components/ui/badge";
import { Card, CardContent, CardHeader, CardTitle } from "@multica/ui/components/ui/card";
import { Separator } from "@multica/ui/components/ui/separator";
import { Skeleton } from "@multica/ui/components/ui/skeleton";

const GRAFANA_URL = "https://grafana.smartexpertlabs.com";

function overallTone(overall: string): "ok" | "warn" | "down" | "unknown" {
  if (overall === "ok") return "ok";
  if (overall === "warn") return "warn";
  if (overall === "down") return "down";
  return "unknown";
}

function StatusBanner({ snapshot }: { snapshot: SEInfraHealthSnapshot }) {
  const { t } = useT("health");
  const tone = overallTone(snapshot.overall);
  const toneMap = {
    ok: { icon: CheckCircle2, cls: "text-emerald-600", label: t(($) => $.overall_ok) },
    warn: { icon: AlertTriangle, cls: "text-amber-600", label: t(($) => $.overall_warn) },
    down: { icon: XCircle, cls: "text-red-600", label: t(($) => $.overall_down) },
    unknown: { icon: Activity, cls: "text-muted-foreground", label: t(($) => $.overall_unknown) },
  } as const;
  const { icon: Icon, cls, label } = toneMap[tone];
  return (
    <div className="flex items-center gap-3">
      <Icon className={`h-8 w-8 ${cls}`} aria-hidden />
      <div>
        <div className="text-lg font-semibold">{label}</div>
        {snapshot.generated_at ? (
          <div className="text-sm text-muted-foreground">
            {t(($) => $.generated)}: {snapshot.generated_at}
          </div>
        ) : null}
      </div>
    </div>
  );
}

function HostTile({ host }: { host: { name: string; load1: number; memAvailGB: number; memTotalGB: number; swapUsedGB: number; swapTotalGB: number } }) {
  const { t } = useT("health");
  const memPct = host.memTotalGB > 0 ? Math.round((host.memAvailGB / host.memTotalGB) * 100) : 0;
  const swapPct = host.swapTotalGB > 0 ? Math.round((host.swapUsedGB / host.swapTotalGB) * 100) : 0;
  return (
    <Card>
      <CardHeader className="pb-2">
        <CardTitle className="text-sm font-medium">{host.name}</CardTitle>
      </CardHeader>
      <CardContent className="grid grid-cols-3 gap-2 text-sm">
        <div>
          <div className="text-muted-foreground">{t(($) => $.load)}</div>
          <div className={`font-semibold ${host.load1 >= 20 ? "text-red-600" : host.load1 >= 10 ? "text-amber-600" : ""}`}>
            {host.load1.toFixed(1)}
          </div>
        </div>
        <div>
          <div className="text-muted-foreground">{t(($) => $.memory)}</div>
          <div className={`font-semibold ${memPct < 15 ? "text-red-600" : memPct < 30 ? "text-amber-600" : ""}`}>
            {memPct}%
          </div>
        </div>
        <div>
          <div className="text-muted-foreground">{t(($) => $.swap)}</div>
          <div className={`font-semibold ${swapPct > 50 ? "text-red-600" : swapPct > 10 ? "text-amber-600" : ""}`}>
            {swapPct}%
          </div>
        </div>
      </CardContent>
    </Card>
  );
}

function HistoryChart({ snapshot }: { snapshot: SEInfraHealthSnapshot }) {
  const { t } = useT("health");
  const load = snapshot.history["load1"] ?? [];
  const mem = snapshot.history["mem_avail_gb"] ?? [];
  if (load.length === 0) return null;
  // Recharts needs one row per timestamp: merge series by index (both share
  // the same step from the collector).
  const rows = load.map((point, i) => {
    const ts = point[0] ?? 0;
    const value = point[1] ?? 0;
    return {
      time: new Date(ts * 1000).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" }),
      load: value,
      mem: mem[i]?.[1],
    };
  });
  return (
    <Card>
      <CardHeader className="pb-2">
        <CardTitle className="text-sm font-medium">{t(($) => $.history_24h)}</CardTitle>
      </CardHeader>
      <CardContent className="h-48">
        <ResponsiveContainer width="100%" height="100%">
          <AreaChart data={rows} margin={{ top: 4, right: 8, bottom: 0, left: -16 }}>
            <CartesianGrid strokeDasharray="3 3" opacity={0.3} />
            <XAxis dataKey="time" tick={{ fontSize: 10 }} interval={Math.max(1, Math.floor(rows.length / 8))} />
            <YAxis tick={{ fontSize: 10 }} />
            <Tooltip />
            <Area type="monotone" dataKey="load" stroke="#f59e0b" fill="#f59e0b" fillOpacity={0.15} isAnimationActive={false} />
            <Area type="monotone" dataKey="mem" stroke="#10b981" fill="#10b981" fillOpacity={0.15} isAnimationActive={false} />
          </AreaChart>
        </ResponsiveContainer>
      </CardContent>
    </Card>
  );
}

function TargetGroups({ snapshot }: { snapshot: SEInfraHealthSnapshot }) {
  const { t } = useT("health");
  const external = snapshot.targets.filter((x) => x.job.startsWith("blackbox"));
  const services = snapshot.targets.filter((x) => !x.job.startsWith("blackbox") && x.job !== "node" && x.job !== "cadvisor");
  const hosts = snapshot.targets.filter((x) => x.job === "node");
  const containers = snapshot.targets.filter((x) => x.job === "cadvisor");

  const group = (title: string, items: typeof snapshot.targets) => {
    if (items.length === 0) return null;
    return (
      <div>
        <div className="mb-2 text-sm font-medium text-muted-foreground">{title}</div>
        <div className="flex flex-wrap gap-1.5">
          {items.map((x) => (
            <Badge
              key={`${x.job}/${x.instance}`}
              variant="outline"
              className={x.health === "up" ? "border-emerald-500/40 text-emerald-700" : "border-red-500/50 text-red-700"}
            >
              {x.health === "up" ? (
                <CheckCircle2 className="mr-1 h-3 w-3" aria-hidden />
              ) : (
                <XCircle className="mr-1 h-3 w-3" aria-hidden />
              )}
              {x.instance || x.job}
            </Badge>
          ))}
        </div>
      </div>
    );
  };

  return (
    <div className="space-y-4">
      {group(t(($) => $.nodes), hosts)}
      {group(t(($) => $.containers), containers)}
      {group(t(($) => $.services), services)}
      {group(t(($) => $.external), external)}
    </div>
  );
}

// SE-37663: live agent fleet section (limits + runtimes), merged into the
// infra health endpoint by the backend. Failing agents only — healthy ones
// stay in the collapsed count to avoid noise.
function AgentFleetSection({ fleet }: { fleet: SEAgentFleet }) {
  const { t } = useT("health");
  const banner = fleet.quota_streak_active || fleet.auth_streak_active;
  const offline = fleet.runtimes.filter((r) => r.status !== "online");
  return (
    <Card>
      <CardHeader className="pb-2">
        <CardTitle className="flex items-center gap-2 text-sm font-medium">
          {banner ? (
            <XCircle className="h-4 w-4 text-red-600" aria-hidden />
          ) : (
            <CheckCircle2 className="h-4 w-4 text-emerald-600" aria-hidden />
          )}
          {t(($) => $.fleet_title)}
        </CardTitle>
      </CardHeader>
      <CardContent className="space-y-3 text-sm">
        {banner ? (
          <div className="rounded-md border border-red-500/50 bg-red-500/10 px-3 py-2 text-red-700">
            {fleet.quota_streak_active ? t(($) => $.fleet_quota_banner) : null}
            {fleet.quota_streak_active && fleet.auth_streak_active ? " " : null}
            {fleet.auth_streak_active ? t(($) => $.fleet_auth_banner) : null}
          </div>
        ) : null}

        {fleet.failing_agents.length > 0 ? (
          <div className="grid gap-2 sm:grid-cols-2 lg:grid-cols-3">
            {fleet.failing_agents.map((a) => (
              <div key={a.agent_id} className="rounded-md border px-3 py-2">
                <div className="font-medium">{a.agent_name}</div>
                <div className="text-muted-foreground">
                  {t(($) => $.fleet_failed)}:{" "}
                  <span className="font-semibold text-red-600">{a.failed}</span>
                  {" · "}
                  {t(($) => $.fleet_completed)}: {a.completed}
                </div>
                <div className="text-xs text-muted-foreground">
                  {t(($) => $.fleet_quota)}: {a.quota_failed} · {t(($) => $.fleet_auth)}: {a.auth_failed}
                </div>
                {a.last_failure_at ? (
                  <div className="text-xs text-muted-foreground">
                    {t(($) => $.fleet_last_failure)}: {a.last_failure_at}
                  </div>
                ) : null}
                {a.last_error_excerpt ? (
                  <div
                    className="mt-1 line-clamp-2 text-xs text-muted-foreground"
                    title={a.last_error_excerpt}
                  >
                    {a.last_error_excerpt}
                  </div>
                ) : null}
              </div>
            ))}
          </div>
        ) : (
          <div className="text-muted-foreground">{t(($) => $.fleet_all_clear)}</div>
        )}

        {offline.length > 0 ? (
          <div className="text-xs text-muted-foreground">
            {t(($) => $.fleet_runtimes_offline)}: {offline.map((r) => r.name).join(", ")}
          </div>
        ) : (
          <div className="text-xs text-muted-foreground">
            {t(($) => $.fleet_runtimes_online)}: {fleet.runtimes.length}
          </div>
        )}
      </CardContent>
    </Card>
  );
}

export function HealthPage() {
  const workspaceId = useWorkspaceId();
  const { t } = useT("health");
  const { data, isLoading, isError } = useQuery(seInfraHealthOptions(workspaceId));

  if (isLoading) {
    return (
      <div className="space-y-4 p-6">
        <Skeleton className="h-10 w-64" />
        <Skeleton className="h-24 w-full" />
        <Skeleton className="h-48 w-full" />
      </div>
    );
  }

  if (isError || !data || (!data.generated_at && data.targets.length === 0)) {
    return (
      <div className="p-6">
        <Card>
          <CardContent className="flex items-center gap-3 pt-6 text-sm text-muted-foreground">
            <AlertTriangle className="h-5 w-5 text-amber-600" aria-hidden />
            {t(($) => $.unavailable)}
          </CardContent>
        </Card>
      </div>
    );
  }

  return (
    <main className="flex min-h-0 min-w-0 flex-1 flex-col">
      <div className="min-h-0 flex-1 space-y-6 overflow-y-auto p-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold">{t(($) => $.title)}</h1>
          <p className="text-sm text-muted-foreground">{t(($) => $.subtitle)}</p>
        </div>
        <a
          href={GRAFANA_URL}
          target="_blank"
          rel="noreferrer"
          className="inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground"
        >
          {t(($) => $.grafana)}
          <ExternalLink className="h-3.5 w-3.5" aria-hidden />
        </a>
      </div>

      <StatusBanner snapshot={data} />
      <Separator />

      {data.agent_fleet ? <AgentFleetSection fleet={data.agent_fleet} /> : null}

      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
        {Object.entries(data.nodes).map(([name, node]) => (
          <HostTile
            key={name}
            host={{
              name,
              load1: node.load1,
              memAvailGB: node.mem_avail_gb,
              memTotalGB: node.mem_total_gb,
              swapUsedGB: node.swap_used_gb,
              swapTotalGB: node.swap_total_gb,
            }}
          />
        ))}
      </div>

      <HistoryChart snapshot={data} />
      <TargetGroups snapshot={data} />
      </div>
    </main>
  );
}
