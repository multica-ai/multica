"use client";

import { Server } from "@multica/ui/icons";
import { useQuery } from "@tanstack/react-query";
import type { AgentRuntime } from "@multica/core/types";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { runtimeListOptions } from "@multica/core/runtimes/queries";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { cn } from "@multica/ui/lib/utils";
import { CollectionPageState } from "../../layout/collection-page";
import { OBJECT_PAGE_BODY, ObjectPage } from "../../layout/object-page";
import { RuntimeDetail } from "./runtime-detail";
import { buildRuntimeMachines } from "./runtime-machines";
import { QoderDetailPage } from "./qoder-detail-page";
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

  const segments = [{ href: paths.runtimes(), label: t(($) => $.page.title) }];

  if (isLoading) {
    return (
      <ObjectPage
        layoutId="multica_runtime_detail_layout"
        segments={segments}
        leaf={<Skeleton className="h-4 w-40" />}
      >
        <div className={cn(OBJECT_PAGE_BODY, "space-y-8")}>
          <div className="flex items-center gap-3">
            <Skeleton className="size-12 rounded-xl" />
            <div className="flex-1 space-y-2">
              <Skeleton className="h-5 w-56" />
              <Skeleton className="h-4 w-40" />
            </div>
          </div>
          <Skeleton className="h-48 w-full rounded-lg" />
        </div>
      </ObjectPage>
    );
  }

  const target = resolveRuntimeSettingsTarget(
    runtimes,
    decodedMachineId,
    decodedRuntimeId,
  );
  if (!target) {
    return (
      <ObjectPage
        layoutId="multica_runtime_detail_layout"
        segments={segments}
        leaf={
          <span className="truncate text-muted-foreground">
            {t(($) => $.detail_page.not_found_title)}
          </span>
        }
      >
        <CollectionPageState
          icon={Server}
          title={t(($) => $.detail_page.not_found_title)}
          description={t(($) => $.detail_page.not_found_hint)}
        />
      </ObjectPage>
    );
  }
  const { machine, runtime } = target;
  if (runtime.provider === "qoder_cloud") return <QoderDetailPage />;

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
