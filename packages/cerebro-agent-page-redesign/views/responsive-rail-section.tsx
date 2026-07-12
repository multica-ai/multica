import type { ReactNode } from "react";
import { ChevronDown } from "lucide-react";

export function ResponsiveRailSection({
  title,
  count,
  children,
  className = "border-b",
}: {
  title: string;
  count?: number;
  children: ReactNode;
  className?: string;
}) {
  return (
    <details open className={`group ${className}`}>
      <summary className="flex cursor-pointer list-none items-center justify-between px-5 py-4 text-[10.5px] font-semibold uppercase tracking-wider text-muted-foreground md:hidden">
        <span className="flex items-center gap-2">
          {title}
          {count !== undefined && (
            <span className="rounded bg-muted px-1.5 py-0.5 font-mono text-[10.5px]">
              {count}
            </span>
          )}
        </span>
        <ChevronDown className="h-4 w-4 transition-transform group-open:rotate-180" />
      </summary>
      <div className="px-5 pb-4 md:block md:py-4">{children}</div>
    </details>
  );
}
