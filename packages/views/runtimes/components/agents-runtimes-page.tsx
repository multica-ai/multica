"use client";

import { AgentsPage, type AgentsPageProps } from "../../agents/components/agents-page";
import { SegmentedToggle } from "../../common/segmented-toggle";
import { useT } from "../../i18n";
import { useNavigation } from "../../navigation";
import { useWorkspacePaths } from "@multica/core/paths";
import { RuntimesPage, type RuntimesPageProps } from "./runtimes-page";

export type AgentsRuntimesPageProps = AgentsPageProps & RuntimesPageProps;

function isAgentsPath(pathname: string, agentsHref: string): boolean {
  return pathname === agentsHref || pathname.startsWith(`${agentsHref}/`);
}

export function AgentsRuntimesPage({
  localDaemonId,
  localMachineName,
  hasLocalMachine,
  bootstrapping,
  cloudRuntimeEnabled,
}: AgentsRuntimesPageProps = {}) {
  const { t } = useT("layout");
  const { pathname, push } = useNavigation();
  const paths = useWorkspacePaths();
  const agentsHref = paths.agents();
  const tab = isAgentsPath(pathname, agentsHref) ? "agents" : "runtimes";

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex shrink-0 items-center border-b px-4 py-2 sm:px-6">
        <div className="w-full max-w-xs">
          <SegmentedToggle
            value={tab}
            onChange={(next) => {
              push(next === "agents" ? agentsHref : paths.runtimes());
            }}
            options={[
              ["runtimes", t(($) => $.nav.runtimes)],
              ["agents", t(($) => $.nav.agents)],
            ]}
          />
        </div>
      </div>
      {tab === "agents" ? (
        <AgentsPage
          localDaemonId={localDaemonId}
          localMachineName={localMachineName}
          hasLocalMachine={hasLocalMachine}
        />
      ) : (
        <RuntimesPage
          localDaemonId={localDaemonId}
          localMachineName={localMachineName}
          hasLocalMachine={hasLocalMachine}
          bootstrapping={bootstrapping}
          cloudRuntimeEnabled={cloudRuntimeEnabled}
        />
      )}
    </div>
  );
}
