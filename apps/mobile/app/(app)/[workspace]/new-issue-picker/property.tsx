/**
 * Single-select custom property picker for the in-progress new-issue draft.
 * See ./status.tsx. `propertyId` names the property; the definition comes
 * from the workspace property list the form already loaded.
 */
import { router, useLocalSearchParams } from "expo-router";
import { useQuery } from "@tanstack/react-query";
import { PropertyOptionPickerBody } from "@/components/issue/pickers/property-option-picker-body";
import { propertyListOptions } from "@/data/queries/properties";
import { useNewIssueDraftStore } from "@/data/stores/new-issue-draft-store";
import { useWorkspaceStore } from "@/data/workspace-store";

export default function NewIssuePropertyPickerRoute() {
  const { propertyId } = useLocalSearchParams<{ propertyId: string }>();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const { data: properties = [] } = useQuery(propertyListOptions(wsId));
  const value = useNewIssueDraftStore((s) => (propertyId ? s.properties[propertyId] ?? null : null));
  const setProperty = useNewIssueDraftStore((s) => s.setProperty);
  const property = properties.find((p) => p.id === propertyId);
  if (!property || !propertyId) return null;

  return (
    <PropertyOptionPickerBody
      property={property}
      value={value}
      onChange={(next) => {
        setProperty(propertyId, next);
        router.back();
      }}
    />
  );
}
