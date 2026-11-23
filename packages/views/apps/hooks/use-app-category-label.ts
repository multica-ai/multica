import { useMemo } from "react";
import type { AppCatalogEntry } from "@multica/core/types";
import { useT } from "../../i18n";
import {
  formatCategoryName,
  knownCategory,
  type AppCategoryGroup,
  type KnownAppCategory,
} from "../lib/categories";

/** Translated labels for known app categories; Composio's name, title-cased, for the rest. */
export function useAppCategoryLabel() {
  const { t } = useT("settings");
  return useMemo(() => {
    const known = (key: KnownAppCategory) => t(($) => $.connected_apps.hub.categories[key]);
    return {
      group: (group: AppCategoryGroup) => (group.known ? known(group.known) : formatCategoryName(group.name)),
      entry: (entry: AppCatalogEntry | undefined) => {
        const key = knownCategory(entry);
        if (key) return known(key);
        return entry?.category?.trim() ? formatCategoryName(entry.category) : undefined;
      },
    };
  }, [t]);
}
