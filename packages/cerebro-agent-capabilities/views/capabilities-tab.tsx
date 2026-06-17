// TECH-3642 — the per-agent Capabilities tab on the agent detail page.
//
// "Capabilities" is the word agents already use (it matches the MCP/agent
// interop standards), so a human reading the UI and an agent reading via
// `multica agent capabilities` / the `get_agent_capabilities` MCP tool meet the
// exact same concept and the exact same fields. This tab is the human surface
// of GET /api/agents/{id}/capabilities; the CLI and MCP are the agent surfaces.
//
// The card is read-only: it is a consolidated *view* of four facts that already
// live elsewhere (skills, tool grants, credentials, sandbox/MCP limits). Editing
// each of those still happens on its own dedicated tab — this tab answers
// "what can this agent do and what is it bounded by?" in one place.

"use client";

import type { ComponentType, ReactNode } from "react";
import { ShieldCheck, BookOpenText, Wrench, KeyRound, Lock } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import type { Agent, AgentRuntime } from "@multica/core/types";
import { Badge } from "@multica/ui/components/ui/badge";
import {
  getAgentCapabilities,
  type AgentCapabilities,
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
    credentials: [],
    limits: { mcp_servers: [], has_mcp_config: false },
  };

  const enabledTools = caps.tools.filter((t) => t.enabled);
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

      {/* MAY — enabled platform tool grants. */}
      <Section
        icon={Wrench}
        title="May use"
        subtitle="Tools"
        count={enabledTools.length}
      >
        {enabledTools.length === 0 ? (
          <Empty>No tools enabled.</Empty>
        ) : (
          <div className="flex flex-wrap gap-1.5">
            {enabledTools.map((t) => (
              <Badge key={t.name} variant="secondary">
                {t.name}
              </Badge>
            ))}
          </div>
        )}
      </Section>

      {/* ACCESS — credential names/types only, never values. */}
      <Section
        icon={KeyRound}
        title="Has access to"
        subtitle="Credentials"
        count={caps.credentials.length}
      >
        {caps.credentials.length === 0 ? (
          <Empty>No credentials bound.</Empty>
        ) : (
          <ul className="flex flex-col gap-2">
            {caps.credentials.map((c) => (
              <li
                key={c.name}
                className="flex items-center justify-between gap-2 rounded-md border p-3"
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
