"use client";

import { useEffect, useMemo, useState } from "react";
import { toast } from "sonner";
import { useQuery } from "@tanstack/react-query";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { Button } from "@multica/ui/components/ui/button";
import { useDispatchThreadIssueTask } from "@multica/core/channels";
import { useWorkspaceId } from "@multica/core/hooks";
import { useAuthStore } from "@multica/core/auth";
import {
  agentListOptions,
  memberListOptions,
} from "@multica/core/workspace/queries";
import { projectListOptions } from "@multica/core/projects/queries";
import { ApiError } from "@multica/core/api";
import { canAssignAgent } from "../../issues/components/pickers/assignee-picker";
import { useT } from "../../i18n";

interface ThreadToIssueDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  channelId: string;
  parentMessageId: string;
}

/**
 * Dialog body for "Convert thread → issue task" — opened from the
 * channel ThreadPanel header. Picks an agent (required), optionally a
 * project, optionally a parent issue identifier, and optional free-text
 * instruction. On submit, POSTs to the dispatch endpoint and toasts
 * success/failure.
 *
 * The agent runs in **issue-task mode** (full `multica issue ...`
 * access). This is the documented escape hatch for the channel-mention
 * constraint that forbids agents from creating issues during chat.
 */
export function ThreadToIssueDialog({
  open,
  onOpenChange,
  channelId,
  parentMessageId,
}: ThreadToIssueDialogProps) {
  const { t } = useT("channels");
  const wsId = useWorkspaceId();
  const userId = useAuthStore((s) => s.user?.id);

  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { data: projects = [] } = useQuery(projectListOptions(wsId));

  const memberRole = useMemo(
    () => members.find((m) => m.user_id === userId)?.role,
    [members, userId],
  );

  // Visible = not archived AND assignable by this user. Mirrors the
  // quick-create modal's filter so the user sees the same agent list
  // here as they do for "create issue → assign agent."
  const visibleAgents = useMemo(
    () =>
      agents.filter(
        (a) => !a.archived_at && canAssignAgent(a, userId, memberRole),
      ),
    [agents, userId, memberRole],
  );

  const visibleProjects = useMemo(
    () => projects.filter((p) => !p.archived_at),
    [projects],
  );

  const dispatch = useDispatchThreadIssueTask(channelId, parentMessageId);

  const [agentId, setAgentId] = useState<string>("");
  const [projectId, setProjectId] = useState<string>("");
  const [parentIssue, setParentIssue] = useState<string>("");
  const [instruction, setInstruction] = useState<string>("");

  // Reset form whenever the dialog opens for a fresh thread. Default
  // assignee = the first visible agent (typically the workspace's
  // primary coding agent), so the most common path is a single-click
  // from open → submit.
  useEffect(() => {
    if (!open) return;
    setAgentId(visibleAgents[0]?.id ?? "");
    setProjectId("");
    setParentIssue("");
    setInstruction("");
    // Intentionally exclude `visibleAgents` so re-renders that change
    // its identity don't reset user input mid-dialog.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, parentMessageId]);

  const handleSubmit = async () => {
    if (!agentId) return;
    try {
      await dispatch.mutateAsync({
        agent_id: agentId,
        project_id: projectId || undefined,
        parent_issue_id: parentIssue.trim() || undefined,
        instruction: instruction.trim() || undefined,
      });
      const agent = visibleAgents.find((a) => a.id === agentId);
      toast.success(
        t(($) => $.thread_to_issue_dialog.dispatched_toast, {
          agent: agent?.name ?? "agent",
        }),
      );
      onOpenChange(false);
    } catch (e) {
      const message =
        e instanceof ApiError
          ? e.message
          : e instanceof Error
            ? e.message
            : String(e);
      toast.error(
        t(($) => $.thread_to_issue_dialog.dispatch_failed, { message }),
      );
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{t(($) => $.thread_to_issue_dialog.title)}</DialogTitle>
          <DialogDescription>
            {t(($) => $.thread_to_issue_dialog.description)}
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-4 py-2">
          <div className="space-y-2">
            <Label htmlFor="thread-issue-agent">
              {t(($) => $.thread_to_issue_dialog.agent_label)}
            </Label>
            {visibleAgents.length === 0 ? (
              <p className="text-sm text-muted-foreground">
                {t(($) => $.thread_to_issue_dialog.no_agents)}
              </p>
            ) : (
              <Select value={agentId} onValueChange={(v) => setAgentId(v ?? "")}>
                <SelectTrigger id="thread-issue-agent">
                  <SelectValue
                    placeholder={t(
                      ($) => $.thread_to_issue_dialog.agent_placeholder,
                    )}
                  />
                </SelectTrigger>
                <SelectContent>
                  {visibleAgents.map((a) => (
                    <SelectItem key={a.id} value={a.id}>
                      {a.name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            )}
          </div>

          <div className="space-y-2">
            <Label htmlFor="thread-issue-project">
              {t(($) => $.thread_to_issue_dialog.project_label)}
            </Label>
            <Select
              value={projectId || "__none__"}
              onValueChange={(v) => setProjectId(v && v !== "__none__" ? v : "")}
            >
              <SelectTrigger id="thread-issue-project">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="__none__">
                  {t(($) => $.thread_to_issue_dialog.project_none)}
                </SelectItem>
                {visibleProjects.map((p) => (
                  <SelectItem key={p.id} value={p.id}>
                    {p.title}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          <div className="space-y-2">
            <Label htmlFor="thread-issue-parent">
              {t(($) => $.thread_to_issue_dialog.parent_issue_label)}
            </Label>
            <Input
              id="thread-issue-parent"
              value={parentIssue}
              onChange={(e) => setParentIssue(e.target.value)}
              placeholder={t(
                ($) => $.thread_to_issue_dialog.parent_issue_placeholder,
              )}
            />
          </div>

          <div className="space-y-2">
            <Label htmlFor="thread-issue-instruction">
              {t(($) => $.thread_to_issue_dialog.instruction_label)}
            </Label>
            <Textarea
              id="thread-issue-instruction"
              value={instruction}
              onChange={(e) => setInstruction(e.target.value)}
              placeholder={t(
                ($) => $.thread_to_issue_dialog.instruction_placeholder,
              )}
              rows={3}
            />
          </div>
        </div>

        <DialogFooter>
          <Button
            variant="outline"
            onClick={() => onOpenChange(false)}
            disabled={dispatch.isPending}
          >
            {t(($) => $.thread_to_issue_dialog.cancel)}
          </Button>
          <Button
            onClick={handleSubmit}
            disabled={!agentId || dispatch.isPending}
          >
            {dispatch.isPending
              ? t(($) => $.thread_to_issue_dialog.submitting)
              : t(($) => $.thread_to_issue_dialog.submit)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
