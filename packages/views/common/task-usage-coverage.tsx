"use client";

import type { AgentTask } from "@multica/core/types";
import { CircleAlert, Info } from "lucide-react";
import { Tooltip, TooltipContent, TooltipTrigger } from "@multica/ui/components/ui/tooltip";
import { useT } from "../i18n";
import { usageCoverage } from "./usage-coverage";

/** Shared by the execution row, transcript and per-run usage breakdown. */
export function TaskUsageCoverage({ task, compact = false }: { task: AgentTask; compact?: boolean }) {
  const { t } = useT("issues");
  if (!task.usage?.length && !task.usage_sources?.length) return null;
  const coverage = usageCoverage(task.usage_sources);
  let label: string;
  let detail: string;
  switch (coverage) {
    case "all_agents":
      label = t(($) => $.usage_coverage.all_agents);
      detail = t(($) => $.usage_coverage.all_agents_detail);
      break;
    case "main_agent":
      label = t(($) => $.usage_coverage.main_agent);
      detail = t(($) => $.usage_coverage.main_agent_detail);
      break;
    case "partial":
      label = t(($) => $.usage_coverage.partial);
      detail = t(($) => $.usage_coverage.partial_detail);
      break;
    case "unavailable":
      label = t(($) => $.usage_coverage.unavailable);
      detail = t(($) => $.usage_coverage.unavailable_detail);
      break;
    default:
      label = t(($) => $.usage_coverage.unknown);
      detail = t(($) => $.usage_coverage.unknown_detail);
  }
  const limited = coverage === "partial" || coverage === "main_agent" || coverage === "unavailable";
  const Icon = limited ? CircleAlert : Info;
  return (
    <Tooltip>
      <TooltipTrigger render={
        <span tabIndex={0} aria-label={`${label}: ${detail}`} className={`inline-flex shrink-0 items-center gap-1 text-micro ${limited ? "text-warning" : "text-muted-foreground"}`}>
          <Icon aria-hidden="true" className="size-3" />
          {!compact && label}
        </span>
      } />
      <TooltipContent className="max-w-xs">{detail}</TooltipContent>
    </Tooltip>
  );
}
