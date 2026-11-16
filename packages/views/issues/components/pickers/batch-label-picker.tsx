"use client";

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Check, Minus, X } from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";
import type { BatchIssueLabelOps, Issue, Label } from "@multica/core/types";
import { useWorkspaceId } from "@multica/core/hooks";
import { labelListOptions } from "@multica/core/labels";
import { paletteColor } from "@multica/core/appearance";
import { matchesPinyin } from "../../../editor/extensions/pinyin-match";
import { PickerEmpty, PickerItem, PickerPanel, PropertyPicker } from "./property-picker";
import { useT } from "../../../i18n";

type LabelState = "all" | "some" | "none";
type Pick = { state: "all" | "none"; pending: boolean };

/** Resolves true only when the write applied to every selected issue. */
export type BatchLabelApply = (ops: BatchIssueLabelOps) => Promise<boolean>;

const EMPTY_LABELS: Label[] = [];
const NO_PICKS: ReadonlyMap<string, Pick> = new Map();

/**
 * Label rows for a selection, each with a tri-state box: checked when every
 * issue has the label, a dash when only some do. Picking a row adds the label
 * to every issue, or removes it from all when every issue already has it.
 */
function useBatchLabelRows(issues: readonly Issue[], query: string, onApply: BatchLabelApply) {
  const wsId = useWorkspaceId();
  const { data: labels = EMPTY_LABELS } = useQuery(labelListOptions(wsId, "issue"));
  const counts = useMemo(() => {
    const map = new Map<string, number>();
    for (const issue of issues) {
      for (const label of issue.labels ?? []) map.set(label.id, (map.get(label.id) ?? 0) + 1);
    }
    return map;
  }, [issues]);
  // The write lands through realtime a moment later; hold the picked state
  // until the issues themselves change so the box does not flicker back. A
  // write that did not fully apply drops its hold, so the real counts show and
  // a retry repeats the same operation instead of inverting it.
  const [held, setHeld] = useState<{ issues: readonly Issue[]; picks: ReadonlyMap<string, Pick> }>({
    issues,
    picks: new Map(),
  });
  const picks = held.issues === issues ? held.picks : NO_PICKS;

  const total = issues.length;
  const stateOf = (labelId: string): LabelState => {
    const pick = picks.get(labelId);
    if (pick) return pick.state;
    const n = counts.get(labelId) ?? 0;
    return n === 0 ? "none" : n === total ? "all" : "some";
  };
  const countOf = (labelId: string) => {
    const pick = picks.get(labelId);
    if (pick) return pick.state === "all" ? total : 0;
    return counts.get(labelId) ?? 0;
  };
  const toggle = (labelId: string) => {
    if (picks.get(labelId)?.pending) return;
    const removing = stateOf(labelId) === "all";
    const pick: Pick = { state: removing ? "none" : "all", pending: true };
    setHeld({ issues, picks: new Map(picks).set(labelId, pick) });
    const settle = (applied: boolean) =>
      setHeld((prev) => {
        if (prev.picks.get(labelId) !== pick) return prev;
        const next = new Map(prev.picks);
        if (applied) next.set(labelId, { ...pick, pending: false });
        else next.delete(labelId);
        return { issues: prev.issues, picks: next };
      });
    onApply(removing ? { remove: [labelId] } : { add: [labelId] }).then(
      (applied) => settle(applied === true),
      () => settle(false),
    );
  };

  const q = query.trim().toLowerCase();
  const visible = q
    ? labels.filter((label) => label.name.toLowerCase().includes(q) || matchesPinyin(label.name, q))
    : labels;
  const anyLabeled = issues.some((issue) => (issue.labels?.length ?? 0) > 0);
  return { visible, stateOf, countOf, toggle, total, anyLabeled };
}

function LabelRows({ rows }: { rows: ReturnType<typeof useBatchLabelRows> }) {
  if (rows.visible.length === 0) return <PickerEmpty />;
  return (
    <>
      {rows.visible.map((label) => {
        const state = rows.stateOf(label.id);
        const count = rows.countOf(label.id);
        return (
          <PickerItem key={label.id} selected={false} onClick={() => rows.toggle(label.id)}>
            <span
              role="presentation"
              data-state={state}
              className={cn(
                "flex size-3.5 shrink-0 items-center justify-center rounded-xs border",
                state === "none" ? "border-input" : "border-primary bg-primary text-primary-foreground",
              )}
            >
              {state === "all" ? <Check className="size-3" /> : state === "some" ? <Minus className="size-3" /> : null}
            </span>
            <span
              className="inline-block size-2.5 shrink-0 rounded-full"
              style={{ backgroundColor: paletteColor(label.color) }}
              aria-hidden
            />
            <span className="min-w-0 flex-1 truncate">{label.name}</span>
            {count > 0 && (
              <span className="shrink-0 text-caption tabular-nums text-muted-foreground">
                {count}/{rows.total}
              </span>
            )}
          </PickerItem>
        );
      })}
    </>
  );
}

function ClearLabelsButton({ count, onClick }: { count: number; onClick: () => void }) {
  const { t } = useT("issues");
  return (
    <button
      type="button"
      onClick={onClick}
      className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-body text-muted-foreground transition-colors hover:bg-accent"
    >
      <X className="h-3.5 w-3.5" />
      <span>{t(($) => $.batch.clear_labels, { count })}</span>
    </button>
  );
}

export function BatchLabelPicker({
  issues,
  onApply,
  open,
  onOpenChange,
  trigger,
  triggerRender,
  align = "center",
}: {
  issues: readonly Issue[];
  onApply: BatchLabelApply;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  trigger: React.ReactNode;
  triggerRender?: React.ReactElement;
  align?: "start" | "center" | "end";
}) {
  const { t } = useT("issues");
  const [query, setQuery] = useState("");
  const rows = useBatchLabelRows(issues, query, onApply);
  return (
    <PropertyPicker
      open={open}
      onOpenChange={onOpenChange}
      width="w-72"
      align={align}
      side="top"
      searchable
      searchPlaceholder={t(($) => $.batch.labels_search)}
      onSearchChange={setQuery}
      trigger={trigger}
      triggerRender={triggerRender}
      footer={
        rows.anyLabeled ? (
          <ClearLabelsButton
            count={rows.total}
            onClick={() => {
              void onApply({ clear: true });
              onOpenChange(false);
            }}
          />
        ) : undefined
      }
    >
      <LabelRows rows={rows} />
    </PropertyPicker>
  );
}

/** The same rows inside a menu's submenu. */
export function BatchLabelPanel({
  issues,
  onApply,
  onClose,
}: {
  issues: readonly Issue[];
  onApply: BatchLabelApply;
  onClose: () => void;
}) {
  const { t } = useT("issues");
  const [query, setQuery] = useState("");
  const rows = useBatchLabelRows(issues, query, onApply);
  return (
    <PickerPanel
      searchable
      autoFocusSearch
      searchPlaceholder={t(($) => $.batch.labels_search)}
      onSearchChange={setQuery}
      footer={
        rows.anyLabeled ? (
          <ClearLabelsButton
            count={rows.total}
            onClick={() => {
              void onApply({ clear: true });
              onClose();
            }}
          />
        ) : undefined
      }
    >
      <LabelRows rows={rows} />
    </PickerPanel>
  );
}
