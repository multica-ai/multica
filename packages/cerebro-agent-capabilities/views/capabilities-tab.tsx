// TECH-3642 — the per-agent Capabilities tab on the agent detail page.
//
// "Capabilities" is the word agents already use (it matches the MCP/agent
// interop standards), so a human reading the UI and an agent reading via
// `multica agent capabilities` / the `get_agent_capabilities` MCP tool meet the
// exact same concept and the exact same fields. This tab is the human surface
// of GET /api/agents/{id}/capabilities; the CLI and MCP are the agent surfaces.
//
// Layout: everything is a compact, wrapping pill so a long list stays scannable
// instead of a tall column. Tools, repo permissions, and connection tools are
// colour-coded by their effective verdict (allow / ask / block).

"use client";

import type { ComponentType, ReactNode } from "react";
import {
  ShieldCheck,
  BookOpenText,
  Wrench,
  GitBranch,
  Plug,
  KeyRound,
  Lock,
  Activity,
  AlertTriangle,
} from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import type { Agent, AgentRuntime } from "@multica/core/types";
import {
  getAgentCapabilities,
  type AgentCapabilities,
  type AgentCapabilityTool,
  type AgentCapabilityConnection,
  type AgentCapabilityRepo,
  type AgentCapabilitySecretSet,
  type AgentCapabilityObservedAccess,
  type AgentCapabilityObservedTool,
} from "../api";

export interface AgentCapabilitiesTabExtension {
  id: string;
  labelKey: "capabilities";
  icon: ComponentType<{ className?: string }>;
  render: (context: {
    agent: Agent;
    runtimes: AgentRuntime[];
    canEdit: boolean;
  }) => ReactNode;
}

const capabilitiesKey = (agentId: string) =>
  ["cerebro", "agent-capabilities", agentId] as const;

export function createAgentCapabilitiesTabs(): AgentCapabilitiesTabExtension[] {
  return [
    {
      id: "capabilities",
      labelKey: "capabilities",
      icon: ShieldCheck,
      render: ({ agent }) => <CerebroCapabilitiesTab agent={agent} />,
    },
  ];
}

export function CerebroCapabilitiesTab({ agent }: { agent: Agent }) {
  const { data, isLoading } = useQuery({
    queryKey: capabilitiesKey(agent.id),
    queryFn: () => getAgentCapabilities(agent.id),
    enabled: !!agent.id,
  });

  if (isLoading) {
    return (
      <p className="text-sm text-muted-foreground">Loading capabilities…</p>
    );
  }

  const caps: AgentCapabilities = data ?? {
    agent_id: agent.id,
    name: agent.name,
    model: agent.model,
    description: agent.description,
    skills: [],
    tools: [],
    repos: [],
    connections: [],
    credentials: [],
    infisical_secrets: [],
    agent_secrets: {
      source: "agent_custom_env",
      status: "not_configured",
      count: 0,
      names: [],
      redacted: false,
      runtime_id: "",
    },
    runtime_secrets: {
      source: "runtime",
      status: "unknown",
      count: 0,
      names: [],
      redacted: false,
      runtime_id: "",
    },
    observed_access: {
      status: "unknown",
      window_days: 30,
      task_count: 0,
      tools: [],
      drift_count: 0,
    },
    limits: { mcp_servers: [], has_mcp_config: false },
  };

  const sandboxText = formatSandbox(caps.limits.sandbox);

  return (
    <div className="flex flex-col gap-5">
      <p className="text-sm text-muted-foreground">
        Registered and observed access for this agent — the same fields agents
        read via the CLI and MCP. Unknown sources are marked explicitly. Green =
        allowed, amber = needs approval, red = blocked.
      </p>

      {/* Skills */}
      <Section icon={BookOpenText} title="Skills" count={caps.skills.length}>
        {caps.skills.length === 0 ? (
          <Empty>No skills loaded.</Empty>
        ) : (
          <PillRow>
            {caps.skills.map((s) => (
              <Pill key={s.id || s.name} title={s.description}>
                {s.name}
              </Pill>
            ))}
          </PillRow>
        )}
      </Section>

      {/* Tools + permissions */}
      <Section icon={Wrench} title="Tools" count={caps.tools.length}>
        {caps.tools.length === 0 ? (
          <Empty>No tools resolved.</Empty>
        ) : (
          <PillRow>
            {caps.tools.map((t) => (
              <ToolPill key={`${t.key}:${t.source}`} tool={t} />
            ))}
          </PillRow>
        )}
      </Section>

      {/* Repos */}
      <Section icon={GitBranch} title="Repos" count={caps.repos.length}>
        {caps.repos.length === 0 ? (
          <Empty>No repositories.</Empty>
        ) : (
          <div className="flex flex-col gap-2">
            {caps.repos.map((repo) => (
              <RepoRow key={repo.url} repo={repo} />
            ))}
          </div>
        )}
      </Section>

      {/* Connections */}
      <Section icon={Plug} title="Connections" count={caps.connections.length}>
        {caps.connections.length === 0 ? (
          <Empty>No connections.</Empty>
        ) : (
          <div className="flex flex-col gap-2.5">
            {caps.connections.map((c) => (
              <ConnectionRow key={c.name} connection={c} />
            ))}
          </div>
        )}
      </Section>

      {/* Credentials */}
      <Section icon={KeyRound} title="Credentials" count={caps.credentials.length}>
        {caps.credentials.length === 0 ? (
          <Empty>No credentials bound.</Empty>
        ) : (
          <PillRow>
            {caps.credentials.map((c) => (
              <Pill key={c.name} title={c.type}>
                {c.name}
                {c.type && <span className="text-muted-foreground">· {c.type}</span>}
              </Pill>
            ))}
          </PillRow>
        )}
      </Section>

      {/* Infisical secrets */}
      <Section
        icon={KeyRound}
        title="Infisical secrets"
        count={caps.infisical_secrets.length}
      >
        {caps.infisical_secrets.length === 0 ? (
          <Empty>No Infisical folders.</Empty>
        ) : (
          <PillRow>
            {caps.infisical_secrets.map((s) => (
              <Pill key={`${s.environment}:${s.path}`} title={s.environment} mono>
                {s.path}
                {s.environment && (
                  <span className="text-muted-foreground">· {s.environment}</span>
                )}
              </Pill>
            ))}
          </PillRow>
        )}
      </Section>

      {/* Agent secrets (custom_env) — TECH-3738 Bid A, names owner/admin-only */}
      <SecretSetSection
        title="Agent secrets"
        subtitle="from the agent's own custom env"
        set={caps.agent_secrets}
      />

      {/* Runtime secrets — inherited from the runtime the agent runs on */}
      <SecretSetSection
        title="Runtime secrets"
        subtitle="inherited from the agent's runtime"
        set={caps.runtime_secrets}
      />

      {/* Observed access (TECH-3738 Bid B) — what the agent actually used */}
      <ObservedAccessSection observed={caps.observed_access} />

      {/* Limits */}
      <Section icon={Lock} title="Limits">
        <PillRow>
          {caps.limits.mcp_servers.length === 0 && !caps.limits.has_mcp_config ? (
            <Empty>No MCP config.</Empty>
          ) : (
            caps.limits.mcp_servers.map((name) => (
              <Pill key={name} title="MCP server">
                {name}
              </Pill>
            ))
          )}
          {sandboxText && (
            <Pill title={sandboxText}>sandbox policy</Pill>
          )}
        </PillRow>
      </Section>
    </div>
  );
}

// permissionPill maps the effective verdict to pill colours, reusing the cerebro
// fork's emerald/amber/destructive palette (see simple-tool-policy-table). Enum
// drift downgrades to a neutral pill (API Response Compatibility rule).
function permissionPill(permission: string): string {
  switch (permission) {
    case "allow":
      return "border-emerald-600/40 text-emerald-700 dark:text-emerald-300";
    case "ask":
      return "border-amber-600/40 text-amber-700 dark:text-amber-400";
    case "deny":
      return "border-destructive/50 text-destructive";
    default:
      return "";
  }
}

function toolTitle(t: AgentCapabilityTool): string {
  const parts = [t.permission || "unknown"];
  if (t.decided_by) parts.push(`via ${t.decided_by}`);
  if (t.reason) parts.push(t.reason);
  if (t.capped_by_groups.length > 0)
    parts.push(`capped by ${t.capped_by_groups.join(", ")}`);
  return parts.join(" · ");
}

function ToolPill({ tool }: { tool: AgentCapabilityTool }) {
  return (
    <Pill className={permissionPill(tool.permission)} title={toolTitle(tool)}>
      {tool.title || tool.key}
    </Pill>
  );
}

function RepoRow({ repo }: { repo: AgentCapabilityRepo }) {
  return (
    <div className="flex flex-wrap items-center gap-1.5">
      <span className="font-mono text-xs text-muted-foreground">
        {shortRepo(repo.url)}
      </span>
      {repo.permissions.map((p) => (
        <Pill
          key={p.key}
          className={permissionPill(p.permission)}
          title={toolTitle(p)}
        >
          {p.title || p.key}
        </Pill>
      ))}
    </div>
  );
}

function ConnectionRow({
  connection,
}: {
  connection: AgentCapabilityConnection;
}) {
  const isMcp = connection.type === "mcp_http";
  return (
    <div className="flex flex-col gap-1.5 rounded-md border p-2.5">
      <div className="flex flex-wrap items-center gap-1.5">
        <Pill>{isMcp ? "MCP" : "API"}</Pill>
        <span className="text-sm font-medium">
          {connection.display_name || connection.name}
        </span>
        {connection.enabled === false && (
          <Pill className="text-muted-foreground">disabled</Pill>
        )}
        {connection.url && (
          <span className="ml-auto truncate font-mono text-xs text-muted-foreground">
            {connection.url}
          </span>
        )}
      </div>
      {connection.tools.length > 0 && (
        <PillRow>
          {connection.tools.map((t) => (
            <Pill
              key={t.name}
              className={permissionPill(t.permission)}
              title={`${t.permission || "no policy"}${t.description ? " · " + t.description : ""}`}
            >
              {t.name}
            </Pill>
          ))}
        </PillRow>
      )}
      {connection.endpoints.length > 0 && (
        <PillRow>
          {connection.endpoints.map((ep) => (
            <Pill key={ep.path} mono title={ep.methods.join(", ")}>
              <span className="text-muted-foreground">
                {ep.methods.join("/")}
              </span>
              {ep.path}
            </Pill>
          ))}
        </PillRow>
      )}
    </div>
  );
}

function Section({
  icon: Icon,
  title,
  count,
  children,
}: {
  icon: ComponentType<{ className?: string }>;
  title: string;
  count?: number;
  children: ReactNode;
}) {
  return (
    <section className="flex flex-col gap-2">
      <div className="flex items-center gap-2">
        <Icon className="h-4 w-4 text-muted-foreground" />
        <h3 className="text-sm font-medium">{title}</h3>
        {typeof count === "number" && (
          <span className="text-xs text-muted-foreground">· {count}</span>
        )}
      </div>
      {children}
    </section>
  );
}

// SecretSetSection renders one names-only secret group (agent custom_env or
// runtime bindings). It always shows an explicit status badge so an empty list
// is never ambiguous, and it honours the backend's redaction: when names are
// withheld it shows the count and a lock note instead of the names — never a
// secret value, and never a name the caller is not allowed to see.
function SecretSetSection({
  title,
  subtitle,
  set,
}: {
  title: string;
  subtitle: string;
  set: AgentCapabilitySecretSet;
}) {
  return (
    <Section icon={KeyRound} title={title} count={set.count}>
      <div className="flex flex-wrap items-center gap-1.5">
        <StatusBadge status={set.status} />
        <span className="text-xs text-muted-foreground">{subtitle}</span>
      </div>
      {set.redacted ? (
        <p className="mt-1.5 flex items-center gap-1 text-xs text-muted-foreground">
          <Lock className="h-3 w-3" />
          {set.count} secret{set.count === 1 ? "" : "s"} — names visible to
          workspace owners/admins only.
        </p>
      ) : set.names.length > 0 ? (
        <PillRow>
          {set.names.map((name) => (
            <Pill key={name} mono title="secret name (value never shown)">
              {name}
            </Pill>
          ))}
        </PillRow>
      ) : (
        <Empty>
          {set.status === "unknown"
            ? "Could not determine secrets for this source."
            : "No secrets configured."}
        </Empty>
      )}
    </Section>
  );
}

// StatusBadge surfaces the source's scan status so "nothing here" and "we don't
// know" look different. Enum drift downgrades to a neutral badge (API Response
// Compatibility rule) rather than crashing the tab.
function StatusBadge({ status }: { status: string }) {
  switch (status) {
    case "known":
      return (
        <Pill className="border-emerald-600/40 text-emerald-700 dark:text-emerald-300">
          known
        </Pill>
      );
    case "not_configured":
      return <Pill className="text-muted-foreground">none</Pill>;
    case "unknown":
      return (
        <Pill className="border-amber-600/40 text-amber-700 dark:text-amber-400">
          unknown
        </Pill>
      );
    case "stale":
      return (
        <Pill className="border-amber-600/40 text-amber-700 dark:text-amber-400">
          stale
        </Pill>
      );
    default:
      return <Pill className="text-muted-foreground">{status || "—"}</Pill>;
  }
}

// ObservedAccessSection renders what the agent actually USED recently (Bid B),
// distinct from the declared layers above. The headline signal is drift: tools
// the agent invoked that its declared policy does not sanction (blocked or
// unmapped) — surfaced as a red banner and per-tool warning. Tools only; observed
// secret use is never shown because it is not recorded anywhere.
function ObservedAccessSection({
  observed,
}: {
  observed: AgentCapabilityObservedAccess;
}) {
  return (
    <Section
      icon={Activity}
      title="Observed access"
      count={observed.tools.length}
    >
      <div className="flex flex-wrap items-center gap-1.5">
        <StatusBadge status={observed.status} />
        <span className="text-xs text-muted-foreground">
          tools actually used in the last {observed.window_days} days
          {observed.task_count > 0
            ? ` · ${observed.task_count} task${observed.task_count === 1 ? "" : "s"}`
            : ""}
        </span>
      </div>

      {observed.drift_count > 0 && (
        <p className="mt-1.5 flex items-center gap-1 text-xs text-destructive">
          <AlertTriangle className="h-3 w-3" />
          {observed.drift_count} tool{observed.drift_count === 1 ? "" : "s"} used
          that the declared policy does not allow (blocked or unmapped) — review.
        </p>
      )}

      {observed.tools.length > 0 ? (
        <PillRow>
          {observed.tools.map((t) => (
            <ObservedToolPill key={t.name} tool={t} />
          ))}
        </PillRow>
      ) : (
        <Empty>
          {observed.status === "unknown"
            ? "Could not determine recent activity."
            : "No tool use recorded in this window."}
        </Empty>
      )}
    </Section>
  );
}

// observedPill maps the observed-vs-declared verdict to pill colours: allowed =
// green, needs-approval = amber, blocked/unmapped (drift) = red. Enum drift
// downgrades to a neutral pill (API Response Compatibility rule).
function observedPill(status: string): string {
  switch (status) {
    case "allowed":
      return "border-emerald-600/40 text-emerald-700 dark:text-emerald-300";
    case "needs_approval":
      return "border-amber-600/40 text-amber-700 dark:text-amber-400";
    case "blocked":
    case "unmapped":
      return "border-destructive/50 text-destructive";
    default:
      return "";
  }
}

function observedTitle(t: AgentCapabilityObservedTool): string {
  const parts = [`used ${t.uses}×`];
  if (t.last_used) parts.push(`last ${t.last_used.slice(0, 10)}`);
  switch (t.status) {
    case "allowed":
      parts.push("policy: allowed");
      break;
    case "needs_approval":
      parts.push("policy: needs approval");
      break;
    case "blocked":
      parts.push("policy: BLOCKED — used despite being denied");
      break;
    case "unmapped":
      parts.push("no policy on record for this tool");
      break;
  }
  return parts.join(" · ");
}

function ObservedToolPill({ tool }: { tool: AgentCapabilityObservedTool }) {
  return (
    <Pill className={observedPill(tool.status)} title={observedTitle(tool)}>
      {tool.drift && <AlertTriangle className="h-3 w-3" />}
      {tool.name}
      <span className="text-muted-foreground">· {tool.uses}</span>
    </Pill>
  );
}

function PillRow({ children }: { children: ReactNode }) {
  return <div className="flex flex-wrap gap-1.5">{children}</div>;
}

function Pill({
  children,
  className,
  title,
  mono,
}: {
  children: ReactNode;
  className?: string;
  title?: string;
  mono?: boolean;
}) {
  return (
    <span
      title={title}
      className={`inline-flex items-center gap-1 rounded-full border px-2 py-0.5 text-xs ${
        mono ? "font-mono" : ""
      } ${className ?? ""}`}
    >
      {children}
    </span>
  );
}

function Empty({ children }: { children: ReactNode }) {
  return <p className="text-xs text-muted-foreground">{children}</p>;
}

// Pretty-print the opaque sandbox blob for the pill tooltip. Returns "" when
// there is nothing to show.
function formatSandbox(sandbox: unknown): string {
  if (sandbox == null) return "";
  if (typeof sandbox === "string") return sandbox.trim();
  try {
    const text = JSON.stringify(sandbox, null, 2);
    return text === "{}" || text === "[]" ? "" : text;
  } catch {
    return "";
  }
}

// shortRepo trims the scheme and trailing .git so a repo pill row stays compact.
function shortRepo(url: string): string {
  return url.replace(/^https?:\/\//, "").replace(/\.git$/, "");
}
