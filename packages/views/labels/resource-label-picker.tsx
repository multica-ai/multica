"use client";

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Check, Search, Tag } from "lucide-react";
import { useWorkspaceId } from "@multica/core/hooks";
import { useFeatureEnabled } from "@multica/core/config";
import { RESOURCE_LABELS_FLAG } from "@multica/core/feature-flags";
import {
  labelListOptions,
  resourceLabelsOptions,
  useAttachResourceLabel,
  useDetachResourceLabel,
} from "@multica/core/labels";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@multica/ui/components/ui/popover";
import { useT } from "../i18n";
import { LabelChip } from "./label-chip";

export function ResourceLabelPicker({
  resourceType,
  resourceId,
  canEdit,
}: {
  resourceType: "agent" | "skill";
  resourceId: string;
  canEdit: boolean;
}) {
  const { t } = useT("labels");
  const wsId = useWorkspaceId();
  const resourceLabelsEnabled = useFeatureEnabled(RESOURCE_LABELS_FLAG, false);
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const { data: catalog = [] } = useQuery({
    ...labelListOptions(wsId, resourceType),
    enabled: resourceLabelsEnabled,
  });
  const { data: selected = [] } = useQuery(
    {
      ...resourceLabelsOptions(wsId, resourceType, resourceId),
      enabled: resourceLabelsEnabled,
    },
  );
  const attach = useAttachResourceLabel(resourceType, resourceId);
  const detach = useDetachResourceLabel(resourceType, resourceId);
  const selectedIds = useMemo(() => new Set(selected.map((label) => label.id)), [selected]);
  const filtered = catalog.filter((label) =>
    label.name.toLowerCase().includes(query.trim().toLowerCase()),
  );

  if (!resourceLabelsEnabled) return null;

  const content = selected.length > 0 ? (
    <div className="flex flex-wrap justify-start gap-1 sm:justify-end">
      {selected.map((label) => (
        <LabelChip key={label.id} label={label} />
      ))}
    </div>
  ) : (
    <span className="text-sm text-muted-foreground">{t(($) => $.resource_picker.empty)}</span>
  );

  if (!canEdit) return content;

  return (
    <Popover
      open={open}
      onOpenChange={(next) => {
        setOpen(next);
        if (!next) setQuery("");
      }}
    >
      <PopoverTrigger
        render={
          <Button
            type="button"
            variant="outline"
            className="h-auto min-h-9 w-full justify-start px-2.5 py-1.5 sm:justify-end"
          >
            {selected.length > 0 ? content : (
              <>
                <Tag className="size-3.5 text-muted-foreground" />
                {t(($) => $.resource_picker.add)}
              </>
            )}
          </Button>
        }
      />
      <PopoverContent align="end" className="w-72 p-2">
        <div className="relative mb-2">
          <Search className="pointer-events-none absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
          <Input
            autoFocus
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            placeholder={t(($) => $.resource_picker.search)}
            className="h-8 pl-8 text-sm"
          />
        </div>
        <div className="max-h-64 space-y-0.5 overflow-y-auto">
          {filtered.map((label) => {
            const isSelected = selectedIds.has(label.id);
            return (
              <button
                key={label.id}
                type="button"
                onClick={() =>
                  isSelected ? detach.mutate(label.id) : attach.mutate(label.id)
                }
                className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-sm hover:bg-accent"
              >
                <span
                  className="size-2.5 shrink-0 rounded-full"
                  style={{ backgroundColor: label.color }}
                />
                <span className="min-w-0 flex-1 truncate">{label.name}</span>
                {isSelected ? <Check className="size-3.5 text-primary" /> : null}
              </button>
            );
          })}
          {filtered.length === 0 ? (
            <p className="px-2 py-6 text-center text-xs text-muted-foreground">
              {catalog.length === 0
                ? t(($) => $.resource_picker.no_labels)
                : t(($) => $.resource_picker.no_results)}
            </p>
          ) : null}
        </div>
      </PopoverContent>
    </Popover>
  );
}
