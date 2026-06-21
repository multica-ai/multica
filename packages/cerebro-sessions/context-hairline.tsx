"use client";

// A whisper-subtle context-window indicator for the active chapter (FIR-1769
// P3). It is a hairline that only warms as the active chapter's context fills
// toward the model's window; near full it shows a single muted nudge to Start
// fresh. The fraction comes from real per-run token usage when available
// (cross-runtime/model, FIR-1709) and falls back to a char-count estimate
// before any run has happened — `approximate` controls how it is labelled.
const NUDGE_THRESHOLD = 0.8;
const WARM_THRESHOLD = 0.5;

export function SessionContextHairline({
  fraction,
  approximate = true,
  detail,
}: {
  fraction: number;
  approximate?: boolean;
  detail?: string;
}) {
  const pct = Math.max(0, Math.min(1, fraction));
  // Warm from quiet → destructive only as it fills; semantic tokens only.
  const fill =
    pct >= NUDGE_THRESHOLD
      ? "bg-destructive"
      : pct >= WARM_THRESHOLD
        ? "bg-destructive/50"
        : "bg-muted-foreground/30";
  const rounded = Math.round(pct * 100);
  const title = detail
    ? detail
    : `${approximate ? "~" : ""}${rounded}% of context window${approximate ? " (estimate)" : ""}`;
  return (
    <div className="px-4 pt-1">
      <div
        className="h-px w-full overflow-hidden rounded-full bg-border"
        role="presentation"
        title={title}
      >
        <div
          className={`h-full ${fill} transition-all`}
          style={{ width: `${pct * 100}%` }}
        />
      </div>
      {pct >= NUDGE_THRESHOLD ? (
        <p className="mt-1 text-[11px] text-muted-foreground">
          This chapter is almost full — consider <span className="font-medium">Start fresh</span>.
        </p>
      ) : null}
    </div>
  );
}
