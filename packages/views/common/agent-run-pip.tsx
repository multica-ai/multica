"use client";

import { Clock } from "lucide-react"; // CEREBRO-PATCH(agent-run-pip-scheduled): TECH-3322 clock icon for scheduled wakeups

// CEREBRO-PATCH(agent-run-pip-states): shared pip distinguishing active vs queued runs (JEH-1332)
// CEREBRO-PATCH(agent-run-pip-sub): FIR-2326 — orange "sub" state marks a row whose sub-issue is running.
// CEREBRO-PATCH(agent-run-pip-scheduled): TECH-3322 — "scheduled" renders a clock for issues with a pending wakeup (queued to run later, not running now).
export type AgentRunState = "active" | "queued" | "sub" | "scheduled";

export function taskStatusToRunState(status: string): Exclude<AgentRunState, "sub" | "scheduled"> {
  return status === "queued" ? "queued" : "active";
}

const RUN_PIP_COLOR: Record<Exclude<AgentRunState, "scheduled">, string> = {
  active: "bg-blue-500",
  queued: "bg-muted-foreground",
  sub: "bg-orange-500",
};

const RUN_PIP_TITLE: Record<AgentRunState, string> = {
  active: "Agent is working",
  queued: "Agent is queued",
  sub: "A sub-issue is running",
  scheduled: "Scheduled to run",
};

export function AgentRunPip({
  state,
  className = "",
  title,
}: {
  state: AgentRunState;
  className?: string;
  // CEREBRO-PATCH(agent-run-pip-scheduled): TECH-3322 — optional title override carries the approximate wakeup time.
  title?: string;
}) {
  const label = title ?? RUN_PIP_TITLE[state];

  // CEREBRO-PATCH(agent-run-pip-scheduled): TECH-3322 — a scheduled wakeup shows a static clock instead of the live pulsing dot.
  if (state === "scheduled") {
    return (
      <Clock
        aria-label={label}
        className={`inline-flex size-3 shrink-0 text-muted-foreground ${className}`}
      >
        <title>{label}</title>
      </Clock>
    );
  }

  const color = RUN_PIP_COLOR[state];
  return (
    <span
      title={label}
      className={`relative inline-flex size-2 shrink-0 items-center justify-center ${className}`}
    >
      <span className={`absolute inline-flex size-2 animate-ping rounded-full ${color} opacity-50`} />
      <span className={`relative inline-flex size-1.5 rounded-full ${color}`} />
    </span>
  );
}
