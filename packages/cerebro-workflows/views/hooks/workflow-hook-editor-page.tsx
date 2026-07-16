"use client";

import { useCurrentWorkspace } from "@multica/core/paths";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import { useNavigation } from "@multica/views/navigation";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { createHookDraft, type WorkflowHook } from "../../core/hook-types";
import { createWorkflowHook, publishWorkflowHook, testWorkflowHook, updateWorkflowHook } from "../../core/hook-api";
import { workflowHookDetailOptions, workflowHookKeys, workflowHookRunsOptions } from "../../core/queries";
import { HookEditor } from "./hook-editor";
import { useHookDirectory } from "./use-hook-directory";

export function WorkflowHookEditorPage({ hookId }: { hookId?: string }) {
  const enabled = useFeatureFlag("cerebro_workflow_hooks");
  const workspace = useCurrentWorkspace();
  const navigation = useNavigation();
  if (!enabled) return null;
  if (!workspace) return <div className="flex h-full items-center justify-center text-sm text-muted-foreground">Loading workspace context…</div>;
	return <WorkflowHookEditorLoaded wsId={workspace.id} workspaceSlug={workspace.slug} hookId={hookId} onNavigate={(path) => navigation.push(path)} />;
}

function WorkflowHookEditorLoaded({ wsId, workspaceSlug, hookId, onNavigate }: { wsId: string; workspaceSlug: string; hookId?: string; onNavigate: (path: string) => void }) {
	const queryClient = useQueryClient();
	const directory = useHookDirectory(wsId);
	const detail = useQuery(workflowHookDetailOptions(wsId, hookId ?? ""));
	const runs = useQuery(workflowHookRunsOptions(wsId, hookId ?? ""));
	const save = useMutation({
		mutationFn: (hook: WorkflowHook) => hookId ? updateWorkflowHook(hookId, hook) : createWorkflowHook(hook),
		onSuccess: (saved) => {
			queryClient.invalidateQueries({ queryKey: workflowHookKeys.all(wsId) });
			if (!hookId && saved.id) onNavigate(`/${workspaceSlug}/workflows/hooks/${saved.id}`);
		},
	});
	const publish = useMutation({ mutationFn: () => publishWorkflowHook(hookId ?? ""), onSuccess: () => queryClient.invalidateQueries({ queryKey: workflowHookKeys.all(wsId) }) });
	const test = useMutation({
		mutationFn: () => testWorkflowHook(hookId ?? "", {}),
		onSuccess: () => {
			queryClient.invalidateQueries({ queryKey: workflowHookKeys.detail(wsId, hookId ?? "") });
			queryClient.invalidateQueries({ queryKey: workflowHookKeys.runs(wsId, hookId ?? "") });
		},
	});
	if (hookId && detail.isLoading) return <div className="flex h-full items-center justify-center text-sm text-muted-foreground">Loading hook…</div>;
	if (hookId && detail.isError) return <div className="flex h-full items-center justify-center text-sm text-destructive">Failed to load hook.</div>;
	const hook = detail.data ?? createHookDraft();
	return <HookEditor initialHook={hook} directory={directory} runs={runs.data ?? []} canPublish={hook.can_publish === true} onTest={() => test.mutate()} onBack={() => onNavigate(`/${workspaceSlug}/workflows/hooks`)} onSave={(next) => next.mode === "enforce" ? publish.mutate() : save.mutate(next)} />;
}
