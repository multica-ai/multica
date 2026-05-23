"use client";

import { useMemo, useState } from "react";
import { Link2, Plus } from "lucide-react";
import { toast } from "sonner";
import { cn } from "@multica/ui/lib/utils";
import { Button } from "@multica/ui/components/ui/button";
import {
  useDeleteReference,
  useIssueReferences,
  type IssueReference,
  type ReferenceObject,
} from "@multica/cerebro-references/core";
import { AddReferenceDialog } from "./add-reference-dialog";
import { ObjectIcon } from "./object-icon";
import { ReferenceRow } from "./reference-row";

// Friendly section labels per object group. Unmapped kinds show their wire
// string so a brand-new edge-type still groups and labels itself without a
// frontend change.
const GROUP_LABELS: Record<string, string> = {
  github_pr: "GitHub pull requests",
};

function groupLabel(object: ReferenceObject): string {
  return GROUP_LABELS[object] ?? object;
}

function groupByObject(
  references: IssueReference[],
): Array<{ object: ReferenceObject; items: IssueReference[] }> {
  const map = new Map<ReferenceObject, IssueReference[]>();
  for (const ref of references) {
    const list = map.get(ref.object) ?? [];
    list.push(ref);
    map.set(ref.object, list);
  }
  return Array.from(map.entries()).map(([object, items]) => ({ object, items }));
}

// Issue-detail section: lists references grouped per object, with an
// "Add reference" affordance and per-row delete. Reads live data via
// useIssueReferences; new references arriving over WS invalidate the
// query and re-render here automatically.
export function IssueReferenceList({
  issueId,
  className,
}: {
  issueId: string;
  className?: string;
}) {
  const { data: references = [], isLoading } = useIssueReferences(issueId);
  const deleteReference = useDeleteReference(issueId);
  const [dialogOpen, setDialogOpen] = useState(false);
  const [pendingDeleteId, setPendingDeleteId] = useState<string | null>(null);

  const groups = useMemo(() => groupByObject(references), [references]);

  const handleDelete = (reference: IssueReference) => {
    setPendingDeleteId(reference.id);
    deleteReference.mutate(reference.id, {
      onSuccess: () => toast.success("Reference removed"),
      onError: () => toast.error("Could not remove the reference"),
      onSettled: () => setPendingDeleteId(null),
    });
  };

  // Hide the whole section while the first load is in flight and there is
  // nothing cached yet — avoids a flash of the empty state.
  const showEmpty = !isLoading && references.length === 0;

  return (
    <div data-testid="issue-reference-list" className={cn("mt-4", className)}>
      <div className="mb-2 flex items-center justify-between">
        <div className="flex items-center gap-1.5 text-xs font-medium text-muted-foreground">
          <Link2 className="size-3.5" aria-hidden="true" />
          References
        </div>
        <Button
          variant="ghost"
          size="sm"
          className="h-7 gap-1 text-xs"
          onClick={() => setDialogOpen(true)}
        >
          <Plus className="size-3.5" />
          Add reference
        </Button>
      </div>

      {showEmpty ? (
        <button
          type="button"
          onClick={() => setDialogOpen(true)}
          className="flex w-full items-center gap-2 rounded-md border border-dashed border-border px-2.5 py-2 text-left text-xs text-muted-foreground transition-colors hover:bg-muted"
        >
          <Plus className="size-3.5" />
          Link a GitHub PR or other object to this issue
        </button>
      ) : (
        <div className="flex flex-col gap-3">
          {groups.map(({ object, items }) => (
            <div key={object} className="flex flex-col gap-1">
              <div className="flex items-center gap-1.5 px-0.5 text-[11px] uppercase tracking-wide text-muted-foreground">
                <ObjectIcon object={object} className="size-3" />
                {groupLabel(object)}
              </div>
              {items.map((reference) => (
                <ReferenceRow
                  key={reference.id}
                  reference={reference}
                  onDelete={handleDelete}
                  deleting={pendingDeleteId === reference.id}
                />
              ))}
            </div>
          ))}
        </div>
      )}

      <AddReferenceDialog
        issueId={issueId}
        open={dialogOpen}
        onOpenChange={setDialogOpen}
      />
    </div>
  );
}
