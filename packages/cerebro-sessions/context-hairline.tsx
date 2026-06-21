"use client";

// A whisper-subtle context-window indicator for the active chapter (FIR-1769
// P3). It is a hairline that only warms as the chapter's estimated content
// grows toward a 200k window; near full it shows a single muted nudge to
// Start fresh. The fraction is an approximation (see context-estimate.ts) and
// is never presented as exact.
const NUDGE_THRESHOLD = 0.8;
const WARM_THRESHOLD = 0.5;

export function SessionContextHairline({ fraction }: { fraction: number }) {
  const pct = Math.max(0, Math.min(1, fraction));
  // Warm from quiet → destructive only as it fills; semantic tokens only.
  const fill =
    pct >= NUDGE_THRESHOLD
      ? "bg-destructive"
      : pct >= WARM_THRESHOLD
        ? "bg-destructive/50"
        : "bg-muted-foreground/30";
  const rounded = Math.round(pct * 100);
  return (
    <div className="px-4 pt-1">
      <div
        className="h-px w-full overflow-hidden rounded-full bg-border"
        role="presentation"
        title={`~${rounded}% of context window (estimate)`}
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
