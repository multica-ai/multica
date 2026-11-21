"use client";

import { useEffect, useId, useRef } from "react";
import { X, Plus, Filter, ExternalLink } from "@multica/ui/icons";
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
import type { WebhookEventFilter } from "@multica/core/types";
import { useT } from "../../i18n";
import { useConfigStore } from "@multica/core/config";
import {
  CONDITION_OPS,
  MAX_CONDITIONS,
  MAX_CONDITION_PATH_LENGTH,
  MAX_CONDITION_VALUES,
  type ConditionOp,
} from "./webhook-payload-condition";
import {
  newConditionDraft,
  newRuleDraft,
  resolveEventFilterDrafts,
  type ConditionFieldError,
  type ConditionRowDraft,
  type EventFilterDraft,
} from "./webhook-event-filter-draft";
import { isTraditionalChinese } from "../../common/docs-locale";

interface WebhookEventFilterSectionProps {
  drafts: EventFilterDraft[];
  onChange: (drafts: EventFilterDraft[]) => void;
  // Set once a save was attempted: incomplete fields are marked from then on,
  // and the first of them takes focus.
  showErrors: boolean;
}

const INPUT =
  "h-7 w-full min-w-0 rounded-md border bg-background px-2.5 text-caption outline-none focus:ring-1 focus:ring-ring aria-invalid:border-destructive aria-invalid:focus:ring-destructive";

// Rules and conditions are edited where they are shown: "Add rule" appends an
// empty rule card, "Add condition" an empty condition row, and every field is
// an input, existing ones included. What the inputs hold is what Save sends,
// so there is no per-row commit to forget, and an incomplete row stops the
// save and says where instead of being dropped.
export function WebhookEventFilterSection({
  drafts,
  onChange,
  showErrors,
}: WebhookEventFilterSectionProps) {
  const { t, i18n } = useT("autopilots");
  const idPrefix = useId();
  const rootRef = useRef<HTMLDivElement>(null);
  const payloadSupported = useConfigStore((state) => state.webhookPayloadConditionsSupported);
  const { errors } = resolveEventFilterDrafts(drafts);
  const docsHref = isTraditionalChinese(i18n.language)
    ? `https://multica.ai/docs/zh-Hant/autopilots#${encodeURIComponent("事件過濾")}`
    : i18n.language?.startsWith("zh")
      ? `https://multica.ai/docs/zh/autopilots#${encodeURIComponent("事件过滤")}`
      : i18n.language?.startsWith("fr")
        ? `https://multica.ai/docs/fr/autopilots#${encodeURIComponent("filtres-dévénements")}`
        : "https://multica.ai/docs/autopilots#event-filters";

  // A blocked save lands the user on the first field that blocked it.
  useEffect(() => {
    if (!showErrors) return;
    rootRef.current?.querySelector<HTMLElement>('[aria-invalid="true"]')?.focus();
  }, [showErrors]);

  // A row added with a button is typed into next, so its first field takes
  // focus once it renders.
  const focusKeyRef = useRef<string | null>(null);
  useEffect(() => {
    const key = focusKeyRef.current;
    if (!key) return;
    focusKeyRef.current = null;
    rootRef.current?.querySelector<HTMLElement>(`[data-focus-key="${key}"]`)?.focus();
  }, [drafts]);

  const updateRule = (key: string, patch: Partial<EventFilterDraft>) =>
    onChange(drafts.map((d) => (d.key === key ? { ...d, ...patch } : d)));

  const updateCondition = (ruleKey: string, rowKey: string, patch: Partial<ConditionRowDraft>) =>
    onChange(
      drafts.map((d) =>
        d.key === ruleKey
          ? { ...d, conditions: d.conditions.map((c) => (c.key === rowKey ? { ...c, ...patch } : c)) }
          : d,
      ),
    );

  const conditionErrorText = (error: ConditionFieldError) => {
    switch (error) {
      case "path_required":
        return t(($) => $.dialog.event_filter_error_path_required);
      case "path_invalid":
        return t(($) => $.dialog.event_filter_error_path_invalid, {
          path: MAX_CONDITION_PATH_LENGTH,
        });
      case "value_required":
        return t(($) => $.dialog.event_filter_error_value_required);
      case "values_too_many":
        return t(($) => $.dialog.event_filter_error_too_many_values, {
          values: MAX_CONDITION_VALUES,
        });
    }
  };

  const opLabel = (op: ConditionOp) => t(($) => $.dialog.event_filter_condition_ops[op]);

  return (
    <div ref={rootRef} className="space-y-2">
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

      {drafts.map((rule) => {
        const eventError = showErrors ? errors.events[rule.key] : undefined;
        const eventErrorId = `${idPrefix}-${rule.key}-event-error`;
        const canAddCondition = payloadSupported && rule.conditions.length < MAX_CONDITIONS;
        return (
          <div
            key={rule.key}
            role="group"
            aria-label={rule.event.trim() || t(($) => $.dialog.event_filter_new_rule_label)}
            className="space-y-2 rounded-md border bg-background p-2 text-caption"
          >
            <div className="flex items-start gap-1.5">
              <div className="min-w-0 flex-[3]">
                <input
                  type="text"
                  data-focus-key={rule.key}
                  value={rule.event}
                  onChange={(e) => updateRule(rule.key, { event: e.target.value })}
                  aria-label={t(($) => $.dialog.event_filter_event_label)}
                  aria-invalid={eventError ? true : undefined}
                  aria-describedby={eventError ? eventErrorId : undefined}
                  placeholder={t(($) => $.dialog.event_filter_event_placeholder)}
                  className={cn(INPUT, "font-mono")}
                />
                {eventError && (
                  <p id={eventErrorId} className="mt-1 text-micro text-destructive">
                    {t(($) => $.dialog.event_filter_error_event_required)}
                  </p>
                )}
              </div>
              <input
                type="text"
                value={rule.actions}
                onChange={(e) => updateRule(rule.key, { actions: e.target.value })}
                aria-label={t(($) => $.dialog.event_filter_actions_label)}
                placeholder={t(($) => $.dialog.event_filter_actions_placeholder)}
                className={cn(INPUT, "flex-[2]")}
              />
              <TooltipHint label={t(($) => $.dialog.event_filter_remove_label)}>
                <Button
                  type="button"
                  variant="ghost"
                  size="icon-xs"
                  className="mt-1 shrink-0"
                  onClick={() => onChange(drafts.filter((d) => d.key !== rule.key))}
                  aria-label={t(($) => $.dialog.event_filter_remove_label)}
                >
                  <X />
                </Button>
              </TooltipHint>
            </div>

            {rule.conditions.length > 0 && (
              <div className="space-y-2">
                <p className="text-micro text-muted-foreground">
                  {t(($) => $.dialog.event_filter_condition_all)}
                </p>
                {rule.conditions.map((row) => {
                  const pathError = showErrors ? errors.paths[row.key] : undefined;
                  const valueError = showErrors ? errors.values[row.key] : undefined;
                  const pathErrorId = `${idPrefix}-${row.key}-path-error`;
                  const valueErrorId = `${idPrefix}-${row.key}-value-error`;
                  return (
                    <div
                      key={row.key}
                      role="group"
                      aria-label={t(($) => $.dialog.event_filter_condition_label)}
                      className="space-y-1.5 border-l-2 pl-2"
                    >
                      <div className="flex items-start gap-1.5">
                        <div className="min-w-0 flex-1">
                          <input
                            type="text"
                            data-focus-key={row.key}
                            value={row.path}
                            onChange={(e) => updateCondition(rule.key, row.key, { path: e.target.value })}
                            aria-label={t(($) => $.dialog.event_filter_condition_path_label)}
                            aria-invalid={pathError ? true : undefined}
                            aria-describedby={pathError ? pathErrorId : undefined}
                            placeholder={t(($) => $.dialog.event_filter_condition_path_placeholder)}
                            className={cn(INPUT, "font-mono")}
                          />
                          {pathError && (
                            <p id={pathErrorId} className="mt-1 text-micro text-destructive">
                              {conditionErrorText(pathError)}
                            </p>
                          )}
                        </div>
                        <TooltipHint label={t(($) => $.dialog.event_filter_condition_remove_label)}>
                          <Button
                            type="button"
                            variant="ghost"
                            size="icon-xs"
                            className="mt-1 shrink-0"
                            onClick={() =>
                              updateRule(rule.key, {
                                conditions: rule.conditions.filter((c) => c.key !== row.key),
                              })
                            }
                            aria-label={t(($) => $.dialog.event_filter_condition_remove_label)}
                          >
                            <X />
                          </Button>
                        </TooltipHint>
                      </div>
                      <div className="flex items-start gap-1.5">
                        <Select
                          items={CONDITION_OPS.map((op) => ({ value: op, label: opLabel(op) }))}
                          value={row.op}
                          onValueChange={(v) => {
                            if (v) updateCondition(rule.key, row.key, { op: v as ConditionOp });
                          }}
                        >
                          <SelectTrigger
                            size="sm"
                            aria-label={t(($) => $.dialog.event_filter_condition_op_label)}
                            className={cn("text-caption", row.op === "exists" ? "w-full" : "w-28 shrink-0")}
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
                        {row.op !== "exists" && (
                          <>
                            <div className="min-w-0 flex-1">
                              <input
                                type="text"
                                value={row.value}
                                onChange={(e) =>
                                  updateCondition(rule.key, row.key, { value: e.target.value })
                                }
                                aria-label={t(($) => $.dialog.event_filter_condition_value_label)}
                                aria-invalid={valueError ? true : undefined}
                                aria-describedby={valueError ? valueErrorId : undefined}
                                placeholder={
                                  row.op === "in"
                                    ? t(($) => $.dialog.event_filter_condition_values_placeholder)
                                    : t(($) => $.dialog.event_filter_condition_value_placeholder)
                                }
                                className={cn(INPUT, "font-mono")}
                              />
                              {valueError && (
                                <p id={valueErrorId} className="mt-1 text-micro text-destructive">
                                  {conditionErrorText(valueError)}
                                </p>
                              )}
                            </div>
                            <label className="inline-flex h-7 shrink-0 items-center gap-1.5 whitespace-nowrap text-muted-foreground">
                              <Checkbox
                                checked={row.ignoreCase}
                                onCheckedChange={(checked) =>
                                  updateCondition(rule.key, row.key, { ignoreCase: checked === true })
                                }
                              />
                              {t(($) => $.dialog.event_filter_condition_ignore_case)}
                            </label>
                          </>
                        )}
                      </div>
                    </div>
                  );
                })}
              </div>
            )}

            {canAddCondition && (
              <Button
                type="button"
                variant="ghost"
                size="xs"
                className="text-muted-foreground"
                onClick={() => {
                  const row = newConditionDraft();
                  focusKeyRef.current = row.key;
                  updateRule(rule.key, { conditions: [...rule.conditions, row] });
                }}
              >
                <Plus />
                {t(($) => $.dialog.event_filter_condition_add_label)}
              </Button>
            )}
          </div>
        );
      })}

      <Button
        type="button"
        variant="ghost"
        size="xs"
        className="text-muted-foreground"
        onClick={() => {
          const rule = newRuleDraft();
          focusKeyRef.current = rule.key;
          onChange([...drafts, rule]);
        }}
      >
        <Plus />
        {t(($) => $.dialog.event_filter_add_rule)}
      </Button>
      <p className="text-micro text-muted-foreground">
        {t(($) => $.dialog.event_filter_hint)}
      </p>
    </div>
  );
}

// How many rules the trigger row spells out before folding the rest into "+N".
const SUMMARY_RULE_LIMIT = 3;

// The read-only readback of a webhook's filters on its trigger row, so the row
// says what it will ignore without opening the editor. No filters is a state
// worth stating: it means every delivery starts a run.
export function WebhookEventFilterSummary({ filters }: { filters: WebhookEventFilter[] }) {
  const { t } = useT("autopilots");
  const shown = filters.slice(0, SUMMARY_RULE_LIMIT);
  const hidden = filters.length - shown.length;
  return (
    <div className="flex min-w-0 flex-wrap items-center gap-1.5 text-caption text-muted-foreground">
      <Filter className="size-3 shrink-0" aria-label={t(($) => $.dialog.event_filter_label)} />
      {filters.length === 0 ? (
        <span>{t(($) => $.trigger_row.event_filter_summary_all)}</span>
      ) : (
        <>
          {shown.map((f, idx) => {
            const conditionCount = f.conditions?.length ?? 0;
            const actions = f.actions && f.actions.length > 0 ? f.actions.join(", ") : null;
            const conditions =
              conditionCount > 0
                ? t(($) => $.trigger_row.event_filter_summary_conditions, { count: conditionCount })
                : null;
            const text = [f.event, actions, conditions].filter(Boolean).join(" · ");
            return (
              <span
                key={idx}
                title={text}
                className="inline-block max-w-full truncate rounded-xs border bg-background px-1.5"
              >
                <span className="font-mono text-foreground">{f.event}</span>
                {actions && <span> {actions}</span>}
                {conditions && <span> · {conditions}</span>}
              </span>
            );
          })}
          {hidden > 0 && <span>+{hidden}</span>}
        </>
      )}
    </div>
  );
}
