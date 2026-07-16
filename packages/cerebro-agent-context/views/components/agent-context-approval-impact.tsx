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
    "This engine drops it without a trace — nothing in the log will tell you it did not land.",
  ignored_logged: "This engine logs that it ignored the setting.",
};

const DELIVERY_NOTES: Record<string, string> = {
  prepended:
    "This engine has no system-prompt channel: the new instruction is spliced into the task message and no longer carries system-prompt semantics.",
  ignored: "This engine discards a custom system prompt entirely.",
};

const CONSEQUENCE_NOTES: Record<string, string> = {
  no_runtime_effect: "Reaches no run — it is a label on the agent.",
  unknown_field: "We have no mapping for this field, so we cannot say.",
};

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
        Checking what this changes…
      </p>
    );
  }
  if (isError || !data) {
    return (
      <p className="text-xs text-muted-foreground">
        What this changes is unavailable right now — the field diff above still
        applies.
      </p>
    );
  }

  const { impact, runtime } = data;

  if (impact.status !== "known") {
    return (
      <p className="text-xs text-muted-foreground">
        We cannot confirm what this engine does with these settings — it has no
        authoritative support entry. Unknown is not the same as ineffective:
        nothing is claimed to land, and nothing is claimed to be dropped.
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
      <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
        <span className="font-medium text-foreground">
          What approving this changes
        </span>
        <span>on</span>
        <Badge variant="outline">{impact.provider}</Badge>
        {runtime.cli_version ? (
          <Badge variant="outline">{runtime.cli_version}</Badge>
        ) : null}
      </div>

      {ineffective.length > 0 && (
        <div className="rounded-md border border-amber-500/40 bg-amber-500/10 p-2.5">
          <div className="flex items-center gap-1.5 text-xs font-medium">
            <TriangleAlert className="size-3.5 text-amber-600" />
            Will not take effect on {impact.provider}
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
            System prompt
          </div>
          <p className="text-xs text-muted-foreground">{deliveryNote}</p>
        </div>
      )}

      {effective.length > 0 && (
        <div>
          <div className="mb-1.5 text-xs font-medium text-muted-foreground">
            Takes effect on the next run
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
          <span className="font-medium text-foreground">{row.field}</span>
          {CONSEQUENCE_NOTES[row.consequence] ?? "We cannot say."}
        </p>
      ))}
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
        <span className="font-medium">{row.field}</span>
      </div>
      <p className="mt-0.5 text-muted-foreground">
        {NO_EFFECT_REASONS[row.handling] ??
          "This engine does not act on the setting."}
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
        <span className="font-medium">{row.field}</span>
        <p className="text-muted-foreground">
          {row.delivered_by === "multica"
            ? "Multica applies this itself, on every engine."
            : "This engine acts on it."}
        </p>
      </div>
    </div>
  );
}
