import { useCallback } from "react";
import { useIssueStatuses } from "@multica/core/issue-statuses/hooks";
import { useStrings } from "../strings";

export interface StatusPresentation {
  key: string;
  label: string;
  color: string | null;
  icon: string | null;
}

/** Built-in statuses use the app's own labels; custom ones keep the name the workspace gave them. */
export function useStatusPresentation(wsId: string): (key: string) => StatusPresentation {
  const catalog = useIssueStatuses(wsId);
  const strings = useStrings();
  return useCallback(
    (key: string) => ({
      key,
      label: strings.status[key as keyof typeof strings.status] ?? catalog.labelOf(key),
      color: catalog.colorOf(key),
      icon: catalog.iconOf(key),
    }),
    [catalog, strings],
  );
}
