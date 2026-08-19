import { StatusBadge } from "@/components/status-badge";
import { SheetHeader, SheetTitle } from "@multica/ui/components/ui/sheet";
import type { WorkspaceListItem, WorkspaceStatus } from "@/lib/types";

interface DetailHeaderProps {
  workspace: WorkspaceListItem;
  status: WorkspaceStatus;
}

export function DetailHeader({ workspace, status }: DetailHeaderProps) {
  return (
    <SheetHeader className="border-b border-border pb-4">
      <div className="flex items-center gap-2">
        {/* Plan §6: panel workspace-name header uses the serif face; every
            other UI text stays sans (font-heading, set by SheetTitle's
            default). Override just the family here rather than in the
            shared Sheet primitive, which every other consumer relies on
            staying sans. */}
        <SheetTitle className="font-serif text-title">{workspace.name}</SheetTitle>
        <StatusBadge status={status} />
      </div>
      <p className="text-label text-muted-foreground">{workspace.slug}</p>
    </SheetHeader>
  );
}
