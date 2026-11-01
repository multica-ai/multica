"use client";

import { useState, type FormEvent, type ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { Search } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Spinner } from "@multica/ui/components/ui/spinner";
import {
  knowledgeSearchOptions,
  type KnowledgeBase,
  type KnowledgeHit,
} from "@multica/core/knowledge";
import { useT } from "../i18n";
import { isWebLink } from "./web-link";

/**
 * Retrieval only: the server returns the closest passages however weak, so
 * each result shows its score and source for the reader to judge.
 */
export function KnowledgeSearch({
  wsId,
  knowledgeBases,
}: {
  wsId: string;
  knowledgeBases: KnowledgeBase[];
}) {
  const { t } = useT("knowledge");
  const [draft, setDraft] = useState("");
  const [submitted, setSubmitted] = useState("");
  const search = useQuery(knowledgeSearchOptions(wsId, { q: submitted }));

  const names = new Map(knowledgeBases.map((kb) => [kb.key, kb.name]));

  const submit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    setSubmitted(draft.trim());
  };

  let results: ReactNode = null;
  if (submitted) {
    if (search.isPending) {
      results = <Spinner />;
    } else if (search.isError) {
      results = (
        <p role="alert" className="text-caption text-destructive">
          {t(($) => $.search.error)}
        </p>
      );
    } else if (search.data.state === "offline") {
      results = (
        <p role="status" className="text-caption text-muted-foreground">
          {t(($) => $.offline.title)}
        </p>
      );
    } else if (search.data.hits.length === 0) {
      results = (
        <p className="text-caption text-muted-foreground">
          {t(($) => $.search.no_results)}
        </p>
      );
    } else {
      results = (
        <ul className="flex flex-col divide-y rounded-lg border">
          {search.data.hits.map((hit, index) => (
            <SearchHit
              key={`${hit.knowledge_base}-${hit.document_id}-${index}`}
              hit={hit}
              knowledgeBaseName={names.get(hit.knowledge_base) ?? hit.knowledge_base}
            />
          ))}
        </ul>
      );
    }
  }

  return (
    <section className="flex flex-col gap-3">
      <form role="search" onSubmit={submit} className="flex items-center gap-2">
        <div className="relative min-w-0 flex-1">
          <Search
            aria-hidden="true"
            className="pointer-events-none absolute left-2.5 top-1/2 size-4 -translate-y-1/2 text-muted-foreground"
          />
          <Input
            type="search"
            value={draft}
            onChange={(event) => setDraft(event.target.value)}
            placeholder={t(($) => $.search.placeholder)}
            aria-label={t(($) => $.search.label)}
            className="pl-8"
          />
        </div>
        <Button type="submit" variant="outline" disabled={draft.trim() === ""}>
          {t(($) => $.search.submit)}
        </Button>
      </form>
      {submitted ? (
        <div className="flex flex-col gap-2">
          <h2 className="text-caption font-medium text-muted-foreground">
            {t(($) => $.search.results)}
          </h2>
          {results}
        </div>
      ) : null}
    </section>
  );
}

function SearchHit({
  hit,
  knowledgeBaseName,
}: {
  hit: KnowledgeHit;
  knowledgeBaseName: string;
}) {
  const { t } = useT("knowledge");
  return (
    <li className="flex flex-col gap-1 px-4 py-3">
      <div className="flex min-w-0 items-baseline justify-between gap-3">
        <h3 className="min-w-0 truncate text-body font-medium">{hit.title}</h3>
        <span className="shrink-0 text-caption tabular-nums text-muted-foreground">
          {t(($) => $.search.score, { score: hit.score.toFixed(2) })}
        </span>
      </div>
      <p className="line-clamp-3 text-caption text-muted-foreground">{hit.content}</p>
      <div className="flex min-w-0 flex-wrap items-center gap-x-2 text-caption text-muted-foreground">
        <span>{knowledgeBaseName}</span>
        {hit.source ? (
          <>
            <span aria-hidden="true">·</span>
            <span className="text-faint-foreground">{t(($) => $.search.source)}</span>
            {isWebLink(hit.source) ? (
              <a
                href={hit.source}
                target="_blank"
                rel="noreferrer"
                className="min-w-0 truncate text-primary hover:underline"
              >
                {hit.source}
              </a>
            ) : (
              <span className="min-w-0 truncate">{hit.source}</span>
            )}
          </>
        ) : null}
      </div>
    </li>
  );
}
