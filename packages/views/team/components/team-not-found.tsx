"use client";

import { AlertCircle } from "@multica/ui/icons";
import { useT } from "../../i18n";

export function TeamNotFound() {
  const { t } = useT("team");
  return (
    <div className="flex flex-1 flex-col items-center justify-center gap-3 px-6 text-center text-muted-foreground">
      <AlertCircle className="h-8 w-8" />
      <p className="text-body">{t(($) => $.detail.not_found)}</p>
    </div>
  );
}
