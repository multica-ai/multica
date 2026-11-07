import { useMemo } from "react";
import { useReferenceProvenanceFilterCatalog } from "@tutti-os/workspace-file-reference/react";
import type { ReferenceProvenanceCatalog } from "@tutti-os/workspace-file-reference/contracts";
import { resolveAgentGUIReferenceProvenanceFilterCatalog } from "../model/agentReferenceProvenanceCatalog";
import type { AgentComposerReferenceProvenanceFilters } from "./AgentComposer.types";

const DISABLED_REFERENCE_PROVENANCE_CATALOG: ReferenceProvenanceCatalog = {
  enabledDimensions: [],
  agentOptions: [],
  memberOptions: []
};

export function useAgentMentionProvenanceFilters(
  input: Parameters<typeof resolveAgentGUIReferenceProvenanceFilterCatalog>[0]
): AgentComposerReferenceProvenanceFilters | null {
  const catalog = useMemo(
    () => resolveAgentGUIReferenceProvenanceFilterCatalog(input),
    [input.agentTargets, input.injectedCatalog, input.legacyAgentFilterEnabled]
  );
  const effectiveCatalog = catalog ?? DISABLED_REFERENCE_PROVENANCE_CATALOG;
  const session = useReferenceProvenanceFilterCatalog(effectiveCatalog);
  const file = useReferenceProvenanceFilterCatalog(effectiveCatalog);
  const issue = useReferenceProvenanceFilterCatalog(effectiveCatalog);
  const agent = useReferenceProvenanceFilterCatalog(effectiveCatalog);
  const app = useReferenceProvenanceFilterCatalog(effectiveCatalog);
  const member = useReferenceProvenanceFilterCatalog(DISABLED_REFERENCE_PROVENANCE_CATALOG);

  return useMemo(
    () =>
      session.snapshot.catalog.enabledDimensions.length === 0
        ? null
        : {
            byFilter: {
              session,
              file,
              issue,
              agent,
              member,
              app
            }
          },
    [agent, app, file, issue, session, member]
  );
}
