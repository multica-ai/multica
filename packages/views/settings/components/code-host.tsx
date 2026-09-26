import type { ReactNode } from "react";
import { cn } from "@multica/ui/lib/utils";

/** The square logo tile code hosts share on this page. */
export function HostMark({ children }: { children: ReactNode }) {
  return (
    <span
      aria-hidden="true"
      className="flex size-9 shrink-0 items-center justify-center rounded-lg border border-surface-border bg-background text-foreground"
    >
      {children}
    </span>
  );
}

export function HostStatus({
  tone,
  label,
}: {
  tone: "success" | "muted";
  label: string;
}) {
  return (
    <span
      className={cn(
        "inline-flex items-center gap-1.5 text-caption font-normal",
        tone === "success" ? "text-success" : "text-muted-foreground",
      )}
    >
      {tone === "success" ? (
        <span aria-hidden="true" className="size-1.5 rounded-full bg-success" />
      ) : null}
      {label}
    </span>
  );
}
