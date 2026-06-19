// CEREBRO: FIR-1521 — inbox row indicator for a scheduled wakeup. Jesper feedback:
// this must be a DOT like the running-job pip (AgentRunPip), NOT a clock — a clock
// reads as an issue-status icon. So it renders the same pulsing dot as the live-run
// indicator, in the orange "warning" hue to mean "scheduled to run" (vs the blue
// "running now" dot). No Danish copy; the tooltip carries the English label.
"use client";

export function CerebroInboxWakeupPip({
  title,
  className = "",
}: {
  /** RFC3339 fire time for a time trigger; absent for status / CI triggers.
   *  Accepted for call-site compatibility; the dot itself shows no countdown. */
  fireAt?: string;
  /** Tooltip / accessible label (e.g. "Scheduled run ~Thu, 11:53"). */
  title?: string;
  className?: string;
}) {
  return (
    <span
      title={title}
      aria-label={title}
      className={`relative inline-flex size-2 shrink-0 items-center justify-center ${className}`}
    >
      <span className="absolute inline-flex size-2 animate-ping rounded-full bg-warning opacity-50" />
      <span className="relative inline-flex size-1.5 rounded-full bg-warning" />
    </span>
  );
}
