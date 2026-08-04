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
  Settings2,
} from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import type { Agent, AgentRuntime } from "@multica/core/types";
import {
  getAgentCapabilities,
  type AgentCapabilities,
  type AgentCapabilitySkill,
  type AgentCapabilityTool,
  type AgentCapabilityConnection,
  type AgentCapabilityRepo,
  type AgentCapabilityCredential,
  type AgentCapabilitySecretSet,
  type AgentCapabilityObservedAccess,
  type AgentCapabilityObservedTool,
  type AgentCapabilityAvailabilitySummary,
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
    availability: {
      runtime_type: "",
      status: "unknown",
      verified: 0,
      discovered: 0,
      declared: 0,
      unproven: 0,
    },
    limits: { mcp_servers: [], has_mcp_config: false },
    runtime_options: {
      status: "unknown",
      provider: "",
      cli_version: "",
      runtime_id: "",
      exec_options: [],
      silently_ignored: [],
    },
  };

  const sandboxText = formatSandbox(caps.limits.sandbox);

  return (
    <div className="flex flex-col gap-5">
      <section
        className="overflow-hidden rounded-lg border bg-card"
        aria-labelledby="agent-access-guide"
      >
        <div className="border-b px-4 py-3">
          <h2 id="agent-access-guide" className="text-sm font-semibold">
            What can this agent do?
          </h2>
          <p className="mt-1 text-xs text-muted-foreground">
            This page is the agent&apos;s current general access. Use Task access
            on a run for the narrower answer on one task.
          </p>
        </div>
        <div className="grid gap-px bg-border md:grid-cols-3">
          <AccessGuideItem
            icon={Activity}
            title="General access"
            body="Read the lists below. Green is callable, amber needs Approval, and red is blocked."
          />
          <AccessGuideItem
            icon={Lock}
            title="One task"
            body="Open the task run and expand Task access. Its locked list is checked on every tool call."
          />
          <AccessGuideItem
            icon={Settings2}
            title="Change access"
            body="Use Settings → Permissions for defaults and Permission profiles, or the agent's Tools page for its specific rule."
          />
        </div>
      </section>

      <p className="text-sm text-muted-foreground">
        Registered and observed access for this agent — the same fields agents
        read via the CLI and MCP. Unknown sources are marked explicitly.
      </p>

      {/* Skills — two layers the runtime loads: workspace-bound + built-in */}
      <SkillsSection skills={caps.skills} />

      {/* Tools + permissions */}
      <Section icon={Wrench} title="Tools" count={caps.tools.length}>
        {/* FIR-3398 — availability headline: how many granted tools are actually
            PROVED on the agent's runtime vs merely declared. status=unknown means
            the runtime's proof could not be read (never "nothing is proved"). */}
        <ToolsAvailabilitySummary
          availability={caps.availability}
          total={caps.tools.length}
        />
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
              <Pill
                key={c.name}
                className={permissionPill(c.permission)}
                title={credentialTitle(c)}
              >
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

function AccessGuideItem({
  icon: Icon,
  title,
  body,
}: {
  icon: ComponentType<{ className?: string }>;
  title: string;
  body: string;
}) {
  return (
    <div className="min-w-0 bg-card p-4">
      <div className="flex items-center gap-2 text-sm font-medium">
        <Icon className="size-4 text-muted-foreground" />
        {title}
      </div>
      <p className="mt-1.5 text-xs leading-relaxed text-muted-foreground">
        {body}
      </p>
    </div>
  );
}

// permissionPill maps the effective verdict to pill colours, reusing the cerebro
// fork's emerald/amber/destructive permission palette. Enum
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

// credentialTitle builds the hover text for a credential pill. When the per-actor
// grant is live (cerebro_credentials_per_actor on) the verdict leads; otherwise it
// falls back to the credential type so the pill is never tooltip-less.
function credentialTitle(c: AgentCapabilityCredential): string {
  const parts: string[] = [];
  if (c.permission) parts.push(`grant: ${c.permission}`);
  if (c.type) parts.push(c.type);
  return parts.join(" · ");
}

function toolTitle(t: AgentCapabilityTool): string {
  const parts = [t.permission || "unknown"];
  if (t.delivery_channel) parts.push(`delivery: ${t.delivery_channel}`);
  if (t.decided_by) parts.push(`via ${t.decided_by}`);
  if (t.reason) parts.push(t.reason);
  if (t.managed_externally)
    parts.push(`managed by ${t.external_security_owner || "an external security gate"}`);
  if (t.capped_by_groups.length > 0)
    parts.push(`capped by ${t.capped_by_groups.join(", ")}`);
  if (t.callable_identities.length > 0)
    parts.push(`exact callables: ${t.callable_identities.join(", ")}`);
  if (t.authorized_callables.length > 0)
    parts.push(`authorized callables: ${t.authorized_callables.join(", ")}`);
  if (t.verdict)
    parts.push(`Task Mandate: ${t.verdict.code} · recovery: ${t.verdict.recovery_action}`);
  // FIR-3398: fold the tool's runtime-availability proof into the tooltip, so a
  // reviewer hovering a pill sees whether the granted tool is actually present on
  // the agent's runtime — not just that policy allows it.
  parts.push(
    t.availability.proven
      ? "proved on runtime"
      : `not proved on runtime: ${t.availability.reason}`,
  );
  if (hasEffectiveTruth(t)) {
    parts.push(`allowed: ${yesNo(t.allowed)}`);
    parts.push(`available: ${yesNo(t.available)}`);
    parts.push(`enforced: ${yesNo(t.enforced)}`);
    parts.push(`callable: ${yesNo(t.callable)}`);
    parts.push(`verified: ${yesNo(t.verified)}`);
    if (t.blocked_reason) parts.push(`blocked: ${t.blocked_reason}`);
    if (t.how_to_fix) parts.push(`how to fix: ${t.how_to_fix}`);
  }
  return parts.join(" · ");
}

function hasEffectiveTruth(t: AgentCapabilityTool): boolean {
  return [t.allowed, t.available, t.enforced, t.callable, t.verified].some(
    (value) => typeof value === "boolean",
  );
}

function yesNo(value: boolean | undefined): string {
  return typeof value === "boolean" ? (value ? "yes" : "no") : "unknown";
}

// ToolsAvailabilitySummary is the FIR-3398 headline for the Tools section: of the
// tools policy grants this agent, how many are actually PROVED present on its
// runtime. status=unknown (the runtime's proof could not be read) shows an
// explicit "unknown" note rather than implying nothing is proved — an empty proof
// must never read as "no tools work". Renders nothing when there are no tools.
function ToolsAvailabilitySummary({
  availability,
  total,
}: {
  availability: AgentCapabilityAvailabilitySummary;
  total: number;
}) {
  if (total === 0) return null;
  const runtime = availability.runtime_type
    ? ` ${availability.runtime_type}`
    : "";
  return (
    <div className="flex flex-wrap items-center gap-1.5">
      <StatusBadge status={availability.status} />
      {availability.status === "known" ? (
        <span className="text-xs text-muted-foreground">
          {availability.verified} of {total} proved on the{runtime} runtime
          {availability.discovered > 0
            ? ` · ${availability.discovered} discovered`
            : ""}
          {availability.declared > 0
            ? ` · ${availability.declared} only declared`
            : ""}
        </span>
      ) : (
        <span className="text-xs text-muted-foreground">
          runtime availability unknown — tools are shown as granted, not as proved
        </span>
      )}
    </div>
  );
}

function ToolPill({ tool }: { tool: AgentCapabilityTool }) {
  // FIR-3398: an unproved tool (policy allows it but the runtime has not shown it
  // exists) renders with a dashed border, so "granted" and "actually there" read
  // differently at a glance. The permission colour is unchanged.
  const truthPresent = hasEffectiveTruth(tool);
  const verified = tool.verified ?? tool.availability.proven;
  const availabilityCue = verified ? "" : "border-dashed";
  const verdictClass = truthPresent
    ? tool.callable
      ? permissionPill("allow")
      : tool.allowed === false
        ? permissionPill("deny")
        : permissionPill("ask")
    : permissionPill(tool.permission);
  return (
    <Pill
      className={`${verdictClass} ${availabilityCue}`}
      title={toolTitle(tool)}
    >
      {tool.title || tool.key}
      {tool.managed_externally && (
        <span className="text-muted-foreground">
          · managed by {tool.external_security_owner || "external security gate"}
        </span>
      )}
      {truthPresent && (
        <span className="text-muted-foreground">
          · {tool.callable ? "callable" : "blocked"}
        </span>
      )}
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
        <ToolPill key={p.key} tool={p} />
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
              className={capabilityTruthPill(t.callable, t.permission)}
              title={connectionActionTitle(t)}
            >
              {t.name}
              {t.callable !== undefined && (
                <span className="text-muted-foreground">
                  · {t.callable ? "callable" : "blocked"}
                </span>
              )}
            </Pill>
          ))}
        </PillRow>
      )}
      {connection.endpoints.length > 0 && (
        <PillRow>
          {connection.endpoints.map((ep) => (
            <Pill
              key={`${ep.methods.join("/")}:${ep.path}`}
              mono
              className={capabilityTruthPill(ep.callable, ep.permission)}
              title={connectionActionTitle(ep)}
            >
              <span className="text-muted-foreground">
                {ep.methods.join("/")}
              </span>
              {ep.path}
              {ep.callable !== undefined && (
                <span className="text-muted-foreground">
                  · {ep.callable ? "callable" : "blocked"}
                </span>
              )}
            </Pill>
          ))}
        </PillRow>
      )}
    </div>
  );
}

function capabilityTruthPill(callable: boolean | undefined, permission: string): string {
  if (callable === undefined) return permissionPill(permission);
  if (callable) return permissionPill("allow");
  return permission === "deny" ? permissionPill("deny") : permissionPill("ask");
}

function connectionActionTitle(action: {
  permission: string;
  allowed?: boolean;
  available?: boolean;
  enforced?: boolean;
  callable?: boolean;
  verified?: boolean;
  blocked_reason?: string;
  how_to_fix?: string;
  verdict?: { code: string; recovery_action: string };
  description?: string;
  summary?: string;
}): string {
  const lines = [
    `Permission: ${action.permission || "unknown"}`,
    `Allowed: ${yesNo(action.allowed)}`,
    `Available: ${yesNo(action.available)}`,
    `Enforced: ${yesNo(action.enforced)}`,
    `Callable: ${yesNo(action.callable)}`,
    `Verified: ${yesNo(action.verified)}`,
  ];
  if (action.description || action.summary) lines.push(action.description || action.summary || "");
  if (action.blocked_reason) lines.push(`Blocked: ${action.blocked_reason}`);
  if (action.how_to_fix) lines.push(`How to fix: ${action.how_to_fix}`);
  if (action.verdict) lines.push(`Task Mandate: ${action.verdict.code} · recovery: ${action.verdict.recovery_action}`);
  return lines.join("\n");
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

// SkillsSection renders the skills the agent loads in two clearly-labelled
// layers, mirroring what the runtime assembles at claim time: workspace-bound
// skills (the set `multica agent get` lists) and the platform built-in skills
// every agent additionally receives. Showing both — and naming the source —
// is why the card's total can exceed the CLI's workspace-only count without
// the two views contradicting each other.
function SkillsSection({ skills }: { skills: AgentCapabilitySkill[] }) {
  const builtin = skills.filter((s) => s.source === "builtin");
  const workspace = skills.filter((s) => s.source !== "builtin");
  return (
    <Section icon={BookOpenText} title="Skills" count={skills.length}>
      {skills.length === 0 ? (
        <Empty>No skills loaded.</Empty>
      ) : (
        <div className="flex flex-col gap-2">
          <p className="text-xs text-muted-foreground">
            {workspace.length} workspace · {builtin.length} built-in — the runtime
            loads both. <code>multica agent get</code> lists only the{" "}
            {workspace.length} workspace skill
            {workspace.length === 1 ? "" : "s"}; the built-in skills load for
            every agent.
          </p>
          {workspace.length > 0 && (
            <PillRow>
              {workspace.map((s) => (
                <Pill key={s.id || s.name} title={s.description}>
                  {s.name}
                </Pill>
              ))}
            </PillRow>
          )}
          {builtin.length > 0 && (
            <PillRow>
              {builtin.map((s) => (
                <Pill
                  key={s.id || s.name}
                  className="border-dashed text-muted-foreground"
                  title={s.description || "Platform built-in skill"}
                >
                  {s.name}
                  <span className="text-muted-foreground">· built-in</span>
                </Pill>
              ))}
            </PillRow>
          )}
        </div>
      )}
    </Section>
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

// ObservedAccessSection renders what the agent used or attempted recently (Bid
// B). Drift means the real call outcome disagreed with declared policy or the
// Task Mandate.
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
          tools used or attempted in the last {observed.window_days} days
          {observed.task_count > 0
            ? ` · ${observed.task_count} task${observed.task_count === 1 ? "" : "s"}`
            : ""}
        </span>
      </div>

      {observed.drift_count > 0 && (
        <p className="mt-1.5 flex items-start gap-1 text-xs text-destructive">
          <AlertTriangle className="mt-0.5 h-3 w-3 shrink-0" />
          <span>
            <strong>
              Reviewer: {observed.drift_count} of the {observed.tools.length} tools
              below (red) were used or attempted but actual access did not match
              the declared policy
            </strong>{" "}
            — blocked, rejected by the Task Mandate, or unmapped. Confirm each
            is expected, or correct the access configuration. The other tools
            match the declared policy.
          </span>
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

      {/* Honest-scope disclaimer: only tool use is recorded. Secret/credential
          use is not tracked anywhere, so this section can never show which
          secrets an agent actually read — an empty secret signal here would be
          false assurance, so we say so explicitly. */}
      <p className="mt-1.5 text-xs text-muted-foreground">
        Tool use only. Secret and credential <em>use</em> is not tracked — this
        section cannot show which secrets an agent actually read, only which
        tools it ran.
      </p>
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
  const parts: string[] = [];
  if (t.uses > 0) {
    parts.push(`used ${t.uses}×`);
  }
  if (t.mandate_denials > 0) {
    parts.push(`Task Mandate denied ${t.mandate_denials}×`);
  }
  if (t.last_used) parts.push(`last ${t.last_used.slice(0, 10)}`);
  switch (t.status) {
    case "allowed":
      parts.push("policy: allowed");
      break;
    case "needs_approval":
      parts.push("policy: needs approval");
      break;
    case "blocked":
      parts.push(
        t.mandate_denials > 0
          ? t.permission === "allow"
            ? "policy: allowed, but Task Mandate blocked the call"
            : `Task Mandate blocked the call; policy: ${t.permission || "unmapped"}`
          : "policy: BLOCKED — used despite being denied",
      );
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
      <span className="text-muted-foreground">
        ·{" "}
        {tool.mandate_denials > 0
          ? `${tool.mandate_denials} denied`
          : tool.uses}
      </span>
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
