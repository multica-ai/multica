"use client";
import { useFeatureEnabled } from "@multica/core/config";
import { DOCS_V1_FLAG } from "@multica/core/feature-flags";
import { FileText } from "@multica/ui/icons";
import { CollectionPageState } from "../../layout/collection-page";
import { useT } from "../../i18n";

/**
 * Renders the Docs pages only while the server's docs_v1 flag is on. A
 * typed or restored /docs URL otherwise lands on an empty state instead of a
 * page whose requests the server answers 403 docs_disabled.
 */
export function DocsGate({ children }: { children: React.ReactNode }) {
  const { t } = useT("docs");
  const enabled = useFeatureEnabled(DOCS_V1_FLAG, false);
  if (enabled) return <>{children}</>;
  return (
    <div className="flex h-full min-h-0 flex-col justify-center">
      <CollectionPageState
        icon={FileText}
        title={t(($) => $.unavailable.title)}
        description={t(($) => $.unavailable.description)}
      />
    </div>
  );
}
