import { useParams } from "react-router-dom";
import {
  RuntimeDetailPage as SharedRuntimeDetailPage,
  RuntimeSettingsPage as SharedRuntimeSettingsPage,
} from "@multica/views/runtimes";
import { DaemonRuntimeActions } from "../components/daemon-runtime-card";
import { useDesktopRuntimeContext } from "../components/use-desktop-runtime-context";

export function RuntimeDetailPage() {
  const { id } = useParams<{ id: string }>();
  const context = useDesktopRuntimeContext();

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

  if (!id || !runtimeId) return null;
  return <SharedRuntimeSettingsPage machineId={id} runtimeId={runtimeId} />;
}
