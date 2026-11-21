"use client";

import { useQuery } from "@tanstack/react-query";
import { FileText } from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";
import { useFeatureEnabled } from "@multica/core/config";
import { docTreeOptions } from "@multica/core/docs";
import { DOCS_V1_FLAG } from "@multica/core/feature-flags";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { AppLink } from "../../navigation";
import { useT } from "../../i18n";

/**
 * Navigable doc chip for `mention://doc/<id>` and pasted doc URLs. Same shape
 * and width cap as ProjectChip, so an issue that cites a doc and a project
 * reads as one family. The title comes from the doc tree, so a rename shows
 * everywhere the doc is cited; the label written into the Markdown is only the
 * fallback for a doc this workspace no longer has.
 */
export function DocMentionCard({
  docId,
  fallbackLabel,
}: {
  docId: string;
  fallbackLabel?: string;
}) {
  const wsId = useWorkspaceId();
  const p = useWorkspacePaths();
  const { t } = useT("docs");
  // While docs_v1 is off the chip keeps its saved label instead of asking a
  // server that answers 403.
  const docsEnabled = useFeatureEnabled(DOCS_V1_FLAG, false);
  const { data: tree } = useQuery({ ...docTreeOptions(wsId), enabled: docsEnabled });
  const doc = tree?.docs.find((d) => d.id === docId);
  const title = doc
    ? doc.title || t(($) => $.untitled)
    : fallbackLabel || t(($) => $.untitled);
  return (
    <AppLink
      href={p.docDetail(docId)}
      newTabTitle={title}
      className="doc-mention inline-flex max-w-full"
    >
      <span className="doc-chip mx-0.5 inline-flex min-w-0 max-w-[min(18rem,100%)] items-center gap-1.5 rounded-md border px-2 py-0.5 text-caption">
        <FileText className="size-3.5 shrink-0 text-muted-foreground" />
        <span
          className={cn(
            "min-w-0 truncate",
            doc ? "text-foreground" : "text-muted-foreground",
          )}
        >
          {title}
        </span>
      </span>
    </AppLink>
  );
}
