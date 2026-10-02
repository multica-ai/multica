"use client";

import { useEffect, useState } from "react";
import { CalendarDays, Check, ExternalLink, X } from "lucide-react";
import { toast } from "sonner";
import type { Issue, IssueProperty, IssuePropertyValue } from "@multica/core/types";
import { hasUnknownActorRef } from "@multica/core/types";
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
import { useLocale, useT } from "../../../i18n";
import { PropertyPicker, PickerItem } from "./property-picker";
import { ActorPropertyPicker, ActorPropertyDisplay } from "./actor-property-picker";

const EDITABLE_PROPERTY_TYPES = [
  "select",
  "multi_select",
  "date",
  "checkbox",
  "text",
  "number",
  "url",
  "actor",
  "multi_actor",
  "multi_text",
  "multi_url",
];

/**
 * Whether the editor must degrade to read-only (Clear is still offered, so a
 * stale value can always be cleaned up). Three reasons:
 *
 *   1. The definition is archived.
 *   2. The definition's type is newer than this build.
 *   3. A single `actor` value references a kind this build cannot parse. It
 *      would otherwise render as empty and the user, believing the field is
 *      unset, would overwrite a value they were never shown. `multi_actor` is
 *      exempt: its toggle round-trips unknown entries instead of replacing the
 *      whole value (MUL-6286 review).
 */
export function isCustomPropertyReadOnly(
  property: IssueProperty,
  value: IssuePropertyValue | undefined,
): boolean {
  if (property.archived) return true;
  if (!EDITABLE_PROPERTY_TYPES.includes(property.type)) return true;
  if (property.type === "actor" && hasUnknownActorRef(value)) return true;
  return false;
}

/**
 * Value editor for one custom property on one issue. The editor shape
 * follows the definition type:
 *
 *   select        → PropertyPicker with one PickerItem per option
 *   multi_select  → PropertyPicker with toggling items (stays open)
 *   date          → Calendar popover (mirrors DueDatePicker)
 *   checkbox      → Yes / No picker
 *   actor         → member picker (commits and closes)
 *   multi_actor   → member picker with toggling items (stays open)
 *   text/number/url → popover with an input, Enter commits
 *   multi_text/multi_url → popover with removable rows + an appending input
 *
 * Archived definitions render read-only: the popover only offers Clear
 * (the server rejects new values on archived properties but always allows
 * unset). Unknown types from newer servers degrade to the read-only view.
 */
export function CustomPropertyValueEditor({
  issue,
  property,
  defaultOpen = false,
  open,
  onOpenChange,
}: {
  issue: Issue;
  property: IssueProperty;
  defaultOpen?: boolean;
  open?: boolean;
  onOpenChange?: (open: boolean) => void;
}) {
  const setProperty = useSetIssueProperty();
  const unsetProperty = useUnsetIssueProperty();
  const value = issue.properties[property.id];
  const onError = (error: unknown) =>
    toast.error(error instanceof Error ? error.message : String(error));

  return (
    <CustomPropertyValueInput
      property={property}
      value={value}
      defaultOpen={defaultOpen}
      open={open}
      onOpenChange={onOpenChange}
      onChange={(next) => {
        if (next === undefined) {
          unsetProperty.mutate(
            { issueId: issue.id, propertyId: property.id },
            { onError },
          );
          return;
        }
        setProperty.mutate(
          { issueId: issue.id, propertyId: property.id, value: next },
          { onError },
        );
      }}
    />
  );
}

/**
 * Mutation-free custom-property editor. Create flows use this while an issue
 * still exists only as a draft; issue detail wraps it above with the normal
 * optimistic mutations.
 */
export function CustomPropertyValueInput({
  property,
  value,
  onChange,
  defaultOpen = false,
  open: controlledOpen,
  onOpenChange: controlledOnOpenChange,
  trigger,
  triggerRender,
}: {
  property: IssueProperty;
  value: IssuePropertyValue | undefined;
  onChange: (value: IssuePropertyValue | undefined) => void;
  defaultOpen?: boolean;
  open?: boolean;
  onOpenChange?: (open: boolean) => void;
  trigger?: React.ReactNode;
  triggerRender?: React.ReactElement<Record<string, unknown>>;
}) {
  const { t } = useT("issues");
  const [internalOpen, setInternalOpen] = useState(defaultOpen);
  const open = controlledOpen ?? internalOpen;
  const setOpen = controlledOnOpenChange ?? setInternalOpen;
  const hasValue = value !== undefined;

  const commit = (next: IssuePropertyValue) => onChange(next);
  const clear = () => onChange(undefined);
  const valueTrigger = trigger ?? (
    <CustomPropertyValueDisplay property={property} value={value} />
  );

  const emptyLabel = (
    <span className="text-muted-foreground">
      {t(($) => $.pickers.custom_property.empty)}
    </span>
  );

  // Empty value as the first row, not a footer button — the position every
  // other picker uses for "no value", and being a real row it can carry the
  // checkmark when the property is unset.
  const emptyRow = (
    <PickerItem
      emptyValue
      selected={!hasValue}
      onClick={() => {
        clear();
        setOpen(false);
      }}
    >
      <span className="text-muted-foreground">{t(($) => $.pickers.custom_property.none)}</span>
    </PickerItem>
  );

  const readOnly = isCustomPropertyReadOnly(property, value);

  if (readOnly) {
    return (
      <PropertyPicker
        open={open}
        onOpenChange={setOpen}
        align="start"
        trigger={valueTrigger}
        triggerRender={triggerRender}
      >
        {emptyRow}
        <p className="px-2 py-1.5 text-caption text-muted-foreground">
          {property.archived
            ? t(($) => $.pickers.custom_property.archived_hint)
            : t(($) => $.pickers.custom_property.unknown_value_hint)}
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
          trigger={valueTrigger}
          triggerRender={triggerRender}
        >
          {emptyRow}
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
          trigger={valueTrigger}
          triggerRender={triggerRender}
        >
          {emptyRow}
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
    case "actor":
    case "multi_actor":
      return (
        <ActorPropertyPicker
          property={property}
          value={value}
          onChange={onChange}
          open={open}
          onOpenChange={setOpen}
          trigger={valueTrigger}
          triggerRender={triggerRender}
          emptyRow={emptyRow}
        />
      );
    case "multi_text":
    case "multi_url":
      return (
        <ListPropertyEditor
          property={property}
          value={value}
          open={open}
          onOpenChange={setOpen}
          onCommit={commit}
          onClear={clear}
          trigger={valueTrigger}
          triggerRender={triggerRender}
        />
      );
    case "date": {
      const date = typeof value === "string" ? dateOnlyToLocalDate(value) : undefined;
      return (
        <Popover open={open} onOpenChange={setOpen}>
          <PopoverTrigger
            className={triggerRender ? undefined : "flex items-center gap-1.5 cursor-pointer rounded-xs px-1 -mx-1 hover:bg-accent/30 transition-colors overflow-hidden"}
            render={triggerRender}
          >
            {valueTrigger}
          </PopoverTrigger>
          <PopoverContent className="w-auto p-0" align="start">
            {/* Empty value above the calendar — same position as DateOnlyPicker. */}
            <button
              type="button"
              onClick={() => {
                clear();
                setOpen(false);
              }}
              className="flex w-full items-center gap-3 border-b px-3 py-2 text-left text-body transition-colors hover:bg-accent"
            >
              <span className="flex min-w-0 flex-1 items-center gap-2 text-muted-foreground">
                {t(($) => $.pickers.custom_property.none)}
              </span>
              <Check className={`h-3.5 w-3.5 shrink-0 text-muted-foreground ${date ? "invisible" : ""}`} />
            </button>
            <Calendar
              mode="single"
              selected={date}
              onSelect={(d: Date | undefined) => {
                if (d) commit(toDateOnly(d));
                else clear();
                setOpen(false);
              }}
            />
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
          trigger={valueTrigger}
          triggerRender={triggerRender}
        >
          {emptyRow}
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
          trigger={valueTrigger}
          triggerRender={triggerRender}
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
  trigger,
  triggerRender,
}: {
  property: IssueProperty;
  value: IssuePropertyValue | undefined;
  open: boolean;
  onOpenChange: (v: boolean) => void;
  onCommit: (next: IssuePropertyValue) => void;
  onClear: () => void;
  emptyLabel: React.ReactNode;
  trigger?: React.ReactNode;
  triggerRender?: React.ReactElement<Record<string, unknown>>;
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
      <PopoverTrigger
        className={triggerRender ? undefined : "flex items-center gap-1.5 cursor-pointer rounded-xs px-1 -mx-1 hover:bg-accent/30 transition-colors overflow-hidden"}
        render={triggerRender}
      >
        {trigger ?? (value === undefined ? (
          emptyLabel
        ) : (
          <CustomPropertyValueDisplay property={property} value={value} />
        ))}
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
 * List editor for multi_text / multi_url: current entries as removable rows
 * above an input that appends on Enter. The popover stays open across
 * additions (multi-select interaction); each add/remove commits the whole
 * array, and removing the last entry clears the property.
 */
function ListPropertyEditor({
  property,
  value,
  open,
  onOpenChange,
  onCommit,
  onClear,
  trigger,
  triggerRender,
}: {
  property: IssueProperty;
  value: IssuePropertyValue | undefined;
  open: boolean;
  onOpenChange: (v: boolean) => void;
  onCommit: (next: IssuePropertyValue) => void;
  onClear: () => void;
  trigger?: React.ReactNode;
  triggerRender?: React.ReactElement<Record<string, unknown>>;
}) {
  const { t } = useT("issues");
  const [draft, setDraft] = useState("");
  useEffect(() => {
    if (open) setDraft("");
  }, [open]);

  const items = Array.isArray(value) ? value : [];
  const placeholder =
    property.type === "multi_url"
      ? t(($) => $.pickers.custom_property.url_placeholder)
      : t(($) => $.pickers.custom_property.value_placeholder);

  const add = () => {
    const trimmed = draft.trim();
    if (!trimmed) return;
    setDraft("");
    if (items.includes(trimmed)) return;
    onCommit([...items, trimmed]);
  };
  const remove = (item: string) => {
    const next = items.filter((entry) => entry !== item);
    if (next.length === 0) onClear();
    else onCommit(next);
  };

  return (
    <Popover open={open} onOpenChange={onOpenChange}>
      <PopoverTrigger
        className={triggerRender ? undefined : "flex items-center gap-1.5 cursor-pointer rounded-xs px-1 -mx-1 hover:bg-accent/30 transition-colors overflow-hidden"}
        render={triggerRender}
      >
        {trigger}
      </PopoverTrigger>
      <PopoverContent className="w-64 p-2" align="start">
        {items.length > 0 && (
          <ul className="mb-2 max-h-48 overflow-y-auto">
            {items.map((item) => (
              <li key={item} className="flex min-w-0 items-center gap-1 rounded-sm px-1 py-0.5 hover:bg-accent/40">
                <span className="min-w-0 flex-1 truncate text-body">{item}</span>
                {property.type === "multi_url" && (
                  <Button
                    type="button"
                    variant="ghost"
                    size="icon-sm"
                    aria-label={t(($) => $.pickers.custom_property.open_link)}
                    onClick={() => window.open(item, "_blank", "noopener,noreferrer")}
                  >
                    <ExternalLink className="size-3.5" />
                  </Button>
                )}
                <Button
                  type="button"
                  variant="ghost"
                  size="icon-sm"
                  aria-label={t(($) => $.pickers.custom_property.remove_item, { value: item })}
                  onClick={() => remove(item)}
                >
                  <X className="size-3.5" />
                </Button>
              </li>
            ))}
          </ul>
        )}
        <form
          onSubmit={(event) => {
            event.preventDefault();
            add();
          }}
          className="flex items-center gap-2"
        >
          <Input
            autoFocus
            value={draft}
            onChange={(event) => setDraft(event.target.value)}
            placeholder={placeholder}
            className="h-8"
          />
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
  const locale = useLocale();
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
              className="inline-flex max-w-32 items-center gap-1 rounded-full border border-surface-border px-1.5 py-px text-micro"
            >
              <span className="size-2 shrink-0 rounded-full" style={{ backgroundColor: option.color }} />
              <span className="truncate">{option.name}</span>
            </span>
          ))}
        </span>
      );
    }
    case "actor":
    case "multi_actor":
      return (
        <ActorPropertyDisplay
          value={value}
          emptyLabel={
            <span className="text-muted-foreground">
              {t(($) => $.pickers.custom_property.empty)}
            </span>
          }
        />
      );
    case "multi_text":
    case "multi_url": {
      const items = Array.isArray(value) ? value : [];
      if (items.length === 0) {
        return (
          <span className="text-muted-foreground">
            {t(($) => $.pickers.custom_property.empty)}
          </span>
        );
      }
      return (
        <span className="flex min-w-0 flex-wrap items-center gap-1">
          {items.map((item) =>
            property.type === "multi_url" ? (
              // A real <a> would nest inside the popover trigger button
              // (invalid HTML), so the chip opens the link itself and stops
              // the click from also toggling the editor.
              <span
                key={item}
                role="link"
                tabIndex={0}
                title={item}
                onClick={(event) => {
                  event.stopPropagation();
                  window.open(item, "_blank", "noopener,noreferrer");
                }}
                onKeyDown={(event) => {
                  if (event.key === "Enter") {
                    event.stopPropagation();
                    window.open(item, "_blank", "noopener,noreferrer");
                  }
                }}
                className="inline-flex max-w-48 cursor-pointer items-center gap-1 rounded-full border border-surface-border px-1.5 py-px text-micro hover:bg-accent/50"
              >
                <ExternalLink className="size-2.5 shrink-0 text-muted-foreground" />
                <span className="truncate">{item}</span>
              </span>
            ) : (
              <span
                key={item}
                className="inline-flex max-w-48 items-center gap-1 rounded-full border border-surface-border px-1.5 py-px text-micro"
              >
                <span className="truncate">{item}</span>
              </span>
            ),
          )}
        </span>
      );
    }
    case "date":
      return (
        <span className="flex items-center gap-1.5">
          <CalendarDays className="h-3.5 w-3.5 text-muted-foreground" />
          {typeof value === "string"
            ? formatDateOnly(value, { month: "short", day: "numeric" }, locale)
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
