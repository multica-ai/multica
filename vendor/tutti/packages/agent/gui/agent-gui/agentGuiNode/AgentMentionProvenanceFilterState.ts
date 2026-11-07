import type { ReferenceProvenanceFilter } from "@tutti-os/workspace-file-reference/contracts";
import { referenceProvenanceFilterCacheKey } from "@tutti-os/workspace-file-reference/core";
import type { AgentMentionFilterId } from "./AgentMentionSearchContracts";

const EMPTY_PROVENANCE_FILTERS: Record<
  AgentMentionFilterId,
  ReferenceProvenanceFilter | null
> = {
  session: null,
  file: null,
  issue: null,
  agent: null,
  member: null,
  app: null
};

export class AgentMentionProvenanceFilterState {
  private filters = EMPTY_PROVENANCE_FILTERS;

  replace(
    filters: Record<AgentMentionFilterId, ReferenceProvenanceFilter | null>,
    activeFilter: AgentMentionFilterId
  ): { changed: boolean; value: ReferenceProvenanceFilter | null } {
    const previous = this.value(activeFilter);
    this.filters = filters;
    const value = this.value(activeFilter);
    return {
      changed: provenanceFilterKey(previous) !== provenanceFilterKey(value),
      value
    };
  }

  value(filter: AgentMentionFilterId): ReferenceProvenanceFilter | null {
    return this.filters[filter];
  }
}

function provenanceFilterKey(filter: ReferenceProvenanceFilter | null): string {
  return filter ? referenceProvenanceFilterCacheKey(filter) : "disabled";
}
