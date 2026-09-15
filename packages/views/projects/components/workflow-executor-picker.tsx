"use client";

import { useState } from "react";
import { ChevronDown } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { ActorAvatar } from "../../common/actor-avatar";
import { matchesPinyin } from "../../editor/extensions/pinyin-match";
import {
  PropertyPicker,
  PickerItem,
  PickerEmpty,
} from "../../issues/components/pickers/property-picker";
import { useT } from "../../i18n";

export function WorkflowExecutorPicker({
  id,
  type,
  value,
  choices,
  disabled,
  invalid,
  loading,
  loadError,
  onRetry,
  onChange,
}: {
  id: string;
  type: "agent" | "squad";
  value: string;
  choices: { value: string; label: string }[];
  disabled: boolean;
  invalid: boolean;
  loading: boolean;
  loadError: boolean;
  onRetry: () => void;
  onChange: (id: string) => void;
}) {
  const { t } = useT("projects");
  const [open, setOpen] = useState(false);
  const [search, setSearch] = useState("");
  const selected = choices.find((choice) => choice.value === value);
  const query = search.trim().toLowerCase();
  const filtered = choices.filter((choice) =>
    choice.label.toLowerCase().includes(query) || matchesPinyin(choice.label, query),
  );

  return (
    <PropertyPicker
      open={open && !disabled}
      onOpenChange={setOpen}
      align="start"
      width="w-[var(--anchor-width)]"
      searchable
      searchPlaceholder={t(($) => type === "agent" ? $.workflow.search_agents : $.workflow.search_squads)}
      onSearchChange={setSearch}
      triggerRender={
        <Button
          id={id}
          variant="outline"
          disabled={disabled}
          aria-invalid={invalid || undefined}
          aria-describedby={invalid ? `${id}-error` : undefined}
          className="w-full justify-start font-normal"
        />
      }
      trigger={
        <>
          {selected && (
            <span aria-hidden="true" className="shrink-0"><ActorAvatar actorType={type} actorId={selected.value} size="sm" profileLink={false} /></span>
          )}
          <span className={selected ? "min-w-0 flex-1 truncate text-left" : "min-w-0 flex-1 truncate text-left text-muted-foreground"}>
            {selected?.label ?? (value
              ? t(($) => $.workflow.unavailable)
              : t(($) => type === "agent" ? $.workflow.choose_agent : $.workflow.choose_squad))}
          </span>
          <ChevronDown className="size-4 shrink-0 text-muted-foreground" />
        </>
      }
    >
      {loading ? (
        <p role="status" className="p-3 text-caption text-muted-foreground">{t(($) => $.workflow_rules.loading)}</p>
      ) : loadError ? (
        <div className="space-y-2 p-3">
          <p role="alert" className="text-caption text-destructive">{t(($) => $.workflow.executors_load_error)}</p>
          <Button size="sm" variant="outline" onClick={onRetry}>{t(($) => $.workflow.retry)}</Button>
        </div>
      ) : choices.length === 0 ? (
        <p role="status" className="p-3 text-caption text-muted-foreground">
          {t(($) => type === "agent" ? $.workflow.no_agents : $.workflow.no_squads)}
        </p>
      ) : filtered.length === 0 ? <PickerEmpty /> : filtered.map((choice) => (
        <PickerItem
          key={choice.value}
          selected={choice.value === value}
          onClick={() => {
            onChange(choice.value);
            setOpen(false);
          }}
        >
          <span aria-hidden="true" className="shrink-0"><ActorAvatar actorType={type} actorId={choice.value} size="sm" profileLink={false} /></span>
          <span className="min-w-0 truncate">{choice.label}</span>
        </PickerItem>
      ))}
    </PropertyPicker>
  );
}
