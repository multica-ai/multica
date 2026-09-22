"use client";

import { useState } from "react";
import { ArrowLeft, Search } from "lucide-react";
import { toast } from "sonner";
import { useAnswerKnowledge } from "@multica/core/knowledge";
import type { KnowledgeAnswerResponse } from "@multica/core/types";
import { useWorkspacePaths } from "@multica/core/paths";
import { Button } from "@multica/ui/components/ui/button";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { AppLink, useNavigation } from "../navigation";
import { useT } from "../i18n";

export function KnowledgeAskPage({ baseId }: { baseId: string }) {
  const { t } = useT("knowledge");
  const paths = useWorkspacePaths();
  const navigation = useNavigation();
  const answer = useAnswerKnowledge(baseId);
  const [question, setQuestion] = useState("");
  const [result, setResult] = useState<KnowledgeAnswerResponse | null>(null);

  async function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!question.trim()) return;
    try { setResult(await answer.mutateAsync({ question: question.trim(), limit: 10, mode: "hybrid" })); }
    catch (error) { toast.error(error instanceof Error ? error.message : t(($) => $.error)); }
  }

	const citedIds = new Set(result?.citations.map((citation) => citation.id) ?? []);
	const citedSources = result?.citations.length
		? result.search.results.filter((source) => citedIds.has(source.chunk_id))
		: result?.search.results ?? [];
	const sourceNumbers = new Map(citedSources.map((source, index) => [source.chunk_id, index + 1]));
	const answerParagraphs = result?.answer.split(/\n{2,}/).map((paragraph) => paragraph.trim()).filter(Boolean) ?? [];
	const paragraphCitations = new Map<number, string[]>();
	result?.citations.forEach((citation, index) => {
		const paragraph = citation.answer_paragraph ?? citation.paragraph ?? (answerParagraphs.length === 1 ? 1 : 0);
		const ids = paragraphCitations.get(paragraph) ?? [];
		ids.push(citation.id || `citation-${index}`);
		paragraphCitations.set(paragraph, ids);
	});
  return <div className="flex h-full min-h-0 flex-col"><header className="flex items-center gap-2 border-b px-4 py-3"><Button variant="ghost" size="icon-sm" aria-label={t(($) => $.back)} onClick={() => navigation.push(paths.knowledgeBase(baseId))}><ArrowLeft className="size-4" /></Button><h1 className="text-title font-semibold">{t(($) => $.ask)}</h1></header><div className="min-h-0 flex-1 overflow-auto p-6"><div className="mx-auto max-w-3xl space-y-6"><form onSubmit={submit} className="space-y-3"><Textarea value={question} onChange={(event) => setQuestion(event.target.value)} placeholder={t(($) => $.ask_placeholder)} rows={5} autoFocus /><Button type="submit" disabled={answer.isPending || !question.trim()}><Search className="size-4" />{answer.isPending ? t(($) => $.answering) : t(($) => $.ask)}</Button></form>{result && <section className="space-y-4 rounded-xl border bg-card p-5"><h2 className="text-heading font-semibold">{t(($) => $.answer)}</h2>{result.insufficient_evidence && <p className="rounded-lg bg-muted p-3 text-caption text-muted-foreground">{t(($) => $.insufficient)}</p>}{result.answer ? <div className="space-y-3 text-body leading-relaxed">{answerParagraphs.map((paragraph, index) => <p key={`${index}-${paragraph.slice(0, 24)}`}>{paragraph}{(paragraphCitations.get(index + 1) ?? []).map((citationId) => { const sourceNumber = sourceNumbers.get(citationId); const source = citedSources.find((item) => item.chunk_id === citationId); return sourceNumber && source ? <AppLink key={citationId} href={paths.knowledgeDocument(baseId, source.document_id)} className="ml-1 align-super text-micro font-semibold text-primary underline" aria-label={t(($) => $.citation_marker, { index: sourceNumber })}>[{sourceNumber}]</AppLink> : null; })}</p>)}</div> : <p className="text-caption text-muted-foreground">{t(($) => $.no_answer)}</p>}{result.generation_error && <p role="alert" className="text-caption text-destructive">{result.generation_error}</p>}<div><h3 className="mb-2 text-body font-medium">{t(($) => $.citations)}</h3><div className="space-y-2">{citedSources.map((source, index) => <AppLink key={source.chunk_id} href={paths.knowledgeDocument(baseId, source.document_id)} className="block rounded-lg border p-3 hover:bg-accent/40"><div className="flex items-center gap-2"><span className="rounded-sm bg-muted px-1.5 py-0.5 text-micro">[{index + 1}]</span><p className="text-caption font-medium text-primary underline">{source.title}</p></div><p className="mt-1 line-clamp-3 text-micro text-muted-foreground">{source.text}</p></AppLink>)}</div></div></section>}</div></div></div>;
}
