"use client";

import { useState, type ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { Button } from "@multica/ui/components/ui/button";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@multica/ui/components/ui/alert-dialog";
import { Spinner } from "@multica/ui/components/ui/spinner";
import {
  knowledgeDocumentsOptions,
  usePublishKnowledgeDocument,
  useDiscardKnowledgeDocument,
  type KnowledgeBase,
  type KnowledgeDocument,
} from "@multica/core/knowledge";
import { useT, useTimeAgo } from "../i18n";
import { isWebLink } from "./web-link";

/**
 * Drafts agents and members saved, waiting for an owner or admin. Publishing
 * makes a draft searchable; discarding deletes it. Both wait for the server,
 * so a row stays until the change has landed.
 */
export function KnowledgeDrafts({
  wsId,
  knowledgeBases,
  canManage,
}: {
  wsId: string;
  knowledgeBases: KnowledgeBase[];
  canManage: boolean;
}) {
  const { t } = useT("knowledge");
  const timeAgo = useTimeAgo();
  const drafts = useQuery(knowledgeDocumentsOptions(wsId, { state: "draft" }));
  const publish = usePublishKnowledgeDocument(wsId);
  const discard = useDiscardKnowledgeDocument(wsId);
  const [toDiscard, setToDiscard] = useState<KnowledgeDocument | null>(null);

  const authorLabel = (kind: string): string => {
    switch (kind) {
      case "agent":
        return t(($) => $.drafts.author.agent);
      case "member":
        return t(($) => $.drafts.author.member);
      case "system":
        return t(($) => $.drafts.author.system);
      default:
        return "";
    }
  };

  const onPublish = (doc: KnowledgeDocument) =>
    publish.mutate(
      { id: doc.id },
      {
        onSuccess: () => toast.success(t(($) => $.toast.published)),
        onError: () => toast.error(t(($) => $.toast.publish_failed)),
      },
    );

  const confirmDiscard = async () => {
    if (!toDiscard) return;
    try {
      await discard.mutateAsync({ id: toDiscard.id });
      toast.success(t(($) => $.toast.discarded));
      setToDiscard(null);
    } catch {
      toast.error(t(($) => $.toast.discard_failed));
    }
  };

  let list: ReactNode;
  if (drafts.isPending) {
    list = <Spinner />;
  } else if (drafts.isError) {
    list = (
      <p role="alert" className="text-caption text-destructive">
        {t(($) => $.load_error.title)}
      </p>
    );
  } else if (drafts.data.state === "offline") {
    list = (
      <p role="status" className="text-caption text-muted-foreground">
        {t(($) => $.offline.title)}
      </p>
    );
  } else if (drafts.data.documents.length === 0) {
    list = (
      <p className="text-caption text-muted-foreground">{t(($) => $.drafts.empty)}</p>
    );
  } else {
    list = (
      <ul className="flex flex-col divide-y rounded-lg border">
        {drafts.data.documents.map((doc, index) => {
          const busy = doc.id !== "" && (
            (publish.isPending && publish.variables?.id === doc.id) ||
            (discard.isPending && discard.variables?.id === doc.id)
          );
          const actionable = canManage && doc.id !== "";
          const kbName =
            knowledgeBases.find((base) => base.key === doc.knowledge_base)?.name ??
            doc.knowledge_base;
          const author = authorLabel(doc.created_by_type);
          return (
            <li
              key={doc.id || `${doc.knowledge_base}-${doc.document_id}-${index}`}
              className="flex min-w-0 items-center gap-3 px-4 py-3"
            >
              <div className="flex min-w-0 flex-1 flex-col gap-1">
                <span className="min-w-0 truncate text-body font-medium">{doc.title}</span>
                <div className="flex min-w-0 flex-wrap items-center gap-x-2 text-caption text-muted-foreground">
                  <span>{kbName}</span>
                  {author ? (
                    <>
                      <span aria-hidden="true">·</span>
                      <span>{author}</span>
                    </>
                  ) : null}
                  {doc.created_at ? (
                    <>
                      <span aria-hidden="true">·</span>
                      <span>{timeAgo(doc.created_at)}</span>
                    </>
                  ) : null}
                  {doc.url && isWebLink(doc.url) ? (
                    <>
                      <span aria-hidden="true">·</span>
                      <a
                        href={doc.url}
                        target="_blank"
                        rel="noreferrer"
                        className="min-w-0 truncate text-primary hover:underline"
                      >
                        {doc.url}
                      </a>
                    </>
                  ) : null}
                </div>
                {doc.error ? (
                  <p className="text-caption text-destructive">{doc.error}</p>
                ) : null}
              </div>
              {actionable ? (
                <div className="flex shrink-0 items-center gap-2">
                  <Button
                    size="sm"
                    variant="outline"
                    disabled={busy}
                    aria-busy={busy || undefined}
                    onClick={() => onPublish(doc)}
                  >
                    {t(($) => $.drafts.publish)}
                  </Button>
                  <Button
                    size="sm"
                    variant="ghost"
                    disabled={busy}
                    onClick={() => setToDiscard(doc)}
                  >
                    {t(($) => $.drafts.discard)}
                  </Button>
                </div>
              ) : null}
            </li>
          );
        })}
      </ul>
    );
  }

  return (
    <section className="flex flex-col gap-3">
      {list}
      <AlertDialog
        open={toDiscard !== null}
        onOpenChange={(open) => {
          if (!open) setToDiscard(null);
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t(($) => $.discard_title)}</AlertDialogTitle>
            <AlertDialogDescription>{t(($) => $.discard_description)}</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t(($) => $.cancel)}</AlertDialogCancel>
            <AlertDialogAction
              variant="destructive"
              disabled={discard.isPending}
              onClick={() => void confirmDiscard()}
            >
              {t(($) => $.drafts.discard)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </section>
  );
}
