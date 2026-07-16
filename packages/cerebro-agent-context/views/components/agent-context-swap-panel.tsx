"use client";

// FIR-3212 — Setup screen, engine swap (mockup M2). The runtime support panel
// next to this one describes the engine the agent runs on TODAY. This one
// answers the question asked one second before the change: if I move this agent
// to that engine, what stops working?
//
// Honesty rules (why this panel is not just a field diff):
//   - A setting the target drops SILENTLY ranks above one it drops with a log
//     line. Both stop working; only one of them tells anybody. The lost list is
//     ordered on that, and the severity is in the DOM, not just the colour.
//   - A system prompt that survives as prepended text has NOT been retained —
//     it arrives with its authority removed. No honoured/ignored pair can say
//     that, so the prompt gets its own sentence.
//   - An engine the matrix has no entry for renders as "we cannot say". Serving
//     a confident loss list for an unknown engine would talk an operator out of
//     a swap that works.
//
// The outcomes are computed server-side (capabilities.SwapImpactFor). This file
// renders them; it must not re-derive them from the handling strings.

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ArrowLeftRight, TriangleAlert, ShieldAlert, Check } from "lucide-react";
import type { Agent, AgentRuntime } from "@multica/core/types";
import {
  getAgentCapabilitySwap,
  type AgentCapabilitySwapFieldTransition,
} from "@multica/cerebro-agent-capabilities";
import { Badge } from "@multica/ui/components/ui/badge";

// Why a lost field is lost, in the operator's terms. Keyed on the target's
// handling — an unrecognised value falls through to a neutral sentence rather
// than crashing (enum drift downgrades, not crashes).
const LOST_REASONS: Record<string, string> = {
  ignored_silent: "This engine drops it without a trace — nothing in the log.",
  ignored_logged: "This engine logs that it ignored the setting.",
};

const PROMPT_NOTES: Record<string, string> = {
  downgraded_to_prepend:
    "The instruction still reaches the model, but it is spliced into the task message and no longer carries system-prompt semantics.",
  ignored: "This engine discards a custom system prompt entirely.",
  gained_native:
    "This engine has a real system-prompt channel, which the current one lacks.",
};

export function AgentContextSwapPanel({
  agent,
  runtimes,
}: {
  agent: Agent;
  runtimes: AgentRuntime[];
}) {
  const [targetId, setTargetId] = useState("");
  const candidates = runtimes.filter((rt) => rt.id !== agent.runtime_id);

  const { data, isLoading, isError } = useQuery({
    queryKey: ["cerebro", "agent-capability-swap", agent.id, targetId],
    queryFn: () => getAgentCapabilitySwap(agent.id, targetId),
    enabled: targetId !== "",
  });

  return (
    <div className="rounded-lg border p-4">
      <div className="flex items-center gap-2">
        <ArrowLeftRight className="size-4 text-muted-foreground" />
        <span className="text-sm font-medium">Engine swap</span>
      </div>
      <p className="mt-1 text-xs text-muted-foreground">
        Check what a different engine would change for this agent before you
        propose it.
      </p>

      <label
        className="mt-3 block text-xs font-medium text-muted-foreground"
        htmlFor="cerebro-swap-target"
      >
        Compare with another engine
      </label>
      <select
        id="cerebro-swap-target"
        className="mt-1 w-full rounded-md border bg-background p-1.5 text-xs"
        value={targetId}
        onChange={(e) => setTargetId(e.target.value)}
      >
        <option value="">Select an engine…</option>
        {candidates.map((rt) => (
          <option key={rt.id} value={rt.id}>
            {rt.name}
            {rt.provider ? ` (${rt.provider})` : ""}
          </option>
        ))}
      </select>

      {targetId !== "" && isLoading && (
        <p className="mt-3 text-xs text-muted-foreground">
          Checking that engine…
        </p>
      )}
      {targetId !== "" && isError && (
        <p className="mt-3 text-xs text-muted-foreground">
          The comparison is unavailable right now.
        </p>
      )}

      {data && !isLoading && !isError && (
        <SwapResult swap={data} />
      )}
    </div>
  );
}

function SwapResult({
  swap,
}: {
  swap: Awaited<ReturnType<typeof getAgentCapabilitySwap>>;
}) {
  const { impact, target } = swap;

  if (impact.status !== "known") {
    return (
      <p className="mt-3 text-xs text-muted-foreground">
        We cannot confirm what this engine does with the agent&apos;s settings —
        it has no authoritative support entry. Unknown is not the same as
        unsupported: nothing is claimed to break, and nothing is claimed to work.
      </p>
    );
  }

  // Silent first: a drop nobody logs is the one an operator keeps trusting.
  const lost = impact.fields
    .filter((row) => row.outcome === "lost")
    .sort((a, b) => Number(b.silent_on_target) - Number(a.silent_on_target));
  const gained = impact.fields.filter((row) => row.outcome === "gained");
  const promptNote = impact.system_prompt
    ? PROMPT_NOTES[impact.system_prompt.outcome]
    : undefined;

  const nothingChanges = lost.length === 0 && gained.length === 0 && !promptNote;

  return (
    <div className="mt-3 space-y-3">
      <div className="flex items-center gap-1.5 text-xs">
        <Badge variant="outline">{impact.from}</Badge>
        <ArrowLeftRight className="size-3 text-muted-foreground" />
        <Badge variant="secondary">{impact.to}</Badge>
        {target.cli_version ? (
          <Badge variant="outline">{target.cli_version}</Badge>
        ) : null}
      </div>

      {nothingChanges ? (
        <p className="flex items-center gap-1.5 text-xs text-muted-foreground">
          <Check className="size-3.5" />
          Nothing changes on this engine — every setting is handled the same way.
        </p>
      ) : (
        <>
          {lost.length > 0 && (
            <div className="rounded-md border border-amber-500/40 bg-amber-500/10 p-2.5">
              <div className="flex items-center gap-1.5 text-xs font-medium">
                <TriangleAlert className="size-3.5 text-amber-600" />
                Stops working on {impact.to}
              </div>
              <div className="mt-2 space-y-2">
                {lost.map((row) => (
                  <LostField key={row.field} row={row} />
                ))}
              </div>
            </div>
          )}

          {promptNote && (
            <div className="rounded-md border p-2.5">
              <div className="mb-1 text-xs font-medium text-muted-foreground">
                System prompt
              </div>
              <p className="text-xs text-muted-foreground">{promptNote}</p>
            </div>
          )}

          {gained.length > 0 && (
            <div>
              <div className="mb-1.5 text-xs font-medium text-muted-foreground">
                Starts working on {impact.to}
              </div>
              <div className="flex flex-wrap gap-1.5">
                {gained.map((row) => (
                  <Badge key={row.field} variant="secondary">
                    {row.field}
                  </Badge>
                ))}
              </div>
            </div>
          )}
        </>
      )}
    </div>
  );
}

function LostField({ row }: { row: AgentCapabilitySwapFieldTransition }) {
  const severity = row.silent_on_target ? "silent" : "logged";
  return (
    <div
      data-testid="swap-lost-field"
      data-severity={severity}
      className="text-xs"
    >
      <div className="flex items-center gap-1.5">
        {row.silent_on_target && (
          <ShieldAlert className="size-3.5 shrink-0 text-amber-600" />
        )}
        <span className="font-medium">{row.field}</span>
      </div>
      <p className="mt-0.5 text-muted-foreground">
        {LOST_REASONS[row.to_handling] ??
          "This engine does not act on the setting."}
      </p>
    </div>
  );
}
