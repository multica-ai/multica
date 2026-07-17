"use client";

import {
  forwardRef,
  useImperativeHandle,
  useState,
} from "react";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  propertyListOptions,
  useSetIssueProperty,
} from "@multica/core/properties";
import type {
  IssueProperty,
  IssuePropertyValue,
} from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@multica/ui/components/ui/popover";
import { toast } from "sonner";

export interface CreateIssuePropertiesHandle {
  applyToIssue: (issueId: string) => Promise<void>;
  reset: () => void;
}

const KNOWN_TYPES = new Set([
  "text",
  "number",
  "select",
  "multi_select",
  "date",
  "checkbox",
  "url",
]);

export const CreateIssueProperties = forwardRef<CreateIssuePropertiesHandle>(
  function CreateIssueProperties(_props, ref) {
    const workspaceId = useWorkspaceId();
    const { data: catalog = [] } = useQuery(propertyListOptions(workspaceId));
    const setProperty = useSetIssueProperty();
    const [values, setValues] = useState<Record<string, IssuePropertyValue>>({});

    useImperativeHandle(
      ref,
      () => ({
        applyToIssue: async (issueId) => {
          let failures = 0;
          for (const [propertyId, value] of Object.entries(values)) {
            try {
              await setProperty.mutateAsync({ issueId, propertyId, value });
            } catch {
              failures += 1;
            }
          }
          if (failures > 0) {
            toast.error(
              failures === 1
                ? "Issue created, but one custom field could not be saved"
                : `Issue created, but ${failures} custom fields could not be saved`,
            );
          }
        },
        reset: () => setValues({}),
      }),
      [setProperty, values],
    );

    const visibleProperties = catalog.filter(
      (property) => !property.archived && KNOWN_TYPES.has(property.type),
    );
    if (visibleProperties.length === 0) return null;

    return (
      <>
        {visibleProperties.map((property) => (
          <CreateIssuePropertyField
            key={property.id}
            property={property}
            value={values[property.id]}
            onChange={(value) =>
              setValues((current) => {
                if (value === undefined) {
                  const next = { ...current };
                  delete next[property.id];
                  return next;
                }
                return { ...current, [property.id]: value };
              })
            }
          />
        ))}
      </>
    );
  },
);

function CreateIssuePropertyField({
  property,
  value,
  onChange,
}: {
  property: IssueProperty;
  value: IssuePropertyValue | undefined;
  onChange: (value: IssuePropertyValue | undefined) => void;
}) {
  const [open, setOpen] = useState(false);
  const [draft, setDraft] = useState("");
  const label = formatValue(property, value);

  const closeWith = (next: IssuePropertyValue | undefined) => {
    onChange(next);
    setOpen(false);
  };

  return (
    <Popover
      open={open}
      onOpenChange={(nextOpen) => {
        setOpen(nextOpen);
        if (nextOpen) setDraft(value === undefined ? "" : String(value));
      }}
    >
      <PopoverTrigger
        aria-label={property.name}
        className="inline-flex max-w-56 items-center gap-1.5 rounded-full border px-2.5 py-1 text-xs transition-colors hover:bg-accent/60"
      >
        <span className="truncate">{property.name}</span>
        {label && <span className="truncate text-muted-foreground">· {label}</span>}
      </PopoverTrigger>
      <PopoverContent align="start" className="w-64 p-2">
        {property.type === "select" && (
          <OptionList
            property={property}
            selected={typeof value === "string" ? [value] : []}
            onSelect={(optionId) => closeWith(optionId)}
          />
        )}
        {property.type === "multi_select" && (
          <OptionList
            property={property}
            selected={Array.isArray(value) ? value : []}
            onSelect={(optionId) => {
              const selected = Array.isArray(value) ? value : [];
              const next = selected.includes(optionId)
                ? selected.filter((id) => id !== optionId)
                : [...selected, optionId];
              onChange(next.length > 0 ? next : undefined);
            }}
          />
        )}
        {property.type === "checkbox" && (
          <div className="space-y-1">
            <ChoiceButton selected={value === true} onClick={() => closeWith(true)}>
              Yes
            </ChoiceButton>
            <ChoiceButton selected={value === false} onClick={() => closeWith(false)}>
              No
            </ChoiceButton>
          </div>
        )}
        {property.type === "date" && (
          <Input
            autoFocus
            type="date"
            value={typeof value === "string" ? value : ""}
            onChange={(event) => closeWith(event.target.value || undefined)}
          />
        )}
        {["text", "number", "url"].includes(property.type) && (
          <form
            onSubmit={(event) => {
              event.preventDefault();
              const trimmed = draft.trim();
              if (!trimmed) {
                closeWith(undefined);
                return;
              }
              if (property.type === "number") {
                const parsed = Number(trimmed);
                if (!Number.isNaN(parsed)) closeWith(parsed);
                return;
              }
              closeWith(trimmed);
            }}
          >
            <Input
              autoFocus
              type={property.type === "number" ? "number" : "text"}
              inputMode={property.type === "number" ? "decimal" : undefined}
              step={property.type === "number" ? "any" : undefined}
              value={draft}
              onChange={(event) => setDraft(event.target.value)}
              placeholder={property.type === "url" ? "https://…" : "Enter value…"}
            />
          </form>
        )}
        {value !== undefined && (
          <Button
            type="button"
            variant="ghost"
            size="xs"
            className="mt-2 w-full justify-start text-muted-foreground"
            onClick={() => closeWith(undefined)}
          >
            Clear
          </Button>
        )}
      </PopoverContent>
    </Popover>
  );
}

function OptionList({
  property,
  selected,
  onSelect,
}: {
  property: IssueProperty;
  selected: string[];
  onSelect: (optionId: string) => void;
}) {
  return (
    <div className="space-y-1">
      {(property.config.options ?? []).map((option) => (
        <ChoiceButton
          key={option.id}
          selected={selected.includes(option.id)}
          onClick={() => onSelect(option.id)}
        >
          <span
            className="size-2.5 shrink-0 rounded-full"
            style={{ backgroundColor: option.color }}
          />
          <span className="truncate">{option.name}</span>
        </ChoiceButton>
      ))}
    </div>
  );
}

function ChoiceButton({
  selected,
  onClick,
  children,
}: {
  selected: boolean;
  onClick: () => void;
  children: React.ReactNode;
}) {
  return (
    <button
      type="button"
      aria-pressed={selected}
      onClick={onClick}
      className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-sm hover:bg-accent"
    >
      {children}
    </button>
  );
}

function formatValue(
  property: IssueProperty,
  value: IssuePropertyValue | undefined,
): string {
  if (value === undefined) return "";
  const options = property.config.options ?? [];
  if (property.type === "select") {
    return options.find((option) => option.id === value)?.name ?? "";
  }
  if (property.type === "multi_select") {
    const ids = Array.isArray(value) ? value : [];
    return options
      .filter((option) => ids.includes(option.id))
      .map((option) => option.name)
      .join(", ");
  }
  if (property.type === "checkbox") return value === true ? "Yes" : "No";
  return String(value);
}
