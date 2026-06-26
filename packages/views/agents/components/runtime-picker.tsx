"use client";

import { useEffect, useMemo, useState } from "react";
import { ChevronDown, Cloud, Loader2, Lock, Monitor } from "lucide-react";
import { ProviderLogo } from "../../runtimes/components/provider-logo";
import { ActorAvatar } from "../../common/actor-avatar";
import { splitRuntimeName } from "../../runtimes/components/runtime-machines";
import type { MemberWithUser, RuntimeDevice } from "@multica/core/types";
import {
  Popover,
  PopoverTrigger,
  PopoverContent,
} from "@multica/ui/components/ui/popover";
import { Label } from "@multica/ui/components/ui/label";
import { useT } from "../../i18n";

export type RuntimeFilter = "mine" | "all";

// ---------------------------------------------------------------------------
// MUL-3772: the single "Runtime" dropdown is split into two cascading
// selectors — Machine (the physical/cloud host) and Agent runtime (the CLI
// backend on that host). A RuntimeDevice is already a (machine × CLI) pair;
// the label the daemon builds is `<provider> (<deviceName>)`, so the split is
// purely a frontend regrouping. The submit payload is unchanged: a resolved
// (machine, runtime) collapses back to one `runtime_id`.
// ---------------------------------------------------------------------------

/** A host grouping one-or-more agent-runtime (CLI) instances. */
export interface AgentRuntimeMachine {
  /** Stable group key — daemon id, else owner+device, else runtime id. */
  key: string;
  /** Display label: hostname/device name. */
  label: string;
  /** Runtime owner (shared across a daemon's runtimes); null for cloud. */
  ownerId: string | null;
  /** True when any runtime on this machine is online. */
  online: boolean;
  /** True when this is a cloud-mode machine. */
  cloud: boolean;
  runtimes: RuntimeDevice[];
}

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
  const [filter, setFilter] = useState<RuntimeFilter>("mine");

  const machines = useMemo(
    () => buildAgentRuntimeMachines(runtimes),
    [runtimes],
  );
  const hasOtherMachines = machines.some((m) => m.ownerId !== currentUserId);

  const filteredMachines = useMemo(
    () => computeFilteredMachines(machines, filter, currentUserId),
    [machines, filter, currentUserId],
  );

  const selectedMachine =
    machines.find((m) =>
      m.runtimes.some((r) => r.id === selectedRuntimeId),
    ) ?? null;

  // Sole source of truth for seeding the parent's selection when it's empty
  // — first mount with no template runtime, runtimes arriving later over WS,
  // or a filter toggle clearing to a set with no usable item. Only fires
  // when `selectedRuntimeId === ""` so a duplicate-mode pre-fill (template
  // runtime) is never silently overwritten.
  useEffect(() => {
    if (selectedRuntimeId !== "") return;
    const firstUsable = firstUsableRuntime(filteredMachines, currentUserId);
    if (firstUsable) onSelect(firstUsable.id);
  }, [filteredMachines, selectedRuntimeId, currentUserId, onSelect]);

  // On filter toggle, recompute the selection to a usable runtime in the new
  // machine set. Pushes `""` when nothing matches; the seeding effect above
  // is a no-op in that case (correct: no usable item to pick).
  const handleFilterChange = (next: RuntimeFilter) => {
    if (next === filter) return;
    setFilter(next);
    const nextMachines = computeFilteredMachines(machines, next, currentUserId);
    const firstUsable = firstUsableRuntime(nextMachines, currentUserId);
    onSelect(firstUsable?.id ?? "");
  };

  // Switching machine moves the selection to that machine's first usable
  // runtime (or its first runtime if all are locked — the Create gate still
  // blocks submit, but the user can see what's there).
  const handleMachineSelect = (machine: AgentRuntimeMachine) => {
    const usable = machine.runtimes.find((r) =>
      isRuntimeUsableForUser(r, currentUserId),
    );
    onSelect((usable ?? machine.runtimes[0])?.id ?? "");
  };

  return (
    <>
      <MachinePicker
        machines={filteredMachines}
        selectedMachine={selectedMachine}
        members={members}
        currentUserId={currentUserId}
        runtimesLoading={runtimesLoading}
        runtimesEmpty={runtimes.length === 0}
        filter={filter}
        hasOtherMachines={hasOtherMachines}
        onFilterChange={handleFilterChange}
        onSelect={handleMachineSelect}
      />
      <AgentRuntimePicker
        machine={selectedMachine}
        selectedRuntimeId={selectedRuntimeId}
        currentUserId={currentUserId}
        runtimesLoading={runtimesLoading}
        onSelect={onSelect}
      />
    </>
  );
}

// ---------------------------------------------------------------------------
// Machine selector — owns the Mine/All filter (relabelled but functionally
// the runtime filter it replaced) and the online/owner/Cloud chrome.
// ---------------------------------------------------------------------------

function MachinePicker({
  machines,
  selectedMachine,
  members,
  currentUserId,
  runtimesLoading,
  runtimesEmpty,
  filter,
  hasOtherMachines,
  onFilterChange,
  onSelect,
}: {
  machines: AgentRuntimeMachine[];
  selectedMachine: AgentRuntimeMachine | null;
  members: MemberWithUser[];
  currentUserId: string | null;
  runtimesLoading?: boolean;
  runtimesEmpty: boolean;
  filter: RuntimeFilter;
  hasOtherMachines: boolean;
  onFilterChange: (next: RuntimeFilter) => void;
  onSelect: (machine: AgentRuntimeMachine) => void;
}) {
  const { t } = useT("agents");
  const [open, setOpen] = useState(false);

  const ownerName = (machine: AgentRuntimeMachine): string | null => {
    if (!machine.ownerId) return null;
    return members.find((m) => m.user_id === machine.ownerId)?.name ?? null;
  };

  const machineSubtitle = (machine: AgentRuntimeMachine): string => {
    const owner = ownerName(machine);
    const count = t(($) => $.create_dialog.machine_runtime_count, {
      count: machine.runtimes.length,
    });
    return owner ? `${owner} · ${count}` : count;
  };

  return (
    <div className="flex flex-col min-w-0">
      <div className="flex h-6 items-center justify-between">
        <Label className="text-xs text-muted-foreground">
          {t(($) => $.create_dialog.machine_label)}
        </Label>
        {hasOtherMachines && (
          <div className="flex items-center gap-0.5 rounded-md bg-muted p-0.5">
            <button
              type="button"
              onClick={() => onFilterChange("mine")}
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
              onClick={() => onFilterChange("all")}
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
      <Popover open={open} onOpenChange={setOpen}>
        <PopoverTrigger
          data-testid="machine-picker-trigger"
          disabled={runtimesEmpty && !runtimesLoading}
          className="flex w-full min-w-0 items-center gap-3 rounded-lg border border-border bg-background px-3 py-2.5 mt-1.5 text-left text-sm transition-colors hover:bg-muted disabled:pointer-events-none disabled:opacity-50"
        >
          {runtimesLoading ? (
            <Loader2 className="h-4 w-4 shrink-0 animate-spin text-muted-foreground" />
          ) : selectedMachine?.cloud ? (
            <Cloud className="h-4 w-4 shrink-0 text-muted-foreground" />
          ) : (
            <Monitor className="h-4 w-4 shrink-0 text-muted-foreground" />
          )}
          <div className="min-w-0 flex-1">
            <div className="flex items-center gap-2">
              <span className="truncate font-medium">
                {runtimesLoading
                  ? t(($) => $.create_dialog.runtime_loading)
                  : (selectedMachine?.label ??
                    t(($) => $.create_dialog.runtime_none))}
              </span>
              {selectedMachine?.cloud && (
                <span className="shrink-0 rounded bg-info/10 px-1.5 py-0.5 text-xs font-medium text-info">
                  {t(($) => $.create_dialog.runtime_cloud_badge)}
                </span>
              )}
            </div>
            {selectedMachine && (
              <div className="truncate text-xs text-muted-foreground">
                {machineSubtitle(selectedMachine)}
              </div>
            )}
          </div>
          {selectedMachine && (
            <span
              className={`h-2 w-2 shrink-0 rounded-full ${
                selectedMachine.online ? "bg-success" : "bg-muted-foreground/40"
              }`}
            />
          )}
          <ChevronDown
            className={`h-4 w-4 shrink-0 text-muted-foreground transition-transform ${
              open ? "rotate-180" : ""
            }`}
          />
        </PopoverTrigger>
        <PopoverContent
          align="start"
          className="w-[var(--anchor-width)] p-1 max-h-60 overflow-y-auto"
        >
          {machines.map((machine) => {
            const disabled = !isMachineUsableForUser(machine, currentUserId);
            const disabledTitle = disabled
              ? t(($) => $.create_dialog.runtime_private_locked_tooltip)
              : undefined;
            const owner = ownerName(machine);
            return (
              <button
                key={machine.key}
                type="button"
                disabled={disabled}
                title={disabledTitle}
                onClick={() => {
                  if (disabled) return;
                  onSelect(machine);
                  setOpen(false);
                }}
                className={`flex w-full items-center gap-3 rounded-md px-3 py-2.5 text-left text-sm transition-colors ${
                  disabled
                    ? "cursor-not-allowed opacity-50"
                    : machine.key === selectedMachine?.key
                      ? "bg-accent"
                      : "hover:bg-accent/50"
                }`}
              >
                {machine.cloud ? (
                  <Cloud className="h-4 w-4 shrink-0 text-muted-foreground" />
                ) : (
                  <Monitor className="h-4 w-4 shrink-0 text-muted-foreground" />
                )}
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-2">
                    <span className="truncate font-medium">
                      {machine.label}
                    </span>
                    {machine.cloud && (
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
                    {owner && machine.ownerId ? (
                      <>
                        <ActorAvatar
                          actorType="member"
                          actorId={machine.ownerId}
                          size={14}
                        />
                        <span className="truncate">
                          {machineSubtitle(machine)}
                        </span>
                      </>
                    ) : (
                      <span className="truncate">
                        {machineSubtitle(machine)}
                      </span>
                    )}
                  </div>
                </div>
                <span
                  className={`h-2 w-2 shrink-0 rounded-full ${
                    machine.online ? "bg-success" : "bg-muted-foreground/40"
                  }`}
                />
              </button>
            );
          })}
        </PopoverContent>
      </Popover>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Agent-runtime selector — cascades off the selected machine. A machine with
// a single runtime renders the selector read-only (stable layout, nothing to
// choose) per the approved RFC default.
// ---------------------------------------------------------------------------

function AgentRuntimePicker({
  machine,
  selectedRuntimeId,
  currentUserId,
  runtimesLoading,
  onSelect,
}: {
  machine: AgentRuntimeMachine | null;
  selectedRuntimeId: string;
  currentUserId: string | null;
  runtimesLoading?: boolean;
  onSelect: (id: string) => void;
}) {
  const { t } = useT("agents");
  const [open, setOpen] = useState(false);

  const runtimes = machine?.runtimes ?? [];
  const selectedRuntime =
    runtimes.find((r) => r.id === selectedRuntimeId) ?? null;
  const single = runtimes.length === 1;

  const runtimeTitle = (runtime: RuntimeDevice): string =>
    splitRuntimeName(runtime.name).base;

  const runtimeSubtitle = (runtime: RuntimeDevice): string => {
    const kind = runtime.profile_id
      ? t(($) => $.create_dialog.runtime_kind_custom)
      : t(($) => $.create_dialog.runtime_kind_builtin);
    const header = runtime.launch_header?.trim();
    return header ? `${header} · ${kind}` : kind;
  };

  const triggerContent = (
    <>
      {selectedRuntime ? (
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
            {selectedRuntime
              ? runtimeTitle(selectedRuntime)
              : t(($) => $.create_dialog.agent_runtime_none)}
          </span>
        </div>
        {selectedRuntime && (
          <div className="truncate text-xs text-muted-foreground">
            {runtimeSubtitle(selectedRuntime)}
          </div>
        )}
      </div>
    </>
  );

  return (
    <div className="flex flex-col min-w-0">
      <div className="flex h-6 items-center">
        <Label className="text-xs text-muted-foreground">
          {t(($) => $.create_dialog.agent_runtime_label)}
        </Label>
      </div>
      {single ? (
        // Read-only: a single-runtime machine has nothing to pick. Render the
        // same chrome (minus the chevron / hover) so the layout doesn't jump
        // when switching to a multi-runtime machine.
        <div
          data-testid="agent-runtime-readonly"
          className="flex w-full min-w-0 items-center gap-3 rounded-lg border border-border bg-muted/30 px-3 py-2.5 mt-1.5 text-left text-sm"
        >
          {triggerContent}
        </div>
      ) : (
        <Popover open={open} onOpenChange={setOpen}>
          <PopoverTrigger
            data-testid="agent-runtime-trigger"
            disabled={!machine || runtimesLoading}
            className="flex w-full min-w-0 items-center gap-3 rounded-lg border border-border bg-background px-3 py-2.5 mt-1.5 text-left text-sm transition-colors hover:bg-muted disabled:pointer-events-none disabled:opacity-50"
          >
            {triggerContent}
            <ChevronDown
              className={`h-4 w-4 shrink-0 text-muted-foreground transition-transform ${
                open ? "rotate-180" : ""
              }`}
            />
          </PopoverTrigger>
          <PopoverContent
            align="start"
            className="w-[var(--anchor-width)] p-1 max-h-60 overflow-y-auto"
          >
            {runtimes.map((runtime) => {
              const disabled = !isRuntimeUsableForUser(runtime, currentUserId);
              const disabledTitle = disabled
                ? t(($) => $.create_dialog.runtime_private_locked_tooltip)
                : undefined;
              return (
                <button
                  key={runtime.id}
                  type="button"
                  disabled={disabled}
                  title={disabledTitle}
                  onClick={() => {
                    if (disabled) return;
                    onSelect(runtime.id);
                    setOpen(false);
                  }}
                  className={`flex w-full items-center gap-3 rounded-md px-3 py-2.5 text-left text-sm transition-colors ${
                    disabled
                      ? "cursor-not-allowed opacity-50"
                      : runtime.id === selectedRuntimeId
                        ? "bg-accent"
                        : "hover:bg-accent/50"
                  }`}
                >
                  <ProviderLogo
                    provider={runtime.provider}
                    className="h-4 w-4 shrink-0"
                  />
                  <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-2">
                      <span className="truncate font-medium">
                        {runtimeTitle(runtime)}
                      </span>
                      {disabled && (
                        <span className="shrink-0 inline-flex items-center gap-1 rounded bg-muted px-1.5 py-0.5 text-[10px] font-medium text-muted-foreground">
                          <Lock className="h-3 w-3" />
                          {t(($) => $.create_dialog.runtime_private_badge)}
                        </span>
                      )}
                    </div>
                    <div className="mt-0.5 truncate text-xs text-muted-foreground">
                      {runtimeSubtitle(runtime)}
                    </div>
                  </div>
                  <span
                    className={`h-2 w-2 shrink-0 rounded-full ${
                      runtime.status === "online"
                        ? "bg-success"
                        : "bg-muted-foreground/40"
                    }`}
                  />
                </button>
              );
            })}
          </PopoverContent>
        </Popover>
      )}
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

function isMachineUsableForUser(
  machine: AgentRuntimeMachine,
  currentUserId: string | null,
): boolean {
  return machine.runtimes.some((r) => isRuntimeUsableForUser(r, currentUserId));
}

// Group the flat runtime list into machines. A daemon-backed runtime groups
// by `daemon_id`; cloud / daemon-less runtimes (daemon_id: null) fall back to
// owner+device so two members' identically-named hosts never collapse into
// one row, then to the runtime id when even the device name is missing.
export function buildAgentRuntimeMachines(
  runtimes: RuntimeDevice[],
): AgentRuntimeMachine[] {
  const groups = new Map<string, RuntimeDevice[]>();
  const order: string[] = [];
  for (const runtime of runtimes) {
    const key = machineKey(runtime);
    const existing = groups.get(key);
    if (existing) {
      existing.push(runtime);
    } else {
      groups.set(key, [runtime]);
      order.push(key);
    }
  }

  return order.map((key) => {
    const group = groups
      .get(key)!
      .toSorted((a, b) => a.provider.localeCompare(b.provider));
    const first = group[0]!;
    return {
      key,
      label: machineLabel(group),
      ownerId: first.owner_id,
      online: group.some((r) => r.status === "online"),
      cloud: group.some((r) => r.runtime_mode === "cloud"),
      runtimes: group,
    } satisfies AgentRuntimeMachine;
  });
}

function machineKey(runtime: RuntimeDevice): string {
  if (runtime.daemon_id) return `daemon:${runtime.daemon_id}`;
  const device =
    splitRuntimeName(runtime.name).hostname ??
    (runtime.device_info?.trim() || null);
  if (device) return `device:${runtime.owner_id ?? "_"}:${device}`;
  return `runtime:${runtime.id}`;
}

function machineLabel(runtimes: RuntimeDevice[]): string {
  const first = runtimes[0]!;
  const host = splitRuntimeName(first.name).hostname;
  if (host) return host;
  const device = first.device_info?.trim();
  if (device) return device;
  return first.name;
}

function computeFilteredMachines(
  machines: AgentRuntimeMachine[],
  filter: RuntimeFilter,
  currentUserId: string | null,
): AgentRuntimeMachine[] {
  const filtered =
    filter === "mine" && currentUserId
      ? machines.filter((m) => m.ownerId === currentUserId)
      : machines;
  return filtered.toSorted((a, b) => {
    const aMine = a.ownerId === currentUserId;
    const bMine = b.ownerId === currentUserId;
    if (aMine && !bMine) return -1;
    if (!aMine && bMine) return 1;
    const aUsable = isMachineUsableForUser(a, currentUserId);
    const bUsable = isMachineUsableForUser(b, currentUserId);
    if (aUsable && !bUsable) return -1;
    if (!aUsable && bUsable) return 1;
    return a.label.localeCompare(b.label);
  });
}

// First selectable runtime across the (already sorted) machine set — used to
// seed and to re-seed on filter change. Honors the per-runtime visibility
// gate so a locked private runtime is never auto-selected.
function firstUsableRuntime(
  machines: AgentRuntimeMachine[],
  currentUserId: string | null,
): RuntimeDevice | undefined {
  for (const machine of machines) {
    const usable = machine.runtimes.find((r) =>
      isRuntimeUsableForUser(r, currentUserId),
    );
    if (usable) return usable;
  }
  return undefined;
}
