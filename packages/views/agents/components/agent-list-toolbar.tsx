"use client";

import {
  ALL_ACCESS_SCOPES,
  effectiveAccessScope,
  type AgentAvailability,
} from "@multica/core/agents";
import type { MemberWithUser } from "@multica/core/types";
import { runtimeDisplayLabel } from "@multica/core/runtimes";
import { resolvePublicFileUrl } from "@multica/core/workspace/avatar-url";
import type { AgentListFilters } from "@multica/core/agents/stores";
import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuSub,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { ActorAvatar } from "@multica/ui/components/common/actor-avatar";
import { FILTER_ITEM_CLASS, HoverCheck } from "../../common/hover-check";
import { CollectionFilterButton } from "../../layout/collection-page";
import { availabilityConfig } from "../presence";
import { useT } from "../../i18n";
import type { AgentListRow } from "./agents-page";

const AVAILABILITY_VALUES: AgentAvailability[] = [
  "online",
  "unstable",
  "offline",
];

export function countActiveFilterDimensions(
  filters: AgentListFilters,
): number {
  let count = 0;
  if (filters.availability.length > 0) count++;
  if (filters.runtimes.length > 0) count++;
  if (filters.owners.length > 0) count++;
  if (filters.models.length > 0) count++;
  if (filters.access.length > 0) count++;
  return count;
}

const ACCESS_SCOPES = ALL_ACCESS_SCOPES;

function CountBadge({ value }: { value: number }) {
  return (
    <span className="ml-auto pl-3 text-caption text-muted-foreground">{value}</span>
  );
}

function SubTriggerCount({ value }: { value: number }) {
  if (value === 0) return null;
  return <span className="text-caption font-medium text-primary">{value}</span>;
}

/** The header's filter icon and the per-dimension filter menu it opens. */
export function AgentListFilterMenu({
  filters,
  onToggleFilter,
  allRows,
  members,
}: {
  filters: AgentListFilters;
  onToggleFilter: (key: keyof AgentListFilters, value: string) => void;
  /** Rows within the current scope, unfiltered — filter option lists and
   *  counts derive from this set. */
  allRows: AgentListRow[];
  members: MemberWithUser[];
}) {
  const { t } = useT("agents");

  // Option lists with counts, derived from the scope's unfiltered rows so
  // toggling one dimension doesn't make the others' options vanish.
  const availabilityCounts = new Map<string, number>();
  const runtimeOptions = new Map<string, { name: string; count: number }>();
  const accessCounts = new Map<string, number>();
  for (const row of allRows) {
    if (row.presence) {
      availabilityCounts.set(
        row.presence.availability,
        (availabilityCounts.get(row.presence.availability) ?? 0) + 1,
      );
    }
    const rt = row.runtime;
    if (rt) {
      const entry = runtimeOptions.get(rt.id);
      if (entry) entry.count += 1;
      else runtimeOptions.set(rt.id, { name: runtimeDisplayLabel(rt), count: 1 });
    }
    const a = effectiveAccessScope(row.agent.permission_mode, row.agent.invocation_targets);
    accessCounts.set(a, (accessCounts.get(a) ?? 0) + 1);
  }

  // Owner options: members who own at least one agent in the current scope.
  const memberById = new Map(members.map((m) => [m.user_id, m]));
  const ownerCounts = new Map<string, number>();
  const modelCounts = new Map<string, number>();
  for (const row of allRows) {
    const oid = row.agent.owner_id;
    if (oid) ownerCounts.set(oid, (ownerCounts.get(oid) ?? 0) + 1);
    const model = row.agent.model;
    if (model) modelCounts.set(model, (modelCounts.get(model) ?? 0) + 1);
  }

  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <CollectionFilterButton count={countActiveFilterDimensions(filters)} />
        }
      />
      <DropdownMenuContent align="start" className="w-auto">
        <DropdownMenuSub>
          <DropdownMenuSubTrigger>
            <span className="flex-1">
              {t(($) => $.toolbar.section_availability)}
            </span>
            <SubTriggerCount value={filters.availability.length} />
          </DropdownMenuSubTrigger>
          <DropdownMenuSubContent className="w-auto min-w-44">
            {AVAILABILITY_VALUES.map((value) => {
              const visual = availabilityConfig[value];
              return (
                <DropdownMenuCheckboxItem
                  key={value}
                  checked={filters.availability.includes(value)}
                  onCheckedChange={() => onToggleFilter("availability", value)}
                  className={FILTER_ITEM_CLASS}
                >
                  <HoverCheck checked={filters.availability.includes(value)} />
                  <span
                    className={`size-1.5 shrink-0 rounded-full ${visual.dotClass}`}
                  />
                  {t(($) => $.availability[value])}
                  <CountBadge value={availabilityCounts.get(value) ?? 0} />
                </DropdownMenuCheckboxItem>
              );
            })}
          </DropdownMenuSubContent>
        </DropdownMenuSub>

        {/* Access — effective access scope (workspace / specific-people / owner-only).
            Mirrors Availability's keyboard/ARIA pattern. */}
        <DropdownMenuSub>
          <DropdownMenuSubTrigger>
            <span className="flex-1">
              {t(($) => $.toolbar.section_access)}
            </span>
            <SubTriggerCount value={filters.access.length} />
          </DropdownMenuSubTrigger>
          <DropdownMenuSubContent className="w-auto min-w-44">
            {ACCESS_SCOPES.map((value) => (
              <DropdownMenuCheckboxItem
                key={value}
                checked={filters.access.includes(value)}
                onCheckedChange={() => onToggleFilter("access", value)}
                className={FILTER_ITEM_CLASS}
              >
                <HoverCheck checked={filters.access.includes(value)} />
                <span className="min-w-0 truncate">
                  {t(($) =>
                    value === "workspace"
                      ? $.access.scope_labels.workspace
                      : value === "specific-people"
                        ? $.access.scope_labels.specific_people
                        : $.access.scope_labels.owner_only,
                  )}
                </span>
                <CountBadge value={accessCounts.get(value) ?? 0} />
              </DropdownMenuCheckboxItem>
            ))}
          </DropdownMenuSubContent>
        </DropdownMenuSub>

        <DropdownMenuSub>
          <DropdownMenuSubTrigger>
            <span className="flex-1">
              {t(($) => $.toolbar.section_runtime)}
            </span>
            <SubTriggerCount value={filters.runtimes.length} />
          </DropdownMenuSubTrigger>
          <DropdownMenuSubContent className="max-h-72 w-auto min-w-48 overflow-y-auto">
            {[...runtimeOptions.entries()].map(([id, { name, count }]) => (
              <DropdownMenuCheckboxItem
                key={id}
                checked={filters.runtimes.includes(id)}
                onCheckedChange={() => onToggleFilter("runtimes", id)}
                className={FILTER_ITEM_CLASS}
              >
                <HoverCheck checked={filters.runtimes.includes(id)} />
                <span className="min-w-0 truncate">{name}</span>
                <CountBadge value={count} />
              </DropdownMenuCheckboxItem>
            ))}
          </DropdownMenuSubContent>
        </DropdownMenuSub>

        {/* Owner — the same person-axis as the Mine scope. Picking an
            owner here leaves the clean "mine" view for "all" (store
            rule), so Mine + owner never coexist. */}
        <DropdownMenuSub>
          <DropdownMenuSubTrigger>
            <span className="flex-1">
              {t(($) => $.toolbar.section_owner)}
            </span>
            <SubTriggerCount value={filters.owners.length} />
          </DropdownMenuSubTrigger>
          <DropdownMenuSubContent className="max-h-72 w-auto min-w-48 overflow-y-auto">
            {[...ownerCounts.entries()].map(([userId, count]) => {
              const m = memberById.get(userId);
              return (
                <DropdownMenuCheckboxItem
                  key={userId}
                  checked={filters.owners.includes(userId)}
                  onCheckedChange={() => onToggleFilter("owners", userId)}
                  className={FILTER_ITEM_CLASS}
                >
                  <HoverCheck checked={filters.owners.includes(userId)} />
                  <ActorAvatar
                    name={m?.name ?? userId.slice(0, 8)}
                    initials={(m?.name ?? "?").slice(0, 2).toUpperCase()}
                    avatarUrl={resolvePublicFileUrl(m?.avatar_url ?? null)}
                    size="sm"
                  />
                  <span className="min-w-0 truncate">
                    {m?.name ?? userId.slice(0, 8)}
                  </span>
                  <CountBadge value={count} />
                </DropdownMenuCheckboxItem>
              );
            })}
          </DropdownMenuSubContent>
        </DropdownMenuSub>

        {/* Model — runtime-native model id (categorical column → filter) */}
        {modelCounts.size > 0 && (
          <DropdownMenuSub>
            <DropdownMenuSubTrigger>
              <span className="flex-1">
                {t(($) => $.toolbar.section_model)}
              </span>
              <SubTriggerCount value={filters.models.length} />
            </DropdownMenuSubTrigger>
            <DropdownMenuSubContent className="max-h-72 w-auto min-w-44 overflow-y-auto">
              {[...modelCounts.entries()].map(([model, count]) => (
                <DropdownMenuCheckboxItem
                  key={model}
                  checked={filters.models.includes(model)}
                  onCheckedChange={() => onToggleFilter("models", model)}
                  className={FILTER_ITEM_CLASS}
                >
                  <HoverCheck checked={filters.models.includes(model)} />
                  <span className="min-w-0 truncate">{model}</span>
                  <CountBadge value={count} />
                </DropdownMenuCheckboxItem>
              ))}
            </DropdownMenuSubContent>
          </DropdownMenuSub>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
