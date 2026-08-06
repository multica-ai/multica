"use client";

// FIR-3212 — approval consequences (mockup M3). The field diff beside this panel
// proves WHICH fields changed. It cannot answer the only question the approver
// actually has: if I click Approve, what changes about what this agent does?
//
// The gap is not cosmetic. An agent on hermes has its system prompt discarded
// (hermes.go:72), so a proposal that rewrites its instructions renders as a
// large, confident green diff and changes nothing at all. The approval is
// correctly versioned, correctly reviewed, and a no-op — and nothing in the UI
// ever said so.
//
// Honesty rules (why this is not a second diff):
//   - A change the engine drops SILENTLY ranks above one it drops with a log
//     line. Both do nothing; only one leaves evidence. The severity is in the
//     DOM, not just the colour.
//   - A field Multica applies itself (sandbox, skills, secret names) takes
//     effect on every engine. Crediting the engine for our own enforcement
//     would mislead the moment the agent is moved.
//   - An instruction that survives as prepended text has NOT simply taken
//     effect — it arrives with its authority removed, and that gets its own
//     sentence.
//   - An engine with no matrix entry renders as "we cannot say". Telling an
//     owner their proposal does nothing, when we simply have no entry, would
//     block a change that works.
//
// The consequences are computed server-side (capabilities.ApprovalImpactFor).
// This file renders them; it must not re-derive them from the handling strings.

import { useQuery } from "@tanstack/react-query";
import { Check, ShieldAlert, TriangleAlert, Info } from "lucide-react";
import type { Agent } from "@multica/core/types";
import {
  getAgentCapabilityApproval,
  type AgentCapabilityApprovalFieldConsequence,
} from "@multica/cerebro-agent-capabilities";
import { Badge } from "@multica/ui/components/ui/badge";

// Why a change does not land, in the approver's terms. Keyed on the engine's
// handling — an unrecognised value falls through to a neutral sentence rather
// than crashing (enum drift downgrades, not crashes).
const NO_EFFECT_REASONS: Record<string, string> = {
  ignored_silent:
    "The selected engine will ignore this setting and will not leave a warning.",
  ignored_logged:
    "The selected engine will ignore this setting and record a warning.",
};

const DELIVERY_NOTES: Record<string, string> = {
  prepended:
    "The selected engine receives the role as part of the task instead of as a high-priority instruction.",
  ignored: "The selected engine cannot use this custom role.",
};

const CONSEQUENCE_NOTES: Record<string, string> = {
  no_runtime_effect: "This is descriptive only and does not change a run.",
  unknown_field: "We cannot yet confirm how this changes a run.",
};

const FIELD_LABELS: Record<string, string> = {
  instructions: "Role and instructions",
  runtime_id: "Where it runs",
  model: "Model",
  thinking_level: "Reasoning effort",
  workspace_brief_mode: "Workspace guidance",
  tools_brief_mode: "Tool descriptions",
  system_prompt_mode: "Instruction delivery",
  speed_mode: "Response speed",
  max_turns: "Maximum steps",
  timeout_minutes: "Time limit",
  description: "Internal description",
  skills: "Skills",
  allowed_secret_names: "Secrets",
};

function fieldLabel(field: string): string {
  return FIELD_LABELS[field] ?? field.replaceAll("_", " ");
}

export function AgentContextApprovalImpact({
  agent,
  changedFields,
}: {
  agent: Agent;
  changedFields: string[];
}) {
  const { data, isLoading, isError } = useQuery({
    queryKey: [
      "cerebro",
      "agent-capability-approval",
      agent.id,
      // The field list is part of the identity of the answer: two proposals on
      // the same agent touching different fields are different questions.
      changedFields.join(","),
    ],
    queryFn: () => getAgentCapabilityApproval(agent.id, changedFields),
    enabled: changedFields.length > 0,
  });

  // A proposal that changes nothing has no consequences to explain. Rendering an
  // empty box would read as a loading state that never resolves.
  if (changedFields.length === 0) return null;

  if (isLoading) {
    return (
      <p className="text-xs text-muted-foreground">
        Checking what will change after approval…
      </p>
    );
  }
  if (isError || !data) {
    return (
      <p className="text-xs text-muted-foreground">
        The outcome summary is unavailable right now. The exact change is still
        shown above.
      </p>
    );
  }

  const { impact, runtime } = data;

  if (impact.status !== "known") {
    return (
      <p className="text-xs text-muted-foreground">
        We cannot confirm how the selected engine handles these settings. This
        does not mean the change will fail; it means the outcome is not known.
      </p>
    );
  }

  // Silent first: a drop nobody logs is the one an approver keeps trusting.
  const ineffective = impact.fields
    .filter(
      (row) =>
        row.consequence === "no_effect_silent" ||
        row.consequence === "no_effect_logged",
    )
    .sort((a, b) => Number(b.silent) - Number(a.silent));
  const effective = impact.fields.filter(
    (row) => row.consequence === "takes_effect",
  );
  // Neither a win nor an engine drop: reported so the panel never silently
  // omits a field the diff above is showing.
  const inert = impact.fields.filter(
    (row) =>
      row.consequence === "no_runtime_effect" ||
      row.consequence === "unknown_field",
  );
  const deliveryNote = impact.system_prompt
    ? DELIVERY_NOTES[impact.system_prompt.delivery]
    : undefined;

  return (
    <div className="space-y-3">
      <div>
        <p className="text-sm font-semibold">What changes after approval</p>
        <p className="mt-1 text-xs text-muted-foreground">
          {effective.length}{" "}
          {effective.length === 1 ? "change takes" : "changes take"} effect on
          the next run
          {ineffective.length > 0
            ? `; ${ineffective.length} ${
                ineffective.length === 1 ? "change does" : "changes do"
              } not`
            : ""}
          .
        </p>
      </div>

      {ineffective.length > 0 && (
        <div className="rounded-md border border-amber-500/40 bg-amber-500/10 p-2.5">
          <div className="flex items-center gap-1.5 text-xs font-medium">
            <TriangleAlert className="size-3.5 text-amber-600" />
            Will not work on the selected engine
          </div>
          <div className="mt-2 space-y-2">
            {ineffective.map((row) => (
              <IneffectiveField key={row.field} row={row} />
            ))}
          </div>
        </div>
      )}

      {deliveryNote && (
        <div className="rounded-md border p-2.5">
          <div className="mb-1 text-xs font-medium text-muted-foreground">
            How the role is delivered
          </div>
          <p className="text-xs text-muted-foreground">{deliveryNote}</p>
        </div>
      )}

      {effective.length > 0 && (
        <div>
          <div className="mb-1.5 text-xs font-medium text-muted-foreground">
            Changes on the next run
          </div>
          <div className="space-y-1.5">
            {effective.map((row) => (
              <EffectiveField key={row.field} row={row} />
            ))}
          </div>
        </div>
      )}

      {inert.map((row) => (
        <p
          key={row.field}
          data-testid={`approval-inert-${row.field}`}
          className="flex items-center gap-1.5 text-xs text-muted-foreground"
        >
          <Info className="size-3.5 shrink-0" />
          <span className="font-medium text-foreground">
            {fieldLabel(row.field)}
          </span>
          {CONSEQUENCE_NOTES[row.consequence] ?? "We cannot say."}
        </p>
      ))}

      <details className="rounded-md border border-dashed px-3 py-2">
        <summary className="cursor-pointer text-xs font-medium">
          Advanced details
        </summary>
        <div className="mt-2 flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
          <span>Engine</span>
          <Badge variant="outline">{impact.provider}</Badge>
          {runtime.cli_version ? (
            <Badge variant="outline">{runtime.cli_version}</Badge>
          ) : null}
        </div>
      </details>
    </div>
  );
}

function IneffectiveField({
  row,
}: {
  row: AgentCapabilityApprovalFieldConsequence;
}) {
  return (
    <div
      data-testid={`approval-ineffective-${row.field}`}
      data-severity={row.silent ? "silent" : "logged"}
      className="text-xs"
    >
      <div className="flex items-center gap-1.5">
        {row.silent && (
          <ShieldAlert className="size-3.5 shrink-0 text-amber-600" />
        )}
        <span className="font-medium">{fieldLabel(row.field)}</span>
      </div>
      <p className="mt-0.5 text-muted-foreground">
        {NO_EFFECT_REASONS[row.handling] ??
          "The selected engine does not use this setting."}
      </p>
    </div>
  );
}

function EffectiveField({
  row,
}: {
  row: AgentCapabilityApprovalFieldConsequence;
}) {
  return (
    <div
      data-testid={`approval-effective-${row.field}`}
      data-delivered-by={row.delivered_by}
      className="flex items-start gap-1.5 text-xs"
    >
      <Check className="mt-0.5 size-3.5 shrink-0 text-emerald-600" />
      <div>
        <span className="font-medium">{fieldLabel(row.field)}</span>
        <p className="text-muted-foreground">
          {row.delivered_by === "multica"
            ? "Multica applies this on every engine."
            : "The selected engine applies this."}
        </p>
      </div>
    </div>
  );
}
