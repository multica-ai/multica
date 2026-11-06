"use client";

import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { availableCodeGraphsOptions } from "@multica/core/workspace/codegraph";
import { useT } from "../../i18n";

export function RunCodeGraphs({ taskId }: { taskId: string }) {
  const wsId = useWorkspaceId();
  const { t } = useT("layout");
  const graphs = useQuery({
    ...availableCodeGraphsOptions(wsId, taskId),
    enabled: Boolean(wsId && taskId),
  });
  if (!graphs.isSuccess || !graphs.data.length) return null;
  return (
    <details className="mx-4 rounded-lg border px-3 py-2 text-caption">
      <summary className="cursor-pointer font-medium">
        {t(($) => $.code_graph.scope.available)} ({graphs.data.length})
      </summary>
      <p className="mt-2 text-muted-foreground">
        {t(($) => $.code_graph.scope.current_help)}
      </p>
      <ul className="mt-2 space-y-2">
        {graphs.data.map((repo) => (
          <li key={repo.id} className="break-words">
            {repo.name} · {repo.branch}
            <p className="text-muted-foreground">
              {repo.sources
                .map((source) =>
                  t(($) => $.code_graph.scope[`source_${source}`]),
                )
                .join(" · ")}
            </p>
            {!repo.active_build_id && (
              <p className="text-muted-foreground">
                {t(($) => $.code_graph.scope.awaiting_build)}
              </p>
            )}
          </li>
        ))}
      </ul>
    </details>
  );
}
