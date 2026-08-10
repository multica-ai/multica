"use client";

import { useState } from "react";
import { useCurrentWorkspace } from "@multica/core/paths";
import { useNavigation } from "@multica/views/navigation";
import { useQuery } from "@tanstack/react-query";
import { activeHookRulesOptions, recentHookRunsOptions, workflowHooksListOptions } from "../../core/queries";
import { ActiveRulesPanel } from "./active-rules-panel";
import { HookHistoryPage } from "./hook-history-page";
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
	const [agentId, setAgentId] = useState("");
	const [issueId, setIssueId] = useState("");
	const hooks = useQuery(workflowHooksListOptions(wsId));
	const activeRules = useQuery(activeHookRulesOptions(wsId, agentId, issueId));
	const directory = useHookDirectory(wsId);
	if (hooks.isLoading) return <div className="flex h-full items-center justify-center text-sm text-muted-foreground">Loading hooks…</div>;
	if (hooks.isError) return <div className="flex h-full items-center justify-center text-sm text-destructive">Failed to load hooks.</div>;
	return <HooksPage hooks={hooks.data?.hooks ?? []} partialErrors={hooks.data?.partial_errors ?? []} directory={directory} activeRulesPanel={<ActiveRulesPanel directory={directory} agentId={agentId} issueId={issueId} onAgentChange={setAgentId} onIssueChange={setIssueId} rules={activeRules.data?.rules ?? []} loading={activeRules.isLoading && Boolean(agentId && issueId)} error={activeRules.isError && Boolean(agentId && issueId)} />} onOpenHook={(id) => onNavigate(id ? `${base}/${id}` : `${base}/new`)} onOpenHistory={() => onNavigate(`${base}/history`)} />;
}

export function WorkflowHookHistoryPage() {
	const workspace = useCurrentWorkspace();
	const navigation = useNavigation();
	if (!workspace) return <div className="flex h-full items-center justify-center text-sm text-muted-foreground">Loading workspace context…</div>;
	const base = `/${workspace.slug}/workflows/hooks`;
	return <WorkflowHookHistoryLoaded wsId={workspace.id} base={base} onNavigate={(path) => navigation.push(path)} />;
}

function WorkflowHookHistoryLoaded({ wsId, base, onNavigate }: { wsId: string; base: string; onNavigate: (path: string) => void }) {
	const runs = useQuery(recentHookRunsOptions(wsId));
	const directory = useHookDirectory(wsId);
	return <HookHistoryPage runs={runs.data ?? []} directory={directory} loading={runs.isLoading} error={runs.isError} onBack={() => onNavigate(base)} onOpenHook={(familyId) => onNavigate(`${base}/${familyId}`)} />;
}
