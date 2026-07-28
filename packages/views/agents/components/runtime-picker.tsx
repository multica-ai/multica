"use client";

import { useEffect, useMemo, useState } from "react";
import {
  Check,
  ChevronLeft,
  Cloud,
  Loader2,
  Lock,
  Monitor,
} from "lucide-react";
import type { MemberWithUser, RuntimeDevice } from "@multica/core/types";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@multica/ui/components/ui/alert-dialog";
import { cn } from "@multica/ui/lib/utils";
import { ProviderLogo } from "../../runtimes/components/provider-logo";
import {
  buildRuntimeMachines,
  runtimeRowLabel,
  type RuntimeMachine,
} from "../../runtimes/components/runtime-machines";
import {
  useCloudPreview,
  withCloudPreviewRuntime,
} from "../../runtimes/components/cloud-preview";
import { useT } from "../../i18n";

export interface RuntimePickerProps {
  runtimes: RuntimeDevice[];
  runtimesLoading?: boolean;
  members: MemberWithUser[];
  currentUserId: string | null;
  selectedRuntimeId: string;
  onSelect: (id: string) => void;
  disabled?: boolean;
}

/**
 * Two-level execution picker used by agent creation.
 *
 * Level one selects a physical or managed machine. Level two only shows the
 * runtimes installed on that machine. Keeping those concepts separate avoids
 * presenting a cloud location and a machine/runtime tuple as peers.
 */
export function RuntimePicker({
  runtimes: providedRuntimes,
  runtimesLoading = false,
  members,
  currentUserId,
  selectedRuntimeId,
  onSelect,
  disabled = false,
}: RuntimePickerProps) {
  const { t } = useT("agents");
  const cloudOn = useCloudPreview((state) => state.cloudOn);
  const setCloudOn = useCloudPreview((state) => state.setCloudOn);
  const [pendingCloudMachineId, setPendingCloudMachineId] = useState<
    string | null
  >(null);
  const workspaceId = providedRuntimes[0]?.workspace_id ?? "cloud-preview";
  const runtimes = useMemo(
    () =>
      withCloudPreviewRuntime(
        providedRuntimes,
        workspaceId,
        currentUserId,
      ),
    [currentUserId, providedRuntimes, workspaceId],
  );
  const machines = useMemo(() => {
    const grouped = buildRuntimeMachines(
      runtimes.filter(
        (runtime) =>
          runtime.runtime_mode !== "cloud" ||
          isRuntimeUsableForUser(runtime, currentUserId),
      ),
      { now: Date.now(), currentUserId },
    );
    return grouped.toSorted((left, right) => {
      const leftCloud = left.mode === "cloud";
      const rightCloud = right.mode === "cloud";
      if (leftCloud !== rightCloud) return leftCloud ? -1 : 1;
      if (left.isCurrent !== right.isCurrent) return left.isCurrent ? -1 : 1;
      return left.title.localeCompare(right.title);
    });
  }, [currentUserId, runtimes]);
  const selectedRuntime =
    runtimes.find((runtime) => runtime.id === selectedRuntimeId) ?? null;
  const selectedRuntimeMachine = selectedRuntime
    ? machines.find((machine) =>
        machine.runtimes.some((runtime) => runtime.id === selectedRuntime.id),
      ) ?? null
    : null;
  const [selectedMachineId, setSelectedMachineId] = useState<string | null>(
    () => selectedRuntimeMachine?.id ?? null,
  );
  const selectedMachine =
    machines.find((machine) => machine.id === selectedMachineId) ?? null;

  useEffect(() => {
    if (
      selectedRuntimeMachine &&
      selectedRuntimeMachine.id !== selectedMachineId
    ) {
      setSelectedMachineId(selectedRuntimeMachine.id);
    }
  }, [selectedMachineId, selectedRuntimeMachine]);

  useEffect(() => {
    if (selectedMachineId && !selectedMachine) {
      setSelectedMachineId(null);
      if (selectedRuntimeId) onSelect("");
    }
  }, [onSelect, selectedMachine, selectedMachineId, selectedRuntimeId]);

  const commitMachineSelection = (machine: RuntimeMachine) => {
    setSelectedMachineId(machine.id);
    if (
      !machine.runtimes.some((runtime) => runtime.id === selectedRuntimeId)
    ) {
      onSelect("");
    }
  };

  const chooseMachine = (machine: RuntimeMachine) => {
    if (disabled || !machineHasUsableRuntime(machine, currentUserId)) return;
    if (machine.mode === "cloud" && !cloudOn) {
      setPendingCloudMachineId(machine.id);
      return;
    }
    commitMachineSelection(machine);
  };

  const enableCloudAndContinue = () => {
    const machine = machines.find(
      (candidate) => candidate.id === pendingCloudMachineId,
    );
    setPendingCloudMachineId(null);
    if (!machine || disabled) return;
    setCloudOn(true);
    commitMachineSelection(machine);
  };

  const returnToMachines = () => {
    if (disabled) return;
    setSelectedMachineId(null);
    onSelect("");
  };

  if (!selectedMachine) {
    return (
      <>
        <div className="min-w-0 px-4 py-4">
          <StepHeading
            number={1}
            title={t(($) => $.create_dialog.execution_picker.device_title)}
            description={t(
              ($) => $.create_dialog.execution_picker.device_description,
            )}
          />
          {runtimesLoading && providedRuntimes.length === 0 ? (
            <div className="mt-4 flex min-h-32 items-center justify-center rounded-xl border border-dashed text-sm text-muted-foreground">
              <Loader2
                aria-hidden="true"
                className="mr-2 size-4 animate-spin"
              />
              {t(($) => $.create_dialog.runtime_loading)}
            </div>
          ) : (
            <div className="mt-4 grid gap-3 md:grid-cols-2 xl:grid-cols-3">
              {machines.map((machine) => (
                <MachineChoice
                  key={machine.id}
                  machine={machine}
                  ownerName={machineOwnerName(machine, members)}
                  cloudOn={cloudOn}
                  disabled={
                    disabled ||
                    !machineHasUsableRuntime(machine, currentUserId)
                  }
                  onSelect={() => chooseMachine(machine)}
                />
              ))}
            </div>
          )}
        </div>

        <AlertDialog
          open={pendingCloudMachineId !== null}
          onOpenChange={(open) => {
            if (!open) setPendingCloudMachineId(null);
          }}
        >
          <AlertDialogContent>
            <AlertDialogHeader>
              <AlertDialogTitle>
                {t(
                  ($) =>
                    $.create_dialog.execution_picker.cloud_enable_dialog.title,
                )}
              </AlertDialogTitle>
              <AlertDialogDescription>
                {t(
                  ($) =>
                    $.create_dialog.execution_picker.cloud_enable_dialog
                      .description,
                )}
              </AlertDialogDescription>
            </AlertDialogHeader>
            <AlertDialogFooter>
              <AlertDialogCancel>
                {t(
                  ($) =>
                    $.create_dialog.execution_picker.cloud_enable_dialog.cancel,
                )}
              </AlertDialogCancel>
              <AlertDialogAction onClick={enableCloudAndContinue}>
                {t(
                  ($) =>
                    $.create_dialog.execution_picker.cloud_enable_dialog
                      .confirm,
                )}
              </AlertDialogAction>
            </AlertDialogFooter>
          </AlertDialogContent>
        </AlertDialog>
      </>
    );
  }

  const isCloudMachine = selectedMachine.mode === "cloud";
  const machineTitle = isCloudMachine
    ? "Multica Cloud"
    : selectedMachine.title;

  return (
    <div className="min-w-0">
      <div className="border-b px-4 py-3">
        <button
          type="button"
          disabled={disabled}
          onClick={returnToMachines}
          className="inline-flex touch-manipulation items-center gap-1.5 rounded-md text-sm font-medium text-muted-foreground transition-colors hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:pointer-events-none disabled:opacity-50"
        >
          <ChevronLeft aria-hidden="true" className="size-4" />
          {t(($) => $.create_dialog.execution_picker.change_device)}
        </button>
        <div className="mt-3 flex min-w-0 items-center gap-3">
          <span className="flex size-10 shrink-0 items-center justify-center rounded-xl border bg-muted/40">
            {isCloudMachine ? (
              <Cloud
                aria-hidden="true"
                className="size-5 text-muted-foreground"
              />
            ) : (
              <Monitor
                aria-hidden="true"
                className="size-5 text-muted-foreground"
              />
            )}
          </span>
          <div className="min-w-0">
            <div className="truncate font-semibold">{machineTitle}</div>
            <div className="truncate text-xs text-muted-foreground">
              {isCloudMachine
                ? t(
                    ($) =>
                      $.create_dialog.execution_picker.cloud_device_description,
                  )
                : selectedMachine.subtitle ??
                  machineOwnerName(selectedMachine, members) ??
                  t(
                    ($) =>
                      $.create_dialog.execution_picker.computer_description,
                  )}
            </div>
          </div>
        </div>
      </div>

      <fieldset disabled={disabled} className="px-4 py-4">
        <legend className="sr-only">
          {t(($) => $.create_dialog.execution_picker.runtime_title)}
        </legend>
        <StepHeading
          number={2}
          title={t(($) => $.create_dialog.execution_picker.runtime_title)}
          description={t(
            ($) => $.create_dialog.execution_picker.runtime_description,
          )}
        />
        <div
          className="mt-4 grid gap-3 sm:grid-cols-2 lg:grid-cols-3"
          role="radiogroup"
          aria-label={t(
            ($) => $.create_dialog.execution_picker.runtime_title,
          )}
        >
          {selectedMachine.runtimes.map((runtime) => {
            const locked = !isRuntimeUsableForUser(runtime, currentUserId);
            const offline = runtime.status !== "online";
            const unavailable = locked || offline;
            const selected = runtime.id === selectedRuntimeId;
            return (
              <button
                key={runtime.id}
                type="button"
                role="radio"
                aria-checked={selected}
                disabled={disabled || unavailable}
                onClick={() => onSelect(runtime.id)}
                className={cn(
                  "group relative flex min-h-28 touch-manipulation items-start gap-3 rounded-xl border p-4 text-left transition-colors",
                  "hover:bg-muted/50 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2",
                  selected
                    ? "border-foreground bg-muted/40"
                    : "border-border bg-background",
                  unavailable && "cursor-not-allowed opacity-50",
                )}
              >
                <span className="flex size-9 shrink-0 items-center justify-center rounded-lg border bg-background">
                  <ProviderLogo
                    provider={runtime.provider}
                    className="size-4"
                  />
                </span>
                <span className="min-w-0 flex-1">
                  <span className="block truncate font-medium">
                    {runtimeRowLabel(runtime, machineTitle)}
                  </span>
                  <span className="mt-1 block text-xs leading-5 text-muted-foreground">
                    {offline
                      ? t(
                          ($) =>
                            $.create_dialog.execution_picker.runtime_offline,
                        )
                      : t(
                          ($) =>
                            $.create_dialog.execution_picker.runtime_ready,
                        )}
                  </span>
                </span>
                {locked ? (
                  <Lock
                    aria-hidden="true"
                    className="mt-1 size-4 shrink-0 text-muted-foreground"
                  />
                ) : selected ? (
                  <span className="flex size-5 shrink-0 items-center justify-center rounded-full bg-foreground text-background">
                    <Check aria-hidden="true" className="size-3" />
                  </span>
                ) : null}
              </button>
            );
          })}
        </div>
      </fieldset>
    </div>
  );
}

function StepHeading({
  number,
  title,
  description,
}: {
  number: number;
  title: string;
  description: string;
}) {
  return (
    <div className="flex min-w-0 items-start gap-3">
      <span className="flex size-6 shrink-0 items-center justify-center rounded-full bg-foreground text-xs font-semibold text-background">
        {number}
      </span>
      <div className="min-w-0">
        <div className="font-semibold">{title}</div>
        <p className="mt-0.5 text-pretty text-sm leading-5 text-muted-foreground">
          {description}
        </p>
      </div>
    </div>
  );
}

function MachineChoice({
  machine,
  ownerName,
  cloudOn,
  disabled,
  onSelect,
}: {
  machine: RuntimeMachine;
  ownerName: string | null;
  cloudOn: boolean;
  disabled: boolean;
  onSelect: () => void;
}) {
  const { t } = useT("agents");
  const isCloud = machine.mode === "cloud";
  const title = isCloud ? "Multica Cloud" : machine.title;
  const onlineCount = machine.runtimes.filter(
    (runtime) => runtime.status === "online",
  ).length;

  return (
    <button
      type="button"
      disabled={disabled}
      onClick={onSelect}
      className="group flex min-h-36 touch-manipulation flex-col rounded-xl border bg-background p-4 text-left transition-colors hover:border-foreground/40 hover:bg-muted/40 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-50"
    >
      <span className="flex w-full items-start justify-between gap-3">
        <span className="flex size-10 items-center justify-center rounded-xl border bg-muted/30">
          {isCloud ? (
            <Cloud aria-hidden="true" className="size-5 text-muted-foreground" />
          ) : (
            <Monitor
              aria-hidden="true"
              className="size-5 text-muted-foreground"
            />
          )}
        </span>
        {isCloud ? (
          <span className="rounded-md bg-info/10 px-2 py-1 text-[11px] font-medium text-info">
            {t(($) => $.create_dialog.execution_picker.recommended)}
          </span>
        ) : null}
      </span>
      <span className="mt-4 block w-full truncate font-semibold">{title}</span>
      <span className="mt-1 block w-full truncate text-xs text-muted-foreground">
        {isCloud
          ? t(
              ($) =>
                $.create_dialog.execution_picker.cloud_device_description,
            )
          : ownerName ??
            machine.subtitle ??
            t(($) => $.create_dialog.execution_picker.computer_description)}
      </span>
      <span className="mt-auto pt-3 text-xs tabular-nums text-muted-foreground">
        {isCloud && !cloudOn
          ? t(($) => $.create_dialog.execution_picker.cloud_not_enabled)
          : t(($) => $.create_dialog.execution_picker.runtime_summary, {
              online: onlineCount,
              total: machine.runtimes.length,
            })}
      </span>
    </button>
  );
}

function machineHasUsableRuntime(
  machine: RuntimeMachine,
  currentUserId: string | null,
): boolean {
  return machine.runtimes.some(
    (runtime) =>
      runtime.status === "online" &&
      isRuntimeUsableForUser(runtime, currentUserId),
  );
}

function machineOwnerName(
  machine: RuntimeMachine,
  members: MemberWithUser[],
): string | null {
  const ownerId = machine.runtimes.find((runtime) => runtime.owner_id)?.owner_id;
  if (!ownerId) return null;
  return members.find((member) => member.user_id === ownerId)?.name ?? null;
}

export function isRuntimeUsableForUser(
  runtime: RuntimeDevice,
  currentUserId: string | null,
): boolean {
  if (!currentUserId) return true;
  if (runtime.owner_id === currentUserId) return true;
  return runtime.visibility === "public";
}
