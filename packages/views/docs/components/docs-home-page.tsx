"use client";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { docTreeOptions } from "@multica/core/docs";
import { FileText, Folder } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { TruncatedTooltip } from "@multica/ui/components/common/truncated-tooltip";
import { PageHeader } from "../../layout/page-header";
import { AppLink } from "../../navigation";
import { useT } from "../../i18n";
import { DocsGate } from "./docs-gate";
import { usePrefetchDoc, useWarmDocPage } from "./use-prefetch-doc";
export function DocsHomePage({
  folderId = null,
}: {
  folderId?: string | null;
}) {
  return (
    <DocsGate>
      <DocsHomePageContent folderId={folderId} />
    </DocsGate>
  );
}

function DocsHomePageContent({ folderId }: { folderId: string | null }) {
  const { t } = useT("docs");
  const wsId = useWorkspaceId();
  const p = useWorkspacePaths();
  const tree = useQuery(docTreeOptions(wsId));
  const prefetchDoc = usePrefetchDoc();
  const folders =
    tree.data?.folders.filter((f) => f.parentId === folderId) ?? [];
  const docs = tree.data?.docs.filter((d) => d.folderId === folderId) ?? [];
  useWarmDocPage(docs[0]?.id ?? tree.data?.docs[0]?.id);
  const headerTitle = folderId
    ? tree.data?.folders.find((f) => f.id === folderId)?.title
    : t(($) => $.kb);
  return (
    <div className="flex h-full min-h-0 flex-col">
      <PageHeader>
        <TruncatedTooltip content={headerTitle} side="bottom" focusable>
          <h1 className="min-w-0 truncate text-body font-medium">
            {headerTitle}
          </h1>
        </TruncatedTooltip>
      </PageHeader>
      <div className="min-h-0 flex-1 overflow-auto p-6">
        <div className="mx-auto max-w-3xl">
          {tree.isError ? (
            <Button onClick={() => void tree.refetch()}>
              {t(($) => $.editor.retry)}
            </Button>
          ) : tree.isPending ? (
            t(($) => $.history.loading)
          ) : !folders.length && !docs.length ? (
            <p className="text-body text-muted-foreground">
              {t(($) => $.tree.empty)}
            </p>
          ) : (
            <ul>
              {folders.map((f) => (
                <li key={f.id}>
                  <TruncatedTooltip content={f.title}>
                    <AppLink
                      href={p.docFolder(f.id)}
                      className="flex items-center gap-3 rounded-md p-3 text-body hover:bg-surface-hover"
                    >
                      <Folder className="size-4 shrink-0" />
                      <span className="min-w-0 truncate">{f.title}</span>
                    </AppLink>
                  </TruncatedTooltip>
                </li>
              ))}
              {docs.map((d) => {
                const title = d.title || t(($) => $.untitled);
                return (
                  <li key={d.id}>
                    <TruncatedTooltip content={title}>
                      <AppLink
                        href={p.docDetail(d.id)}
                        {...prefetchDoc(d.id)}
                        className="flex items-center gap-3 rounded-md p-3 text-body hover:bg-surface-hover"
                      >
                        <FileText className="size-4 shrink-0" />
                        <span className="min-w-0 truncate">{title}</span>
                      </AppLink>
                    </TruncatedTooltip>
                  </li>
                );
              })}
            </ul>
          )}
        </div>
      </div>
    </div>
  );
}
