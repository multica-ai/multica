import { cn } from "@multica/ui/lib/utils";
import type { AgentRuntime } from "@multica/core/types";
import { useT } from "@multica/i18n/react";
import { ProviderLogo } from "../../runtimes/components/provider-logo";

/**
 * One-line runtime row for Step 3's web CLI expand. Provider logo,
 * name + subtitle, online indicator on the right. Selection state is
 * driven by the caller (kept stateless so both StepPlatformFork and
 * any future embedder can share it without duplicating the picker
 * plumbing).
 */
export function CompactRuntimeRow({
  runtime,
  selected,
  onSelect,
}: {
  runtime: AgentRuntime;
  selected: boolean;
  onSelect: () => void;
}) {
  const online = runtime.status === "online";
  const t = useT("onboarding");
  return (
    <div
      role="button"
      tabIndex={0}
      onClick={onSelect}
      onKeyDown={(e) => {
        if (e.key === "Enter" || e.key === " ") {
          e.preventDefault();
          onSelect();
        }
      }}
      className={cn(
        "flex cursor-pointer flex-row items-center gap-3 rounded-lg border bg-card p-4 transition-colors",
        selected
          ? "border-primary ring-1 ring-primary"
          : "hover:border-foreground/20",
      )}
    >
      <ProviderLogo provider={runtime.provider} className="h-5 w-5" />
      <div className="min-w-0 flex-1">
        <div className="truncate text-sm font-medium">{runtime.name}</div>
        <div className="text-xs text-muted-foreground">{runtime.provider}</div>
      </div>
      <span
        className={cn(
          "h-2 w-2 shrink-0 rounded-full",
          online ? "bg-success" : "bg-muted-foreground/40",
        )}
        aria-label={online ? t("online") : t("offline")}
      />
    </div>
  );
}
