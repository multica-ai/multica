"use client";

// FIR-1709 — feeds the P3 hairline with the TRUTHFUL context measurement.
//
// Mia's hairline (context-hairline.tsx) renders a fraction; until now that
// fraction was a char-count guess (context-estimate.ts). This container swaps
// in the real signal from the server: the input-token footprint of the most
// recent agent run inside the active session, measured across runtimes and
// models (task_usage + the run's model context window). Before any run has
// recorded usage (has_data=false) it falls back to the estimate, clearly
// labelled, so the indicator is never blank or misleadingly exact.

import type { TimelineGroup } from "./grouping";
import { estimateSessionContextFraction } from "./context-estimate";
import { SessionContextHairline } from "./context-hairline";
import { useContextUsage } from "./use-sessions";

function formatTokens(n: number): string {
  if (n >= 1000) return `${Math.round(n / 1000)}k`;
  return `${n}`;
}

export function ActiveSessionContextHairline({
  issueId,
  groups,
}: {
  issueId: string;
  groups: TimelineGroup[];
}) {
  const { data } = useContextUsage(issueId);

  // Real measurement once a run with usage exists in this session.
  if (data?.has_data && data.max_context_tokens > 0) {
    const fraction = data.used_percent / 100;
    const cache =
      data.cache_share_percent > 0 ? ` · ${data.cache_share_percent}% from cache` : "";
    const model = data.model ? ` · ${data.model}` : "";
    const detail = `${data.used_percent}% of context window (${formatTokens(
      data.input_tokens,
    )} / ${formatTokens(data.max_context_tokens)} tokens${cache}${model})`;
    return (
      <SessionContextHairline fraction={fraction} approximate={false} detail={detail} />
    );
  }

  // No run yet → fall back to the content-size estimate, labelled as such.
  return <SessionContextHairline fraction={estimateSessionContextFraction(groups)} />;
}
