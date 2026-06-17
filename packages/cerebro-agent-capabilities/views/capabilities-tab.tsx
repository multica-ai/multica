// TECH-3642 — the per-agent Capabilities tab on the agent detail page.
//
// "Capabilities" is the word agents already use (it matches the MCP/agent
// interop standards), so a human reading the UI and an agent reading via
// `multica agent capabilities` / the `get_agent_capabilities` MCP tool meet the
// exact same concept and the exact same fields. This tab is the human surface
// of GET /api/agents/{id}/capabilities; the CLI and MCP are the agent surfaces.
//
// The card is a consolidated, read-only *view* of facts that already live
// elsewhere, read from the SAME stores their dedicated admin tabs use:
//   - Skills        → what the agent CAN do
//   - Tools         → what it MAY use, each with its effective permission
//                     (allow/ask/block) and which layer decided it
//   - Connections   → external systems it reaches + their endpoints / tools
//   - Credentials   → bound secrets + Infisical folders it may read (names only)
//   - Limits        → sandbox + MCP boundaries it runs inside
// Editing each still happens on its own dedicated tab.

"use client";

import type { ComponentType, ReactNode } from "react";
import {
  ShieldCheck,
  BookOpenText,
  Wrench,
  Plug,
  KeyRound,
  Lock,
} from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import type { Agent, AgentRuntime } from "@multica/core/types";
import { Badge } from "@multica/ui/components/ui/badge";
import {
  getAgentCapabilities,
  type AgentCapabilities,
  type AgentCapabilityTool,
  type AgentCapabilityConnection,
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
    connections: [],
    credentials: [],
    infisical_secrets: [],
    limits: { mcp_servers: [], has_mcp_config: false },
  };

  const sandboxText = formatSandbox(caps.limits.sandbox);

  return (
    <div className="flex flex-col gap-6">
      <p className="text-sm text-muted-foreground">
        Everything this agent can do, may use, has access to, and is limited by —
        the same fields agents read via the CLI and MCP.
      </p>

      {/* CAN — skills the agent loads. */}
      <Section
        icon={BookOpenText}
        title="Can do"
        subtitle="Skills"
        count={caps.skills.length}
      >
        {caps.skills.length === 0 ? (
          <Empty>No skills loaded.</Empty>
        ) : (
          <ul className="flex flex-col gap-2">
            {caps.skills.map((s) => (
              <li key={s.id || s.name} className="rounded-md border p-3">
                <p className="text-sm font-medium">{s.name}</p>
                {s.description && (
                  <p className="mt-0.5 text-xs text-muted-foreground">
                    {s.description}
                  </p>
                )}
              </li>
            ))}
          </ul>
        )}
      </Section>

      {/* MAY — every tool resolved for this agent, with its effective verdict. */}
      <Section
        icon={Wrench}
        title="May use"
        subtitle="Tools & permissions"
        count={caps.tools.length}
      >
        {caps.tools.length === 0 ? (
          <Empty>No tools resolved.</Empty>
        ) : (
          <div className="overflow-hidden rounded-md border">
            <div className="grid grid-cols-[1fr_auto] items-center gap-4 border-b bg-muted/30 px-3 py-2 text-xs font-medium text-muted-foreground">
              <span>Tool</span>
              <span>Permission</span>
            </div>
            <ul>
              {caps.tools.map((t) => (
                <ToolRow key={`${t.key}:${t.source}`} tool={t} />
              ))}
            </ul>
            <PermissionLegend />
          </div>
        )}
      </Section>

      {/* CONNECTIONS — external systems + their endpoints / tools. */}
      <Section
        icon={Plug}
        title="Connections"
        subtitle="Endpoints & tools"
        count={caps.connections.length}
      >
        {caps.connections.length === 0 ? (
          <Empty>No connections.</Empty>
        ) : (
          <div className="flex flex-col gap-3">
            {caps.connections.map((c) => (
              <ConnectionCard key={c.name} connection={c} />
            ))}
          </div>
        )}
      </Section>

      {/* ACCESS — credential + Infisical names only, never values. */}
      <Section
        icon={KeyRound}
        title="Has access to"
        subtitle="Credentials & Infisical secrets"
      >
        <div className="grid gap-4 sm:grid-cols-2">
          <div>
            <p className="mb-2 text-xs font-medium uppercase tracking-wide text-muted-foreground">
              Platform credentials · {caps.credentials.length}
            </p>
            {caps.credentials.length === 0 ? (
              <Empty>No credentials bound.</Empty>
            ) : (
              <ul className="flex flex-col gap-2">
                {caps.credentials.map((c) => (
                  <li
                    key={c.name}
                    className="flex items-center justify-between gap-2 rounded-md border p-2.5"
                  >
                    <span className="text-sm font-medium">{c.name}</span>
                    {c.type && (
                      <Badge variant="outline" className="shrink-0">
                        {c.type}
                      </Badge>
                    )}
                  </li>
                ))}
              </ul>
            )}
          </div>
          <div>
            <p className="mb-2 text-xs font-medium uppercase tracking-wide text-muted-foreground">
              Infisical secrets · {caps.infisical_secrets.length}
            </p>
            {caps.infisical_secrets.length === 0 ? (
              <Empty>No Infisical folders.</Empty>
            ) : (
              <ul className="flex flex-col gap-2">
                {caps.infisical_secrets.map((s) => (
                  <li
                    key={`${s.environment}:${s.path}`}
                    className="flex items-center justify-between gap-2 rounded-md border p-2.5"
                  >
                    <span className="font-mono text-xs">{s.path}</span>
                    {s.environment && (
                      <Badge variant="outline" className="shrink-0">
                        {s.environment}
                      </Badge>
                    )}
                  </li>
                ))}
              </ul>
            )}
          </div>
        </div>
      </Section>

      {/* LIMITS — sandbox policy + MCP server surface. */}
      <Section icon={Lock} title="Limited by" subtitle="Sandbox · MCP">
        <div className="flex flex-col gap-3">
          <div>
            <p className="text-xs font-medium text-muted-foreground">Sandbox</p>
            {sandboxText ? (
              <pre className="mt-1 overflow-x-auto rounded-md border bg-muted/40 p-3 text-xs">
                {sandboxText}
              </pre>
            ) : (
              <Empty>No sandbox policy.</Empty>
            )}
          </div>
          <div>
            <p className="text-xs font-medium text-muted-foreground">
              MCP servers
            </p>
            {caps.limits.mcp_servers.length === 0 ? (
              <Empty>
                {caps.limits.has_mcp_config
                  ? "MCP config present, no named servers."
                  : "No MCP config."}
              </Empty>
            ) : (
              <div className="mt-1 flex flex-wrap gap-1.5">
                {caps.limits.mcp_servers.map((name) => (
                  <Badge key={name} variant="secondary">
                    {name}
                  </Badge>
                ))}
              </div>
            )}
          </div>
        </div>
      </Section>
    </div>
  );
}

// permissionDisplay maps the server's effective verdict to a label + colour,
// reusing the cerebro fork's emerald/amber/destructive palette (see
// simple-tool-policy-table). Enum drift downgrades to a neutral "Unknown"
// rather than crashing (API Response Compatibility rule).
function permissionDisplay(permission: string): {
  label: string;
  className: string;
} {
  switch (permission) {
    case "allow":
      return { label: "Allow", className: "text-emerald-700 dark:text-emerald-300" };
    case "ask":
      return { label: "Ask", className: "text-amber-700 dark:text-amber-400" };
    case "deny":
      return { label: "Block", className: "text-destructive" };
    default:
      return { label: permission || "Unknown", className: "text-muted-foreground" };
  }
}

function ToolRow({ tool }: { tool: AgentCapabilityTool }) {
  const verdict = permissionDisplay(tool.permission);
  return (
    <li className="grid grid-cols-[1fr_auto] items-center gap-4 border-b px-3 py-2.5 last:border-b-0">
      <div className="min-w-0">
        <div className="truncate font-mono text-sm">{tool.title || tool.key}</div>
        {(tool.source || tool.managed_externally) && (
          <div className="truncate text-xs text-muted-foreground">
            {tool.source}
            {tool.managed_externally ? " · managed elsewhere" : ""}
          </div>
        )}
      </div>
      <div className="text-right">
        <span className={`text-sm font-medium ${verdict.className}`}>
          {verdict.label}
        </span>
        {tool.decided_by && (
          <span className="ml-2 text-xs text-muted-foreground">
            via {tool.decided_by}
          </span>
        )}
        {tool.capped_by_groups.length > 0 && (
          <div className="text-xs text-muted-foreground">
            capped by {tool.capped_by_groups.join(", ")}
          </div>
        )}
      </div>
    </li>
  );
}

function PermissionLegend() {
  return (
    <div className="flex flex-wrap gap-x-4 gap-y-1 border-t bg-muted/20 px-3 py-2 text-xs text-muted-foreground">
      <span>
        <span className="font-medium text-emerald-700 dark:text-emerald-300">
          Allow
        </span>{" "}
        — may call freely
      </span>
      <span>
        <span className="font-medium text-amber-700 dark:text-amber-400">
          Ask
        </span>{" "}
        — needs approval
      </span>
      <span>
        <span className="font-medium text-destructive">Block</span> — denied
      </span>
    </div>
  );
}

function ConnectionCard({
  connection,
}: {
  connection: AgentCapabilityConnection;
}) {
  const isMcp = connection.type === "mcp_http";
  return (
    <div className="overflow-hidden rounded-md border">
      <div className="flex items-center gap-2 border-b bg-muted/30 px-3 py-2">
        <Badge variant="secondary" className="shrink-0">
          {isMcp ? "MCP" : "API"}
        </Badge>
        <span className="text-sm font-medium">
          {connection.display_name || connection.name}
        </span>
        {connection.url && (
          <span className="ml-auto truncate font-mono text-xs text-muted-foreground">
            {connection.url}
          </span>
        )}
      </div>
      <div className="px-3 py-2.5">
        {connection.tools.length > 0 && (
          <div className="mb-2">
            <p className="mb-1.5 text-xs uppercase tracking-wide text-muted-foreground">
              Tools · {connection.tools.length}
            </p>
            <div className="flex flex-wrap gap-1.5">
              {connection.tools.map((t) => (
                <Badge key={t.name} variant="outline" title={t.description}>
                  {t.name}
                </Badge>
              ))}
            </div>
          </div>
        )}
        {connection.endpoints.length > 0 && (
          <div>
            <p className="mb-1.5 text-xs uppercase tracking-wide text-muted-foreground">
              Endpoints · {connection.endpoints.length}
            </p>
            <ul className="flex flex-col gap-1">
              {connection.endpoints.map((ep) => (
                <li key={ep.path} className="flex items-center gap-2">
                  <span className="flex shrink-0 gap-1">
                    {ep.methods.map((m) => (
                      <Badge key={m} variant="outline" className="px-1.5 py-0 text-[10px]">
                        {m}
                      </Badge>
                    ))}
                  </span>
                  <span className="truncate font-mono text-xs">{ep.path}</span>
                </li>
              ))}
            </ul>
          </div>
        )}
        {connection.tools.length === 0 &&
          connection.endpoints.length === 0 && (
            <Empty>No endpoints or tools discovered.</Empty>
          )}
      </div>
    </div>
  );
}

function Section({
  icon: Icon,
  title,
  subtitle,
  count,
  children,
}: {
  icon: ComponentType<{ className?: string }>;
  title: string;
  subtitle: string;
  count?: number;
  children: ReactNode;
}) {
  return (
    <section className="flex flex-col gap-2">
      <div className="flex items-center gap-2">
        <Icon className="h-4 w-4 text-muted-foreground" />
        <h3 className="text-sm font-medium">{title}</h3>
        <span className="text-xs text-muted-foreground">{subtitle}</span>
        {typeof count === "number" && (
          <span className="text-xs text-muted-foreground">· {count}</span>
        )}
      </div>
      {children}
    </section>
  );
}

function Empty({ children }: { children: ReactNode }) {
  return <p className="text-xs text-muted-foreground">{children}</p>;
}

// Pretty-print the opaque sandbox blob. It arrives as already-parsed JSON
// (object/array/string) from the lenient schema; stringify objects, pass
// primitives through. Returns "" when there is nothing to show.
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
