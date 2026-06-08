"use client";

// FIR-2409 — the friendly "Agent-start" surface onto the trigger_other_agent
// capability. The full tool-policy table (ToolPolicyTable) exposes every tool at
// every layer and is deliberately technical; this view shows ONLY "who may start
// an agent they do not own", in plain language, at the two layers an owner edits
// most: the workspace-wide default and a per-agent override. It reuses the same
// persisted chain + mutations as the table, so there is no parallel model — only
// a focused presentation. Group/member overrides remain available in the full
// table; this tab is the everyday control Jesper asked for.

import { Bot, ShieldAlert, ShieldCheck, ShieldQuestion } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { agentListOptions } from "@multica/core/workspace/queries";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import {
  toolPolicyTableOptions,
  useClearToolPolicy,
  useSetToolPolicy,
  type ToolSetting,
} from "../core";
import type { ExtraSettingsTab } from "./workspace-settings-tab";

// The capability key the mention gate resolves (see platformcatalog +
// server/internal/cerebro/mentiongate/gate.go). Never rename — stored rows bind
// to it.
const TRIGGER_KEY = "trigger_other_agent";

// The three concrete choices for "may someone start an agent they don't own",
// phrased the way the issue describes them (run-request vs. fully blocked).
const CHOICES: {
  value: Exclude<ToolSetting, "inherit">;
  label: string;
  help: string;
  icon: React.ComponentType<{ className?: string }>;
}[] = [
  {
    value: "allow",
    label: "Others can start",
    help: "Anyone with access can start the agent.",
    icon: ShieldCheck,
  },
  {
    value: "ask",
    label: "Run request",
    help: "A mention becomes a run request to the owner.",
    icon: ShieldQuestion,
  },
  {
    value: "deny",
    label: "Owner only",
    help: "Only the owner can start the agent.",
    icon: ShieldAlert,
  },
];

// SegmentedChoice renders the 3 (or 4, with an Inherit option) buttons. The
// caller passes the current value and a setter; `null` selected means "follow
// the default" (only offered on the per-agent rows via includeInherit).
function SegmentedChoice({
  value,
  onChange,
  includeInherit,
  disabled,
}: {
  value: ToolSetting | null;
  onChange: (next: ToolSetting) => void;
  includeInherit?: boolean;
  disabled?: boolean;
}) {
  return (
    <div className="flex flex-wrap gap-1.5">
      {includeInherit ? (
        <Button
          type="button"
          size="sm"
          variant={!value || value === "inherit" ? "default" : "outline"}
          disabled={disabled}
          onClick={() => onChange("inherit")}
        >
          Follow default
        </Button>
      ) : null}
      {CHOICES.map((c) => {
        const Icon = c.icon;
        const selected = value === c.value;
        return (
          <Button
            key={c.value}
            type="button"
            size="sm"
            variant={selected ? "default" : "outline"}
            disabled={disabled}
            onClick={() => onChange(c.value)}
            title={c.help}
          >
            <Icon className={cn("size-3.5", selected ? "" : "text-muted-foreground")} />
            {c.label}
          </Button>
        );
      })}
    </div>
  );
}

// WorkspaceDefault edits the workspace-layer setting of trigger_other_agent.
// A null/absent workspace setting means today's baseline applies (owners/admins
// master key, members blocked) — shown as "Andre må starte" only when explicitly
// set; otherwise we surface the effective default text below the control.
function WorkspaceDefault({ wsId }: { wsId: string }) {
  const { data: rows } = useQuery(toolPolicyTableOptions(wsId, {}));
  const setPolicy = useSetToolPolicy();
  const row = rows?.find((r) => r.tool_key === TRIGGER_KEY && !r.resource_pattern);
  const current = row?.layers.workspace ?? null;

  return (
    <div className="rounded-lg border p-4 space-y-3">
      <div>
        <h3 className="text-sm font-semibold">Default for the entire workspace</h3>
        <p className="text-sm text-muted-foreground">
          Applies when an agent has no own rule below. If you change nothing,
          today's behavior applies: owners and admins can start anything, ordinary
          members send a run request.
        </p>
      </div>
      <SegmentedChoice
        value={current}
        disabled={setPolicy.isPending}
        onChange={(next) =>
          setPolicy.mutate({
            tool_key: TRIGGER_KEY,
            layer: "workspace",
            subject_id: wsId,
            setting: next,
          })
        }
      />
    </div>
  );
}

// AgentTriggerRow edits the agent-layer override for one agent. Inherit (the
// default) clears the row so the agent follows the workspace default.
function AgentTriggerRow({
  wsId,
  agent,
}: {
  wsId: string;
  agent: { id: string; name: string; visibility: string };
}) {
  const { data: rows } = useQuery(toolPolicyTableOptions(wsId, { agentId: agent.id }));
  const setPolicy = useSetToolPolicy();
  const clearPolicy = useClearToolPolicy();
  const row = rows?.find((r) => r.tool_key === TRIGGER_KEY && !r.resource_pattern);
  const current = row?.layers.agent ?? null;
  const busy = setPolicy.isPending || clearPolicy.isPending;

  function onChange(next: ToolSetting) {
    if (next === "inherit") {
      clearPolicy.mutate({ tool_key: TRIGGER_KEY, layer: "agent", subject_id: agent.id });
      return;
    }
    setPolicy.mutate({
      tool_key: TRIGGER_KEY,
      layer: "agent",
      subject_id: agent.id,
      setting: next,
    });
  }

  return (
    <div className="flex flex-col gap-2 border-b py-3 last:border-b-0 sm:flex-row sm:items-center sm:justify-between">
      <div className="flex min-w-0 items-center gap-2">
        <Bot className="size-4 shrink-0 text-muted-foreground" />
        <span className="truncate text-sm font-medium">{agent.name}</span>
        {agent.visibility === "private" ? (
          <Badge variant="outline" className="shrink-0">
            Privat
          </Badge>
        ) : null}
      </div>
      <SegmentedChoice value={current} includeInherit disabled={busy} onChange={onChange} />
    </div>
  );
}

export function AgentTriggerTab() {
  const wsId = useWorkspaceId();
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const active = agents.filter((a) => !a.archived_at);

  return (
    <div className="space-y-4">
      <div>
        <h2 className="text-base font-semibold">Agent start</h2>
        <p className="text-sm text-muted-foreground">
          Control who may start (trigger) an agent they do not own. Set a
          default for the entire workspace, and give individual agents their own
          rule where it makes sense.
        </p>
      </div>

      <WorkspaceDefault wsId={wsId} />

      <div className="rounded-lg border p-4">
        <h3 className="mb-1 text-sm font-semibold">Per agent</h3>
        <p className="mb-2 text-sm text-muted-foreground">
          A rule here overrides the default for that specific agent. "Follow default"
          removes the rule again.
        </p>
        {active.length === 0 ? (
          <p className="py-3 text-sm text-muted-foreground">No agents yet.</p>
        ) : (
          <div>
            {active.map((a) => (
              <AgentTriggerRow
                key={a.id}
                wsId={wsId}
                agent={{ id: a.id, name: a.name, visibility: a.visibility }}
              />
            ))}
          </div>
        )}
      </div>
    </div>
  );
}

// useCerebroAgentTriggerSettingsTabs returns the Agent-start tab when the
// cerebro_agent_trigger_permissions flag is on. Spread into SettingsPage's
// extraAccountTabs alongside the other cerebro permission tabs.
export function useCerebroAgentTriggerSettingsTabs(): ExtraSettingsTab[] {
  const enabled = useFeatureFlag("cerebro_agent_trigger_permissions");
  if (!enabled) return [];
  return [
    {
      value: "agent-start",
      label: "Agent-start",
      icon: Bot,
      content: <AgentTriggerTab />,
    },
  ];
}
