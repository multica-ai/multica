"use client";

import { useState } from "react";
import { useCurrentWorkspace } from "@multica/core/paths";
import { Button } from "@multica/ui/components/ui/button";
import { useNavigation } from "@multica/views/navigation";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { createHookDraft, type WorkflowHook } from "../../core/hook-types";
import { createWorkflowHook, deleteWorkflowHook, disableWorkflowHook, discardWorkflowHookDraft, publishWorkflowHook, testWorkflowHook, updateWorkflowHook } from "../../core/hook-api";
import { workflowHookDetailOptions, workflowHookKeys, workflowHookRunsOptions } from "../../core/queries";
import { HookEditor } from "./hook-editor";
import { useHookDirectory } from "./use-hook-directory";
import { HookTemplatePicker } from "./hook-template-picker";
import { toast } from "sonner";

export function WorkflowHookEditorPage({ hookId }: { hookId?: string }) {
  const workspace = useCurrentWorkspace();
  const navigation = useNavigation();
  if (!workspace) return <div className="flex h-full items-center justify-center text-sm text-muted-foreground">Loading workspace context…</div>;
	return <WorkflowHookEditorLoaded wsId={workspace.id} workspaceSlug={workspace.slug} hookId={hookId} onNavigate={(path) => navigation.push(path)} />;
}

function WorkflowHookEditorLoaded({ wsId, workspaceSlug, hookId, onNavigate }: { wsId: string; workspaceSlug: string; hookId?: string; onNavigate: (path: string) => void }) {
	const queryClient = useQueryClient();
	const [newDraft, setNewDraft] = useState<WorkflowHook | null>(null);
	const directory = useHookDirectory(wsId);
	const detail = useQuery(workflowHookDetailOptions(wsId, hookId ?? ""));
	const runs = useQuery(workflowHookRunsOptions(wsId, hookId ?? ""));
	const save = useMutation({
		mutationFn: (hook: WorkflowHook) => hookId ? updateWorkflowHook(hookId, hook) : createWorkflowHook(hook),
		onSuccess: (saved) => {
			queryClient.invalidateQueries({ queryKey: workflowHookKeys.all(wsId) });
			const routeID = saved.family_id ?? saved.id;
			if (routeID && routeID !== hookId) onNavigate(`/${workspaceSlug}/workflows/hooks/${routeID}`);
			toast.success("Hook draft saved");
		},
		onError: () => toast.error("The hook could not be saved"),
	});
	const publish = useMutation({ mutationFn: (id: string) => publishWorkflowHook(id), onSuccess: (published) => { queryClient.invalidateQueries({ queryKey: workflowHookKeys.all(wsId) }); const routeID = published.family_id ?? published.id; if (routeID) onNavigate(`/${workspaceSlug}/workflows/hooks/${routeID}`); toast.success("Hook published"); }, onError: () => toast.error("The hook could not be published") });
	const test = useMutation({
		mutationFn: ({ id, eventId, revision }: { id: string; eventId: string; revision: number }) => testWorkflowHook(id, eventId, revision),
		onSuccess: () => {
			queryClient.invalidateQueries({ queryKey: workflowHookKeys.detail(wsId, hookId ?? "") });
			queryClient.invalidateQueries({ queryKey: workflowHookKeys.runs(wsId, hookId ?? "") });
		},
		onError: () => toast.error("The test could not run"),
	});
	const disable = useMutation({ mutationFn: (id: string) => disableWorkflowHook(id), onSuccess: (disabled) => { queryClient.invalidateQueries({ queryKey: workflowHookKeys.all(wsId) }); const routeID = disabled.family_id ?? disabled.id; if (routeID) onNavigate(`/${workspaceSlug}/workflows/hooks/${routeID}`); toast.success("Hook disabled"); }, onError: () => toast.error("The hook could not be disabled") });
	const discard = useMutation({ mutationFn: (id: string) => discardWorkflowHookDraft(id), onSuccess: (remaining) => { queryClient.invalidateQueries({ queryKey: workflowHookKeys.all(wsId) }); const routeID = remaining.family_id ?? remaining.id; if (remaining.id && routeID) onNavigate(`/${workspaceSlug}/workflows/hooks/${routeID}`); else onNavigate(`/${workspaceSlug}/workflows/hooks`); toast.success("Draft discarded"); }, onError: () => toast.error("The Draft could not be discarded") });
	const remove = useMutation({ mutationFn: (id: string) => deleteWorkflowHook(id), onSuccess: () => { queryClient.invalidateQueries({ queryKey: workflowHookKeys.all(wsId) }); toast.success("Hook deleted"); onNavigate(`/${workspaceSlug}/workflows/hooks`); }, onError: () => toast.error("The hook could not be deleted") });
	const duplicate = useMutation({ mutationFn: (hook: WorkflowHook) => createWorkflowHook({ ...hook, id: undefined, version: 1, name: `${hook.name || "Untitled hook"} copy`, mode: "dry_run", baseline_run_count: 0, can_publish: false }), onSuccess: (created) => { queryClient.invalidateQueries({ queryKey: workflowHookKeys.all(wsId) }); if (created.id) onNavigate(`/${workspaceSlug}/workflows/hooks/${created.id}`); toast.success("Hook duplicated"); }, onError: () => toast.error("The hook could not be duplicated") });
	if (hookId && detail.isLoading) return <div className="flex h-full items-center justify-center text-sm text-muted-foreground">Loading hook…</div>;
	if (hookId && detail.isError) return <HookLoadError onRetry={() => void detail.refetch()} />;
	if (!hookId && !newDraft) return <HookTemplatePicker onSelect={setNewDraft} onBack={() => onNavigate(`/${workspaceSlug}/workflows/hooks`)} />;
	const hook = detail.data ?? newDraft ?? createHookDraft();
	const pending = save.isPending || publish.isPending || test.isPending || disable.isPending || discard.isPending || remove.isPending || duplicate.isPending;
	return <HookEditor initialHook={hook} directory={directory} runs={runs.data ?? []} canPublish={hook.can_publish === true} pending={pending}
		onBack={() => onNavigate(`/${workspaceSlug}/workflows/hooks`)}
		onSave={(next) => save.mutateAsync(next)}
		onPublish={(_next, saved) => publish.mutateAsync((saved as WorkflowHook | undefined)?.id ?? hook.id ?? "")}
		onTest={async (next, eventId) => { if (next.id && next.revision) await test.mutateAsync({ id: next.id, eventId, revision: next.revision }); }}
		onDisable={hook.id ? () => disable.mutate(hook.id!) : undefined}
		onDiscard={hook.lifecycle.draft_id ? () => discard.mutate(hook.lifecycle.draft_id!) : undefined}
		onDelete={hook.id ? () => remove.mutate(hook.id!) : undefined}
		onDuplicate={hook.id ? (next) => duplicate.mutate(next) : undefined}
	/>;
}

export function HookLoadError({ onRetry }: { onRetry: () => void }) {
	return <div role="alert" className="flex h-full flex-col items-center justify-center gap-3 p-6 text-center">
		<div><h1 className="font-semibold text-destructive">The Hook could not be read</h1><p className="mt-1 text-sm text-muted-foreground">The response was incomplete, so editing is blocked to protect the existing Hook.</p></div>
		<Button variant="outline" onClick={onRetry}>Retry</Button>
	</div>;
}
