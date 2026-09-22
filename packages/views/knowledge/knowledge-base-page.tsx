"use client";

import { useRef, useState } from "react";
import { ArrowLeft, ExternalLink, FileText, GitBranch, RefreshCw, Search, Settings, Upload } from "lucide-react";
import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { knowledgeBaseOptions, knowledgeDocumentListOptions, knowledgeJobListOptions, useCancelKnowledgeJob, useCreateKnowledgeIndex, useCreateKnowledgeURL, useSearchKnowledge, useUploadKnowledgeFile } from "@multica/core/knowledge";
import type { KnowledgeSearchResponse } from "@multica/core/types";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { AppLink, useNavigation } from "../navigation";
import { useT } from "../i18n";

const KNOWLEDGE_JOB_STAGES = ["fetch", "parse", "chunk", "embed", "extract", "activate"];

function jobProgress(job: { stage: string; status: string; progress: Record<string, unknown> }) {
  const explicit = job.progress.percent;
  if (typeof explicit === "number" && Number.isFinite(explicit)) return Math.max(0, Math.min(100, Math.round(explicit)));
  if (job.status === "succeeded") return 100;
  const stageIndex = KNOWLEDGE_JOB_STAGES.indexOf(job.stage);
  return stageIndex < 0 ? 8 : Math.round(((stageIndex + 0.5) / KNOWLEDGE_JOB_STAGES.length) * 100);
}

export function KnowledgeBasePage({ baseId }: { baseId: string }) {
  const { t } = useT("knowledge");
  const workspaceId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const navigation = useNavigation();
  const base = useQuery(knowledgeBaseOptions(workspaceId, baseId));
  const documents = useInfiniteQuery(knowledgeDocumentListOptions(workspaceId, baseId));
  const jobs = useInfiniteQuery(knowledgeJobListOptions(workspaceId, baseId));
  const documentItems = documents.data?.pages.flatMap((page) => page.documents) ?? [];
  const jobItems = jobs.data?.pages.flatMap((page) => page.jobs) ?? [];
  const addURL = useCreateKnowledgeURL(baseId);
  const upload = useUploadKnowledgeFile(baseId);
  const rebuild = useCreateKnowledgeIndex(baseId);
  const cancelJob = useCancelKnowledgeJob(baseId);
  const search = useSearchKnowledge(baseId);
  const fileRef = useRef<HTMLInputElement>(null);
  const [url, setURL] = useState("");
  const [query, setQuery] = useState("");
  const [searchMode, setSearchMode] = useState<"hybrid" | "keyword" | "semantic">("hybrid");
  const [searchLimit, setSearchLimit] = useState("10");
  const [searchResult, setSearchResult] = useState<KnowledgeSearchResponse | null>(null);

  if (base.isPending) return <div className="p-6" role="status">{t(($) => $.loading)}</div>;
  if (base.isError || !base.data) return <div className="space-y-3 p-6" role="alert"><p>{base.error?.message ?? t(($) => $.error)}</p><Button variant="outline" onClick={() => base.refetch()}>{t(($) => $.retry)}</Button></div>;

  async function addSource(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    try {
      await addURL.mutateAsync({ url });
      setURL("");
      toast.success(t(($) => $.source_added));
    } catch (error) { toast.error(error instanceof Error ? error.message : t(($) => $.error)); }
  }

  async function chooseFile(event: React.ChangeEvent<HTMLInputElement>) {
    const file = event.target.files?.[0];
    event.target.value = "";
    if (!file) return;
    try { await upload.mutateAsync({ file }); toast.success(t(($) => $.source_added)); }
    catch (error) { toast.error(error instanceof Error ? error.message : t(($) => $.error)); }
  }

  async function runSearch(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!query.trim()) return;
    const limit = Math.max(1, Math.min(50, Number.parseInt(searchLimit, 10) || 10));
    try { setSearchResult(await search.mutateAsync({ query: query.trim(), limit, mode: searchMode })); }
    catch (error) { toast.error(error instanceof Error ? error.message : t(($) => $.error)); }
  }

  async function rebuildIndex() {
    try { await rebuild.mutateAsync(); toast.success(t(($) => $.index_started)); }
    catch (error) { toast.error(error instanceof Error ? error.message : t(($) => $.error)); }
  }

  async function cancelKnowledgeJob(jobId: string) {
    try {
      await cancelJob.mutateAsync(jobId);
      toast.success(t(($) => $.job_cancelled));
    } catch (error) {
      toast.error(error instanceof Error ? error.message : t(($) => $.cancel_failed));
    }
  }

  return (
    <div className="flex h-full min-h-0 flex-col">
      <header className="flex flex-wrap items-center gap-2 border-b px-4 py-3">
        <Button variant="ghost" size="icon-sm" aria-label={t(($) => $.base_back)} onClick={() => navigation.push(paths.knowledge())}><ArrowLeft className="size-4" /></Button>
        <div className="min-w-0"><h1 className="truncate text-title font-semibold">{base.data.name}</h1><p className="text-micro text-muted-foreground">{base.data.visibility === "workspace" ? t(($) => $.workspace) : t(($) => $.private)} · {t(($) => $.active_index)}: {base.data.active_index_id ?? t(($) => $.none)}</p></div>
        <div className="ml-auto flex flex-wrap gap-2"><Button variant="outline" size="sm" onClick={() => navigation.push(paths.knowledgeBaseSettings(baseId))}><Settings className="size-4" />{t(($) => $.model_settings)}</Button><Button variant="outline" size="sm" onClick={() => navigation.push(paths.knowledgeAsk(baseId))}><Search className="size-4" />{t(($) => $.open_ask)}</Button><Button variant="outline" size="sm" onClick={() => navigation.push(paths.knowledgeGraph(baseId))}><GitBranch className="size-4" />{t(($) => $.open_graph)}</Button><Button size="sm" disabled={rebuild.isPending} onClick={() => void rebuildIndex()}><RefreshCw className="size-4" />{t(($) => $.rebuild_index)}</Button></div>
      </header>
      <div className="min-h-0 flex-1 overflow-auto p-6">
        <div className="mx-auto grid max-w-6xl gap-6">
          <section className="grid gap-4 rounded-xl border bg-card p-4 md:grid-cols-[minmax(0,1fr)_minmax(0,1fr)]">
            <form onSubmit={addSource} className="space-y-3"><h2 className="text-body font-semibold">{t(($) => $.add_url)}</h2><div className="flex gap-2"><Label className="sr-only" htmlFor="knowledge-url">{t(($) => $.url)}</Label><Input id="knowledge-url" type="url" value={url} onChange={(event) => setURL(event.target.value)} placeholder="https://…" required /><Button type="submit" disabled={addURL.isPending}><ExternalLink className="size-4" />{t(($) => $.add)}</Button></div></form>
            <div className="space-y-3"><h2 className="text-body font-semibold">{t(($) => $.upload)}</h2><input ref={fileRef} type="file" className="sr-only" onChange={(event) => void chooseFile(event)} /><Button variant="outline" disabled={upload.isPending} onClick={() => fileRef.current?.click()}><Upload className="size-4" />{t(($) => $.upload)}</Button><p className="text-micro text-muted-foreground">{t(($) => $.supported_formats)}</p></div>
          </section>
          <section className="rounded-xl border bg-card p-4">
            <form onSubmit={runSearch} className="flex flex-wrap items-end gap-3">
              <div className="min-w-56 flex-1"><Label className="sr-only" htmlFor="knowledge-search">{t(($) => $.search)}</Label><Input id="knowledge-search" value={query} onChange={(event) => setQuery(event.target.value)} placeholder={t(($) => $.search_placeholder)} /></div>
              <div className="w-36"><Label className="text-micro" htmlFor="knowledge-search-mode">{t(($) => $.search_mode)}</Label><select id="knowledge-search-mode" className="mt-1 h-9 w-full rounded-md border bg-background px-2 text-caption" value={searchMode} onChange={(event) => setSearchMode(event.target.value as typeof searchMode)}><option value="hybrid">{t(($) => $.mode_hybrid)}</option><option value="keyword">{t(($) => $.mode_keyword)}</option><option value="semantic">{t(($) => $.mode_semantic)}</option></select></div>
              <div className="w-24"><Label className="text-micro" htmlFor="knowledge-search-limit">{t(($) => $.search_limit)}</Label><Input id="knowledge-search-limit" className="mt-1" type="number" min={1} max={50} value={searchLimit} onChange={(event) => setSearchLimit(event.target.value)} /></div>
              <Button type="submit" disabled={search.isPending || !query.trim()}><Search className="size-4" />{t(($) => $.search)}</Button>
            </form>
            <p className="mt-2 text-micro text-muted-foreground">{t(($) => $.search_controls_hint)}</p>
            {searchResult && <div className="mt-5 space-y-3"><div className="flex flex-wrap items-center gap-2"><h2 className="text-body font-semibold">{t(($) => $.results)}</h2><span className="rounded-sm bg-muted px-1.5 py-0.5 text-micro">{searchResult.mode_effective}</span><span className="text-micro text-muted-foreground">{searchResult.rerank_applied ? t(($) => $.rerank_applied) : t(($) => $.rerank_not_applied)}</span><span className="text-micro text-muted-foreground">{t(($) => $.query_id)}: {searchResult.query_id}</span></div>{searchResult.warnings.length > 0 && <p className="text-micro text-muted-foreground">{t(($) => $.warnings, { value: searchResult.warnings.join(", ") })}</p>}{searchResult.results.length === 0 ? <p className="text-caption text-muted-foreground">{t(($) => $.no_results)}</p> : searchResult.results.map((result) => <article key={result.chunk_id} className="rounded-lg border p-3"><div className="flex flex-wrap items-center gap-2"><span className="rounded-sm bg-muted px-1.5 py-0.5 text-micro">#{result.rank}</span><AppLink className="font-medium text-primary underline" href={paths.knowledgeDocument(baseId, result.document_id)}>{result.title}</AppLink><span className="text-micro text-muted-foreground">{result.retrieval_channels.join(" + ")}</span></div><p className="mt-2 whitespace-pre-wrap text-caption">{result.text}</p></article>)}</div>}
          </section>
          <section className="grid gap-6 lg:grid-cols-[minmax(0,1fr)_20rem]">
            <div className="rounded-xl border bg-card p-4"><div className="mb-4 flex items-center justify-between gap-3"><h2 className="text-body font-semibold">{t(($) => $.documents)}</h2><span className="text-micro text-muted-foreground">{t(($) => $.document_count, { count: documentItems.length })}</span></div>{documents.isPending ? <p role="status">{t(($) => $.loading)}</p> : documents.isError ? <p role="alert">{documents.error.message}</p> : documentItems.length ? <><div className="divide-y">{documentItems.map((document) => <AppLink key={document.id} href={paths.knowledgeDocument(baseId, document.id)} className="flex items-center gap-3 py-3 first:pt-0 last:pb-0"><FileText className="size-4 shrink-0 text-primary" /><span className="min-w-0 flex-1 truncate text-caption font-medium">{document.title}</span><span className="text-micro text-muted-foreground">{document.status}</span></AppLink>)}</div>{documents.hasNextPage ? <Button className="mt-4" variant="outline" size="sm" onClick={() => void documents.fetchNextPage()} disabled={documents.isFetchingNextPage}>{documents.isFetchingNextPage ? t(($) => $.loading) : t(($) => $.load_more)}</Button> : null}</> : <p className="text-caption text-muted-foreground">{t(($) => $.no_documents)}</p>}</div>
            <div className="rounded-xl border bg-card p-4"><h2 className="mb-4 text-body font-semibold">{t(($) => $.jobs)}</h2>{jobItems.length ? <><div className="space-y-4">{jobItems.slice(0, 8).map((job) => { const progress = jobProgress(job); return <div key={job.id} className="space-y-1.5"><div className="flex items-center justify-between gap-2 text-caption"><span className="truncate">{job.stage}</span><span className="flex items-center gap-2 text-micro text-muted-foreground">{job.status}{["queued", "running", "waiting_config"].includes(job.status) ? <Button type="button" variant="ghost" size="xs" disabled={cancelJob.isPending} onClick={() => void cancelKnowledgeJob(job.id)}>{t(($) => $.cancel_job)}</Button> : null}</span></div><div className="h-1.5 overflow-hidden rounded-full bg-muted" role="progressbar" aria-label={t(($) => $.job_progress, { value: progress })} aria-valuemin={0} aria-valuemax={100} aria-valuenow={progress}><div className="h-full rounded-full bg-primary transition-[width]" style={{ width: `${progress}%` }} /></div><div className="flex items-center justify-between text-micro text-muted-foreground"><span>{t(($) => $.job_progress, { value: progress })}</span>{job.error_code ? <span className="text-destructive">{job.error_code}</span> : null}</div></div>; })}</div>{jobs.hasNextPage ? <Button className="mt-4" variant="outline" size="sm" onClick={() => void jobs.fetchNextPage()} disabled={jobs.isFetchingNextPage}>{jobs.isFetchingNextPage ? t(($) => $.loading) : t(($) => $.load_more)}</Button> : null}</> : <p className="text-caption text-muted-foreground">{t(($) => $.none)}</p>}</div>
          </section>
        </div>
      </div>
    </div>
  );
}
