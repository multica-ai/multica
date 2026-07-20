"use client";

import { useEffect, useMemo, useState } from "react";
import { ChevronDown, Cloud, Loader2, Lock, Search } from "lucide-react";
import { ProviderLogo } from "../../runtimes/components/provider-logo";
import { ActorAvatar } from "../../common/actor-avatar";
import { runtimeDisplayName } from "@multica/core/runtimes";
import type { MemberWithUser, RuntimeDevice } from "@multica/core/types";
import {
  Popover,
  PopoverTrigger,
  PopoverContent,
} from "@multica/ui/components/ui/popover";
import { Label } from "@multica/ui/components/ui/label";
import { useT } from "../../i18n";
import {
  buildRuntimeMachines,
  filterRuntimeMachines,
  runtimeRowLabel,
} from "../../runtimes/components/runtime-machines";

export type RuntimeFilter = "mine" | "all";

// Above this many runtimes the flat list becomes hard to scan, so we surface
// a search box. Machine grouping kicks in independently whenever more than one
// machine is present.
const SEARCH_THRESHOLD = 6;

export function RuntimePicker({
  runtimes,
  runtimesLoading,
  members,
  currentUserId,
  selectedRuntimeId,
  onSelect,
}: {
  runtimes: RuntimeDevice[];
  runtimesLoading?: boolean;
  members: MemberWithUser[];
  currentUserId: string | null;
  selectedRuntimeId: string;
  onSelect: (id: string) => void;
}) {
  const { t } = useT("agents");
  const [open, setOpen] = useState(false);
  const [filter, setFilter] = useState<RuntimeFilter>("mine");
  const [search, setSearch] = useState("");

  const getOwnerMember = (ownerId: string | null) => {
    if (!ownerId) return null;
    return members.find((m) => m.user_id === ownerId) ?? null;
  };

  const hasOtherRuntimes = runtimes.some((r) => r.owner_id !== currentUserId);

  // Base list honours the mine/all toggle and drives auto-selection; it is
  // intentionally independent of the search box so typing never changes the
  // seeded selection.
  const filteredRuntimes = useMemo(
    () => computeFilteredRuntimes(runtimes, filter, currentUserId),
    [runtimes, filter, currentUserId],
  );

  // Group the (searched) base list by machine so 20+ runtimes read as a
  // handful of named machines, online-first, current machine first.
  const machines = useMemo(() => {
    const all = buildRuntimeMachines(filteredRuntimes, {
      now: Date.now(),
      currentUserId,
    });
    return filterRuntimeMachines(all, search, "all");
  }, [filteredRuntimes, search, currentUserId]);

  const showSearch = runtimes.length > SEARCH_THRESHOLD;

  const selectedRuntime =
    runtimes.find((d) => d.id === selectedRuntimeId) ?? null;

  // Sole source of truth for seeding the parent's selection when it's empty
  // — first mount with no template runtime, runtimes arriving later over
  // WS, or filter toggle clearing to a set with no usable item. Only fires
  // when `selectedRuntimeId === ""` so a duplicate-mode pre-fill (template
  // runtime) is never silently overwritten.
  useEffect(() => {
    if (selectedRuntimeId !== "") return;
    const firstUsable = filteredRuntimes.find((r) =>
      isRuntimeUsableForUser(r, currentUserId),
    );
    if (firstUsable) onSelect(firstUsable.id);
  }, [filteredRuntimes, selectedRuntimeId, currentUserId, onSelect]);

  // On filter toggle, recompute the picker's selection to a usable item
  // in the new filter set. Pushes `""` when nothing matches; the seeding
  // effect above is a no-op in that case (correct: no usable item to pick).
  const handleFilterChange = (next: RuntimeFilter) => {
    if (next === filter) return;
    setFilter(next);
    const nextList = computeFilteredRuntimes(runtimes, next, currentUserId);
    const firstUsable = nextList.find((r) =>
      isRuntimeUsableForUser(r, currentUserId),
    );
    onSelect(firstUsable?.id ?? "");
  };

  return (
    <div className="flex flex-col min-w-0">
      <div className="flex h-6 items-center justify-between">
        <Label className="text-xs text-muted-foreground">
          {t(($) => $.create_dialog.runtime_label)}
        </Label>
        {hasOtherRuntimes && (
          <div className="flex items-center gap-0.5 rounded-md bg-muted p-0.5">
            <button
              type="button"
              onClick={() => handleFilterChange("mine")}
              className={`rounded px-2 py-0.5 text-xs font-medium transition-colors ${
                filter === "mine"
                  ? "bg-background text-foreground shadow-sm"
                  : "text-muted-foreground hover:text-foreground"
              }`}
            >
              {t(($) => $.create_dialog.runtime_filter_mine)}
            </button>
            <button
              type="button"
              onClick={() => handleFilterChange("all")}
              className={`rounded px-2 py-0.5 text-xs font-medium transition-colors ${
                filter === "all"
                  ? "bg-background text-foreground shadow-sm"
                  : "text-muted-foreground hover:text-foreground"
              }`}
            >
              {t(($) => $.create_dialog.runtime_filter_all)}
            </button>
          </div>
        )}
      </div>
      <Popover
        open={open}
        onOpenChange={(next) => {
          setOpen(next);
          if (!next) setSearch("");
        }}
      >
        <PopoverTrigger
          disabled={runtimes.length === 0 && !runtimesLoading}
          className="flex w-full min-w-0 items-center gap-3 rounded-lg border border-border bg-background px-3 py-2.5 mt-1.5 text-left text-sm transition-colors hover:bg-muted disabled:pointer-events-none disabled:opacity-50"
        >
          {runtimesLoading ? (
            <Loader2 className="h-4 w-4 shrink-0 animate-spin text-muted-foreground" />
          ) : selectedRuntime ? (
            <ProviderLogo
              provider={selectedRuntime.provider}
              className="h-4 w-4 shrink-0"
            />
          ) : (
            <Cloud className="h-4 w-4 shrink-0 text-muted-foreground" />
          )}
          <div className="min-w-0 flex-1">
            <div className="flex items-center gap-2">
              <span className="truncate font-medium">
                {runtimesLoading
                  ? t(($) => $.create_dialog.runtime_loading)
                  : selectedRuntime
                    ? runtimeDisplayName(selectedRuntime)
                    : t(($) => $.create_dialog.runtime_none)}
              </span>
              {selectedRuntime?.runtime_mode === "cloud" && (
                <span className="shrink-0 rounded bg-info/10 px-1.5 py-0.5 text-xs font-medium text-info">
                  {t(($) => $.create_dialog.runtime_cloud_badge)}
                </span>
              )}
            </div>
            {selectedRuntime && (
              <div className="truncate text-xs text-muted-foreground">
                {getOwnerMember(selectedRuntime.owner_id)?.name ??
                  selectedRuntime.device_info}
              </div>
            )}
          </div>
          <ChevronDown
            className={`h-4 w-4 shrink-0 text-muted-foreground transition-transform ${
              open ? "rotate-180" : ""
            }`}
          />
        </PopoverTrigger>
        <PopoverContent
          align="start"
          className="w-[var(--anchor-width)] p-1 flex flex-col max-h-72"
        >
          {showSearch && (
            <div className="relative mb-1 shrink-0">
              <Search className="pointer-events-none absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
              <input
                type="text"
                autoFocus
                value={search}
                onChange={(e) => setSearch(e.target.value)}
                placeholder={t(($) => $.create_dialog.runtime_search_placeholder)}
                className="w-full rounded-md border border-border bg-background py-1.5 pl-8 pr-2 text-sm outline-none focus:ring-1 focus:ring-ring"
              />
            </div>
          )}
          <div className="min-h-0 flex-1 overflow-y-auto">
            {machines.length === 0 ? (
              <div className="px-3 py-6 text-center text-xs text-muted-foreground">
                {t(($) => $.create_dialog.runtime_no_results)}
              </div>
            ) : (
              machines.map((machine) => (
                <div key={machine.id}>
                  {/* Always show the machine header — even when a search or a
                      single-machine workspace narrows it to one group — so the
                      grouping stays consistent instead of collapsing to a flat
                      list. */}
                  <div className="flex items-center justify-between gap-2 px-2 pb-0.5 pt-2 text-[11px] font-medium text-muted-foreground">
                    <span className="truncate">{machine.title}</span>
                    <span className="shrink-0 tabular-nums">
                      {t(($) => $.create_dialog.runtime_group_online, {
                        online: machine.onlineCount,
                        total: machine.runtimes.length,
                      })}
                    </span>
                  </div>
                  {machine.runtimes.map((device) => {
                    const ownerMember = getOwnerMember(device.owner_id);
                    const disabled = !isRuntimeUsableForUser(
                      device,
                      currentUserId,
                    );
                    const disabledTitle = disabled
                      ? t(($) => $.create_dialog.runtime_private_locked_tooltip)
                      : undefined;
                    return (
                      <button
                        key={device.id}
                        type="button"
                        disabled={disabled}
                        title={disabledTitle}
                        onClick={() => {
                          if (disabled) return;
                          onSelect(device.id);
                          setOpen(false);
                        }}
                        className={`flex w-full items-center gap-3 rounded-md px-3 py-2.5 text-left text-sm transition-colors ${
                          disabled
                            ? "cursor-not-allowed opacity-50"
                            : device.id === selectedRuntimeId
                              ? "bg-accent"
                              : "hover:bg-accent/50"
                        }`}
                      >
                        <ProviderLogo
                          provider={device.provider}
                          className="h-4 w-4 shrink-0"
                        />
                        <div className="min-w-0 flex-1">
                          <div className="flex items-center gap-2">
                            <span className="truncate font-medium">
                              {runtimeRowLabel(device, machine.title)}
                            </span>
                            {device.runtime_mode === "cloud" && (
                              <span className="shrink-0 rounded bg-info/10 px-1.5 py-0.5 text-xs font-medium text-info">
                                {t(($) => $.create_dialog.runtime_cloud_badge)}
                              </span>
                            )}
                            {disabled && (
                              <span className="shrink-0 inline-flex items-center gap-1 rounded bg-muted px-1.5 py-0.5 text-[10px] font-medium text-muted-foreground">
                                <Lock className="h-3 w-3" />
                                {t(($) => $.create_dialog.runtime_private_badge)}
                              </span>
                            )}
                          </div>
                          <div className="mt-0.5 flex items-center gap-1 text-xs text-muted-foreground">
                            {ownerMember ? (
                              <>
                                <ActorAvatar
                                  actorType="member"
                                  actorId={ownerMember.user_id}
                                  size="xs"
                                />
                                <span className="truncate">
                                  {ownerMember.name}
                                </span>
                              </>
                            ) : (
                              <span className="truncate">
                                {device.device_info}
                              </span>
                            )}
                          </div>
                        </div>
                        <span
                          className={`h-2 w-2 shrink-0 rounded-full ${
                            device.status === "online"
                              ? "bg-success"
                              : "bg-muted-foreground/40"
                          }`}
                        />
                      </button>
                    );
                  })}
                </div>
              ))
            )}
          </div>
        </PopoverContent>
      </Popover>
    </div>
  );
}

// Visibility gate exposed so the parent can defend Create against a locked
// selection (e.g. duplicate of an agent whose runtime is now private).
export function isRuntimeUsableForUser(
  r: RuntimeDevice,
  currentUserId: string | null,
): boolean {
  if (!currentUserId) return true;
  if (r.owner_id === currentUserId) return true;
  return r.visibility === "public";
}

function computeFilteredRuntimes(
  runtimes: RuntimeDevice[],
  filter: RuntimeFilter,
  currentUserId: string | null,
): RuntimeDevice[] {
  const filtered =
    filter === "mine" && currentUserId
      ? runtimes.filter((r) => r.owner_id === currentUserId)
      : runtimes;
  return filtered.toSorted((a, b) => {
    const aMine = a.owner_id === currentUserId;
    const bMine = b.owner_id === currentUserId;
    if (aMine && !bMine) return -1;
    if (!aMine && bMine) return 1;
    const aUsable = isRuntimeUsableForUser(a, currentUserId);
    const bUsable = isRuntimeUsableForUser(b, currentUserId);
    if (aUsable && !bUsable) return -1;
    if (!aUsable && bUsable) return 1;
    return 0;
  });
}
