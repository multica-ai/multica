import { useParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import {
  RuntimeDetailPage as SharedRuntimeDetailPage,
  RuntimeSettingsPage as SharedRuntimeSettingsPage,
} from "@multica/views/runtimes";
import { useWorkspaceId } from "@multica/core/hooks";
import { runtimeListOptions } from "@multica/core/runtimes/queries";
import { useDocumentTitle } from "@/hooks/use-document-title";
import { DaemonRuntimeActions } from "../components/daemon-runtime-card";
import { useDesktopRuntimeContext } from "../components/use-desktop-runtime-context";

export function RuntimeDetailPage() {
  const { id } = useParams<{ id: string }>();
  const wsId = useWorkspaceId();
  const { data: runtimes } = useQuery(runtimeListOptions(wsId));
  const runtime = runtimes?.find((candidate) => candidate.id === id);
  const context = useDesktopRuntimeContext();

  useDocumentTitle(runtime?.name ?? "Runtimes");

  if (!id) return null;
  return (
    <SharedRuntimeDetailPage
      runtimeId={id}
      localDaemonId={context.localDaemonId}
      localMachineName={context.localMachineName}
      localMachineActions={<DaemonRuntimeActions />}
      hasLocalMachine
      bootstrapping={context.bootstrapping}
    />
  );
}

export function RuntimeSettingsPage() {
  const { id, runtimeId } = useParams<{
    id: string;
    runtimeId: string;
  }>();
  const wsId = useWorkspaceId();
  const { data: runtimes } = useQuery(runtimeListOptions(wsId));
  const runtime = runtimes?.find((candidate) => candidate.id === runtimeId);

  useDocumentTitle(runtime?.name ?? "Runtime");

  if (!id || !runtimeId) return null;
  return <SharedRuntimeSettingsPage machineId={id} runtimeId={runtimeId} />;
}
