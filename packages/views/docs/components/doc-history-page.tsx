"use client";
import { useState } from "react";
import { motion } from "motion/react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { useReducedMotion } from "@multica/core/appearance";
import { Button } from "@multica/ui/components/ui/button";
import { UI_EASE_OUT } from "@multica/ui/lib/motion";
import {
  docDetailOptions,
  docVersionsOptions,
  docVersionOptions,
  isDocNotFound,
  useUpdateDoc,
} from "@multica/core/docs";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { useActorName } from "@multica/core/workspace/hooks";
import { ReadonlyContent } from "../../editor";
import { BreadcrumbHeader } from "../../layout/breadcrumb-header";
import { AppLink, useNavigation } from "../../navigation";
import { useT } from "../../i18n";
import { DocNotFound } from "./doc-not-found";
import { DocsGate } from "./docs-gate";

export function DocHistoryPage({ docId }: { docId: string }) {
  return (
    <DocsGate>
      <DocHistoryPageContent docId={docId} />
    </DocsGate>
  );
}

function DocHistoryPageContent({ docId }: { docId: string }) {
  const { t } = useT("docs");
  const reduceMotion = useReducedMotion();
  const wsId = useWorkspaceId();
  const p = useWorkspacePaths();
  const { push } = useNavigation();
  const { getActorName } = useActorName();
  const doc = useQuery(docDetailOptions(wsId, docId));
  const versions = useQuery(docVersionsOptions(wsId, docId));
  const [selected, setSelected] = useState<number | null>(null);
  const number = selected ?? versions.data?.[0]?.number ?? 1;
  const selectedVersion = versions.data?.find((v) => v.number === number);
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
  if (isDocNotFound(doc.error)) return <DocNotFound docId={docId} />;
  return (
    <div className="flex h-full min-h-0 flex-col">
      <BreadcrumbHeader
        segments={[
          { href: p.docs(), label: t(($) => $.kb) },
          {
            href: p.docDetail(docId),
            label: doc.data?.title || t(($) => $.untitled),
            className: "min-w-0 max-w-72 truncate",
          },
        ]}
        leaf={<span className="shrink-0">{t(($) => $.history.title)}</span>}
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
                className={`mb-1 block w-full rounded-md p-3 text-left text-body transition-colors ${number === v.number ? "bg-surface-selected text-surface-selected-foreground" : "hover:bg-surface-hover"}`}
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
                  {(v.contributors.length > 1
                    ? v.contributors
                    : [{ type: v.authorType, id: v.authorId }]
                  )
                    .map((c) => getActorName(c.type, c.id))
                    .join(", ")}
                </span>
                {v.message && (
                  <span className="block truncate text-caption text-muted-foreground">
                    {v.message}
                  </span>
                )}
                {v.docThreadId && (
                  <span className="block truncate text-caption text-muted-foreground">
                    {t(($) => $.history.from_discussion, { quote: v.docThreadQuote ?? "" })}
                  </span>
                )}
                <span className="block text-caption text-muted-foreground">
                  {new Date(v.updatedAt).toLocaleString()}
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
              <motion.div
                key={version.data.number}
                initial={reduceMotion ? false : { opacity: 0 }}
                animate={{ opacity: 1 }}
                transition={{ duration: reduceMotion ? 0 : 0.12, ease: UI_EASE_OUT }}
              >
                <div className="mb-6 flex items-start justify-between gap-3">
                  <h1 className="min-w-0 flex-1 text-display-sm font-semibold leading-snug wrap-anywhere">
                    {version.data.title || t(($) => $.untitled)}
                  </h1>
                  <Button
                    variant="outline"
                    size="sm"
                    className="shrink-0"
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
                {selectedVersion?.docThreadId && (
                  <AppLink
                    href={`${p.docDetail(docId)}?thread=${encodeURIComponent(selectedVersion.docThreadId)}`}
                    className="-mt-4 mb-6 block truncate text-caption text-muted-foreground hover:text-foreground hover:underline"
                  >
                    {t(($) => $.history.from_discussion, { quote: selectedVersion.docThreadQuote ?? "" })}
                  </AppLink>
                )}
                <ReadonlyContent content={version.data.content ?? ""} />
              </motion.div>
            ) : (
              t(($) => $.history.loading)
            )}
          </div>
        </div>
      </div>
    </div>
  );
}
