"use client";

import { Server } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import type { AgentRuntime } from "@multica/core/types";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { runtimeListOptions } from "@multica/core/runtimes/queries";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { RuntimeDetail } from "./runtime-detail";
import { buildRuntimeMachines } from "./runtime-machines";
import { useT } from "../../i18n";

export function RuntimeSettingsPage({
  machineId,
  runtimeId,
}: {
  machineId: string;
  runtimeId: string;
}) {
  const { t } = useT("runtimes");
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const { data: runtimes = [], isLoading } = useQuery(runtimeListOptions(wsId));
  const decodedMachineId = decodeRouteParam(machineId);
  const decodedRuntimeId = decodeRouteParam(runtimeId);

  if (isLoading) {
    return (
      <div className="flex h-full flex-col p-6">
        <Skeleton className="h-12 w-1/2" />
        <div className="mt-6 grid grid-cols-3 gap-3">
          <Skeleton className="h-20 rounded-lg" />
          <Skeleton className="h-20 rounded-lg" />
          <Skeleton className="h-20 rounded-lg" />
        </div>
        <Skeleton className="mt-4 h-48 w-full rounded-lg" />
      </div>
    );
  }

  const target = resolveRuntimeSettingsTarget(
    runtimes,
    decodedMachineId,
    decodedRuntimeId,
  );
  if (!target) {
    return (
      <div className="flex h-full flex-col items-center justify-center text-muted-foreground">
        <Server
          aria-hidden="true"
          className="h-10 w-10 text-muted-foreground/30"
        />
        <p className="mt-3 text-sm">{t(($) => $.detail_page.not_found_title)}</p>
        <p className="mt-1 text-xs text-muted-foreground/70">
          {t(($) => $.detail_page.not_found_hint)}
        </p>
      </div>
    );
  }
  const { machine, runtime } = target;

  return (
    <RuntimeDetail
      runtime={runtime}
      machineHref={paths.runtimeDetail(decodedMachineId)}
      machineLabel={machine.title}
      afterDeleteHref={
        machine.runtimes.length > 1
          ? paths.runtimeDetail(decodedMachineId)
          : paths.runtimes()
      }
    />
  );
}

export function resolveRuntimeSettingsTarget(
  runtimes: AgentRuntime[],
  machineId: string,
  runtimeId: string,
) {
  const machine = buildRuntimeMachines(runtimes, { now: Date.now() }).find(
    (candidate) =>
      candidate.id === machineId ||
      candidate.runtimes.some((item) => item.id === machineId),
  );
  const runtime = machine?.runtimes.find((candidate) => candidate.id === runtimeId);
  return machine && runtime ? { machine, runtime } : null;
}

function decodeRouteParam(value: string): string {
  try {
    return decodeURIComponent(value);
  } catch {
    return value;
  }
}
