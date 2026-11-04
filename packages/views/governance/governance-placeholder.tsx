"use client";

import { Empty, EmptyHeader, EmptyTitle } from "@multica/ui/components/ui/empty";
import { useT } from "../i18n";

/** The empty state of a governance tab whose content is not built yet. */
export function GovernancePlaceholder() {
  const { t } = useT("governance");
  return (
    <Empty>
      <EmptyHeader>
        <EmptyTitle>{t(($) => $.placeholder.title)}</EmptyTitle>
      </EmptyHeader>
    </Empty>
  );
}
