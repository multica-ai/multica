"use client";

import { useState, type ReactNode } from "react";
import type { Issue, IssueProperty } from "@multica/core/types";
import { copyableParentProperties, copyParentIssueFields, type ParentCopyField, type ParentFieldCopy } from "@multica/core/issues";
import { Button } from "@multica/ui/components/ui/button";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import { Popover, PopoverContent, PopoverDescription, PopoverTitle, PopoverTrigger } from "@multica/ui/components/ui/popover";
import { CustomPropertyValueDisplay } from "../issues/components/pickers/custom-property-picker";
import { useT } from "../i18n";

export function CopyParentFields({ parent, properties, onCopy, disabled }: {
  parent: Issue;
  properties: IssueProperty[];
  onCopy: (copy: ParentFieldCopy) => void;
  disabled?: boolean;
}) {
  const { t } = useT("modals");
  const { t: tIssues } = useT("issues");
  const [open, setOpen] = useState(false);
  const [selected, setSelected] = useState<ParentCopyField[]>([]);
  const rows: { key: ParentCopyField; label: string; value: ReactNode }[] = [
    { key: "priority", label: tIssues(($) => $.detail.prop_priority), value: tIssues(($) => $.priority[parent.priority]) },
    { key: "labels", label: tIssues(($) => $.detail.prop_labels), value: (parent.labels ?? []).map((label) => label.name).join(", ") || t(($) => $.copy_parent_fields.no_labels) },
    ...(parent.start_date ? [{ key: "start_date" as const, label: tIssues(($) => $.detail.prop_start_date), value: parent.start_date }] : []),
    ...(parent.due_date ? [{ key: "due_date" as const, label: tIssues(($) => $.detail.prop_due_date), value: parent.due_date }] : []),
    ...copyableParentProperties(parent, properties).map((property) => ({
      key: `property:${property.id}` as const,
      label: property.name,
      value: <CustomPropertyValueDisplay property={property} value={parent.properties[property.id]} />,
    })),
  ];
  return (
    <Popover open={open} onOpenChange={(next) => { setOpen(next); if (next) setSelected([]); }}>
      <PopoverTrigger render={<Button type="button" variant="ghost" size="xs" disabled={disabled} />}>
        {t(($) => $.copy_parent_fields.trigger)}
      </PopoverTrigger>
      <PopoverContent align="end" className="w-80 max-w-[calc(100vw-2rem)] gap-3 p-3">
        <PopoverTitle>{t(($) => $.copy_parent_fields.title)}</PopoverTitle>
        <PopoverDescription>
          {t(($) => $.copy_parent_fields.description, { identifier: parent.identifier })}
        </PopoverDescription>
        <div className="max-h-64 overflow-y-auto">
          {rows.map((row) => (
            <label key={row.key} className="flex min-h-9 cursor-pointer items-center gap-2 rounded-sm px-1 py-1.5 hover:bg-accent">
              <Checkbox
                aria-label={row.label}
                checked={selected.includes(row.key)}
                onCheckedChange={(checked) => setSelected((current) => checked ? [...current, row.key] : current.filter((key) => key !== row.key))}
              />
              <span className="min-w-0 flex-1 break-words text-caption">{row.label}</span>
              <span className="min-w-0 max-w-40 break-words text-right text-caption text-muted-foreground">{row.value}</span>
            </label>
          ))}
        </div>
        <p className="text-micro text-muted-foreground">{t(($) => $.copy_parent_fields.manual_only)}</p>
        <div className="flex justify-end gap-2">
          <Button type="button" variant="ghost" size="sm" onClick={() => setOpen(false)}>{t(($) => $.common.cancel)}</Button>
          <Button type="button" size="sm" disabled={selected.length === 0 || disabled} onClick={() => {
            onCopy(copyParentIssueFields(parent, selected, properties));
            setOpen(false);
          }}>{t(($) => $.copy_parent_fields.apply, { count: selected.length })}</Button>
        </div>
      </PopoverContent>
    </Popover>
  );
}
