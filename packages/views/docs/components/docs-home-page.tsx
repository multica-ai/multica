"use client";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { docTreeOptions } from "@multica/core/docs";
import { FileText, Folder } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { PageHeader } from "../../layout/page-header";
import { AppLink } from "../../navigation";
import { useT } from "../../i18n";
export function DocsHomePage({
  folderId = null,
}: {
  folderId?: string | null;
}) {
  const { t } = useT("docs");
  const wsId = useWorkspaceId();
  const p = useWorkspacePaths();
  const tree = useQuery(docTreeOptions(wsId));
  const folders =
    tree.data?.folders.filter((f) => f.parentId === folderId) ?? [];
  const docs = tree.data?.docs.filter((d) => d.folderId === folderId) ?? [];
  return (
    <div className="flex h-full min-h-0 flex-col">
      <PageHeader>
        <h1 className="text-body font-medium">
          {folderId
            ? tree.data?.folders.find((f) => f.id === folderId)?.title
            : t(($) => $.kb)}
        </h1>
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
                  <AppLink
                    href={p.docFolder(f.id)}
                    className="flex items-center gap-3 rounded-md p-3 text-body hover:bg-surface-hover"
                  >
                    <Folder className="size-4" />
                    <span className="truncate">{f.title}</span>
                  </AppLink>
                </li>
              ))}
              {docs.map((d) => (
                <li key={d.id}>
                  <AppLink
                    href={p.docDetail(d.id)}
                    className="flex items-center gap-3 rounded-md p-3 text-body hover:bg-surface-hover"
                  >
                    <FileText className="size-4" />
                    <span className="truncate">
                      {d.title || t(($) => $.untitled)}
                    </span>
                  </AppLink>
                </li>
              ))}
            </ul>
          )}
        </div>
      </div>
    </div>
  );
}
