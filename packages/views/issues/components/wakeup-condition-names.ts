"use client";

import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { labelListOptions } from "@multica/core/labels/queries";
import { propertyListOptions } from "@multica/core/properties/queries";
import { useActorName } from "@multica/core/workspace/hooks";
import { useStatusLabel } from "../utils/status-label";

/**
 * Names for the values a wakeup condition compares: status keys, labels,
 * properties and assignees. Reads the workspace catalogs issue pages already
 * load.
 */
export function useConditionNames() {
  const wsId = useWorkspaceId();
  // Built-in statuses read in the viewer's language; custom ones by name.
  const statusLabel = useStatusLabel(wsId);
  const { data: labels = [] } = useQuery({ ...labelListOptions(wsId), enabled: !!wsId });
  const { data: properties = [] } = useQuery({ ...propertyListOptions(wsId), enabled: !!wsId });
  const { getActorName } = useActorName();
  return {
    status: statusLabel,
    label: (id: string) => labels.find((l) => l.id === id)?.name,
    property: (id: string) => properties.find((p) => p.id === id),
    actor: (type: string, id: string) => getActorName(type, id),
  };
}
