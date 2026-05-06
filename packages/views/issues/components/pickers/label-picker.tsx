"use client";

import { useMemo, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Tag, Plus, Settings2 } from "lucide-react";
import { toast } from "sonner";
import { Dialog, DialogContent, DialogTitle } from "@multica/ui/components/ui/dialog";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  labelListOptions,
  issueLabelsOptions,
  useAttachLabel,
  useDetachLabel,
  useCreateLabel,
} from "@multica/core/labels";
import { LabelChip } from "../../../labels/label-chip";
import { LabelsPanel } from "../labels-panel";
import {
  PropertyPicker,
  PickerItem,
  PickerEmpty,
} from "./property-picker";
import { useT } from "../../../i18n";

interface LabelPickerProps {
  issueId: string;
  /** Optional controlled open state (for tests / cmd+k integration). */
  open?: boolean;
  onOpenChange?: (open: boolean) => void;
  align?: "start" | "center" | "end";
}

/**
 * Palette of colors used when creating a label inline from the picker.
 * We cycle by hash(name) so the same name always gets the same color,
 * and a color can still be changed afterwards from the Manage dialog.
 */
const INLINE_COLORS = [
  "#ef4444", "#f97316", "#eab308", "#22c55e", "#14b8a6",
  "#3b82f6", "#6366f1", "#a855f7", "#ec4899", "#64748b",
] as const;

function pickInlineColor(name: string): string {
  let hash = 0;
  for (let i = 0; i < name.length; i++) {
    hash = (hash * 31 + name.charCodeAt(i)) >>> 0;
  }
  return INLINE_COLORS[hash % INLINE_COLORS.length] ?? INLINE_COLORS[0]!;
}

/**
 * Multi-select label picker for an issue. Shows currently-attached labels
 * as inline chips above the trigger and lets the user toggle any label in
 * the workspace. Attach/detach are optimistic — the UI updates before the
 * server confirms.
 *
 * When the search term has no matches, offers inline creation: typing a
 * new name and pressing Enter (or clicking the "Create X" row) creates the
 * label with a hash-derived color and attaches it in one motion.
 *
 * A "Manage labels" item at the bottom opens a dialog with the full
 * workspace label management panel (rename, recolor, delete) — keeping
 * users in context without forcing them to navigate away.
 */
export function LabelPicker({
  issueId,
  open: controlledOpen,
  onOpenChange,
  align = "start",
}: LabelPickerProps) {
  const { t } = useT("issues");
  const [internalOpen, setInternalOpen] = useState(false);
  const open = controlledOpen ?? internalOpen;
  const setOpen = onOpenChange ?? setInternalOpen;
  const [filter, setFilter] = useState("");
  const [manageOpen, setManageOpen] = useState(false);

  // Synchronous lock to prevent double-submit on rapid Enter / click. React
  // state (create.isPending, filter) isn't visible until the next render, so
  // two events within the same tick can both pass the canCreate guard and
  // fire two create.mutate calls — the second hits 409 and shows a red toast
  // for an error the user didn't cause. A ref closes the window cleanly.
  const creatingRef = useRef(false);

  const wsId = useWorkspaceId();
  const { data: allLabels = [] } = useQuery(labelListOptions(wsId));
  const { data: attachedLabels = [] } = useQuery(issueLabelsOptions(wsId, issueId));

  const attach = useAttachLabel(issueId);
  const detach = useDetachLabel(issueId);
  const create = useCreateLabel();

  const attachedIds = useMemo(
    () => new Set(attachedLabels.map((l) => l.id)),
    [attachedLabels],
  );

  const query = filter.trim();
  const queryLower = query.toLowerCase();
  const filtered = allLabels.filter((l) => l.name.toLowerCase().includes(queryLower));
  const exactMatch = allLabels.some((l) => l.name.toLowerCase() === queryLower);
  const canCreate = query.length > 0 && !exactMatch && !create.isPending;

  const toggle = (labelId: string) => {
    if (attachedIds.has(labelId)) {
      detach.mutate(labelId);
    } else {
      attach.mutate(labelId);
    }
  };

  const createAndAttach = () => {
    if (!canCreate || creatingRef.current) return;
    creatingRef.current = true;
    const name = query;
    create.mutate(
      { name, color: pickInlineColor(name) },
      {
        onSuccess: (label) => {
          attach.mutate(label.id);
          setFilter("");
        },
        onError: (err: unknown) => {
          toast.error(err instanceof Error ? err.message : t(($) => $.pickers.label.create_failed));
        },
        onSettled: () => {
          creatingRef.current = false;
        },
      },
    );
  };

  const openManage = () => {
    setOpen(false);
    setManageOpen(true);
  };

  const hasLabels = attachedLabels.length > 0;

  return (
    <div className="flex flex-col gap-1.5">
      <PropertyPicker
        open={open}
        onOpenChange={(v: boolean) => {
          setOpen(v);
          if (!v) setFilter("");
        }}
        width="w-80"
        align={align}
        searchable
        searchPlaceholder={t(($) => $.pickers.label.search_placeholder)}
        onSearchChange={setFilter}
        triggerRender={
          hasLabels ? (
            <div className="flex flex-wrap items-center gap-1 cursor-pointer rounded px-1 -mx-1 hover:bg-accent/30 transition-colors" />
          ) : undefined
        }
        trigger={
          hasLabels ? (
            <>
              {attachedLabels.map((l) => (
                <LabelChip
                  key={l.id}
                  label={l}
                  onRemove={() => detach.mutate(l.id)}
                />
              ))}
            </>
          ) : (
            <>
              <Tag className="h-3.5 w-3.5 text-muted-foreground" />
              <span className="text-muted-foreground">{t(($) => $.pickers.label.trigger_label)}</span>
            </>
          )
        }
        footer={
          // Rendered outside the arrow-key listbox so keyboard nav doesn't
          // treat "Manage labels…" as another label option.
          <button
            type="button"
            onClick={openManage}
            className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm text-muted-foreground hover:bg-accent transition-colors"
          >
            <Settings2 className="h-3.5 w-3.5" />
            <span>{t(($) => $.pickers.label.manage_action)}</span>
          </button>
        }
      >
        {filtered.map((label) => {
          const selected = attachedIds.has(label.id);
          return (
            <PickerItem
              key={label.id}
              selected={selected}
              onClick={() => toggle(label.id)}
            >
              <span
                className="inline-block h-3 w-3 shrink-0 rounded-full"
                style={{ backgroundColor: label.color }}
                aria-hidden
              />
              <span className="truncate">{label.name}</span>
            </PickerItem>
          );
        })}
        {filtered.length === 0 && !canCreate && <PickerEmpty />}
        {canCreate && (
          <PickerItem selected={false} onClick={createAndAttach}>
            <Plus className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
            <span className="truncate">
              {t(($) => $.pickers.label.create_action)} <span className="font-medium">&ldquo;{query}&rdquo;</span>
            </span>
            <span
              className="ml-auto inline-block h-3 w-3 shrink-0 rounded-full"
              style={{ backgroundColor: pickInlineColor(query) }}
              aria-hidden
            />
          </PickerItem>
        )}
      </PropertyPicker>

      <Dialog open={manageOpen} onOpenChange={setManageOpen}>
        <DialogContent className="max-w-2xl">
          <DialogTitle className="text-lg font-semibold">{t(($) => $.pickers.label.manage_dialog_title)}</DialogTitle>
          <LabelsPanel />
        </DialogContent>
      </Dialog>
    </div>
  );
}
