"use client";

// Per-permission detail page. For one tool it audits every workspace user
// across Workspace, Runtimes, Agents, Groups, Direct and Effective layers,
// alongside its policy Changes and enforcement Usage.
//
// Gated by cerebro_permission_detail (default OFF). Nothing links here while the
// flag is off, so the page is dormant and the change is reversible.

import { useMemo, useState } from "react";
import { useQueries, useQuery } from "@tanstack/react-query";
import {
  ArrowLeft,
  Bot,
  ChevronDown,
  Search,
  ShieldCheck,
  Users,
} from "lucide-react";

import {
  Tabs,
  TabsList,
  TabsTrigger,
  TabsContent,
} from "@multica/ui/components/ui/tabs";
import { Button } from "@multica/ui/components/ui/button";
import { Badge } from "@multica/ui/components/ui/badge";
import { Input } from "@multica/ui/components/ui/input";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@multica/ui/components/ui/table";

import { useWorkspaceId } from "@multica/core/hooks";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import { getAgentCapabilities } from "@multica/cerebro-agent-capabilities";

import {
  getPermissionChanges,
  getPermissionHolders,
  getPermissionUsage,
} from "../api";
import type { PermissionChange, PermissionUsageRow } from "../api";
import {
  fetchToolPolicyTable,
  permissionRoleAssignmentsOptions,
  permissionRolesOptions,
} from "../core";
import {
  buildPermissionAuditContexts,
  buildPermissionAuditRows,
} from "./permission-audit";
import type {
  PermissionAuditAgent,
  PermissionAuditCell,
  PermissionAuditEffectiveCell,
  PermissionAuditMember,
  PermissionAuditResolved,
  PermissionAuditRow,
} from "./permission-audit";
import { useHolderDirectory } from "./use-holder-directory";

const LAYER_LABEL: Record<string, string> = {
  workspace: "Workspace",
  runtime: "Runtime",
  agent: "Agent",
  group: "Group",
  user: "Member",
  system: "System",
};

// layerLabel names the policy layer for the row's secondary text. Enum drift
// (an unknown future layer) downgrades to the raw string rather than crashing.
function layerLabel(layer: string): string {
  return LAYER_LABEL[layer] ?? layer;
}

// settingLabel turns a stored setting into a human word. An unknown value
// downgrades to itself (or a dash) instead of throwing.
function settingLabel(setting: string): string {
  switch (setting) {
    case "allow":
      return "Allow";
    case "ask":
      return "Ask";
    case "deny":
      return "Deny";
    default:
      return setting || "—";
  }
}

// transitionLabel renders one change-log row's old -> new movement. "" on the
// old side means the layer held no explicit row before the write (Inherit); a
// clear always ends at "Cleared" (back to Inherit).
function transitionLabel(change: PermissionChange): string {
  const from = change.old_setting ? settingLabel(change.old_setting) : "Not set";
  if (change.action === "clear") {
    return `${from} → Cleared`;
  }
  return `${from} → ${settingLabel(change.new_setting)}`;
}

// changeTimeLabel formats a change's RFC3339 timestamp for the row. An
// unparseable or missing value downgrades to the raw string (or a dash)
// instead of rendering "Invalid Date".
function changeTimeLabel(createdAt: string): string {
  if (!createdAt) return "—";
  const d = new Date(createdAt);
  if (Number.isNaN(d.getTime())) return createdAt;
  return d.toLocaleString();
}

const ENFORCEMENT_POINT_LABEL: Record<string, string> = {
  mention_gate: "Agent trigger",
  repo_checkout: "Repo checkout",
  agent_browser_sandbox: "Agent browser",
  gateway_tool: "Gateway tool call",
  gateway_connection_tool: "Connection tool call",
  local_cli: "Local runtime tool call",
  http_action: "Workspace action",
};

// enforcementPointLabel names the gate that applied a usage-log row's
// decision. An unknown future gate downgrades to its raw identifier.
function enforcementPointLabel(point: string): string {
  return ENFORCEMENT_POINT_LABEL[point] ?? point;
}

// usageSubjectLabel names a usage row's subject through the same directory the
// other tabs use. subject_type maps to the directory's layer vocabulary
// ("member" rows are member ids, i.e. the "user" layer); a system subject or
// an unknown type downgrades to a plain word.
function usageSubjectLabel(
  row: PermissionUsageRow,
  labelFor: (layer: string, id: string) => string,
): string {
  switch (row.subject_type) {
    case "member":
      return labelFor("user", row.subject_id);
    case "agent":
      return labelFor("agent", row.subject_id);
    default:
      return "System";
  }
}

function permissionTitle(toolKey: string): string {
  const plain = toolKey
    .replace(/^[^:]+:/, "")
    .replace(/[-_.:]+/g, " ")
    .trim();
  return plain ? `${plain[0]!.toUpperCase()}${plain.slice(1)}` : "Permission";
}

function DecisionBadge({ setting }: { setting: string | null }) {
  const label = settingLabel(setting ?? "");
  const value =
    setting === "disable" ? "Deny" : label === "—" ? "Inherit" : label;
  const variant =
    value === "Deny"
      ? "destructive"
      : value === "Allow"
        ? "default"
        : value === "Ask"
          ? "secondary"
          : "outline";
  return <Badge variant={variant}>{value}</Badge>;
}

function decisionValue(setting: string | null): string {
  const label = settingLabel(setting ?? "");
  return setting === "disable" ? "Deny" : label === "—" ? "Inherit" : label;
}

function AuditCellList({
  cells,
  showLabels = true,
  limit,
}: {
  cells: PermissionAuditCell[];
  showLabels?: boolean;
  limit?: number;
}) {
  if (cells.length === 0) {
    return <span className="text-muted-foreground">—</span>;
  }
  const visibleCells = typeof limit === "number" ? cells.slice(0, limit) : cells;
  return (
    <div className="space-y-2">
      {visibleCells.map((cell) => (
        <div key={cell.id} className="min-w-24">
          {showLabels ? (
            <div
              className="mb-1 truncate text-xs text-foreground"
              title={cell.label}
            >
              {cell.label}
            </div>
          ) : null}
          <DecisionBadge setting={cell.setting} />
        </div>
      ))}
      {typeof limit === "number" && cells.length > limit ? (
        <span className="text-xs text-muted-foreground">
          +{cells.length - limit} more
        </span>
      ) : null}
    </div>
  );
}

function EffectiveCellList({
  cells,
  limit,
}: {
  cells: PermissionAuditEffectiveCell[];
  limit?: number;
}) {
  if (cells.length === 0) {
    return <span className="text-muted-foreground">—</span>;
  }
  return (
    <div className="space-y-2">
      {(typeof limit === "number" ? cells.slice(0, limit) : cells).map(
        (cell, index) => (
          <div key={`${cell.contextLabel}:${index}`}>
            <div
              className="mb-1 truncate text-xs text-foreground"
              title={cell.contextLabel}
            >
              {cell.contextLabel}
            </div>
            <DecisionBadge setting={cell.setting} />
            <div className="mt-1 text-[11px] text-muted-foreground">
              Why: {cell.source}
            </div>
          </div>
        ),
      )}
      {typeof limit === "number" && cells.length > limit ? (
        <span className="text-xs text-muted-foreground">
          +{cells.length - limit} more
        </span>
      ) : null}
    </div>
  );
}

const AUDIT_LAYERS = [
  ["Workspace", "workspace"],
  ["Runtimes", "runtimes"],
  ["Agents", "agents"],
  ["Groups", "groups"],
  ["Direct", "direct"],
] as const;

function effectiveDecision(row: PermissionAuditRow): string {
  const rank: Record<string, number> = { deny: 3, ask: 2, allow: 1 };
  return (
    [...row.effective].sort(
      (a, b) => (rank[b.setting] ?? 0) - (rank[a.setting] ?? 0),
    )[0]?.setting ?? ""
  );
}

function PermissionAuditMatrix({ rows }: { rows: PermissionAuditRow[] }) {
  return (
    <>
      <div className="hidden max-h-[calc(100vh-16rem)] overflow-auto md:block [&>[data-slot=table-container]]:overflow-visible">
        <Table aria-label="Permission audit" className="table-fixed">
          <TableHeader>
            <TableRow>
              <TableHead className="sticky left-0 top-0 z-20 w-40 bg-card">
                User
              </TableHead>
              {[
                "Workspace",
                "Runtimes",
                "Agents",
                "Groups",
                "Direct",
                "Effective",
              ].map((heading) => (
                <TableHead key={heading} className="sticky top-0 z-10 bg-card">
                  {heading}
                </TableHead>
              ))}
            </TableRow>
          </TableHeader>
          <TableBody>
            {rows.map((row) => (
              <TableRow key={row.user.id} className="align-top">
                <TableCell className="sticky left-0 z-10 bg-card align-top whitespace-normal">
                  <div className="font-medium">{row.user.name}</div>
                  <div className="mt-1 text-xs capitalize text-muted-foreground">
                    {row.user.role} · {row.severity} severity
                  </div>
                </TableCell>
                {AUDIT_LAYERS.map(([, key]) => (
                  <TableCell key={key} className="align-top whitespace-normal">
                    <AuditCellList
                      cells={row[key]}
                      showLabels={key !== "workspace" && key !== "direct"}
                      limit={2}
                    />
                  </TableCell>
                ))}
                <TableCell className="align-top whitespace-normal">
                  <EffectiveCellList cells={row.effective} limit={2} />
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </div>

      <div className="space-y-3 md:hidden" data-testid="permission-audit-mobile">
        {rows.map((row) => (
          <details
            key={row.user.id}
            data-testid={`permission-audit-mobile-${row.user.id}`}
            className="group overflow-hidden rounded-lg border bg-card"
          >
            <summary className="flex cursor-pointer list-none items-center gap-3 bg-muted/30 px-4 py-3">
              <div className="min-w-0 flex-1">
                <span className="block truncate font-medium">
                  {row.user.name}
                </span>
                <span className="block text-xs capitalize text-muted-foreground">
                  {row.user.role} · {row.severity} severity
                </span>
              </div>
              <span className="sr-only">
                Effective {decisionValue(effectiveDecision(row))}
              </span>
              <DecisionBadge setting={effectiveDecision(row)} />
              <ChevronDown className="size-4 text-muted-foreground transition-transform group-open:rotate-180" />
            </summary>
            <div className="divide-y">
              {AUDIT_LAYERS.map(([label, key]) => (
                <section
                  key={key}
                  className="grid grid-cols-[6rem_1fr] gap-3 px-4 py-3"
                >
                  <h4 className="text-xs font-medium text-muted-foreground">
                    {label}
                  </h4>
                  <AuditCellList
                    cells={row[key]}
                    showLabels={key !== "workspace" && key !== "direct"}
                  />
                </section>
              ))}
              <section className="grid grid-cols-[6rem_1fr] gap-3 px-4 py-3">
                <h4 className="text-xs font-medium text-muted-foreground">
                  Effective
                </h4>
                <EffectiveCellList cells={row.effective} />
              </section>
            </div>
          </details>
        ))}
      </div>
    </>
  );
}

type AgentUserDecision = {
  member: PermissionAuditMember;
  setting?: string;
  source?: string;
};

function AgentUsersTable({
  agent,
  decisions,
  isLoading,
}: {
  agent: PermissionAuditAgent;
  decisions: AgentUserDecision[];
  isLoading: boolean;
}) {
  if (isLoading) {
    return <p className="p-6 text-sm text-muted-foreground">Loading users…</p>;
  }

  return (
    <Table aria-label={`Users for ${agent.name}`}>
      <TableHeader>
        <TableRow>
          <TableHead>User</TableHead>
          <TableHead>Access</TableHead>
          <TableHead>Why</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {decisions.map(({ member, setting, source }) => (
          <TableRow key={member.id}>
            <TableCell>
              <div className="font-medium">{member.name}</div>
              <div className="text-xs text-muted-foreground">{member.email}</div>
            </TableCell>
            <TableCell>
              {setting ? (
                <DecisionBadge setting={setting} />
              ) : (
                <span className="text-sm text-muted-foreground">Unavailable</span>
              )}
            </TableCell>
            <TableCell className="text-sm text-muted-foreground">
              {source ?? "Unable to resolve"}
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}

export function PermissionDetailPage({
  toolKey,
  onBack,
}: {
  toolKey: string;
  onBack: () => void;
}) {
  const wsId = useWorkspaceId();
  const enabled = useFeatureFlag("cerebro_permission_detail");

  const holdersQuery = useQuery({
    queryKey: ["cerebro", "tool-policy", "holders", wsId, toolKey],
    queryFn: () => getPermissionHolders(wsId, toolKey),
    enabled: enabled && !!wsId && !!toolKey,
  });

  const changesQuery = useQuery({
    queryKey: ["cerebro", "tool-policy", "changes", wsId, toolKey],
    queryFn: () => getPermissionChanges(wsId, toolKey),
    enabled: enabled && !!wsId && !!toolKey,
  });

  const usageQuery = useQuery({
    queryKey: ["cerebro", "tool-policy", "usage", wsId, toolKey],
    queryFn: () => getPermissionUsage(wsId, toolKey),
    enabled: enabled && !!wsId && !!toolKey,
  });
  const rolesQuery = useQuery({
    ...permissionRolesOptions(wsId),
    enabled: enabled && !!wsId,
  });
  const applicableRoles = useMemo(
    () =>
      (rolesQuery.data ?? []).filter((role) =>
        Object.prototype.hasOwnProperty.call(role.permissions, toolKey),
      ),
    [rolesQuery.data, toolKey],
  );
  const roleAssignmentQueries = useQueries({
    queries: applicableRoles.map((role) => ({
      ...permissionRoleAssignmentsOptions(wsId, role.id),
      enabled: enabled && !!wsId,
    })),
  });

  const directory = useHolderDirectory(wsId, enabled);
  const [activeTab, setActiveTab] = useState("audit");
  const [selectedAgentId, setSelectedAgentId] = useState<string | null>(null);
  const [search, setSearch] = useState("");
  const [decisionFilter, setDecisionFilter] = useState<
    "all" | "deny" | "ask" | "allow"
  >("all");
  const contexts = useMemo(
    () =>
      buildPermissionAuditContexts({
        members: directory.members,
        agents: directory.agents,
        runtimes: directory.runtimes,
      }),
    [directory.members, directory.agents, directory.runtimes],
  );
  const resolvedQueries = useQueries({
    queries: contexts.map((context) => ({
      queryKey: [
        "cerebro",
        "tool-policy",
        "permission-audit",
        wsId,
        toolKey,
        context.id,
      ],
      queryFn: async () => {
        const [rows, capabilities] = await Promise.all([
          fetchToolPolicyTable(wsId, {
            userId: context.userId,
            agentId: context.agentId,
            runtimeId: context.runtimeId,
          }),
          context.agentId
            ? getAgentCapabilities(context.agentId).catch(() => null)
            : Promise.resolve(null),
        ]);
        const row = rows.find((candidate) => candidate.tool_key === toolKey);
        if (!row) return null;
        const normalizedToolKey = toolKey.toLocaleLowerCase();
        const capabilityTool = capabilities?.tools.find(
          (tool) =>
            tool.key.toLocaleLowerCase() === normalizedToolKey ||
            tool.title.toLocaleLowerCase() === normalizedToolKey,
        );
        const observedTool = capabilities?.observed_access.tools.find(
          (tool) => tool.name.toLocaleLowerCase() === normalizedToolKey,
        );
        const setting = capabilityTool?.permission || row.effective.setting;
        return {
          setting,
          policySetting: row.effective.setting,
          availabilityLevel: capabilityTool?.availability.level,
          governanceSeverity: observedTool?.drift ? "critical" : undefined,
          decidedBy:
            capabilityTool && setting !== row.effective.setting
              ? "availability"
              : row.effective.decided_by,
          cappedBy: row.effective.capped_by,
        } satisfies PermissionAuditResolved;
      },
      enabled: enabled && Boolean(wsId && toolKey),
    })),
  });
  const resolvedByContext = useMemo(() => {
    const result = new Map<string, PermissionAuditResolved>();
    contexts.forEach((context, index) => {
      const resolved = resolvedQueries[index]?.data;
      if (resolved) result.set(context.id, resolved);
    });
    return result;
  }, [contexts, resolvedQueries]);
  const auditRows = useMemo(
    () =>
      buildPermissionAuditRows({
        members: directory.members,
        agents: directory.agents,
        runtimes: directory.runtimes,
        groups: directory.groups,
        holders: holdersQuery.data?.holders ?? [],
        contexts,
        resolvedByContext,
      }),
    [
      directory.members,
      directory.agents,
      directory.runtimes,
      directory.groups,
      holdersQuery.data?.holders,
      contexts,
      resolvedByContext,
    ],
  );
  const filteredAuditRows = useMemo(() => {
    const needle = search.trim().toLocaleLowerCase();
    return auditRows.filter(
      (row) =>
        `${row.user.name} ${row.user.email}`
          .toLocaleLowerCase()
          .includes(needle) &&
        (decisionFilter === "all" ||
          [
            ...row.workspace,
            ...row.runtimes,
            ...row.agents,
            ...row.groups,
            ...row.direct,
            ...row.effective,
          ].some((cell) => cell.setting === decisionFilter)),
    );
  }, [auditRows, decisionFilter, search]);
  const selectedAgent = directory.agents.find((agent) => agent.id === selectedAgentId);
  const agentUserContexts = useMemo(
    () =>
      activeTab === "agents" && selectedAgent
        ? directory.members.map((member) => ({
            id: `${selectedAgent.id}:${member.id}:${selectedAgent.runtimeId}`,
            userId: member.id,
            agentId: selectedAgent.id,
            ...(selectedAgent.runtimeId ? { runtimeId: selectedAgent.runtimeId } : {}),
          }))
        : [],
    [activeTab, directory.members, selectedAgent],
  );
  const agentUserQueries = useQueries({
    queries: agentUserContexts.map((context) => ({
      queryKey: [
        "cerebro",
        "tool-policy",
        "agent-user-permission-audit",
        wsId,
        toolKey,
        context.agentId,
        context.userId,
        context.runtimeId ?? null,
      ],
      queryFn: async () => {
        const rows = await fetchToolPolicyTable(wsId, context);
        const row = rows.find((candidate) => candidate.tool_key === toolKey);
        if (!row) return null;
        const source = row.effective.capped_by || row.effective.decided_by;
        return {
          setting: row.effective.setting,
          source: source ? layerLabel(source) : "Default",
        };
      },
      enabled: enabled && Boolean(wsId && toolKey && selectedAgent),
    })),
  });
  const agentUserDecisions = useMemo(
    () =>
      agentUserContexts.map((context, index) => ({
        member: directory.members.find((member) => member.id === context.userId)!,
        ...agentUserQueries[index]?.data,
      })),
    [agentUserContexts, agentUserQueries, directory.members],
  );
  const agentUsersLoading = agentUserQueries.some((query) => query.isLoading);
  const permissionQuestions = [
    {
      tab: "audit",
      icon: Users,
      title: "What can each person do?",
      body: "See every member's effective decision and the layers behind it.",
    },
    {
      tab: "roles",
      icon: ShieldCheck,
      title: "Which Permission profiles and assignments apply?",
      body: "See the shared profiles that include this permission and who receives them.",
    },
    {
      tab: "agents",
      icon: Bot,
      title: "What can people do through one agent?",
      body: "Choose an agent and compare every member's effective access through it.",
    },
  ] as const;
  const detailTabs = [
    { tab: "audit", label: "Permission audit" },
    { tab: "roles", label: `Profiles ${applicableRoles.length}` },
    { tab: "agents", label: `Agents ${directory.agents.length}` },
    {
      tab: "changes",
      label: `Changes ${(changesQuery.data?.changes ?? []).length}`,
    },
    {
      tab: "usage",
      label: `Usage ${(usageQuery.data?.usage ?? []).length}`,
    },
  ] as const;

  // Safety fallback: the route always registers, so guard the flag here. Nothing
  // links to the page while off, so this is only reached by a hand-typed URL.
  if (!enabled) {
    return (
      <div className="mx-auto max-w-3xl p-6">
        <Button variant="ghost" size="sm" onClick={onBack} className="mb-4">
          <ArrowLeft className="size-4" /> Back
        </Button>
        <p className="text-sm text-muted-foreground">
          This page is not available.
        </p>
      </div>
    );
  }

  const data = holdersQuery.data;
  // A tool whose policy is stored but not enforced at runtime yet is shown
  // greyed out with an explicit note, so no one mistakes it for a live control.
  const isEnforced = data?.enforced === true;
  const auditLoading =
    holdersQuery.isLoading ||
    directory.isLoading ||
    resolvedQueries.some((query) => query.isLoading);

  return (
    <main className="mx-auto h-full min-h-0 w-full max-w-[1400px] overflow-y-auto p-4 sm:p-6">
      <Button variant="ghost" size="sm" onClick={onBack} className="mb-4">
        <ArrowLeft className="size-4" /> Back
      </Button>

      <header className="mb-5 flex flex-wrap items-start justify-between gap-3">
        <div>
          <div className="flex items-center gap-2">
            <h1 className="text-2xl font-semibold tracking-tight">
              {permissionTitle(toolKey)}
            </h1>
            <Badge variant={isEnforced ? "outline" : "secondary"}>
              {isEnforced ? "Enforced" : "Not enforced"}
            </Badge>
          </div>
          <p className="font-mono text-sm text-muted-foreground">{toolKey}</p>
        </div>
      </header>

      <div className="mb-4 rounded-lg border bg-muted/30 px-4 py-3 text-sm text-muted-foreground">
        Permission profiles are evaluated together with Workspace, Runtime,
        Agent, Group and Member rules. The live result follows this
        permission&apos;s declared override and safety-floor contract.
      </div>

      <section
        className="mb-4 overflow-hidden rounded-xl border bg-card"
        aria-labelledby="permission-detail-questions"
      >
        <div className="border-b px-4 py-3">
          <h2 id="permission-detail-questions" className="font-semibold">
            Choose the question you want to answer
          </h2>
          <p className="mt-1 text-sm text-muted-foreground">
            This page turns one permission around so you can inspect people,
            Permission profiles or agents.
          </p>
        </div>
        <div className="grid gap-px bg-border md:grid-cols-3">
          {permissionQuestions.map(
            ({ tab, icon: Icon, title, body }) => (
              <button
                key={tab}
                type="button"
                aria-pressed={activeTab === tab}
                onClick={() => setActiveTab(tab)}
                className={`min-w-0 bg-card p-4 text-left transition-colors hover:bg-muted/60 ${
                  activeTab === tab ? "ring-2 ring-inset ring-primary" : ""
                }`}
              >
                <span className="flex items-center gap-2 text-sm font-medium">
                  <Icon className="size-4 text-muted-foreground" />
                  {title}
                </span>
                <span className="mt-1.5 block text-xs leading-relaxed text-muted-foreground">
                  {body}
                </span>
              </button>
            ),
          )}
        </div>
      </section>

      <Tabs value={activeTab} onValueChange={setActiveTab}>
        <TabsList className="hidden" hidden>
          {detailTabs.map(({ tab, label }) => (
            <TabsTrigger key={tab} value={tab}>
              {label}
            </TabsTrigger>
          ))}
        </TabsList>
        <div
          role="tablist"
          aria-label="Permission detail views"
          className="mb-3 flex w-full gap-1 overflow-x-auto rounded-lg bg-muted p-1"
        >
          {detailTabs.map(({ tab, label }) => (
            <button
              key={tab}
              type="button"
              role="tab"
              aria-selected={activeTab === tab}
              onClick={() => setActiveTab(tab)}
              className={`shrink-0 rounded-md px-3 py-1.5 text-sm font-medium transition-colors ${
                activeTab === tab
                  ? "bg-background text-foreground shadow-sm"
                  : "text-muted-foreground hover:text-foreground"
              }`}
            >
              {label}
            </button>
          ))}
        </div>

        <TabsContent value="audit" className="pt-2">
          <section
            className="overflow-hidden rounded-xl border bg-card"
            aria-labelledby="permission-audit-heading"
          >
            <div className="flex flex-col gap-3 border-b p-4 lg:flex-row lg:items-end lg:justify-between">
              <div>
                <h2 id="permission-audit-heading" className="font-semibold">
                  Why Access and Permission audit
                </h2>
                <p className="text-sm text-muted-foreground">
                  See the effective answer and why it applies, ordered by live
                  access risk, runtime evidence, and governance findings.
                </p>
              </div>
              <div className="flex flex-col gap-2 sm:flex-row sm:items-center">
                <label className="relative block w-full sm:w-64">
                  <span className="sr-only">Search users</span>
                  <Search className="pointer-events-none absolute left-2.5 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
                  <Input
                    type="search"
                    aria-label="Search users"
                    placeholder="Search users"
                    value={search}
                    onChange={(event) => setSearch(event.target.value)}
                    className="pl-8"
                  />
                </label>
                <div
                  className="flex items-center gap-1 rounded-lg border p-1"
                  aria-label="Filter by decision"
                >
                  {(["all", "deny", "ask", "allow"] as const).map(
                    (decision) => (
                      <Button
                        key={decision}
                        type="button"
                        size="sm"
                        variant={
                          decisionFilter === decision ? "secondary" : "ghost"
                        }
                        aria-pressed={decisionFilter === decision}
                        onClick={() => setDecisionFilter(decision)}
                      >
                        {decision[0]!.toUpperCase() + decision.slice(1)}
                      </Button>
                    ),
                  )}
                </div>
              </div>
            </div>

            {!isEnforced ? (
              <div className="border-b bg-muted/30 px-4 py-3 text-sm text-muted-foreground">
                These settings are visible, but this permission is not enforced
                yet.
              </div>
            ) : null}

            {auditLoading ? (
              <p className="p-6 text-sm text-muted-foreground">Loading…</p>
            ) : filteredAuditRows.length === 0 ? (
              <p className="p-6 text-sm text-muted-foreground">
                {search || decisionFilter !== "all"
                  ? "No users match your filters."
                  : "No users found."}
              </p>
            ) : (
              <div className={!isEnforced ? "opacity-60" : ""}>
                <PermissionAuditMatrix rows={filteredAuditRows} />
              </div>
            )}
          </section>
        </TabsContent>

        <TabsContent value="roles" className="pt-2">
          <section className="overflow-hidden rounded-xl border bg-card">
            <div className="border-b p-4">
              <h2 className="font-semibold">Permission profiles</h2>
              <p className="text-sm text-muted-foreground">
                Reusable profiles that include this permission and the agents or
                members currently assigned to them.
              </p>
            </div>
            {rolesQuery.isLoading ||
            roleAssignmentQueries.some((query) => query.isLoading) ? (
              <p className="p-6 text-sm text-muted-foreground">Loading profiles…</p>
            ) : applicableRoles.length === 0 ? (
              <p className="p-6 text-sm text-muted-foreground">
                No Permission profile includes this permission.
              </p>
            ) : (
              <div className="divide-y">
                {applicableRoles.map((role, index) => {
                  const assignments = roleAssignmentQueries[index]?.data ?? [];
                  const decisions = (role.permissions[toolKey] ?? []).map(
                    (rule) => rule.setting,
                  );
                  const decision = decisions.includes("deny")
                    ? "deny"
                    : decisions.includes("ask")
                      ? "ask"
                      : "allow";
                  return (
                    <article key={role.id} className="space-y-3 p-4">
                      <div className="flex flex-wrap items-start justify-between gap-3">
                        <div className="min-w-0">
                          <div className="flex flex-wrap items-center gap-2">
                            <h3 className="font-medium">{role.name}</h3>
                            <Badge variant="secondary">Version {role.version}</Badge>
                          </div>
                          {role.description ? (
                            <p className="mt-1 text-sm text-muted-foreground">
                              {role.description}
                            </p>
                          ) : null}
                        </div>
                        <DecisionBadge setting={decision} />
                      </div>
                      {assignments.length === 0 ? (
                        <p className="text-sm text-muted-foreground">
                          Not assigned.
                        </p>
                      ) : (
                        <div className="flex flex-wrap gap-2">
                          {assignments.map((assignment) => (
                            <span
                              key={`${assignment.subject_type}:${assignment.subject_id}`}
                              className="inline-flex max-w-full items-center gap-1.5 rounded-md border px-2 py-1 text-xs"
                            >
                              <Badge variant="outline">
                                {assignment.subject_type}
                              </Badge>
                              <span className="truncate">
                                {assignment.subject_display_name ||
                                  assignment.subject_id}
                              </span>
                            </span>
                          ))}
                        </div>
                      )}
                    </article>
                  );
                })}
              </div>
            )}
          </section>
        </TabsContent>

        <TabsContent value="agents" className="pt-2">
          <section className="overflow-hidden rounded-xl border bg-card">
            <div className="border-b p-4">
              <h2 className="font-semibold">Agents</h2>
              <p className="text-sm text-muted-foreground">
                Select an agent to see every user's effective access to this permission.
              </p>
            </div>
            {directory.isLoading ? (
              <p className="p-6 text-sm text-muted-foreground">Loading agents…</p>
            ) : directory.agents.length === 0 ? (
              <p className="p-6 text-sm text-muted-foreground">No agents found.</p>
            ) : (
              <div className="divide-y">
                {directory.agents.map((agent) => (
                  <AgentUsersRow
                    key={agent.id}
                    agent={agent}
                    runtimeName={
                      directory.runtimes.find((runtime) => runtime.id === agent.runtimeId)?.name ??
                      "No runtime"
                    }
                    isSelected={selectedAgentId === agent.id}
                    decisions={agentUserDecisions}
                    isLoading={agentUsersLoading}
                    onSelect={() => setSelectedAgentId(agent.id)}
                  />
                ))}
              </div>
            )}
          </section>
        </TabsContent>

        <TabsContent value="changes" className="pt-4">
          {changesQuery.isLoading || directory.isLoading ? (
            <p className="text-sm text-muted-foreground">Loading…</p>
          ) : (changesQuery.data?.changes ?? []).length === 0 ? (
            <p className="text-sm text-muted-foreground">
              No changes recorded. Recording started when this log shipped, so
              earlier changes are not shown.
            </p>
          ) : (
            <ul className="space-y-2 text-sm">
              {(changesQuery.data?.changes ?? []).map((c, i) => (
                <li
                  key={`${c.created_at}:${c.layer}:${c.subject_id}:${i}`}
                  className="flex items-center justify-between gap-3 rounded border p-2"
                >
                  <div className="min-w-0">
                    <span className="font-medium">
                      {directory.labelFor(c.layer, c.subject_id)}
                    </span>
                    <span className="ml-2 text-xs text-muted-foreground">
                      {layerLabel(c.layer)} layer · {transitionLabel(c)}
                    </span>
                    <div className="text-xs text-muted-foreground">
                      by{" "}
                      {c.actor_type === "member"
                        ? directory.labelFor("user", c.actor_id)
                        : "System"}
                    </div>
                  </div>
                  <span className="shrink-0 text-xs text-muted-foreground">
                    {changeTimeLabel(c.created_at)}
                  </span>
                </li>
              ))}
            </ul>
          )}
        </TabsContent>

        <TabsContent value="usage" className="pt-4">
          {usageQuery.isLoading || directory.isLoading ? (
            <p className="text-sm text-muted-foreground">Loading…</p>
          ) : (usageQuery.data?.usage ?? []).length === 0 ? (
            <p className="text-sm text-muted-foreground">
              No usage recorded. Recording started when this log shipped, so
              earlier usage is not shown.
            </p>
          ) : (
            <ul className="space-y-2 text-sm">
              {(usageQuery.data?.usage ?? []).map((u, i) => (
                <li
                  key={`${u.created_at}:${u.enforcement_point}:${u.subject_id}:${i}`}
                  className="flex items-center justify-between gap-3 rounded border p-2"
                >
                  <div className="min-w-0">
                    <span className="font-medium">
                      {usageSubjectLabel(u, directory.labelFor)}
                    </span>
                    <span className="ml-2 text-xs text-muted-foreground">
                      {enforcementPointLabel(u.enforcement_point)} ·{" "}
                      {settingLabel(u.decision)}
                    </span>
                    {u.resource ? (
                      <div className="truncate text-xs text-muted-foreground">
                        {u.resource}
                      </div>
                    ) : null}
                  </div>
                  <span className="shrink-0 text-xs text-muted-foreground">
                    {changeTimeLabel(u.created_at)}
                  </span>
                </li>
              ))}
            </ul>
          )}
        </TabsContent>
      </Tabs>
    </main>
  );
}

function AgentUsersRow({
  agent,
  runtimeName,
  isSelected,
  decisions,
  isLoading,
  onSelect,
}: {
  agent: PermissionAuditAgent;
  runtimeName: string;
  isSelected: boolean;
  decisions: AgentUserDecision[];
  isLoading: boolean;
  onSelect: () => void;
}) {
  return (
    <div className="p-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <div className="font-medium">{agent.name}</div>
          <div className="text-sm text-muted-foreground">{runtimeName}</div>
        </div>
        <Button
          type="button"
          size="sm"
          variant={isSelected ? "secondary" : "outline"}
          aria-label={`View users for ${agent.name}`}
          onClick={onSelect}
        >
          View users
        </Button>
      </div>
      {isSelected ? (
        <div className="mt-4 overflow-x-auto rounded-lg border">
          <AgentUsersTable agent={agent} decisions={decisions} isLoading={isLoading} />
        </div>
      ) : null}
    </div>
  );
}
