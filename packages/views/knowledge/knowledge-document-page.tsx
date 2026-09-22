"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { ArrowLeft, Eye, FileText, Link as LinkIcon, RefreshCw, Trash2, Upload } from "lucide-react";
import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  knowledgeBlockListOptions,
  knowledgeDocumentOptions,
  knowledgeVersionListOptions,
  useConfirmKnowledgeBlocks,
  useDeleteKnowledgeDocument,
  useIssueKnowledgePreviewCapability,
  useReplaceKnowledgeFile,
  useReplaceKnowledgeURL,
  useReprocessKnowledgeDocument,
} from "@multica/core/knowledge";
import { api } from "@multica/core/api";
import type { KnowledgeDocumentBlock } from "@multica/core/types";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { AppLink, useNavigation } from "../navigation";
import { useT } from "../i18n";

type PreviewState = {
  url: string;
  text?: string;
  mimeType: string;
};

export function KnowledgeDocumentPage({ baseId, documentId }: { baseId: string; documentId: string }) {
  const { t } = useT("knowledge");
  const workspaceId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const navigation = useNavigation();
  const document = useQuery(knowledgeDocumentOptions(workspaceId, baseId, documentId));
  const versions = useInfiniteQuery(knowledgeVersionListOptions(workspaceId, baseId, documentId));
  const versionItems = useMemo(() => versions.data?.pages.flatMap((page) => page.versions) ?? [], [versions.data?.pages]);
  const [selectedVersionId, setSelectedVersionId] = useState("");
  const versionId = selectedVersionId || document.data?.current_version_id || versionItems[0]?.id || "";
  const blocks = useInfiniteQuery(knowledgeBlockListOptions(workspaceId, baseId, documentId, versionId));
  const blockItems = blocks.data?.pages.flatMap((page) => page.blocks) ?? [];
  const selectedVersion = useMemo(() => versionItems.find((version) => version.id === versionId), [versionId, versionItems]);
  const [replaceURL, setReplaceURL] = useState("");
  const [preview, setPreview] = useState<PreviewState | null>(null);
  const [confirmedBlockIds, setConfirmedBlockIds] = useState<Set<string>>(new Set());
  const fileRef = useRef<HTMLInputElement>(null);

  const remove = useDeleteKnowledgeDocument(baseId);
  const reprocess = useReprocessKnowledgeDocument(baseId);
  const replaceFile = useReplaceKnowledgeFile(baseId, documentId);
  const replaceURLMutation = useReplaceKnowledgeURL(baseId, documentId);
  const confirmBlocks = useConfirmKnowledgeBlocks(baseId, documentId);
  const issuePreview = useIssueKnowledgePreviewCapability(baseId, documentId, versionId);

  useEffect(() => () => {
    if (preview?.url) URL.revokeObjectURL(preview.url);
  }, [preview?.url]);

  useEffect(() => {
    setConfirmedBlockIds(new Set());
  }, [versionId]);

  if (document.isPending) return <div className="p-6" role="status">{t(($) => $.loading)}</div>;
  if (document.isError || !document.data) return <div className="p-6" role="alert">{document.error?.message ?? t(($) => $.error)}</div>;

  const modelDerivedBlocks = blockItems.filter((block) => block.model_derived);

  async function deleteDocument() {
    try {
      await remove.mutateAsync({ documentId, expectedRevision: document.data!.revision });
      toast.success(t(($) => $.deleted));
      navigation.push(paths.knowledgeBase(baseId));
    } catch (error) {
      toast.error(error instanceof Error ? error.message : t(($) => $.error));
    }
  }

  async function reprocessDocument() {
    try {
      await reprocess.mutateAsync({ documentId, stage: "parse" });
      toast.success(t(($) => $.source_added));
    } catch (error) {
      toast.error(error instanceof Error ? error.message : t(($) => $.error));
    }
  }

  async function chooseReplacementFile(event: React.ChangeEvent<HTMLInputElement>) {
    const file = event.target.files?.[0];
    event.target.value = "";
    if (!file) return;
    try {
      await replaceFile.mutateAsync({ file, expectedRevision: document.data!.revision });
      toast.success(t(($) => $.source_added));
    } catch (error) {
      toast.error(error instanceof Error ? error.message : t(($) => $.error));
    }
  }

  async function replaceSource(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!replaceURL.trim()) return;
    try {
      await replaceURLMutation.mutateAsync({ url: replaceURL.trim(), expected_revision: document.data!.revision });
      setReplaceURL("");
      toast.success(t(($) => $.source_added));
    } catch (error) {
      toast.error(error instanceof Error ? error.message : t(($) => $.error));
    }
  }

  async function previewVersion() {
    if (!versionId) return;
    try {
      const capability = await issuePreview.mutateAsync();
      const blob = await api.redeemKnowledgePreview(versionId, capability.token);
      const mimeType = blob.type || selectedVersion?.mime_type || "application/octet-stream";
      const url = URL.createObjectURL(blob);
      setPreview({ url, mimeType, text: mimeType.startsWith("text/") ? await blob.text() : undefined });
    } catch (error) {
      toast.error(error instanceof Error ? error.message : t(($) => $.error));
    }
  }

  async function confirmSelectedBlocks() {
    if (!blockItems.length || confirmedBlockIds.size === 0) return;
    const nextBlocks: KnowledgeDocumentBlock[] = blockItems.map((block) => confirmedBlockIds.has(block.block_id) ? { ...block, model_derived: false } : block);
    try {
      await confirmBlocks.mutateAsync({ version_id: versionId, expected_revision: document.data!.revision, blocks: nextBlocks });
      setConfirmedBlockIds(new Set());
      toast.success(t(($) => $.blocks_confirmed));
    } catch (error) {
      toast.error(error instanceof Error ? error.message : t(($) => $.error));
    }
  }

  return (
    <div className="flex h-full min-h-0 flex-col">
      <header className="flex flex-wrap items-center gap-2 border-b px-4 py-3">
        <Button variant="ghost" size="icon-sm" aria-label={t(($) => $.back)} onClick={() => navigation.push(paths.knowledgeBase(baseId))}><ArrowLeft className="size-4" /></Button>
        <FileText className="size-5 text-primary" />
        <div className="min-w-0"><h1 className="truncate text-title font-semibold">{document.data.title}</h1><p className="text-micro text-muted-foreground">{document.data.source_kind} · {document.data.status}</p></div>
        <div className="ml-auto flex flex-wrap gap-2">
          <Button variant="outline" size="sm" disabled={!versionId || issuePreview.isPending} onClick={() => void previewVersion()}><Eye className="size-4" />{issuePreview.isPending ? t(($) => $.preview_loading) : t(($) => $.preview)}</Button>
          <Button variant="outline" size="sm" disabled={reprocess.isPending} onClick={() => void reprocessDocument()}><RefreshCw className="size-4" />{t(($) => $.reprocess)}</Button>
          <Button variant="destructive" size="sm" disabled={remove.isPending} onClick={() => void deleteDocument()}><Trash2 className="size-4" />{t(($) => $.delete)}</Button>
        </div>
      </header>

      <div className="min-h-0 flex-1 overflow-auto p-6">
        <div className="mx-auto grid max-w-6xl gap-6 lg:grid-cols-[18rem_minmax(0,1fr)]">
          <aside className="space-y-4 rounded-xl border bg-card p-4">
            <h2 className="text-body font-semibold">{t(($) => $.versions)}</h2>
            {versions.isPending ? <p role="status">{t(($) => $.loading)}</p> : null}
            {versionItems.map((version) => <button type="button" key={version.id} className={`block w-full rounded-lg border p-3 text-left text-caption ${version.id === versionId ? "border-primary bg-accent/40" : "hover:bg-accent/40"}`} onClick={() => setSelectedVersionId(version.id)}>{t(($) => $.version_status, { number: version.version_number, status: version.status })}</button>)}
            {versions.hasNextPage ? <Button className="w-full" variant="outline" size="sm" onClick={() => void versions.fetchNextPage()} disabled={versions.isFetchingNextPage}>{versions.isFetchingNextPage ? t(($) => $.loading) : t(($) => $.load_more)}</Button> : null}
          </aside>

          <main className="space-y-4">
            <section className="rounded-xl border bg-card p-4">
              <h2 className="mb-1 text-body font-semibold">{t(($) => $.document)}</h2>
              <p className="text-caption text-muted-foreground">{document.data.source_url ?? document.data.source_kind}</p>
              {selectedVersion && <p className="mt-2 text-micro text-muted-foreground">{t(($) => $.version_size, { mime: selectedVersion.mime_type ?? "", size: selectedVersion.byte_size ?? 0 })}</p>}
              <div className="mt-4 grid gap-3 border-t pt-4 md:grid-cols-2">
                <div className="space-y-2"><Label htmlFor="knowledge-replace-file">{t(($) => $.replace_file)}</Label><input ref={fileRef} id="knowledge-replace-file" type="file" className="sr-only" onChange={(event) => void chooseReplacementFile(event)} /><Button variant="outline" disabled={replaceFile.isPending} onClick={() => fileRef.current?.click()}><Upload className="size-4" />{t(($) => $.replace_source)}</Button></div>
                <form onSubmit={replaceSource} className="space-y-2"><Label htmlFor="knowledge-replace-url">{t(($) => $.replace_url)}</Label><div className="flex gap-2"><Input id="knowledge-replace-url" type="url" value={replaceURL} onChange={(event) => setReplaceURL(event.target.value)} placeholder="https://…" /><Button type="submit" disabled={replaceURLMutation.isPending || !replaceURL.trim()}><LinkIcon className="size-4" />{t(($) => $.replace)}</Button></div></form>
              </div>
            </section>

            {preview ? <section className="space-y-3 rounded-xl border bg-card p-4"><h2 className="text-body font-semibold">{t(($) => $.source_preview)}</h2>{preview.mimeType === "application/pdf" || preview.mimeType.startsWith("image/") ? <iframe title={t(($) => $.source_preview)} src={preview.url} className="h-[32rem] w-full rounded-lg border" /> : preview.mimeType.startsWith("text/") ? <pre className="max-h-[32rem] overflow-auto whitespace-pre-wrap rounded-lg bg-muted p-4 text-caption">{preview.text ?? ""}</pre> : <p className="text-caption text-muted-foreground">{t(($) => $.preview_unavailable)} <a className="text-primary underline" href={preview.url} download>{t(($) => $.download_source)}</a></p>}</section> : null}

            <section className="rounded-xl border bg-card p-4">
              <div className="mb-3 flex flex-wrap items-center justify-between gap-2"><h2 className="text-body font-semibold">{t(($) => $.blocks)}</h2>{modelDerivedBlocks.length > 0 ? <Button size="sm" disabled={confirmBlocks.isPending || confirmedBlockIds.size === 0} onClick={() => void confirmSelectedBlocks()}>{confirmBlocks.isPending ? t(($) => $.confirming_blocks) : t(($) => $.confirm_blocks)}</Button> : null}</div>
              {modelDerivedBlocks.length > 0 ? <p className="mb-3 text-micro text-muted-foreground">{t(($) => $.select_block_to_confirm)}</p> : null}
              {blocks.isPending ? <p role="status">{t(($) => $.loading)}</p> : blocks.isError ? <p role="alert">{blocks.error.message}</p> : blockItems.length ? <><div className="space-y-3">{blockItems.map((block) => <article key={block.block_id} className="rounded-lg border p-3"><div className="mb-1 flex items-start gap-2">{block.model_derived ? <input type="checkbox" aria-label={`${t(($) => $.confirm_block)}: ${block.block_id}`} checked={confirmedBlockIds.has(block.block_id)} onChange={(event) => setConfirmedBlockIds((current) => { const next = new Set(current); if (event.target.checked) next.add(block.block_id); else next.delete(block.block_id); return next; })} /> : null}<p className="text-micro text-muted-foreground">{block.kind}{block.model_derived ? ` · ${t(($) => $.model_derived)}` : ""}{block.heading_path?.length ? ` · ${block.heading_path.join(" / ")}` : ""}</p></div><p className="whitespace-pre-wrap text-caption">{block.text}</p></article>)}</div>{blocks.hasNextPage ? <Button className="mt-4" variant="outline" size="sm" onClick={() => void blocks.fetchNextPage()} disabled={blocks.isFetchingNextPage}>{blocks.isFetchingNextPage ? t(($) => $.loading) : t(($) => $.load_more)}</Button> : null}</> : <p className="text-caption text-muted-foreground">{t(($) => $.no_blocks)}</p>}
            </section>
            <AppLink href={paths.knowledgeBase(baseId)} className="text-caption text-primary underline">{t(($) => $.base_back)}</AppLink>
          </main>
        </div>
      </div>
    </div>
  );
}
