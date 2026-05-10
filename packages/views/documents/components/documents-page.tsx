"use client";

import { useT } from "../../i18n";

// Right-pane placeholder for /documents (no document selected). The shell
// in the parent layout owns the page header and tree sidebar.
export default function DocumentsPage() {
  const { t } = useT("documents");
  return (
    <div className="flex flex-1 items-center justify-center text-sm text-muted-foreground">
      {t(($) => $.page.select_prompt)}
    </div>
  );
}
