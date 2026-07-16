"use client";

// FIR-3212 — Setup screen, capability-driven fields. This panel reads the
// runtime_options section of the agent capabilities card and tells the
// operator, for the engine the agent actually runs on: which run settings are
// honoured, which are dropped (logged or silently), and how the engine accepts
// a system prompt.
//
// Honesty rules (the reason this panel exists):
//   - status=unknown means "we cannot say" — it must never render as
//     "supports nothing", and nothing may be greyed out on unknown.
//   - A prepend-only engine (text spliced into the user message) must never be
//     presented as having real system-prompt semantics.
//   - Silent drops are called out explicitly; they are the settings that look
//     live in the UI and do nothing at runtime.

import { useQuery } from "@tanstack/react-query";
import { Cpu, TriangleAlert } from "lucide-react";
import type { Agent } from "@multica/core/types";
import { getAgentCapabilities } from "@multica/cerebro-agent-capabilities";
import { Badge } from "@multica/ui/components/ui/badge";

const HANDLING_LABELS: Record<string, string> = {
  honoured: "Honoured",
  ignored_logged: "Ignored (logged)",
  ignored_silent: "Ignored silently",
};

const MODE_LABELS: Record<string, string> = {
  append: "Append",
  replace: "Replace",
  prepend: "Prepend",
};

export function AgentContextRuntimeSupportPanel({ agent }: { agent: Agent }) {
  const { data, isLoading, isError } = useQuery({
    queryKey: ["agent-capabilities", agent.id],
    queryFn: () => getAgentCapabilities(agent.id),
  });

  const options = data?.runtime_options;

  return (
    <div className="rounded-lg border p-4">
      <div className="flex items-center gap-2">
        <Cpu className="size-4 text-muted-foreground" />
        <span className="text-sm font-medium">Engine support</span>
        {options?.provider ? (
          <Badge variant="secondary">{options.provider}</Badge>
        ) : null}
        {options?.cli_version ? (
          <Badge variant="outline">{options.cli_version}</Badge>
        ) : null}
      </div>
      <p className="mt-1 text-xs text-muted-foreground">
        The fields below come from this engine. Switching engine changes the
        fields.
      </p>

      {isLoading && (
        <p className="mt-3 text-xs text-muted-foreground">
          Loading engine support…
        </p>
      )}
      {isError && (
        <p className="mt-3 text-xs text-muted-foreground">
          Engine support is unavailable right now.
        </p>
      )}

      {options && !isLoading && !isError && (
        <div className="mt-3 space-y-4">
          {options.status !== "known" ? (
            <p className="text-xs text-muted-foreground">
              No authoritative support data for this engine. Settings are shown,
              but whether the engine acts on them cannot be confirmed — unknown
              is not the same as unsupported.
            </p>
          ) : (
            <>
              <SystemPromptSupport
                native={options.system_prompt?.native ?? false}
                modes={options.system_prompt?.modes ?? []}
                known={options.system_prompt != null}
              />

              {options.silently_ignored.length > 0 && (
                <div className="rounded-md border border-amber-500/40 bg-amber-500/10 p-2.5">
                  <div className="flex items-center gap-1.5 text-xs font-medium">
                    <TriangleAlert className="size-3.5 text-amber-600" />
                    This engine drops these settings silently
                  </div>
                  <div className="mt-1.5 flex flex-wrap gap-1.5">
                    {options.silently_ignored.map((field) => (
                      <Badge key={field} variant="outline">
                        {field}
                      </Badge>
                    ))}
                  </div>
                  <p className="mt-1.5 text-xs text-muted-foreground">
                    They look live in the form but change nothing at runtime.
                  </p>
                </div>
              )}

              {options.exec_options.length > 0 && (
                <div>
                  <div className="mb-1.5 text-xs font-medium text-muted-foreground">
                    Run settings
                  </div>
                  <div className="space-y-1">
                    {options.exec_options.map((row) => (
                      <div
                        key={row.field}
                        className="flex items-center justify-between gap-2 text-xs"
                      >
                        <span
                          className={
                            row.effective ? "" : "text-muted-foreground"
                          }
                        >
                          {row.field}
                        </span>
                        <Badge
                          variant={row.effective ? "secondary" : "outline"}
                        >
                          {HANDLING_LABELS[row.handling] ?? row.handling}
                        </Badge>
                      </div>
                    ))}
                  </div>
                </div>
              )}
            </>
          )}
        </div>
      )}
    </div>
  );
}

function SystemPromptSupport({
  native,
  modes,
  known,
}: {
  native: boolean;
  modes: string[];
  known: boolean;
}) {
  if (!known) return null;
  return (
    <div>
      <div className="mb-1.5 text-xs font-medium text-muted-foreground">
        System prompt
      </div>
      {modes.length === 0 ? (
        <p className="text-xs text-muted-foreground">
          This engine ignores a custom system prompt entirely.
        </p>
      ) : (
        <>
          <div className="flex flex-wrap gap-1.5">
            {modes.map((mode) => (
              <Badge key={mode} variant="secondary">
                {MODE_LABELS[mode] ?? mode}
              </Badge>
            ))}
          </div>
          {!native && (
            <p className="mt-1.5 text-xs text-muted-foreground">
              This engine has no native system-prompt channel — the text is
              prepended to the task message and carries no system-prompt
              semantics.
            </p>
          )}
        </>
      )}
    </div>
  );
}
