"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { labelListOptions } from "@multica/core/labels";
import { useUpdateAutopilot } from "@multica/core/autopilots/mutations";
import { useWorkspaceId } from "@multica/core/hooks";
import type { Autopilot } from "@multica/core/types";
import { LabelPicker } from "../../issues/components";
import { LabelChip } from "../../labels/label-chip";
import { useT } from "../../i18n";

/**
 * Drops ids whose label no longer exists. The server removes a deleted label
 * from every autopilot, but a cached autopilot can still carry its id, and
 * saving it back would be rejected. Until the label list loads, ids pass
 * through unchanged so a slow list never clears a saved selection.
 */
export function useDropDeletedLabelIds(): (ids: string[]) => string[] {
  const wsId = useWorkspaceId();
  const { data: labels } = useQuery(labelListOptions(wsId, "issue"));
  return (ids) => {
    if (!labels) return ids;
    const known = new Set(labels.map((label) => label.id));
    return ids.filter((id) => known.has(id));
  };
}

export function sameLabelIds(a: string[], b: string[]): boolean {
  return a.length === b.length && a.every((id) => b.includes(id));
}

/**
 * The detail page's Labels row. Selection is held while the picker is open
 * and saved once on close, like the Subscribers row.
 */
export function AutopilotLabelsProperty({
  autopilot,
  canWrite,
  triggerClassName,
}: {
  autopilot: Autopilot;
  canWrite: boolean;
  triggerClassName: string;
}) {
  const { t } = useT("autopilots");
  const wsId = useWorkspaceId();
  const updateAutopilot = useUpdateAutopilot();
  const dropDeleted = useDropDeletedLabelIds();
  const { data: labels = [] } = useQuery(labelListOptions(wsId, "issue"));
  const [open, setOpen] = useState(false);
  const [draft, setDraft] = useState<string[] | null>(null);

  const savedIds = autopilot.label_ids ?? [];

  if (!canWrite) {
    const saved = savedIds
      .map((id) => labels.find((label) => label.id === id))
      .filter((label) => label !== undefined);
    if (saved.length === 0) {
      return <span className="truncate text-muted-foreground">—</span>;
    }
    return (
      <span className="flex min-w-0 flex-wrap items-center gap-1 py-1.5">
        {saved.map((label) => (
          <LabelChip key={label.id} label={label} />
        ))}
      </span>
    );
  }

  const handleOpenChange = (next: boolean) => {
    setOpen(next);
    if (next) {
      setDraft(savedIds);
      return;
    }
    const nextIds = draft === null ? null : dropDeleted(draft);
    setDraft(null);
    if (nextIds === null || sameLabelIds(nextIds, dropDeleted(savedIds))) return;
    updateAutopilot
      .mutateAsync({ id: autopilot.id, label_ids: nextIds })
      .catch(() => toast.error(t(($) => $.dialog.toast_update_failed)));
  };

  return (
    <LabelPicker
      selectedIds={draft ?? savedIds}
      onSelectedIdsChange={setDraft}
      open={open}
      onOpenChange={handleOpenChange}
      align="start"
      triggerRender={<button type="button" className={`${triggerClassName} flex-wrap py-1.5`} />}
    />
  );
}
