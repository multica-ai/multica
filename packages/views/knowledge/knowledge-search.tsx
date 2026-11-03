"use client";

import { useState, type FormEvent, type ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { Search } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Spinner } from "@multica/ui/components/ui/spinner";
import {
  knowledgeKindsOptions,
  knowledgeSearchOptions,
  type KnowledgeBase,
  type KnowledgeHit,
} from "@multica/core/knowledge";
import { useT } from "../i18n";
import {
  ASK_SECRETARY_RESULT_LIMIT,
  formatSecretaryResult,
  useAskSecretary,
} from "./ask-secretary";
import { AddEvalCaseButton } from "./knowledge-add-eval-case";
import { KnowledgeKindPicker } from "./knowledge-kind-picker";
import { isWebLink } from "./web-link";

/**
 * Retrieval only: the server returns the closest passages however weak, so
 * each result shows its score and source for the reader to judge.
 *
 * With a Secretary in the workspace, a result set can be handed to its chat:
 * the top results go into a prompt that asks for citations by title and link.
 */
export function KnowledgeSearch({
  wsId,
  knowledgeBases,
  secretaryAgentId,
}: {
  wsId: string;
  knowledgeBases: KnowledgeBase[];
  secretaryAgentId: string | null;
}) {
  const { t } = useT("knowledge");
  const [draft, setDraft] = useState("");
  const [submitted, setSubmitted] = useState("");
  const [kind, setKind] = useState("");
  const search = useQuery(knowledgeSearchOptions(wsId, { q: submitted, kind: kind || undefined }));
  const kinds = useQuery(knowledgeKindsOptions(wsId));
  const askSecretary = useAskSecretary();

  const names = new Map(knowledgeBases.map((kb) => [kb.key, kb.name]));
  const askable =
    !!secretaryAgentId &&
    !!search.data &&
    search.data.state === "ready" &&
    search.data.hits.length > 0;

  const submit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    setSubmitted(draft.trim());
  };

  const handleAskSecretary = () => {
    if (!secretaryAgentId || !search.data) return;
    const results = search.data.hits
      .slice(0, ASK_SECRETARY_RESULT_LIMIT)
      .map((hit, index) =>
        formatSecretaryResult(hit, index, names.get(hit.knowledge_base) ?? hit.knowledge_base),
      )
      .join("\n\n");
    askSecretary(
      secretaryAgentId,
      t(($) => $.ask_secretary.prompt, { question: submitted, results }),
    );
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
              wsId={wsId}
              question={submitted}
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
        <KnowledgeKindPicker kinds={kinds.data?.kinds ?? []} value={kind} onValueChange={setKind} />
        <Button type="submit" variant="outline" disabled={draft.trim() === ""}>
          {t(($) => $.search.submit)}
        </Button>
      </form>
      {submitted ? (
        <div className="flex flex-col gap-2">
          <div className="flex min-w-0 items-center justify-between gap-2">
            <h2 className="text-caption font-medium text-muted-foreground">
              {t(($) => $.search.results)}
            </h2>
            {askable ? (
              <Button type="button" variant="outline" size="sm" onClick={handleAskSecretary}>
                {t(($) => $.ask_secretary.button)}
              </Button>
            ) : null}
          </div>
          {results}
        </div>
      ) : null}
    </section>
  );
}

function SearchHit({
  wsId,
  question,
  hit,
  knowledgeBaseName,
}: {
  wsId: string;
  question: string;
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
        <span className="ml-auto">
          <AddEvalCaseButton
            wsId={wsId}
            question={question}
            documentId={hit.document_id}
            documentTitle={hit.title || hit.document_id}
          />
        </span>
      </div>
    </li>
  );
}
