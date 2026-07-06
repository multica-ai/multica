"use client";

import { useMemo, useState } from "react";
import { Plus, X } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { memberListOptions } from "@multica/core/workspace/queries";
import { cn } from "@multica/ui/lib/utils";
import { ActorAvatar } from "../../common/actor-avatar";
import {
  PropertyPicker,
  PickerItem,
  PickerEmpty,
} from "../../issues/components/pickers/property-picker";
import { matchesPinyin } from "../../editor/extensions/pinyin-match";
import { useT } from "../../i18n";

// Fully controlled — parent owns the selection state and ships it to the
// create/update mutation. Members-only on purpose (per RFC, MUL-2533).
export function SubscriberMultiSelect({
  selectedIds,
  onChange,
}: {
  /** User IDs of the currently-selected member subscribers. */
  selectedIds: ReadonlyArray<string>;
  /** Called with the new full list whenever the selection changes. */
  onChange: (next: string[]) => void;
}) {
  const { t } = useT("autopilots");
  const wsId = useWorkspaceId();
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const [open, setOpen] = useState(false);
  const [filter, setFilter] = useState("");

  const selectedSet = useMemo(() => new Set(selectedIds), [selectedIds]);

  const query = filter.trim().toLowerCase();
  const filteredMembers = useMemo(
    () =>
      members.filter(
        (m) =>
          query === "" ||
          m.name.toLowerCase().includes(query) ||
          matchesPinyin(m.name, query),
      ),
    [members, query],
  );

  const selectedMembers = useMemo(
    () => members.filter((m) => selectedSet.has(m.user_id)),
    [members, selectedSet],
  );

  const toggle = (userId: string) => {
    if (selectedSet.has(userId)) {
      onChange(selectedIds.filter((id) => id !== userId));
    } else {
      onChange([...selectedIds, userId]);
    }
  };

  const remove = (userId: string) => {
    onChange(selectedIds.filter((id) => id !== userId));
  };

  return (
    <div className="flex flex-wrap items-center gap-1.5">
      {selectedMembers.map((m) => (
        <span
          key={m.user_id}
          className="inline-flex items-center gap-1 rounded-full border bg-background px-2 py-0.5 text-xs"
        >
          <ActorAvatar actorType="member" actorId={m.user_id} size={14} />
          <span className="max-w-[10rem] truncate">{m.name}</span>
          <button
            type="button"
            onClick={() => remove(m.user_id)}
            className="text-muted-foreground hover:text-foreground transition-colors cursor-pointer"
            aria-label={t(($) => $.dialog.subscribers_remove_tooltip)}
          >
            <X className="size-3" />
          </button>
        </span>
      ))}
      <PropertyPicker
        open={open}
        onOpenChange={(v) => {
          setOpen(v);
          if (!v) setFilter("");
        }}
        width="w-64"
        align="start"
        searchable
        searchPlaceholder={t(($) => $.dialog.subscribers_search_placeholder)}
        onSearchChange={setFilter}
        trigger={
          <span
            className={cn(
              "inline-flex items-center gap-1 rounded-full border border-dashed px-2 py-0.5 text-xs text-muted-foreground",
              "hover:border-primary/40 hover:text-foreground transition-colors cursor-pointer",
            )}
          >
            <Plus className="size-3" />
            {t(($) => $.dialog.subscribers_add)}
          </span>
        }
      >
        {filteredMembers.length === 0 ? (
          <PickerEmpty />
        ) : (
          filteredMembers.map((m) => (
            <PickerItem
              key={m.user_id}
              selected={selectedSet.has(m.user_id)}
              onClick={() => toggle(m.user_id)}
            >
              <ActorAvatar actorType="member" actorId={m.user_id} size={18} />
              <span className="truncate">{m.name}</span>
            </PickerItem>
          ))
        )}
      </PropertyPicker>
    </div>
  );
}
