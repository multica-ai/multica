"use client";

// CEREBRO-PATCH(agent-run-pip-states): shared pip distinguishing active vs queued runs (JEH-1332)
// CEREBRO-PATCH(agent-run-pip-sub): FIR-2326 — orange "sub" state marks a row whose sub-issue is running.
// CEREBRO-PATCH(agent-run-pip-scheduled): FIR-1521 — "scheduled" renders an orange dot (like the running dot, not a clock) for issues with a pending wakeup.
// CEREBRO-PATCH(agent-run-pip-failed): FIR-3901 — "failed" marks an issue whose last run died and that nothing will retry.
export type AgentRunState = "active" | "queued" | "sub" | "scheduled" | "failed";

export function taskStatusToRunState(status: string): Exclude<AgentRunState, "sub" | "scheduled" | "failed"> {
  return status === "queued" ? "queued" : "active";
}

// CEREBRO-PATCH(agent-run-pip-scheduled): FIR-1521 — scheduled uses the warning hue.
const RUN_PIP_COLOR: Record<AgentRunState, string> = {
  active: "bg-blue-500",
  queued: "bg-muted-foreground",
  sub: "bg-orange-500",
  scheduled: "bg-warning",
  failed: "bg-destructive", // CEREBRO-PATCH(agent-run-pip-failed): FIR-3901
};

const RUN_PIP_TITLE: Record<AgentRunState, string> = {
  active: "Agent is working",
  queued: "Agent is queued",
  sub: "A sub-issue is running",
  scheduled: "Scheduled to run",
  failed: "Run failed", // CEREBRO-PATCH(agent-run-pip-failed): FIR-3901
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
