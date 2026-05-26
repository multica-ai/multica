"use client";

// CEREBRO-PATCH(agent-run-pip-states): shared pip distinguishing active vs queued runs (JEH-1332)
// CEREBRO-PATCH(agent-run-pip-sub): FIR-2326 — orange "sub" state marks a row whose sub-issue is running.
export type AgentRunState = "active" | "queued" | "sub";

export function taskStatusToRunState(status: string): Exclude<AgentRunState, "sub"> {
  return status === "queued" ? "queued" : "active";
}

const RUN_PIP_COLOR: Record<AgentRunState, string> = {
  active: "bg-blue-500",
  queued: "bg-muted-foreground",
  sub: "bg-orange-500",
};

const RUN_PIP_TITLE: Record<AgentRunState, string> = {
  active: "Agent is working",
  queued: "Agent is queued",
  sub: "A sub-issue is running",
};

export function AgentRunPip({
  state,
  className = "",
}: {
  state: AgentRunState;
  className?: string;
}) {
  const color = RUN_PIP_COLOR[state];
  const title = RUN_PIP_TITLE[state];

  return (
    <span
      title={title}
      className={`relative inline-flex size-2 shrink-0 items-center justify-center ${className}`}
    >
      <span className={`absolute inline-flex size-2 animate-ping rounded-full ${color} opacity-50`} />
      <span className={`relative inline-flex size-1.5 rounded-full ${color}`} />
    </span>
  );
}
