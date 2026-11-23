import { useMemo } from "react";
import type { AppCatalogEntry } from "@multica/core/types";
import { useT } from "../../i18n";
import { knownCategory, type AppCategoryGroup, type KnownAppCategory } from "../lib/categories";

/** Labels for app categories: translated when Multica knows them, Composio's name otherwise. */
export function useAppCategoryLabel() {
  const { t } = useT("settings");
  return useMemo(() => {
    const known = (key: KnownAppCategory) => t(($) => $.connected_apps.hub.categories[key]);
    return {
      group: (group: AppCategoryGroup) => (group.known ? known(group.known) : group.name),
      entry: (entry: AppCatalogEntry | undefined) => {
        const key = knownCategory(entry);
        return key ? known(key) : entry?.category || undefined;
      },
    };
  }, [t]);
}
