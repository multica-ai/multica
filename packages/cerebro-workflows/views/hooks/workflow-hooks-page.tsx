"use client";

import { useCurrentWorkspace } from "@multica/core/paths";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import { useNavigation } from "@multica/views/navigation";
import { useQuery } from "@tanstack/react-query";
import { workflowHooksListOptions } from "../../core/queries";
import { HooksPage } from "./hooks-page";

export function WorkflowHooksPage() {
  const enabled = useFeatureFlag("cerebro_workflow_hooks");
  const workspace = useCurrentWorkspace();
  const navigation = useNavigation();
  if (!enabled) return null;
  if (!workspace) return <div className="flex h-full items-center justify-center text-sm text-muted-foreground">Loading workspace context…</div>;
  const base = `/${workspace.slug}/workflows/hooks`;
	return <WorkflowHooksPageLoaded wsId={workspace.id} base={base} onNavigate={(path) => navigation.push(path)} />;
}

function WorkflowHooksPageLoaded({ wsId, base, onNavigate }: { wsId: string; base: string; onNavigate: (path: string) => void }) {
	const hooks = useQuery(workflowHooksListOptions(wsId));
	if (hooks.isLoading) return <div className="flex h-full items-center justify-center text-sm text-muted-foreground">Loading hooks…</div>;
	if (hooks.isError) return <div className="flex h-full items-center justify-center text-sm text-destructive">Failed to load hooks.</div>;
	return <HooksPage hooks={hooks.data ?? []} onOpenHook={(id) => onNavigate(id ? `${base}/${id}` : `${base}/new`)} />;
}
