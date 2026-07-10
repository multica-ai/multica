"use client";

// FIR-2283 — human-friendly skill picker for the Issue workflow form.
// Jesper's review ("cardinal sin #1: no human knows a UUID by heart", and
// v2 point 1: "selectors skal være search and select") required a searchable
// picker, not a raw dropdown. Agent and member selection reuse the shared
// AgentPicker / AssigneePicker directly (imported in the form); this file
// adds the one selector the app did not already expose standalone: a
// single-skill picker keyed by the skill NAME (the loop spec keys skills by
// name, not id). It is built on the SAME PropertyPicker shell the app's
// AgentPicker / AssigneePicker use, so it looks and searches identically.

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ChevronsUpDown, Sparkles } from "lucide-react";
import { useWorkspaceId } from "@multica/core/hooks";
import { skillListOptions } from "@multica/core/workspace/queries";
import {
  PropertyPicker,
  PickerItem,
  PickerEmpty,
} from "@multica/views/issues/components";

export function SkillNamePicker({
  value,
  onChange,
  placeholder = "Select a skill",
}: {
  value: string;
  onChange: (name: string) => void;
  placeholder?: string;
}) {
  const wsId = useWorkspaceId();
  const [open, setOpen] = useState(false);
  const [filter, setFilter] = useState("");
  const { data: skills = [] } = useQuery(skillListOptions(wsId));
  const known = skills.some((s) => s.name === value);

  const query = filter.trim().toLowerCase();
  const filtered = query
    ? skills.filter((s) => s.name.toLowerCase().includes(query))
    : skills;

  return (
    <PropertyPicker
      open={open}
      onOpenChange={setOpen}
      width="w-64"
      align="start"
      searchable
      searchPlaceholder="Search skills…"
      onSearchChange={setFilter}
      triggerRender={
        // A bordered, input-looking trigger so the picker reads as a form
        // field (matching the Input/Textarea siblings), not a bare link.
        <button
          type="button"
          className="flex h-9 w-full items-center justify-between gap-2 rounded-md border border-input bg-transparent px-3 py-1 text-sm shadow-sm hover:bg-accent/30"
        />
      }
      trigger={
        <>
          <span className="flex min-w-0 items-center gap-2">
            <Sparkles className="size-3.5 shrink-0 text-muted-foreground" />
            <span className={`truncate ${value ? "" : "text-muted-foreground"}`}>
              {value || placeholder}
            </span>
          </span>
          <ChevronsUpDown className="size-3.5 shrink-0 text-muted-foreground" />
        </>
      }
    >
      {value && (
        <PickerItem
          selected={false}
          onClick={() => {
            onChange("");
            setOpen(false);
          }}
        >
          <span className="text-muted-foreground">Clear selection</span>
        </PickerItem>
      )}
      {/* Preserve a previously-saved skill whose name is no longer in the
          workspace list, so editing an old recipe never silently drops it. */}
      {value && !known && (
        <PickerItem selected onClick={() => setOpen(false)}>
          <Sparkles className="size-3.5 shrink-0 text-muted-foreground" />
          <span className="truncate">{value}</span>
        </PickerItem>
      )}
      {filtered.length === 0 ? (
        <PickerEmpty />
      ) : (
        filtered.map((s) => (
          <PickerItem
            key={s.id}
            selected={s.name === value}
            onClick={() => {
              onChange(s.name);
              setOpen(false);
            }}
          >
            <Sparkles className="size-3.5 shrink-0 text-muted-foreground" />
            <span className="truncate">{s.name}</span>
          </PickerItem>
        ))
      )}
    </PropertyPicker>
  );
}
