"use client";

import { useEffect, useState } from "react";
import { ArrowLeft, Check, GitBranch, RotateCcw, Save, X } from "lucide-react";
import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  knowledgeEntityOptions,
  knowledgeGraphEditListOptions,
  knowledgeGraphOptions,
  knowledgeRelationEvidenceOptions,
  useApplyKnowledgeGraphEdit,
  useRevertKnowledgeGraphEdit,
} from "@multica/core/knowledge";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { AppLink, useNavigation } from "../navigation";
import { useT } from "../i18n";

export function KnowledgeGraphPage({ baseId }: { baseId: string }) {
  const { t } = useT("knowledge");
  const workspaceId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const navigation = useNavigation();
  const [entityId, setEntityId] = useState("");
  const [depth, setDepth] = useState(1);
  const [selectedEntityId, setSelectedEntityId] = useState("");
  const [selectedRelationId, setSelectedRelationId] = useState("");
  const [rename, setRename] = useState("");
  const graph = useQuery(knowledgeGraphOptions(workspaceId, baseId, entityId, depth));
  const entity = useQuery(knowledgeEntityOptions(workspaceId, baseId, selectedEntityId));
  const relationEvidence = useQuery(knowledgeRelationEvidenceOptions(workspaceId, baseId, selectedRelationId));
  const edits = useInfiniteQuery(knowledgeGraphEditListOptions(workspaceId, baseId));
  const editItems = edits.data?.pages.flatMap((page) => page.edits) ?? [];
  const applyEdit = useApplyKnowledgeGraphEdit(baseId);
  const revertEdit = useRevertKnowledgeGraphEdit(baseId);
  const nodeNames = new Map((graph.data?.nodes ?? []).map((node) => [node.id, node.canonical_name]));
  const selectedRelation = graph.data?.relations.find((relation) => relation.id === selectedRelationId);

  useEffect(() => {
    if (entity.data?.entity.canonical_name) setRename(entity.data.entity.canonical_name);
  }, [entity.data?.entity.canonical_name]);

  async function saveRename() {
    if (!entity.data || !rename.trim()) return;
    try {
      await applyEdit.mutateAsync({ operation: "rename", targetId: entity.data.entity.id, payload: { target_type: "entity", canonical_name: rename.trim() }, expectedRevision: entity.data.entity.revision });
      toast.success(t(($) => $.graph_saved));
    } catch (error) {
      toast.error(error instanceof Error ? error.message : t(($) => $.error));
    }
  }

  async function setRelationStatus(operation: "confirm" | "reject_relation") {
    if (!selectedRelation) return;
    try {
      await applyEdit.mutateAsync({ operation, targetId: selectedRelation.id, payload: { target_type: "relation" }, expectedRevision: selectedRelation.revision });
      toast.success(t(($) => $.graph_saved));
    } catch (error) {
      toast.error(error instanceof Error ? error.message : t(($) => $.error));
    }
  }

  async function revert(editId: string) {
    try {
      await revertEdit.mutateAsync(editId);
      toast.success(t(($) => $.graph_reverted));
    } catch (error) {
      toast.error(error instanceof Error ? error.message : t(($) => $.error));
    }
  }

  return (
    <div className="flex h-full min-h-0 flex-col">
      <header className="flex flex-wrap items-center gap-2 border-b px-4 py-3">
        <Button variant="ghost" size="icon-sm" aria-label={t(($) => $.back)} onClick={() => navigation.push(paths.knowledgeBase(baseId))}><ArrowLeft className="size-4" /></Button>
        <GitBranch className="size-5 text-primary" />
        <div><h1 className="text-title font-semibold">{t(($) => $.graph)}</h1><p className="text-micro text-muted-foreground">{t(($) => $.graph_hint)}</p></div>
      </header>
      <div className="min-h-0 flex-1 overflow-auto p-6">
        <div className="mx-auto max-w-6xl space-y-6">
          <form onSubmit={(event) => { event.preventDefault(); void graph.refetch(); }} className="flex flex-wrap items-end gap-3 rounded-xl border bg-card p-4">
            <div className="min-w-64 flex-1 space-y-1.5"><Label htmlFor="graph-entity">{t(($) => $.entity)}</Label><Input id="graph-entity" value={entityId} onChange={(event) => setEntityId(event.target.value)} placeholder={t(($) => $.entity_placeholder)} /></div>
            <div className="space-y-1.5"><Label htmlFor="graph-depth">{t(($) => $.depth)}</Label><select id="graph-depth" className="flex h-9 rounded-md border bg-background px-3 text-caption" value={depth} onChange={(event) => setDepth(Number(event.target.value))}><option value={1}>1</option><option value={2}>2</option></select></div>
            <Button type="submit" disabled={graph.isFetching}>{t(($) => $.load_graph)}</Button>
          </form>

          {graph.isPending ? <p role="status">{t(($) => $.loading)}</p> : graph.isError ? <p role="alert">{graph.error.message}</p> : !graph.data?.nodes.length ? <p className="rounded-xl border p-6 text-caption text-muted-foreground">{t(($) => $.no_graph)}</p> : <div className="grid gap-6 lg:grid-cols-2">
            <section className="rounded-xl border bg-card p-4"><h2 className="mb-3 text-body font-semibold">{t(($) => $.nodes, { count: graph.data.node_count })}</h2><div className="space-y-2">{graph.data.nodes.map((node) => <button type="button" key={node.id} className={`flex w-full items-center justify-between rounded-lg border p-3 text-left hover:bg-accent/40 ${selectedEntityId === node.id ? "border-primary bg-accent/40" : ""}`} onClick={() => { setSelectedEntityId(node.id); setSelectedRelationId(""); setEntityId(node.id); }}><span><span className="font-medium">{node.canonical_name}</span><span className="ml-2 text-micro text-muted-foreground">{node.type}</span></span><span className="text-micro text-muted-foreground">{node.review_status}</span></button>)}</div></section>
            <section className="rounded-xl border bg-card p-4"><h2 className="mb-3 text-body font-semibold">{t(($) => $.relations, { count: graph.data.edge_count })}</h2><div className="space-y-2">{graph.data.relations.map((relation) => <button type="button" key={relation.id} className={`block w-full rounded-lg border p-3 text-left text-caption hover:bg-accent/40 ${selectedRelationId === relation.id ? "border-primary bg-accent/40" : ""}`} onClick={() => { setSelectedRelationId(relation.id); setSelectedEntityId(""); }}><span className="font-medium">{nodeNames.get(relation.source_entity_id) ?? relation.source_entity_id}</span><span className="mx-2 text-muted-foreground">— {relation.predicate} →</span><span className="font-medium">{nodeNames.get(relation.target_entity_id) ?? relation.target_entity_id}</span><span className="ml-2 text-micro text-muted-foreground">{relation.review_status} · {t(($) => $.evidence_count, { count: relation.evidence_count })}</span></button>)}</div></section>
          </div>}
          {graph.data?.truncated && <p className="text-micro text-muted-foreground">{t(($) => $.truncated)}</p>}

          {selectedEntityId && <section className="grid gap-6 rounded-xl border bg-card p-4 lg:grid-cols-[minmax(0,1fr)_minmax(0,1fr)]"><div><h2 className="mb-3 text-body font-semibold">{t(($) => $.entity_detail)}</h2>{entity.isPending ? <p role="status">{t(($) => $.loading)}</p> : entity.isError ? <p role="alert">{entity.error.message}</p> : entity.data ? <><p className="text-caption">{entity.data.entity.canonical_name} · {entity.data.entity.type}</p><div className="mt-3 flex gap-2"><Input aria-label={t(($) => $.rename)} value={rename} onChange={(event) => setRename(event.target.value)} /><Button onClick={() => void saveRename()} disabled={applyEdit.isPending || !rename.trim()}><Save className="size-4" />{t(($) => $.save_edit)}</Button></div></> : null}</div><div><h3 className="mb-3 text-body font-medium">{t(($) => $.evidence)}</h3>{entity.data?.evidence.length ? <div className="space-y-2">{entity.data.evidence.map((item) => <blockquote key={item.id} className="rounded-lg border p-3 text-caption"><p>{item.quote}</p><p className="mt-1 text-micro text-muted-foreground">{item.version_id} · {item.chunk_id}</p></blockquote>)}</div> : <p className="text-caption text-muted-foreground">{t(($) => $.no_evidence)}</p>}</div></section>}

          {selectedRelationId && <section className="rounded-xl border bg-card p-4"><div className="flex flex-wrap items-center justify-between gap-3"><div><h2 className="text-body font-semibold">{t(($) => $.relation_detail)}</h2>{selectedRelation ? <p className="mt-1 text-caption">{selectedRelation.predicate} · {selectedRelation.review_status}</p> : null}</div>{selectedRelation ? <div className="flex gap-2"><Button variant="outline" size="sm" disabled={applyEdit.isPending} onClick={() => void setRelationStatus("confirm")}><Check className="size-4" />{t(($) => $.confirm)}</Button><Button variant="destructive" size="sm" disabled={applyEdit.isPending} onClick={() => void setRelationStatus("reject_relation")}><X className="size-4" />{t(($) => $.reject)}</Button></div> : null}</div><h3 className="mt-4 mb-2 text-body font-medium">{t(($) => $.evidence)}</h3>{relationEvidence.isPending ? <p role="status">{t(($) => $.loading)}</p> : relationEvidence.isError ? <p role="alert">{relationEvidence.error.message}</p> : relationEvidence.data?.evidence.length ? <div className="space-y-2">{relationEvidence.data.evidence.map((item) => <blockquote key={item.id} className="rounded-lg border p-3 text-caption"><p>{item.quote}</p><p className="mt-1 text-micro text-muted-foreground">{item.version_id} · {item.chunk_id}</p></blockquote>)}</div> : <p className="text-caption text-muted-foreground">{t(($) => $.no_evidence)}</p>}</section>}

          <section className="rounded-xl border bg-card p-4"><h2 className="mb-3 text-body font-semibold">{t(($) => $.edit_history)}</h2>{edits.isPending ? <p role="status">{t(($) => $.loading)}</p> : editItems.length ? <><div className="space-y-2">{editItems.map((edit) => <div key={edit.id} className="flex flex-wrap items-center justify-between gap-2 rounded-lg border p-3 text-caption"><span>{edit.operation} · {edit.target_id}</span>{edit.reverted_at ? <span className="text-micro text-muted-foreground">{t(($) => $.reverted)}</span> : <Button variant="ghost" size="xs" disabled={revertEdit.isPending} onClick={() => void revert(edit.id)}><RotateCcw className="size-3.5" />{t(($) => $.revert)}</Button>}</div>)}</div>{edits.hasNextPage ? <Button className="mt-4" variant="outline" size="sm" onClick={() => void edits.fetchNextPage()} disabled={edits.isFetchingNextPage}>{edits.isFetchingNextPage ? t(($) => $.loading) : t(($) => $.load_more)}</Button> : null}</> : <p className="text-caption text-muted-foreground">{t(($) => $.no_edits)}</p>}</section>
          <AppLink href={paths.knowledgeBase(baseId)} className="text-caption text-primary underline">{t(($) => $.base_back)}</AppLink>
        </div>
      </div>
    </div>
  );
}
