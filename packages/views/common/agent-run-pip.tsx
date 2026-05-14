"use client";

// CEREBRO-PATCH(agent-run-pip-states): shared pip distinguishing active vs queued runs (JEH-1332)
export type AgentRunState = "active" | "queued";

export function taskStatusToRunState(status: string): AgentRunState {
  return status === "queued" ? "queued" : "active";
}

export function AgentRunPip({
  state,
  className = "",
}: {
  state: AgentRunState;
  className?: string;
}) {
  const color = state === "active" ? "bg-blue-500" : "bg-muted-foreground";
  const title = state === "active" ? "Agent is working" : "Agent is queued";

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
