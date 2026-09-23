"use client";

import { useEffect, useState } from "react";
import { MessageCircle } from "lucide-react";
import { useQueries, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, ApiError } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import { issueTasksOptions } from "@multica/core/issues/queries";
import { taskMessagesOptions } from "@multica/core/chat/queries";
import { taskInteractionOptions, useTaskInteraction, useIssueConversationDrafts, type TaskInteractionCommand } from "@multica/core/chat";
import type { AgentTask } from "@multica/core/types";
import { createSafeId } from "@multica/core/utils";
import { Button } from "@multica/ui/components/ui/button";
import { Tooltip, TooltipContent, TooltipTrigger } from "@multica/ui/components/ui/tooltip";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { Field, FieldGroup, FieldLabel, FieldError } from "@multica/ui/components/ui/field";
import { Alert, AlertDescription } from "@multica/ui/components/ui/alert";
import { AgentTranscriptDialog } from "../../common/task-transcript/agent-transcript-dialog";
import { Tabs, TabsList, TabsTrigger } from "@multica/ui/components/ui/tabs";
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from "@multica/ui/components/ui/select";
import { IssueConversationChat } from "./issue-conversation-chat";
import { conversationHumanEntries, issueConversationTimeline } from "./issue-conversation-timeline";
import { IssueFullChat } from "./issue-full-chat";
import { useT } from "../../i18n";
import { useIssueConversationView } from "./issue-conversation-view-context";

export function IssueConversationButton({ issueId, tasks }: { issueId: string; tasks: AgentTask[] }) {
  const { t } = useT("agents");
  const [open, setOpen] = useState(false);
  const inlineView = useIssueConversationView();
  if (!tasks.length) return null;
  return <>
    <Tooltip>
      <TooltipTrigger render={<Button variant="ghost" size="icon-sm" className="text-muted-foreground"
        aria-label={t(($) => $.interaction.open)} onClick={() => {
          if (inlineView?.issueId === issueId) inlineView.open((tasks.find((task) => task.status === "running") ?? tasks[0]!).id);
          else setOpen(true);
        }}><MessageCircle aria-hidden="true" /></Button>} />
      <TooltipContent side="bottom">{t(($) => $.interaction.open)}</TooltipContent>
    </Tooltip>
    {open && <IssueConversation issueId={issueId} initialTasks={tasks} onClose={() => setOpen(false)} />}
  </>;
}

export function IssueConversation({ issueId, initialTasks, onClose, initialRunId, inline = false }: { issueId: string; initialTasks: AgentTask[]; onClose: () => void; initialRunId?: string; inline?: boolean }) {
  const { t } = useT("agents");
  const { data: tasks = initialTasks } = useQuery({ ...issueTasksOptions(issueId), initialData: initialTasks, refetchInterval: 1000 });
  const [selectedId, setSelectedId] = useState<string | null>(initialRunId ?? null);
  const [fullChat, setFullChat] = useState(true);
  const [agentId, setAgentId] = useState(() => (initialTasks.find((task) => task.id === initialRunId)
    ?? initialTasks.find((task) => task.status === "running") ?? initialTasks[0])?.agent_id);
  const runs = tasks.filter((task) => task.issue_id === issueId && task.agent_id === agentId)
    .sort((a, b) => Date.parse(a.created_at) - Date.parse(b.created_at) || a.id.localeCompare(b.id));
  const running = runs.filter((task) => !["completed", "cancelled", "failed"].includes(task.status));
  const latest = running[0] ?? runs.at(-1);
  const task = fullChat ? latest : tasks.find((task) => task.id === selectedId) ?? latest;
  const agentIds = [...new Set(tasks.filter((run) => run.issue_id === issueId).map((run) => run.agent_id))];
  if (!task) return null;
  return <IssueRunConversation key={task.id} task={task} onClose={onClose} onFollowup={setSelectedId} inline={inline}
    ambiguous={running.length > 1} fullRuns={fullChat ? runs : undefined}
    agentSelector={agentIds.length > 1 ? <ConversationAgentSelector agentIds={agentIds} selectedId={task.agent_id}
      onSelect={(value) => { setAgentId(value); setSelectedId(null); setFullChat(true); }} /> : undefined}
    history={<div className="flex flex-col gap-2">
      <div className="flex flex-wrap gap-2" role="navigation" aria-label={t(($) => $.interaction.history)}>
      <Button variant={fullChat ? "secondary" : "ghost"} size="xs" aria-current={fullChat ? "true" : undefined} onClick={() => setFullChat(true)}>{t(($) => $.interaction.full_chat)}</Button>
      {runs.map((run) => <Button key={run.id} variant={!fullChat && run.id === task.id ? "secondary" : "ghost"} size="xs" aria-current={!fullChat && run.id === task.id ? "true" : undefined} onClick={() => { setSelectedId(run.id); setFullChat(false); }}>
        {t(($) => $.interaction.run, { number: tasks.filter((candidate) => candidate.agent_id === run.agent_id)
          .sort((a, b) => Date.parse(a.created_at) - Date.parse(b.created_at) || a.id.localeCompare(b.id)).findIndex((candidate) => candidate.id === run.id) + 1 })} · {run.status}
      </Button>)}
      </div>
    </div>}
  />;
}

function ConversationAgentSelector({ agentIds, selectedId, onSelect }: { agentIds: string[]; selectedId: string; onSelect: (id: string) => void }) {
  const workspaceId = useWorkspaceId();
  const { t } = useT("agents");
  const agents = useQueries({ queries: agentIds.map((id) => ({ queryKey: ["issue-conversation-agent", workspaceId, id], queryFn: () => api.getAgent(id), staleTime: 30000 })) });
  const items = agentIds.map((id, index) => ({ value: id, label: agents[index]?.data?.name ?? id }));
  return <Select items={items} value={selectedId} onValueChange={(value) => { if (value) onSelect(value); }}>
    <SelectTrigger size="sm" aria-label={t(($) => $.interaction.agent)}><SelectValue /></SelectTrigger>
    <SelectContent align="start" alignItemWithTrigger={false}><SelectGroup>
      {items.map((item) => <SelectItem key={item.value} value={item.value}>{item.label}</SelectItem>)}
    </SelectGroup></SelectContent>
  </Select>;
}

function IssueRunConversation({ task, history, agentSelector, ambiguous, onClose, onFollowup, inline, fullRuns }: {
  task: AgentTask; history: React.ReactNode; ambiguous: boolean; onClose: () => void; onFollowup: (id: string) => void;
  agentSelector?: React.ReactNode;
  inline?: boolean;
  fullRuns?: AgentTask[];
}) {
  const { t } = useT("agents");
  const workspaceId = useWorkspaceId();
  const queryClient = useQueryClient();
  const interactionQuery = useQuery(taskInteractionOptions(workspaceId, task.id, true));
  const record = interactionQuery.data;
  const { data: agent } = useQuery({ queryKey: ["issue-conversation-agent", workspaceId, task.agent_id], queryFn: () => api.getAgent(task.agent_id), staleTime: 30000 });
  const { data: messages = [] } = useQuery({ ...taskMessagesOptions(task.id), refetchInterval: task.status === "running" ? 1000 : false });
  const mutation = useTaskInteraction(workspaceId, task.id);
  const draftKey = `${workspaceId}:${task.issue_id}:${task.id}`;
  const draft = useIssueConversationDrafts((state) => state.drafts[draftKey]);
  const setDraft = useIssueConversationDrafts((state) => state.setDraft);
  const text = draft?.text ?? "";
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [view, setView] = useState("chat");
  const terminal = ["completed", "cancelled", "failed"].includes(task.status);
  const pending = record?.commands.some((command) => !command.receipt && !command.error) ?? false;
  const connected = !!record && Date.now() - new Date(record.updatedAt).getTime() < 15000;
  const ready = !!record && !ambiguous && (terminal || (connected && !record.finishing && ["working", "awaiting_input"].includes(record.state.state)));
  const disabled = !ready || pending || mutation.isPending || busy;
  const stateLabel = !record ? t(($) => $.interaction.unavailable)
    : terminal ? t(($) => $.interaction.finished)
    : !connected ? t(($) => $.interaction.disconnected)
    : record.state.state === "awaiting_input" ? t(($) => $.interaction.awaiting)
    : record.state.state === "interrupting" || record.commands.some((command) => command.kind === "interrupt" && !command.receipt && !command.error) ? t(($) => $.interaction.interrupting)
    : t(($) => $.interaction.working);

  useEffect(() => {
    if (!draft?.pending || draft.followup) return;
    const accepted = record?.commands.find((command) => command.id === draft.pending?.id);
    if (accepted) setDraft(draftKey, { text: accepted.error ? draft.text : draft.pending.kind === "input" ? "" : draft.text });
  }, [draft, draftKey, record, setDraft]);

  const send = async (kind: "input" | "interrupt" | "finish") => {
    if (!record || disabled) return;
    setError("");
    const command: TaskInteractionCommand = draft?.pending ?? {
      owner: record.owner, id: createSafeId(), activity: record.state.activity, kind,
      ...(kind === "input" ? { text } : {}),
    };
    // An uncertain live send must never silently become a new run's prompt.
    const followup = draft?.pending ? draft.followup === true : terminal;
    setDraft(draftKey, { text, pending: command, followup });
    setBusy(true);
    try {
      if (followup) {
        const result = await api.followupTaskInteraction(task.id, command.id, command.text ?? "");
        if (!result) throw new Error(t(($) => $.interaction.uncertain));
        setDraft(draftKey, { text: "" });
        await queryClient.invalidateQueries({ queryKey: issueTasksOptions(task.issue_id).queryKey });
        onFollowup(result.runId);
      } else {
        const result = await mutation.mutateAsync(command);
        if (!result) throw new Error(t(($) => $.interaction.uncertain));
        setDraft(draftKey, { text: command.kind === "input" ? "" : text });
      }
    } catch (cause) {
      if (cause instanceof ApiError && cause.status >= 400 && cause.status < 500) setDraft(draftKey, { text });
      setError(cause instanceof Error ? cause.message : String(cause));
    } finally { setBusy(false); }
  };

  // Human controls are projections of their durable records, not duplicate
  // task-message writes. Existing rendering retains tool IDs and file diffs.
  const items = issueConversationTimeline(messages, conversationHumanEntries(record, task.created_at, {
    human: t(($) => $.interaction.human), interrupt: t(($) => $.interaction.interrupt),
    finish: t(($) => $.interaction.finish), pending: t(($) => $.interaction.pending),
  }));

  return <AgentTranscriptDialog open inline={inline} onOpenChange={(open) => { if (!open) onClose(); }} task={task}
    agentName={agent?.name ?? t(($) => $.interaction.agent)} items={items} isLive={!terminal}
    showOutcomeInConversation={!fullRuns}
    agentNameSlot={agentSelector}
    headerSlot={history}
    headerActions={!fullRuns && <Tabs value={view} onValueChange={(value) => { if (typeof value === "string") setView(value); }}>
        <TabsList variant="line" aria-label={t(($) => $.interaction.view)}>
          <TabsTrigger value="chat"><MessageCircle aria-hidden="true" />{t(($) => $.interaction.chat)}</TabsTrigger>
          <TabsTrigger value="logs">{t(($) => $.interaction.logs)}</TabsTrigger>
        </TabsList>
      </Tabs>}
    conversationSlot={fullRuns ? <IssueFullChat runs={fullRuns} /> : view === "chat" ? <IssueConversationChat items={items} isLive={!terminal} /> : undefined}
    footerSlot={<div className="max-h-[45vh] shrink-0 overflow-y-auto p-4">
      <FieldGroup>
        <p role="status">{stateLabel}</p>
        {record && !terminal && <p className="text-caption text-muted-foreground">{t(($) => $.interaction.deadline, { time: new Date(record.deadline).toLocaleString() })}</p>}
        {ambiguous && <Alert><AlertDescription>{t(($) => $.interaction.ambiguous)}</AlertDescription></Alert>}
        <Field data-invalid={!!error}>
          <FieldLabel htmlFor={`live-input-${task.id}`}>{terminal ? t(($) => $.interaction.followup) : t(($) => $.interaction.message)}</FieldLabel>
          <Textarea id={`live-input-${task.id}`} value={text} maxLength={65536} disabled={!!draft?.pending || !record}
            aria-invalid={!!error} onChange={(event) => setDraft(draftKey, { text: event.target.value })}
            onKeyDown={(event) => { if ((event.metaKey || event.ctrlKey) && event.key === "Enter") { event.preventDefault(); void send("input"); } }} />
          {error && <FieldError>{error}</FieldError>}
        </Field>
        <div className="flex flex-wrap gap-2">
          <Button disabled={disabled || (!draft?.pending && !text.trim())} onClick={() => void send(draft?.pending?.kind ?? "input")}>
            {draft?.pending ? t(($) => $.interaction.retry) : terminal ? t(($) => $.interaction.followup) : t(($) => $.interaction.send)}
          </Button>
          {!terminal && <>
            <Button variant="outline" disabled={disabled || !!draft?.pending || record?.state.state !== "working"} onClick={() => void send("interrupt")}>{t(($) => $.interaction.interrupt)}</Button>
            <Button variant="outline" disabled={disabled || !!draft?.pending || record?.state.state !== "awaiting_input"} onClick={() => void send("finish")}>{t(($) => $.interaction.finish)}</Button>
            <Button variant="ghost" disabled={busy} onClick={async () => {
              setBusy(true); setError("");
              try { await api.cancelTaskById(task.id); await queryClient.invalidateQueries({ queryKey: issueTasksOptions(task.issue_id).queryKey }); }
              catch (cause) { setError(cause instanceof Error ? cause.message : String(cause)); }
              finally { setBusy(false); }
            }}>{t(($) => $.interaction.cancel)}</Button>
          </>}
        </div>
        {(record?.commands ?? []).flatMap((command) => [...(command.receipt?.clearedSteering ?? []), ...(command.receipt?.clearedFollowUp ?? [])].map((cleared, index) => (
          <details key={`${command.id}-${index}`}><summary>{t(($) => $.interaction.cleared)}</summary><p className="whitespace-pre-wrap">{cleared}</p></details>
        )))}
      </FieldGroup>
    </div>}
  />;
}
