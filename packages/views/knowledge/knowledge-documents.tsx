"use client";

import { useState, type ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { Button } from "@multica/ui/components/ui/button";
import { Badge } from "@multica/ui/components/ui/badge";
import { Spinner } from "@multica/ui/components/ui/spinner";
import {
  knowledgeDocumentsOptions,
  type KnowledgeBase,
  type KnowledgeDocument,
  type KnowledgeDocumentState,
} from "@multica/core/knowledge";
import { useTimeAgo, useT } from "../i18n";
import { isWebLink } from "./web-link";

type DocumentFilter = "all" | Exclude<KnowledgeDocumentState, "unknown">;

const FILTERS: DocumentFilter[] = ["all", "published", "draft", "failed"];

/**
 * The company's documents, by knowledge base and state. Drafts are listed
 * here too, marked, so a reader can tell what agents retrieve from what
 * is only waiting for a person to publish it.
 */
export function KnowledgeDocuments({
  wsId,
  knowledgeBases,
}: {
  wsId: string;
  knowledgeBases: KnowledgeBase[];
}) {
  const { t } = useT("knowledge");
  const [kb, setKb] = useState("");
  const [filter, setFilter] = useState<DocumentFilter>("all");
  const documents = useQuery(
    knowledgeDocumentsOptions(wsId, {
      kb: kb || undefined,
      state: filter === "all" ? undefined : filter,
    }),
  );

  const filterLabel = (value: DocumentFilter): string => {
    switch (value) {
      case "published":
        return t(($) => $.documents.filter.published);
      case "draft":
        return t(($) => $.documents.filter.draft);
      case "failed":
        return t(($) => $.documents.filter.failed);
      default:
        return t(($) => $.documents.filter.all);
    }
  };

  let list: ReactNode;
  if (documents.isPending) {
    list = <Spinner />;
  } else if (documents.isError) {
    list = (
      <p role="alert" className="text-caption text-destructive">
        {t(($) => $.load_error.title)}
      </p>
    );
  } else if (documents.data.state === "offline") {
    list = (
      <p role="status" className="text-caption text-muted-foreground">
        {t(($) => $.offline.title)}
      </p>
    );
  } else if (documents.data.documents.length === 0) {
    list = (
      <p className="text-caption text-muted-foreground">{t(($) => $.documents.empty)}</p>
    );
  } else {
    list = (
      <ul className="flex flex-col divide-y rounded-lg border">
        {documents.data.documents.map((doc, index) => (
          <DocumentRow
            key={doc.id || `${doc.knowledge_base}-${doc.document_id}-${index}`}
            doc={doc}
            knowledgeBaseName={
              knowledgeBases.find((base) => base.key === doc.knowledge_base)?.name ??
              doc.knowledge_base
            }
          />
        ))}
      </ul>
    );
  }

  const kbOptions: Array<{ key: string; label: string }> = [
    { key: "", label: t(($) => $.kb.all) },
    ...knowledgeBases.map((base) => ({ key: base.key, label: base.name })),
  ];

  return (
    <section className="flex flex-col gap-3">
      <div className="flex flex-wrap items-center gap-2">
        {kbOptions.map((option) => (
          <Button
            key={option.key || "all"}
            size="sm"
            variant={kb === option.key ? "secondary" : "ghost"}
            aria-pressed={kb === option.key}
            onClick={() => setKb(option.key)}
          >
            {option.label}
          </Button>
        ))}
        <span aria-hidden="true" className="mx-1 h-4 w-px bg-border" />
        {FILTERS.map((value) => (
          <Button
            key={value}
            size="sm"
            variant={filter === value ? "secondary" : "ghost"}
            aria-pressed={filter === value}
            onClick={() => setFilter(value)}
          >
            {filterLabel(value)}
          </Button>
        ))}
      </div>
      {list}
    </section>
  );
}

function DocumentRow({
  doc,
  knowledgeBaseName,
}: {
  doc: KnowledgeDocument;
  knowledgeBaseName: string;
}) {
  const { t } = useT("knowledge");
  const timeAgo = useTimeAgo();
  return (
    <li className="flex min-w-0 flex-col gap-1 px-4 py-3">
      <div className="flex min-w-0 items-center gap-2">
        <span className="min-w-0 truncate text-body font-medium">{doc.title}</span>
        {doc.state === "draft" ? (
          <Badge variant="outline">{t(($) => $.documents.filter.draft)}</Badge>
        ) : null}
        {doc.state === "failed" ? (
          <Badge variant="destructive">{t(($) => $.documents.filter.failed)}</Badge>
        ) : null}
      </div>
      <div className="flex min-w-0 flex-wrap items-center gap-x-2 text-caption text-muted-foreground">
        <span>{knowledgeBaseName}</span>
        {doc.created_at ? (
          <>
            <span aria-hidden="true">·</span>
            <span>{timeAgo(doc.created_at)}</span>
          </>
        ) : null}
        {doc.url ? (
          <>
            <span aria-hidden="true">·</span>
            {isWebLink(doc.url) ? (
              <a
                href={doc.url}
                target="_blank"
                rel="noreferrer"
                className="min-w-0 truncate text-primary hover:underline"
              >
                {doc.url}
              </a>
            ) : (
              <span className="min-w-0 truncate">{doc.url}</span>
            )}
          </>
        ) : null}
      </div>
      {doc.error ? (
        <p className="text-caption text-destructive">{doc.error}</p>
      ) : null}
    </li>
  );
}
