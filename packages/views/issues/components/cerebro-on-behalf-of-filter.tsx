// CEREBRO-PATCH(issue-on-behalf-of-filter): MUL-2553 — cerebro-only "På vegne af"
// issues filter submenu, extracted to a cerebro-prefixed sibling so the upstream
// issues-header only needs a tiny marked import + render. Filters issues by the
// human their originating agent task acted for (origin_type='agent_task').
"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Check, UserRound } from "lucide-react";
import { useWorkspaceId } from "@multica/core/hooks";
import { memberListOptions } from "@multica/core/workspace/queries";
import {
  DropdownMenuSub,
  DropdownMenuSubTrigger,
  DropdownMenuSubContent,
  DropdownMenuCheckboxItem,
} from "@multica/ui/components/ui/dropdown-menu";
import { ActorAvatar } from "../../common/actor-avatar";

const ITEM_CLASS =
  "group/obo pr-1.5! [&>[data-slot=dropdown-menu-checkbox-item-indicator]]:hidden";

function Indicator({ checked }: { checked: boolean }) {
  return (
    <div
      className="border-input data-[selected=true]:border-primary data-[selected=true]:bg-primary data-[selected=true]:text-primary-foreground pointer-events-none size-4 shrink-0 rounded-[4px] border transition-all select-none *:[svg]:opacity-0 data-[selected=true]:*:[svg]:opacity-100 opacity-0 group-hover/obo:opacity-100 group-focus/obo:opacity-100 data-[selected=true]:opacity-100"
      data-selected={checked}
    >
      <Check className="size-3.5 text-current" />
    </div>
  );
}

/**
 * "Startet på vegne af" filter submenu. `selected` holds workspace user UUIDs
 * (matches the backend `on_behalf_of_ids` param); `onToggle` flips one user.
 */
export function OnBehalfOfFilterSub({
  selected = [],
  onToggle,
}: {
  selected: string[];
  onToggle: (userId: string) => void;
}) {
  const [search, setSearch] = useState("");
  const wsId = useWorkspaceId();
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const query = search.trim().toLowerCase();
  const filtered = members.filter((m) => m.name.toLowerCase().includes(query));

  return (
    <DropdownMenuSub>
      <DropdownMenuSubTrigger>
        <UserRound className="size-3.5" />
        <span className="flex-1">På vegne af</span>
        {selected.length > 0 && (
          <span className="text-xs text-primary font-medium">{selected.length}</span>
        )}
      </DropdownMenuSubTrigger>
      <DropdownMenuSubContent className="w-auto min-w-52 p-0">
        <div className="px-2 py-1.5 border-b border-foreground/5">
          <input
            type="text"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder="Søg medlem…"
            className="w-full bg-transparent text-base md:text-sm placeholder:text-muted-foreground outline-none"
            autoFocus
          />
        </div>
        <div className="max-h-64 overflow-y-auto p-1">
          {filtered.length === 0 ? (
            <div className="px-2 py-3 text-center text-xs text-muted-foreground">
              Ingen medlemmer.
            </div>
          ) : (
            filtered.map((m) => {
              const checked = selected.includes(m.user_id);
              return (
                <DropdownMenuCheckboxItem
                  key={m.user_id}
                  checked={checked}
                  onCheckedChange={() => onToggle(m.user_id)}
                  className={ITEM_CLASS}
                >
                  <Indicator checked={checked} />
                  <ActorAvatar actorType="member" actorId={m.user_id} size={18} />
                  <span className="truncate">{m.name}</span>
                </DropdownMenuCheckboxItem>
              );
            })
          )}
        </div>
      </DropdownMenuSubContent>
    </DropdownMenuSub>
  );
}
