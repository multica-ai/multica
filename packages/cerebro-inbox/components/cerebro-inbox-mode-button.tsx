// TECH-3413 — toolbar entry point mounted in the classic inbox so the user can
// switch to the Dynamic inbox. Only renders when the `cerebro_inbox_dynamic`
// flag is enabled. The dynamic inbox has its own ⋯-menu toggle to switch back.
import { LayoutGrid } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import { useSetInboxMode } from "../dynamic-mode";

export function CerebroInboxModeButton() {
  const dynamicAvailable = useFeatureFlag("cerebro_inbox_dynamic");
  const setMode = useSetInboxMode();
  if (!dynamicAvailable) return null;
  return (
    <Button
      variant="ghost"
      size="sm"
      className="gap-1.5 text-muted-foreground"
      onClick={() => {
        void setMode("dynamic");
      }}
      title="Switch to the dynamic inbox"
    >
      <LayoutGrid className="size-4" />
      <span className="hidden sm:inline">Dynamic inbox</span>
    </Button>
  );
}
