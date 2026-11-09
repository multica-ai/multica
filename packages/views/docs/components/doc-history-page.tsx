"use client";
import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { Button } from "@multica/ui/components/ui/button";
import {
  docDetailOptions,
  docVersionsOptions,
  docVersionOptions,
  useUpdateDoc,
} from "@multica/core/docs";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { useActorName } from "@multica/core/workspace/hooks";
import { ReadonlyContent } from "../../editor";
import { BreadcrumbHeader } from "../../layout/breadcrumb-header";
import { useNavigation } from "../../navigation";
import { useT } from "../../i18n";
export function DocHistoryPage({ docId }: { docId: string }) {
  const { t } = useT("docs");
  const wsId = useWorkspaceId();
  const p = useWorkspacePaths();
  const { push } = useNavigation();
  const { getActorName } = useActorName();
  const doc = useQuery(docDetailOptions(wsId, docId));
  const versions = useQuery(docVersionsOptions(wsId, docId));
  const [selected, setSelected] = useState<number | null>(null);
  const number = selected ?? versions.data?.[0]?.number ?? 1;
  const version = useQuery({
    ...docVersionOptions(wsId, docId, number),
    enabled: !!versions.data?.length,
  });
  const update = useUpdateDoc();
  const restore = async () => {
    if (!doc.data || !version.data) return;
    try {
      await update.mutateAsync({
        id: docId,
        baseRevision: doc.data.revision,
        action: "restore",
        actionRef: number,
      });
      push(p.docDetail(docId));
    } catch (error) {
      toast.error(
        error instanceof Error ? error.message : t(($) => $.errors.save),
      );
      void doc.refetch();
    }
  };
  return (
    <div className="flex h-full min-h-0 flex-col">
      <BreadcrumbHeader
        segments={[
          { href: p.docs(), label: t(($) => $.kb) },
          {
            href: p.docDetail(docId),
            label: doc.data?.title || t(($) => $.untitled),
          },
        ]}
        leaf={t(($) => $.history.title)}
      />
      <div className="flex min-h-0 flex-1 flex-col md:flex-row">
        <div className="max-h-52 shrink-0 overflow-auto border-b p-3 md:max-h-none md:w-60 md:border-r md:border-b-0">
          {versions.isError ? (
            <Button onClick={() => void versions.refetch()}>
              {t(($) => $.editor.retry)}
            </Button>
          ) : (
            versions.data?.map((v) => (
              <button
                type="button"
                key={v.number}
                onClick={() => setSelected(v.number)}
                aria-pressed={number === v.number}
                className={`mb-1 block w-full rounded-md p-3 text-left text-body ${number === v.number ? "bg-surface-selected text-surface-selected-foreground" : "hover:bg-surface-hover"}`}
              >
                <span className="font-medium">
                  {t(($) => $.version_number, { number: v.number })}
                </span>
                <span className="ml-2 text-caption">
                  {v.number === doc.data?.versionCount
                    ? t(($) => $.history.current)
                    : ""}
                </span>
                <span className="mt-1 block truncate text-caption">
                  {getActorName(v.authorType, v.authorId)}
                </span>
                <span className="block text-caption text-muted-foreground">
                  {new Date(v.createdAt).toLocaleString()}
                </span>
              </button>
            ))
          )}
        </div>
        <div className="min-h-0 min-w-0 flex-1 overflow-auto p-6">
          <div className="mx-auto max-w-3xl">
            {version.isError || (!version.isPending && !version.data) ? (
              <Button onClick={() => void version.refetch()}>
                {t(($) => $.editor.retry)}
              </Button>
            ) : version.data ? (
              <>
                <div className="mb-6 flex items-center justify-between gap-3">
                  <h1 className="text-display-sm font-semibold">
                    {version.data.title || t(($) => $.untitled)}
                  </h1>
                  <Button
                    variant="outline"
                    size="sm"
                    disabled={
                      update.isPending ||
                      !doc.data ||
                      number === doc.data.versionCount ||
                      version.data.content === undefined
                    }
                    onClick={() => void restore()}
                  >
                    {t(($) => $.history.restore)}
                  </Button>
                </div>
                <ReadonlyContent content={version.data.content ?? ""} />
              </>
            ) : (
              t(($) => $.history.loading)
            )}
          </div>
        </div>
      </div>
    </div>
  );
}
