"use client";

import { useMemo, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Tag, Plus, Settings2 } from "lucide-react";
import { toast } from "sonner";
import type { Label } from "@multica/core/types";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import {
  labelListOptions,
  issueLabelsOptions,
  useAttachLabel,
  useDetachLabel,
  useCreateLabel,
} from "@multica/core/labels";
import { LabelChip } from "../../../labels/label-chip";
import { useNavigation } from "../../../navigation";
import {
  PropertyPicker,
  PickerItem,
  PickerEmpty,
} from "./property-picker";
import { useT } from "../../../i18n";

interface LabelPickerProps {
  /**
   * The issue whose labels are edited. Omit for **draft mode** (e.g. the
   * create-issue dialog, where the issue doesn't exist yet): pass
   * `selectedIds` + `onSelectedIdsChange` instead and attach the labels to the
   * issue once it's created.
   */
  issueId?: string;
  /** Draft-mode selection. Ignored when `issueId` is set. */
  selectedIds?: string[];
  /** Draft-mode change handler. Ignored when `issueId` is set. */
  onSelectedIdsChange?: (ids: string[]) => void;
  /** Optional controlled open state (for tests / cmd+k integration). */
  open?: boolean;
  onOpenChange?: (open: boolean) => void;
  align?: "start" | "center" | "end";
  /** Open the picker on first mount. Used by progressive-disclosure
   *  sidebars so a newly-added field immediately enters edit state. */
  defaultOpen?: boolean;
  /** Custom trigger element (e.g. a `PillButton` for the create toolbar).
   *  When set, the attached-label chips render inside it without their own
   *  × affordance — a remove <button> can't nest inside a trigger <button>,
   *  so removal happens by toggling the label off in the open picker. */
  triggerRender?: React.ReactElement;
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
 * Multi-select label picker. Shows currently-selected labels as inline chips
 * on the trigger and lets the user toggle any label in the workspace.
 *
 * Two modes:
 * - **Attached mode** (`issueId` set): attach/detach hit the server
 *   optimistically — the UI updates before the server confirms.
 * - **Draft mode** (`issueId` omitted): selection is held by the caller via
 *   `selectedIds` / `onSelectedIdsChange`; nothing is persisted until the
 *   caller attaches the labels itself. Used by the create-issue dialog.
 *
 * When the search term has no matches, offers inline creation: typing a
 * new name and pressing Enter (or clicking the "Create X" row) creates the
 * label with a hash-derived color and selects it in one motion. The created
 * label is a real workspace label in both modes; only the attach step differs.
 *
 * A "Manage labels" item at the bottom opens the workspace Labels settings
 * page, which is the single management surface for issue, agent, and skill
 * label catalogs.
 */
export function LabelPicker({
  issueId,
  selectedIds = [],
  onSelectedIdsChange,
  open: controlledOpen,
  onOpenChange,
  align = "start",
  defaultOpen = false,
  triggerRender,
}: LabelPickerProps) {
  const { t } = useT("issues");
  const [internalOpen, setInternalOpen] = useState(defaultOpen);
  const open = controlledOpen ?? internalOpen;
  const setOpen = onOpenChange ?? setInternalOpen;
  const [filter, setFilter] = useState("");
  const navigation = useNavigation();
  const paths = useWorkspacePaths();

  // Synchronous lock to prevent double-submit on rapid Enter / click. React
  // state (create.isPending, filter) isn't visible until the next render, so
  // two events within the same tick can both pass the canCreate guard and
  // fire two create.mutate calls — the second hits 409 and shows a red toast
  // for an error the user didn't cause. A ref closes the window cleanly.
  const creatingRef = useRef(false);

  // Draft mode when no issue exists yet: hold selection in the caller instead
  // of hitting the attach/detach endpoints.
  const isDraft = issueId === undefined;

  const wsId = useWorkspaceId();
  const { data: allLabels = [] } = useQuery(labelListOptions(wsId));
  // `issueLabelsOptions` disables itself for an empty id, so the draft path
  // never fires the by-issue read.
  const { data: attachedLabels = [] } = useQuery(issueLabelsOptions(wsId, issueId ?? ""));

  // Hooks must run unconditionally; in draft mode the empty id is never used
  // because toggle/create route through onSelectedIdsChange instead.
  const attach = useAttachLabel(issueId ?? "");
  const detach = useDetachLabel(issueId ?? "");
  const create = useCreateLabel();

  // The selected set drives both the trigger chips and the list checkmarks.
  // Draft mode resolves ids against the workspace list (dropping any id whose
  // label was deleted meanwhile) and preserves the user's selection order.
  const selectedLabels = useMemo<Label[]>(() => {
    if (!isDraft) return attachedLabels;
    return selectedIds
      .map((id) => allLabels.find((l) => l.id === id))
      .filter((l): l is Label => Boolean(l));
  }, [isDraft, attachedLabels, selectedIds, allLabels]);

  const selectedIdSet = useMemo(
    () => new Set(selectedLabels.map((l) => l.id)),
    [selectedLabels],
  );

  const query = filter.trim();
  const queryLower = query.toLowerCase();
  const filtered = allLabels.filter((l) => l.name.toLowerCase().includes(queryLower));
  const exactMatch = allLabels.some((l) => l.name.toLowerCase() === queryLower);
  const canCreate = query.length > 0 && !exactMatch && !create.isPending;

  const removeLabel = (labelId: string) => {
    if (isDraft) {
      onSelectedIdsChange?.(selectedIds.filter((id) => id !== labelId));
    } else {
      detach.mutate(labelId);
    }
  };

  const toggle = (labelId: string) => {
    if (isDraft) {
      onSelectedIdsChange?.(
        selectedIdSet.has(labelId)
          ? selectedIds.filter((id) => id !== labelId)
          : [...selectedIds, labelId],
      );
    } else if (selectedIdSet.has(labelId)) {
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
          if (isDraft) {
            onSelectedIdsChange?.([...selectedIds, label.id]);
          } else {
            attach.mutate(label.id);
          }
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
    navigation.push(`${paths.settings()}?tab=labels`);
  };

  const hasLabels = selectedLabels.length > 0;

  // In a custom trigger (PillButton) the trigger is itself a button, so the
  // chips can't carry their own remove button. Otherwise fall back to the
  // chip-wrap div used by the issue-detail sidebar.
  const resolvedTriggerRender =
    triggerRender ??
    (hasLabels ? (
      <div className="flex flex-wrap items-center gap-1 cursor-pointer rounded px-1 -mx-1 hover:bg-accent/30 transition-colors" />
    ) : undefined);

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
        triggerRender={resolvedTriggerRender}
        trigger={
          hasLabels ? (
            <>
              {selectedLabels.map((l) => (
                <LabelChip
                  key={l.id}
                  label={l}
                  onRemove={triggerRender ? undefined : () => removeLabel(l.id)}
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
          const selected = selectedIdSet.has(label.id);
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
    </div>
  );
}
