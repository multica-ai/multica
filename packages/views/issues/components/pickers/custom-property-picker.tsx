"use client";

import { useEffect, useState } from "react";
import { CalendarDays, ExternalLink } from "lucide-react";
import { toast } from "sonner";
import type { Issue, IssueProperty, IssuePropertyValue } from "@multica/core/types";
import {
  useSetIssueProperty,
  useUnsetIssueProperty,
} from "@multica/core/properties";
import {
  toDateOnly,
  dateOnlyToLocalDate,
  formatDateOnly,
} from "@multica/core/issues/date";
import { Calendar } from "@multica/ui/components/ui/calendar";
import {
  Popover,
  PopoverTrigger,
  PopoverContent,
} from "@multica/ui/components/ui/popover";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { useT } from "../../../i18n";
import { PropertyPicker, PickerItem } from "./property-picker";

/**
 * Value editor for one custom property on one issue. The editor shape
 * follows the definition type:
 *
 *   select        → PropertyPicker with one PickerItem per option
 *   multi_select  → PropertyPicker with toggling items (stays open)
 *   date          → Calendar popover (mirrors DueDatePicker)
 *   checkbox      → Yes / No picker
 *   text/number/url → popover with an input, Enter commits
 *
 * Archived definitions render read-only: the popover only offers Clear
 * (the server rejects new values on archived properties but always allows
 * unset). Unknown types from newer servers degrade to the read-only view.
 */
export function CustomPropertyValueEditor({
  issue,
  property,
  defaultOpen = false,
}: {
  issue: Issue;
  property: IssueProperty;
  defaultOpen?: boolean;
}) {
  const { t } = useT("issues");
  const [open, setOpen] = useState(defaultOpen);
  const setProperty = useSetIssueProperty();
  const unsetProperty = useUnsetIssueProperty();

  const value = issue.properties[property.id];
  const hasValue = value !== undefined;

  const onError = (error: unknown) =>
    toast.error(error instanceof Error ? error.message : String(error));

  const commit = (next: IssuePropertyValue) =>
    setProperty.mutate(
      { issueId: issue.id, propertyId: property.id, value: next },
      { onError },
    );
  const clear = () =>
    unsetProperty.mutate({ issueId: issue.id, propertyId: property.id }, { onError });

  const emptyLabel = (
    <span className="text-muted-foreground">
      {t(($) => $.pickers.custom_property.empty)}
    </span>
  );

  const clearFooter = hasValue ? (
    <Button
      variant="ghost"
      size="xs"
      onClick={() => {
        clear();
        setOpen(false);
      }}
      className="w-full justify-start text-muted-foreground hover:text-foreground"
    >
      {t(($) => $.pickers.custom_property.clear_action)}
    </Button>
  ) : undefined;

  // Archived (or unknown-type) definitions: read-only display; the only
  // offered action is Clear so stale values can still be cleaned up.
  const readOnly =
    property.archived ||
    !["select", "multi_select", "date", "checkbox", "text", "number", "url"].includes(
      property.type,
    );

  if (readOnly) {
    return (
      <PropertyPicker
        open={open}
        onOpenChange={setOpen}
        align="start"
        trigger={<CustomPropertyValueDisplay property={property} value={value} />}
        footer={clearFooter}
      >
        <p className="px-2 py-1.5 text-xs text-muted-foreground">
          {t(($) => $.pickers.custom_property.archived_hint)}
        </p>
      </PropertyPicker>
    );
  }

  switch (property.type) {
    case "select": {
      const options = property.config.options ?? [];
      return (
        <PropertyPicker
          open={open}
          onOpenChange={setOpen}
          align="start"
          searchable={options.length > 7}
          trigger={<CustomPropertyValueDisplay property={property} value={value} />}
          footer={clearFooter}
        >
          {options.map((option) => (
            <PickerItem
              key={option.id}
              selected={value === option.id}
              onClick={() => {
                commit(option.id);
                setOpen(false);
              }}
            >
              <span className="size-2.5 shrink-0 rounded-full" style={{ backgroundColor: option.color }} />
              <span className="truncate">{option.name}</span>
            </PickerItem>
          ))}
        </PropertyPicker>
      );
    }
    case "multi_select": {
      const options = property.config.options ?? [];
      const selected = Array.isArray(value) ? value : [];
      const toggle = (optionId: string) => {
        const next = selected.includes(optionId)
          ? selected.filter((id) => id !== optionId)
          : [...selected, optionId];
        if (next.length === 0) clear();
        else commit(next);
      };
      return (
        <PropertyPicker
          open={open}
          onOpenChange={setOpen}
          align="start"
          searchable={options.length > 7}
          trigger={<CustomPropertyValueDisplay property={property} value={value} />}
          footer={clearFooter}
        >
          {options.map((option) => (
            <PickerItem
              key={option.id}
              selected={selected.includes(option.id)}
              onClick={() => toggle(option.id)}
            >
              <span className="size-2.5 shrink-0 rounded-full" style={{ backgroundColor: option.color }} />
              <span className="truncate">{option.name}</span>
            </PickerItem>
          ))}
        </PropertyPicker>
      );
    }
    case "date": {
      const date = typeof value === "string" ? dateOnlyToLocalDate(value) : undefined;
      return (
        <Popover open={open} onOpenChange={setOpen}>
          <PopoverTrigger className="flex items-center gap-1.5 cursor-pointer rounded px-1 -mx-1 hover:bg-accent/30 transition-colors overflow-hidden">
            <CustomPropertyValueDisplay property={property} value={value} />
          </PopoverTrigger>
          <PopoverContent className="w-auto p-0" align="start">
            <Calendar
              mode="single"
              selected={date}
              onSelect={(d: Date | undefined) => {
                if (d) commit(toDateOnly(d));
                else clear();
                setOpen(false);
              }}
            />
            {date && (
              <div className="border-t px-3 py-2">
                <Button
                  variant="ghost"
                  size="xs"
                  onClick={() => {
                    clear();
                    setOpen(false);
                  }}
                  className="text-muted-foreground hover:text-foreground"
                >
                  {t(($) => $.pickers.custom_property.clear_action)}
                </Button>
              </div>
            )}
          </PopoverContent>
        </Popover>
      );
    }
    case "checkbox":
      return (
        <PropertyPicker
          open={open}
          onOpenChange={setOpen}
          align="start"
          trigger={<CustomPropertyValueDisplay property={property} value={value} />}
          footer={clearFooter}
        >
          <PickerItem
            selected={value === true}
            onClick={() => {
              commit(true);
              setOpen(false);
            }}
          >
            {t(($) => $.pickers.custom_property.true_label)}
          </PickerItem>
          <PickerItem
            selected={value === false}
            onClick={() => {
              commit(false);
              setOpen(false);
            }}
          >
            {t(($) => $.pickers.custom_property.false_label)}
          </PickerItem>
        </PropertyPicker>
      );
    default:
      return (
        <TextishPropertyEditor
          property={property}
          value={value}
          open={open}
          onOpenChange={setOpen}
          onCommit={commit}
          onClear={clear}
          emptyLabel={emptyLabel}
        />
      );
  }
}

/** Popover-with-input editor shared by text / number / url. */
function TextishPropertyEditor({
  property,
  value,
  open,
  onOpenChange,
  onCommit,
  onClear,
  emptyLabel,
}: {
  property: IssueProperty;
  value: IssuePropertyValue | undefined;
  open: boolean;
  onOpenChange: (v: boolean) => void;
  onCommit: (next: IssuePropertyValue) => void;
  onClear: () => void;
  emptyLabel: React.ReactNode;
}) {
  const { t } = useT("issues");
  const [draft, setDraft] = useState("");

  useEffect(() => {
    if (open) setDraft(value === undefined ? "" : String(value));
  }, [open, value]);

  const placeholder =
    property.type === "url"
      ? t(($) => $.pickers.custom_property.url_placeholder)
      : property.type === "number"
        ? t(($) => $.pickers.custom_property.number_placeholder)
        : t(($) => $.pickers.custom_property.value_placeholder);

  const submit = () => {
    const trimmed = draft.trim();
    if (!trimmed) {
      if (value !== undefined) onClear();
      onOpenChange(false);
      return;
    }
    if (property.type === "number") {
      const parsed = Number(trimmed);
      if (Number.isNaN(parsed)) return;
      onCommit(parsed);
    } else {
      onCommit(trimmed);
    }
    onOpenChange(false);
  };

  return (
    <Popover open={open} onOpenChange={onOpenChange}>
      <PopoverTrigger className="flex items-center gap-1.5 cursor-pointer rounded px-1 -mx-1 hover:bg-accent/30 transition-colors overflow-hidden">
        {value === undefined ? (
          emptyLabel
        ) : (
          <CustomPropertyValueDisplay property={property} value={value} />
        )}
      </PopoverTrigger>
      <PopoverContent className="w-64 p-2" align="start">
        <form
          onSubmit={(event) => {
            event.preventDefault();
            submit();
          }}
          className="flex items-center gap-2"
        >
          <Input
            autoFocus
            type={property.type === "number" ? "number" : "text"}
            step={property.type === "number" ? "any" : undefined}
            inputMode={property.type === "number" ? "decimal" : undefined}
            value={draft}
            onChange={(event) => setDraft(event.target.value)}
            placeholder={placeholder}
            className="h-8"
          />
          {property.type === "url" && typeof value === "string" && (
            <Button
              type="button"
              variant="ghost"
              size="icon-sm"
              aria-label={t(($) => $.pickers.custom_property.open_link)}
              onClick={() => window.open(value, "_blank", "noopener,noreferrer")}
            >
              <ExternalLink className="size-3.5" />
            </Button>
          )}
        </form>
      </PopoverContent>
    </Popover>
  );
}

/**
 * Read view of a custom property value, shared by row triggers everywhere
 * (sidebar rows now; cards/filters later). Option ids resolve to named,
 * colored chips; unknown ids (option deleted from the definition) are
 * silently dropped rather than rendering raw UUIDs.
 */
export function CustomPropertyValueDisplay({
  property,
  value,
}: {
  property: IssueProperty;
  value: IssuePropertyValue | undefined;
}) {
  const { t } = useT("issues");
  if (value === undefined) {
    return (
      <span className="text-muted-foreground">
        {t(($) => $.pickers.custom_property.empty)}
      </span>
    );
  }
  const options = property.config.options ?? [];
  switch (property.type) {
    case "select": {
      const option = options.find((o) => o.id === value);
      if (!option) {
        return (
          <span className="text-muted-foreground">
            {t(($) => $.pickers.custom_property.empty)}
          </span>
        );
      }
      return (
        <span className="flex min-w-0 items-center gap-1.5">
          <span className="size-2.5 shrink-0 rounded-full" style={{ backgroundColor: option.color }} />
          <span className="truncate">{option.name}</span>
        </span>
      );
    }
    case "multi_select": {
      const ids = Array.isArray(value) ? value : [];
      const selected = options.filter((o) => ids.includes(o.id));
      if (selected.length === 0) {
        return (
          <span className="text-muted-foreground">
            {t(($) => $.pickers.custom_property.empty)}
          </span>
        );
      }
      return (
        <span className="flex min-w-0 flex-wrap items-center gap-1">
          {selected.map((option) => (
            <span
              key={option.id}
              className="inline-flex max-w-32 items-center gap-1 rounded-full border border-surface-border px-1.5 py-px text-[11px]"
            >
              <span className="size-2 shrink-0 rounded-full" style={{ backgroundColor: option.color }} />
              <span className="truncate">{option.name}</span>
            </span>
          ))}
        </span>
      );
    }
    case "date":
      return (
        <span className="flex items-center gap-1.5">
          <CalendarDays className="h-3.5 w-3.5 text-muted-foreground" />
          {typeof value === "string"
            ? formatDateOnly(value, { month: "short", day: "numeric" }, "en-US")
            : String(value)}
        </span>
      );
    case "checkbox":
      return (
        <span>
          {value === true
            ? t(($) => $.pickers.custom_property.true_label)
            : t(($) => $.pickers.custom_property.false_label)}
        </span>
      );
    case "url":
      return (
        <span className="flex min-w-0 items-center gap-1.5">
          <ExternalLink className="h-3 w-3 shrink-0 text-muted-foreground" />
          <span className="truncate">{String(value)}</span>
        </span>
      );
    default:
      return <span className="truncate tabular-nums">{String(value)}</span>;
  }
}
