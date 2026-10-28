"use client";

import { useState } from "react";
import { X, Plus, Filter, ExternalLink, CaseSensitive } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import { TooltipHint } from "@multica/ui/components/common/tooltip-hint";
import { Button } from "@multica/ui/components/ui/button";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import type {
  WebhookEventFilter,
  WebhookPayloadCondition,
} from "@multica/core/types";
import { useT } from "../../i18n";
import {
  CONDITION_OPS,
  EMPTY_CONDITION_DRAFT,
  MAX_CONDITIONS,
  buildCondition,
  formatCondition,
  formatConditionValue,
  type ConditionDraft,
  type ConditionOp,
} from "./webhook-payload-condition";

interface WebhookEventFilterSectionProps {
  filters: WebhookEventFilter[];
  onChange: (filters: WebhookEventFilter[]) => void;
}

export function WebhookEventFilterSection({
  filters,
  onChange,
}: WebhookEventFilterSectionProps) {
  const { t, i18n } = useT("autopilots");
  const [newEvent, setNewEvent] = useState("");
  const [newActions, setNewActions] = useState("");
  const [editingIdx, setEditingIdx] = useState<number | null>(null);
  const [draft, setDraft] = useState<ConditionDraft>(EMPTY_CONDITION_DRAFT);
  const draftCondition = buildCondition(draft);
  const docsHref = i18n.language?.startsWith("zh")
    ? `https://multica.ai/docs/zh/autopilots#${encodeURIComponent("事件过滤")}`
    : i18n.language?.startsWith("fr")
      ? `https://multica.ai/docs/fr/autopilots#${encodeURIComponent("filtres-dévénements")}`
      : "https://multica.ai/docs/autopilots#event-filters";

  const addFilter = () => {
    const event = newEvent.trim();
    if (!event) return;
    const actions = newActions
      .split(",")
      .map((a) => a.trim())
      .filter((a) => a.length > 0);
    const next: WebhookEventFilter = { event };
    if (actions.length > 0) next.actions = actions;
    onChange([...filters, next]);
    setNewEvent("");
    setNewActions("");
  };

  const removeFilter = (idx: number) => {
    onChange(filters.filter((_, i) => i !== idx));
    setEditingIdx(null);
  };

  const updateConditions = (idx: number, conditions: WebhookPayloadCondition[]) => {
    onChange(
      filters.map((f, i) => {
        if (i !== idx) return f;
        const { conditions: _previous, ...rest } = f;
        return conditions.length > 0 ? { ...rest, conditions } : rest;
      }),
    );
  };

  const openEditor = (idx: number) => {
    setEditingIdx(idx);
    setDraft(EMPTY_CONDITION_DRAFT);
  };

  const addCondition = (idx: number) => {
    if (!draftCondition) return;
    updateConditions(idx, [...(filters[idx]?.conditions ?? []), draftCondition]);
    setEditingIdx(null);
  };

  const opLabel = (op: ConditionOp) => t(($) => $.dialog.event_filter_condition_ops[op]);

  return (
    <div className="space-y-2">
      <div className="flex items-center gap-1.5 text-micro font-semibold tracking-[0.08em] text-muted-foreground uppercase">
        <Filter className="size-3" />
        {t(($) => $.dialog.event_filter_label)}
        <a
          href={docsHref}
          target="_blank"
          rel="noopener noreferrer"
          aria-label={t(($) => $.dialog.event_filter_docs_link_label)}
          title={t(($) => $.dialog.event_filter_docs_link_label)}
          className="ml-0.5 inline-flex items-center text-faint-foreground hover:text-foreground transition-colors"
        >
          <ExternalLink className="size-3" />
        </a>
      </div>

      {filters.length > 0 && (
        <div className="space-y-1">
          {filters.map((f, idx) => {
            const conditions = f.conditions ?? [];
            const editing = editingIdx === idx;
            const canAddCondition = !editing && conditions.length < MAX_CONDITIONS;
            return (
              <div key={idx} className="rounded-md border bg-background text-caption">
                <div className="flex min-w-0 items-center gap-2 py-1 pr-1 pl-2.5">
                  <span className="shrink-0 font-mono font-medium text-foreground">{f.event}</span>
                  {f.actions && f.actions.length > 0 && (
                    <span className="min-w-0 truncate text-muted-foreground">
                      {f.actions.join(" · ")}
                    </span>
                  )}
                  <div className="ml-auto flex shrink-0 items-center">
                    {canAddCondition && (
                      <TooltipHint label={t(($) => $.dialog.event_filter_condition_add_label)}>
                        <Button
                          type="button"
                          variant="ghost"
                          size="icon-xs"
                          onClick={() => openEditor(idx)}
                          aria-label={t(($) => $.dialog.event_filter_condition_add_label)}
                        >
                          <Plus />
                        </Button>
                      </TooltipHint>
                    )}
                    <TooltipHint label={t(($) => $.dialog.event_filter_remove_label)}>
                      <Button
                        type="button"
                        variant="ghost"
                        size="icon-xs"
                        onClick={() => removeFilter(idx)}
                        aria-label={t(($) => $.dialog.event_filter_remove_label)}
                      >
                        <X />
                      </Button>
                    </TooltipHint>
                  </div>
                </div>

                {conditions.length > 0 && (
                  <ul className="border-t py-1">
                    {conditions.map((c, cIdx) => (
                      <li
                        key={cIdx}
                        title={formatCondition(c)}
                        className="flex min-w-0 items-center gap-1.5 py-0.5 pr-1 pl-2.5"
                      >
                        <span className="min-w-0 truncate font-mono text-foreground">{c.path}</span>
                        <span className="shrink-0 text-muted-foreground">{opLabel(c.op)}</span>
                        {c.op !== "exists" && (
                          <span className="max-w-[45%] shrink-0 truncate font-mono text-foreground">
                            {formatConditionValue(c)}
                          </span>
                        )}
                        {c.ignore_case && (
                          <CaseSensitive
                            aria-label={t(($) => $.dialog.event_filter_condition_ignore_case)}
                            className="size-3.5 shrink-0 text-muted-foreground"
                          />
                        )}
                        <TooltipHint label={t(($) => $.dialog.event_filter_condition_remove_label)}>
                          <Button
                            type="button"
                            variant="ghost"
                            size="icon-xs"
                            className="ml-auto"
                            onClick={() =>
                              updateConditions(
                                idx,
                                conditions.filter((_, i) => i !== cIdx),
                              )
                            }
                            aria-label={t(($) => $.dialog.event_filter_condition_remove_label)}
                          >
                            <X />
                          </Button>
                        </TooltipHint>
                      </li>
                    ))}
                  </ul>
                )}

                {editing && (
                  <div
                    role="group"
                    aria-label={t(($) => $.dialog.event_filter_condition_add_label)}
                    className="space-y-1.5 border-t p-2"
                  >
                    <input
                      type="text"
                      autoFocus
                      value={draft.path}
                      onChange={(e) => setDraft({ ...draft, path: e.target.value })}
                      aria-label={t(($) => $.dialog.event_filter_condition_path_label)}
                      placeholder={t(($) => $.dialog.event_filter_condition_path_placeholder)}
                      className="h-7 w-full rounded-md border bg-background px-2.5 text-caption font-mono outline-none focus:ring-1 focus:ring-ring"
                    />
                    <div className="flex gap-1.5">
                      <Select
                        items={CONDITION_OPS.map((op) => ({ value: op, label: opLabel(op) }))}
                        value={draft.op}
                        onValueChange={(v) => {
                          if (v) setDraft({ ...draft, op: v as ConditionOp });
                        }}
                      >
                        <SelectTrigger
                          size="sm"
                          aria-label={t(($) => $.dialog.event_filter_condition_op_label)}
                          className={cn("text-caption", draft.op === "exists" ? "w-full" : "w-28 shrink-0")}
                        >
                          <SelectValue />
                        </SelectTrigger>
                        <SelectContent>
                          {CONDITION_OPS.map((op) => (
                            <SelectItem key={op} value={op}>
                              {opLabel(op)}
                            </SelectItem>
                          ))}
                        </SelectContent>
                      </Select>
                      {draft.op !== "exists" && (
                        <input
                          type="text"
                          value={draft.value}
                          onChange={(e) => setDraft({ ...draft, value: e.target.value })}
                          onKeyDown={(e) => {
                            if (e.key === "Enter") {
                              e.preventDefault();
                              addCondition(idx);
                            }
                          }}
                          aria-label={t(($) => $.dialog.event_filter_condition_value_label)}
                          placeholder={
                            draft.op === "in"
                              ? t(($) => $.dialog.event_filter_condition_values_placeholder)
                              : t(($) => $.dialog.event_filter_condition_value_placeholder)
                          }
                          className="h-7 min-w-0 flex-1 rounded-md border bg-background px-2.5 text-caption font-mono outline-none focus:ring-1 focus:ring-ring"
                        />
                      )}
                    </div>
                    <div className="flex items-center gap-1.5">
                      {draft.op !== "exists" && (
                        <label className="inline-flex items-center gap-1.5 whitespace-nowrap text-muted-foreground">
                          <Checkbox
                            checked={draft.ignoreCase}
                            onCheckedChange={(checked) =>
                              setDraft({ ...draft, ignoreCase: checked === true })
                            }
                          />
                          {t(($) => $.dialog.event_filter_condition_ignore_case)}
                        </label>
                      )}
                      <Button
                        type="button"
                        variant="ghost"
                        size="xs"
                        className="ml-auto"
                        onClick={() => setEditingIdx(null)}
                      >
                        {t(($) => $.dialog.event_filter_condition_cancel)}
                      </Button>
                      <Button
                        type="button"
                        variant="secondary"
                        size="xs"
                        onClick={() => addCondition(idx)}
                        disabled={!draftCondition}
                      >
                        {t(($) => $.dialog.event_filter_add)}
                      </Button>
                    </div>
                  </div>
                )}
              </div>
            );
          })}
        </div>
      )}

      <div className="flex gap-2">
        <input
          type="text"
          value={newEvent}
          onChange={(e) => setNewEvent(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter") {
              e.preventDefault();
              addFilter();
            }
          }}
          placeholder={t(($) => $.dialog.event_filter_event_placeholder)}
          className="flex-1 min-w-0 rounded-md border bg-background px-2.5 py-1.5 text-caption font-mono outline-none focus:ring-1 focus:ring-ring"
        />
        <input
          type="text"
          value={newActions}
          onChange={(e) => setNewActions(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter") {
              e.preventDefault();
              addFilter();
            }
          }}
          placeholder={t(($) => $.dialog.event_filter_actions_placeholder)}
          className="w-28 rounded-md border bg-background px-2.5 py-1.5 text-caption outline-none focus:ring-1 focus:ring-ring"
        />
        <button
          type="button"
          onClick={addFilter}
          disabled={!newEvent.trim()}
          className={cn(
            "inline-flex shrink-0 items-center justify-center gap-1 rounded-md border px-2.5 py-1.5 text-caption font-medium transition-colors",
            newEvent.trim()
              ? "border-primary bg-primary text-primary-foreground hover:bg-primary/90 cursor-pointer"
              : "bg-muted text-muted-foreground cursor-not-allowed",
          )}
        >
          <Plus className="size-3.5" />
          {t(($) => $.dialog.event_filter_add)}
        </button>
      </div>
      <p className="text-micro text-muted-foreground">
        {t(($) => $.dialog.event_filter_hint)}
      </p>
    </div>
  );
}
