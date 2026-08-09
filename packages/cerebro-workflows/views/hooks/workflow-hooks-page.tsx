"use client";

import { useCurrentWorkspace } from "@multica/core/paths";
import { useNavigation } from "@multica/views/navigation";
import { useQuery } from "@tanstack/react-query";
import { workflowHooksListOptions } from "../../core/queries";
import { HooksPage } from "./hooks-page";
import { useHookDirectory } from "./use-hook-directory";

export function WorkflowHooksPage() {
  const workspace = useCurrentWorkspace();
  const navigation = useNavigation();
  if (!workspace) return <div className="flex h-full items-center justify-center text-sm text-muted-foreground">Loading workspace context…</div>;
  const base = `/${workspace.slug}/workflows/hooks`;
	return <WorkflowHooksPageLoaded wsId={workspace.id} base={base} onNavigate={(path) => navigation.push(path)} />;
}

function WorkflowHooksPageLoaded({ wsId, base, onNavigate }: { wsId: string; base: string; onNavigate: (path: string) => void }) {
	const hooks = useQuery(workflowHooksListOptions(wsId));
	const directory = useHookDirectory(wsId);
	if (hooks.isLoading) return <div className="flex h-full items-center justify-center text-sm text-muted-foreground">Loading hooks…</div>;
	if (hooks.isError) return <div className="flex h-full items-center justify-center text-sm text-destructive">Failed to load hooks.</div>;
	return <HooksPage hooks={hooks.data?.hooks ?? []} partialErrors={hooks.data?.partial_errors ?? []} directory={directory} onOpenHook={(id) => onNavigate(id ? `${base}/${id}` : `${base}/new`)} />;
}
