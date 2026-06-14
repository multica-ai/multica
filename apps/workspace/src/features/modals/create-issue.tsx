"use client";

import { useEffect, useState, useRef } from "react";
import { CalendarDays, Check, ChevronRight, Download, Loader2, Maximize2, Mic, Minimize2, Paperclip, Pencil, Shapes, Square, Trash2, UserMinus, X as XIcon } from "lucide-react";
import { cn } from "@/lib/utils";
import { toast } from "sonner";
import type { UpdateIssueRequest, IssueStatus, IssuePriority, IssueAssigneeType } from "@/shared/types";
import {
  Dialog,
  DialogContent,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import {
  Popover,
  PopoverTrigger,
  PopoverContent,
} from "@/components/ui/popover";
import { Tooltip, TooltipTrigger, TooltipContent } from "@/components/ui/tooltip";
import { Button } from "@/components/ui/button";
import { ContentEditor, type ContentEditorRef } from "@/features/editor";
import { TitleEditor, type TitleEditorRef } from "@/features/editor";
import { StatusIcon, PriorityIcon, DueDatePicker, IssueDateTimePicker, ParentIssuePicker } from "@/features/issues/components";
import { ALL_STATUSES, STATUS_CONFIG, PRIORITY_ORDER, PRIORITY_CONFIG } from "@/features/issues/config";
import { ProjectPicker } from "@/features/projects/components/project-picker";
import { useWorkspaceStore, useActorName } from "@/features/workspace";
import { useIssueMutations } from "@/features/issues/mutations";
import { useIssueStore } from "@/features/issues";
import { useIssueDraftStore } from "@/features/issues/stores/draft-store";
import { getCreateIssueInitialValues } from "@/features/issues/utils/template";
import { applyVoiceTranscriptToDraft } from "@/features/issues/utils/voice-transcript";
import { useIssueTranscription, useIssueVoiceRecorder } from "@/features/issues/hooks";
import { useIssueTypesQuery } from "@/features/issues/hooks";
import { useFileUpload } from "@/shared/hooks/use-file-upload";
import { FileUploadButton } from "@/components/common/file-upload-button";
import { ActorAvatar } from "@/components/common/actor-avatar";
import { useRouter } from "@/shared/router";
import { api } from "@/shared/api";
import { downloadBlob } from "@/shared/utils";

// ---------------------------------------------------------------------------
// Pill trigger — shared rounded-full button style for toolbar
// ---------------------------------------------------------------------------

function PillButton({
  children,
  className,
  ...props
}: React.ButtonHTMLAttributes<HTMLButtonElement>) {
  return (
    <button
      type="button"
      className={cn(
        "inline-flex items-center gap-1.5 rounded-full border px-2.5 py-1 text-xs",
        "hover:bg-accent/60 transition-colors cursor-pointer",
        className,
      )}
      {...props}
    >
      {children}
    </button>
  );
}

function shortDateTime(date: string | null): string {
  if (!date) return "";

  return new Date(date).toLocaleString("en-US", {
    month: "short",
    day: "numeric",
    hour: "numeric",
    minute: "2-digit",
  });
}

interface PendingIssueAttachment {
  id: string;
  filename: string;
}

// ---------------------------------------------------------------------------
// CreateIssueModal
// ---------------------------------------------------------------------------

export function CreateIssueModal({ onClose, data }: { onClose: () => void; data?: Record<string, unknown> | null }) {
  const router = useRouter();
  const workspaceName = useWorkspaceStore((s) => s.workspace?.name);
  const members = useWorkspaceStore((s) => s.members);
  const agents = useWorkspaceStore((s) => s.agents);
  const { getActorName } = useActorName();
  const { createIssue, addIssueLabel } = useIssueMutations();

  const draft = useIssueDraftStore((s) => s.draft);
  const setDraft = useIssueDraftStore((s) => s.setDraft);
  const clearDraft = useIssueDraftStore((s) => s.clearDraft);
  const initialValues = getCreateIssueInitialValues(draft, data);

  const [title, setTitle] = useState(initialValues.title);
  const titleEditorRef = useRef<TitleEditorRef>(null);
  const descEditorRef = useRef<ContentEditorRef>(null);
  const [status, setStatus] = useState<IssueStatus>(initialValues.status);
  const [selectedProjectId, setSelectedProjectId] = useState<string | undefined>(initialValues.projectId);
  const [priority, setPriority] = useState<IssuePriority>(initialValues.priority);
  const [issueTypeId, setIssueTypeId] = useState<string | undefined>(undefined);
  const [submitting, setSubmitting] = useState(false);
  const [assigneeType, setAssigneeType] = useState<IssueAssigneeType | undefined>(initialValues.assigneeType);
  const [assigneeId, setAssigneeId] = useState<string | undefined>(initialValues.assigneeId);
  const [parentIssueId, setParentIssueId] = useState<string | undefined>(initialValues.parentIssueId);
  const [dueDate, setDueDate] = useState<string | null>(initialValues.dueDate);
  const [startDate, setStartDate] = useState<string | null>(initialValues.startDate);
  const [endDate, setEndDate] = useState<string | null>(initialValues.endDate);
  const [isExpanded, setIsExpanded] = useState(false);

  // Assignee popover
  const [assigneeOpen, setAssigneeOpen] = useState(false);
  const [assigneeFilter, setAssigneeFilter] = useState("");

  // File upload
  const { upload, uploadWithToast } = useFileUpload();
  const [pendingAttachments, setPendingAttachments] = useState<PendingIssueAttachment[]>([]);
  const [editingAttachmentId, setEditingAttachmentId] = useState<string | null>(null);
  const [editingAttachmentFilename, setEditingAttachmentFilename] = useState("");
  const [deletingAttachmentId, setDeletingAttachmentId] = useState<string | null>(null);
  const [downloadingAttachmentId, setDownloadingAttachmentId] = useState<string | null>(null);
  const voiceRecorder = useIssueVoiceRecorder();
  const voiceTranscription = useIssueTranscription();
  const [voiceTranscript, setVoiceTranscript] = useState("");
  const [keepOriginalRecording, setKeepOriginalRecording] = useState(false);
  const [pendingVoiceTranscription, setPendingVoiceTranscription] = useState(false);
  const { data: issueTypes = [] } = useIssueTypesQuery();

  const assigneeQuery = assigneeFilter.toLowerCase();
  const filteredMembers = members.filter((m) => m.name.toLowerCase().includes(assigneeQuery));
  const filteredAgents = agents.filter((a) => !a.archived_at && a.name.toLowerCase().includes(assigneeQuery));

  const assigneeLabel =
    assigneeType && assigneeId
      ? getActorName(assigneeType, assigneeId)
      : "Assignee";
  const selectedIssueType = issueTypes.find((item) => item.id === issueTypeId)
    ?? issueTypes.find((item) => item.key === "task")
    ?? issueTypes[0];

  const dueDateObj = dueDate ? new Date(dueDate) : undefined;

  // Sync field changes to draft store
  const updateTitle = (v: string) => { setTitle(v); setDraft({ title: v }); };
  const updateStatus = (v: IssueStatus) => { setStatus(v); setDraft({ status: v }); };
  const updatePriority = (v: IssuePriority) => { setPriority(v); setDraft({ priority: v }); };
  const updateAssignee = (type?: IssueAssigneeType, id?: string) => {
    setAssigneeType(type); setAssigneeId(id);
    setDraft({ assigneeType: type, assigneeId: id });
  };
  const updateParentIssue = (value?: string | null) => {
    const nextValue = value ?? undefined;
    setParentIssueId(nextValue);
    setDraft({ parentIssueId: nextValue });
  };
  const updateDueDate = (v: string | null) => { setDueDate(v); setDraft({ dueDate: v }); };
  const updateStartDate = (v: string | null) => { setStartDate(v); setDraft({ startDate: v }); };
  const updateEndDate = (v: string | null) => { setEndDate(v); setDraft({ endDate: v }); };
  const handleDateUpdate = (updates: Partial<UpdateIssueRequest>) => {
    if ("start_date" in updates) {
      updateStartDate(updates.start_date ?? null);
    }
    if ("end_date" in updates) {
      updateEndDate(updates.end_date ?? null);
    }
    if ("due_date" in updates) {
      updateDueDate(updates.due_date ?? null);
    }
  };

  const handleUpload = async (file: File) => {
    const result = await uploadWithToast(file);
    if (result) {
      setPendingAttachments((current) => [...current, { id: result.id, filename: result.filename }]);
    }
    return result;
  };

  const startEditingAttachment = (attachment: PendingIssueAttachment) => {
    setEditingAttachmentId(attachment.id);
    setEditingAttachmentFilename(attachment.filename);
  };

  const cancelEditingAttachment = () => {
    setEditingAttachmentId(null);
    setEditingAttachmentFilename("");
  };

  const saveAttachmentFilename = async (attachment: PendingIssueAttachment) => {
    const filename = editingAttachmentFilename.trim();
    if (!filename) {
      toast.error("Filename is required");
      return;
    }
    try {
      const updated = await api.updateAttachment(attachment.id, { filename });
      setPendingAttachments((current) =>
        current.map((item) => (item.id === attachment.id ? { ...item, filename: updated.filename } : item)),
      );
      cancelEditingAttachment();
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "Failed to update attachment");
    }
  };

  const deletePendingAttachment = async (attachment: PendingIssueAttachment) => {
    setDeletingAttachmentId(attachment.id);
    try {
      await api.deleteAttachment(attachment.id);
      setPendingAttachments((current) => current.filter((item) => item.id !== attachment.id));
      if (editingAttachmentId === attachment.id) {
        cancelEditingAttachment();
      }
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "Failed to delete attachment");
    } finally {
      setDeletingAttachmentId(null);
    }
  };

  const downloadPendingAttachment = async (attachment: PendingIssueAttachment) => {
    setDownloadingAttachmentId(attachment.id);
    try {
      const blob = await api.downloadAttachment(attachment.id);
      downloadBlob(blob, attachment.filename);
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "Failed to download attachment");
    } finally {
      setDownloadingAttachmentId(null);
    }
  };

  useEffect(() => {
    if (!issueTypeId && selectedIssueType?.id) {
      setIssueTypeId(selectedIssueType.id);
    }
  }, [issueTypeId, selectedIssueType?.id]);

  useEffect(() => {
    if (!pendingVoiceTranscription || !voiceRecorder.recording) return;
    const recording = voiceRecorder.recording;
    setPendingVoiceTranscription(false);
    void (async () => {
      const result = await voiceTranscription.transcribe(recording.file);
      if (result?.text) {
        setVoiceTranscript(result.text);
        setKeepOriginalRecording(true);
      } else {
        toast.error(voiceTranscription.error ?? "Transcription failed");
      }
    })();
  }, [pendingVoiceTranscription, voiceRecorder.recording, voiceTranscription]);

  const startVoiceCapture = async () => {
    setVoiceTranscript("");
    voiceTranscription.reset();
    await voiceRecorder.start();
  };

  const stopVoiceCapture = () => {
    setPendingVoiceTranscription(true);
    voiceRecorder.stop();
  };

  const discardVoiceTranscript = () => {
    setVoiceTranscript("");
    setKeepOriginalRecording(false);
    setPendingVoiceTranscription(false);
    voiceTranscription.reset();
    voiceRecorder.reset();
  };

  const insertVoiceTranscript = () => {
    const currentDescription = descEditorRef.current?.getMarkdown() ?? "";
    const nextDraft = applyVoiceTranscriptToDraft({
      title,
      description: currentDescription,
      transcript: voiceTranscript,
    });
    updateTitle(nextDraft.title);
    titleEditorRef.current?.setText(nextDraft.title);
    descEditorRef.current?.setMarkdown(nextDraft.description);
    setDraft({ description: nextDraft.description });
    if (nextDraft.titleNeedsManualConfirmation) {
      toast.error("Add a title before creating this issue");
    }
    setVoiceTranscript("");
    voiceTranscription.reset();
  };

  const handleSubmit = async () => {
    if (!title.trim() || submitting) return;
    setSubmitting(true);
    try {
      const issue = await createIssue({
        title: title.trim(),
        description: descEditorRef.current?.getMarkdown()?.trim() || undefined,
        status,
        priority,
        issue_type_id: issueTypeId,
        project_id: selectedProjectId,
        assignee_type: assigneeType,
        assignee_id: assigneeId,
        parent_issue_id: parentIssueId,
        start_date: startDate || undefined,
        end_date: endDate || undefined,
        due_date: dueDate || undefined,
      });

	      if (initialValues.labelIds.length > 0) {
        const labelResults = await Promise.allSettled(
          initialValues.labelIds.map((labelId) => addIssueLabel(issue.id, { labelId })),
        );
        if (labelResults.some((result) => result.status === "rejected")) {
          toast.error("Issue created, but some labels could not be copied");
        }
	      }

	      if (keepOriginalRecording && voiceRecorder.recording) {
	        try {
	          await upload(voiceRecorder.recording.file, { issueId: issue.id });
	        } catch {
	          toast.error("Issue created, but the voice recording was not preserved");
	        }
	      }

      if (pendingAttachments.length > 0) {
        try {
          await api.linkIssueAttachments(issue.id, pendingAttachments.map((attachment) => attachment.id));
        } catch {
          toast.error("Issue created, but some attachments were not linked");
        }
      }

	      clearDraft();
      onClose();
      toast.custom((t) => (
        <div className="bg-popover text-popover-foreground border rounded-lg shadow-lg p-4 w-90">
          <div className="flex items-center gap-2 mb-2">
            <div className="flex items-center justify-center size-5 rounded-full bg-emerald-500/15 text-emerald-500">
              <Check className="size-3" />
            </div>
            <span className="text-sm font-medium">Issue created</span>
          </div>
          <div className="flex items-center gap-2 text-sm text-muted-foreground ml-7">
            <StatusIcon status={issue.status} className="size-3.5 shrink-0" />
            <span className="truncate">{issue.identifier} – {issue.title}</span>
          </div>
          <button
            type="button"
            className="ml-7 mt-2 text-sm text-primary hover:underline cursor-pointer"
            onClick={() => {
              router.push(`/issues/${issue.id}`);
              toast.dismiss(t);
            }}
          >
            View issue
          </button>
        </div>
      ), { duration: 5000 });
    } catch {
      toast.error("Failed to create issue");
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <Dialog open onOpenChange={(v) => { if (!v) onClose(); }}>
      <DialogContent
        showCloseButton={false}
        className={cn(
          "p-0 gap-0 flex flex-col overflow-hidden",
          "top-1/2! left-1/2! -translate-x-1/2!",
          "transition-all! duration-300! ease-out!",
          isExpanded
            ? "max-w-4xl! w-full! h-5/6! -translate-y-1/2!"
            : "max-w-2xl! w-full! h-96! -translate-y-1/2!",
        )}
      >
        <DialogTitle className="sr-only">New Issue</DialogTitle>

        {/* Header */}
        <div className="flex items-center justify-between px-5 pt-3 pb-2 shrink-0">
          <div className="flex items-center gap-1.5 text-xs">
            <span className="text-muted-foreground">{workspaceName}</span>
            <ChevronRight className="size-3 text-muted-foreground/50" />
            <span className="font-medium">New issue</span>
          </div>
          <div className="flex items-center gap-1">
            <Tooltip>
              <TooltipTrigger
                render={
                  <button
                    onClick={() => setIsExpanded(!isExpanded)}
                    className="rounded-sm p-1.5 opacity-70 hover:opacity-100 hover:bg-accent/60 transition-all cursor-pointer"
                  >
                    {isExpanded ? <Minimize2 className="size-4" /> : <Maximize2 className="size-4" />}
                  </button>
                }
              />
              <TooltipContent side="bottom">{isExpanded ? "Collapse" : "Expand"}</TooltipContent>
            </Tooltip>
            <Tooltip>
              <TooltipTrigger
                render={
                  <button
                    onClick={onClose}
                    aria-label="Close new issue dialog"
                    className="rounded-sm p-1.5 opacity-70 hover:opacity-100 hover:bg-accent/60 transition-all cursor-pointer"
                  >
                    <XIcon className="size-4" />
                  </button>
                }
              />
              <TooltipContent side="bottom">Close</TooltipContent>
            </Tooltip>
          </div>
        </div>

        {/* Title */}
        <div className="px-5 pb-2 shrink-0">
          <TitleEditor
            ref={titleEditorRef}
            autoFocus
            defaultValue={initialValues.title}
            placeholder="Issue title"
            className="text-lg font-semibold"
            onChange={(v) => updateTitle(v)}
            onSubmit={handleSubmit}
          />
        </div>

        {/* Description — takes remaining space */}
        <div className="flex-1 min-h-0 overflow-y-auto px-5">
          <ContentEditor
            ref={descEditorRef}
            defaultValue={initialValues.description}
            placeholder="Add description..."
            onUpdate={(md) => setDraft({ description: md })}
            onUploadFile={handleUpload}
            debounceMs={500}
          />
        </div>

        {pendingAttachments.length > 0 && (
          <section aria-label="Pending issue attachments" className="border-t px-5 py-3 shrink-0 space-y-2">
            <div className="flex items-center gap-2 text-sm font-medium">
              <Paperclip className="size-4 text-muted-foreground" />
              <span>Attachments</span>
            </div>
            <div className="space-y-2">
              {pendingAttachments.map((attachment) => (
                <div key={attachment.id} className="flex items-center justify-between gap-3 rounded-md border bg-card px-3 py-2 text-sm">
                  {editingAttachmentId === attachment.id ? (
                    <input
                      value={editingAttachmentFilename}
                      onChange={(event) => setEditingAttachmentFilename(event.target.value)}
                      onKeyDown={(event) => {
                        if (event.key === "Enter") void saveAttachmentFilename(attachment);
                        if (event.key === "Escape") cancelEditingAttachment();
                      }}
                      className="min-w-0 flex-1 rounded-md border bg-background px-2 py-1 outline-none focus:ring-2 focus:ring-ring"
                      aria-label="Pending attachment filename"
                      autoFocus
                    />
                  ) : (
                    <span className="min-w-0 flex-1 truncate">{attachment.filename}</span>
                  )}
                  <div className="flex shrink-0 items-center gap-1">
                    {editingAttachmentId === attachment.id ? (
                      <>
                        <Button size="sm" variant="ghost" onClick={() => void saveAttachmentFilename(attachment)} aria-label="Save pending attachment">
                          <Check className="size-3.5" />
                        </Button>
                        <Button size="sm" variant="ghost" onClick={cancelEditingAttachment} aria-label="Cancel pending attachment edit">
                          <XIcon className="size-3.5" />
                        </Button>
                      </>
                    ) : (
                      <>
                        <Button size="sm" variant="ghost" onClick={() => startEditingAttachment(attachment)} aria-label="Rename pending attachment">
                          <Pencil className="size-3.5" />
                        </Button>
                        <Button
                          size="sm"
                          variant="ghost"
                          onClick={() => void downloadPendingAttachment(attachment)}
                          disabled={downloadingAttachmentId === attachment.id}
                          aria-label="Download pending attachment"
                        >
                          <Download className="size-3.5" />
                        </Button>
                        <Button
                          size="sm"
                          variant="ghost"
                          onClick={() => void deletePendingAttachment(attachment)}
                          disabled={deletingAttachmentId === attachment.id}
                          aria-label="Delete pending attachment"
                        >
                          <Trash2 className="size-3.5" />
                        </Button>
                      </>
                    )}
                  </div>
                </div>
              ))}
            </div>
          </section>
        )}

        {/* Property toolbar */}
        <div className="flex items-center gap-1.5 px-4 py-2 shrink-0 flex-wrap">
          {/* Status */}
          <DropdownMenu>
            <DropdownMenuTrigger
              render={
                <PillButton>
                  <StatusIcon status={status} className="size-3.5" />
                  <span>{STATUS_CONFIG[status].label}</span>
                </PillButton>
              }
            />
            <DropdownMenuContent align="start" className="w-44">
              {ALL_STATUSES.map((s) => (
                <DropdownMenuItem key={s} onClick={() => updateStatus(s)}>
                  <StatusIcon status={s} className="size-3.5" />
                  <span>{STATUS_CONFIG[s].label}</span>
                </DropdownMenuItem>
              ))}
            </DropdownMenuContent>
          </DropdownMenu>

          {/* Priority */}
          <DropdownMenu>
            <DropdownMenuTrigger
              render={
                <PillButton>
                  <PriorityIcon priority={priority} />
                  <span>{PRIORITY_CONFIG[priority].label}</span>
                </PillButton>
              }
            />
            <DropdownMenuContent align="start" className="w-44">
              {PRIORITY_ORDER.map((p) => (
                <DropdownMenuItem key={p} onClick={() => updatePriority(p)}>
                  <span className={`inline-flex items-center gap-1 rounded px-1.5 py-0.5 text-xs font-medium ${PRIORITY_CONFIG[p].badgeBg} ${PRIORITY_CONFIG[p].badgeText}`}>
                    <PriorityIcon priority={p} className="h-3 w-3" inheritColor />
                    {PRIORITY_CONFIG[p].label}
                  </span>
                </DropdownMenuItem>
              ))}
            </DropdownMenuContent>
          </DropdownMenu>

          {/* Issue type */}
          <DropdownMenu>
            <DropdownMenuTrigger
              render={
                <PillButton>
                  <Shapes className="size-3.5 text-muted-foreground" />
                  <span>{selectedIssueType?.name ?? "Issue type"}</span>
                </PillButton>
              }
            />
            <DropdownMenuContent align="start" className="w-48">
              {issueTypes.map((issueType) => (
                <DropdownMenuItem key={issueType.id} onClick={() => setIssueTypeId(issueType.id)}>
                  <Shapes className="size-3.5 text-muted-foreground" />
                  <span>{issueType.name}</span>
                </DropdownMenuItem>
              ))}
            </DropdownMenuContent>
          </DropdownMenu>

          {/* Assignee — Popover for search support */}
          <Popover open={assigneeOpen} onOpenChange={(v) => { setAssigneeOpen(v); if (!v) setAssigneeFilter(""); }}>
            <PopoverTrigger
              render={
                <PillButton>
                  {assigneeType && assigneeId ? (
                    <>
                      <ActorAvatar actorType={assigneeType} actorId={assigneeId} size={16} />
                      <span>{assigneeLabel}</span>
                    </>
                  ) : (
                    <span className="text-muted-foreground">Assignee</span>
                  )}
                </PillButton>
              }
            />
            <PopoverContent align="start" className="w-52 p-0">
              <div className="px-2 py-1.5 border-b">
                <input
                  type="text"
                  value={assigneeFilter}
                  onChange={(e) => setAssigneeFilter(e.target.value)}
                  placeholder="Assign to..."
                  className="w-full bg-transparent text-sm placeholder:text-muted-foreground outline-none"
                />
              </div>
              <div className="p-1 max-h-60 overflow-y-auto">
                {/* Unassigned */}
                <button
                  type="button"
                  onClick={() => {
                    updateAssignee(undefined, undefined);
                    setAssigneeOpen(false);
                  }}
                  className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm hover:bg-accent transition-colors"
                >
                  <UserMinus className="h-3.5 w-3.5 text-muted-foreground" />
                  <span className="text-muted-foreground">Unassigned</span>
                </button>

                {/* Members */}
                {filteredMembers.length > 0 && (
                  <>
                    <div className="px-2 pt-2 pb-1 text-xs font-medium text-muted-foreground uppercase tracking-wider">Members</div>
                    {filteredMembers.map((m) => (
                      <button
                        type="button"
                        key={m.user_id}
                        onClick={() => {
                          updateAssignee("member", m.user_id);
                          setAssigneeOpen(false);
                        }}
                        className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm hover:bg-accent transition-colors"
                      >
                        <ActorAvatar actorType="member" actorId={m.user_id} size={16} />
                        <span>{m.name}</span>
                      </button>
                    ))}
                  </>
                )}

                {/* Agents */}
                {filteredAgents.length > 0 && (
                  <>
                    <div className="px-2 pt-2 pb-1 text-xs font-medium text-muted-foreground uppercase tracking-wider">Agents</div>
                    {filteredAgents.map((a) => (
                      <button
                        type="button"
                        key={a.id}
                        onClick={() => {
                          updateAssignee("agent", a.id);
                          setAssigneeOpen(false);
                        }}
                        className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm hover:bg-accent transition-colors"
                      >
                        <ActorAvatar actorType="agent" actorId={a.id} size={16} />
                        <span>{a.name}</span>
                      </button>
                    ))}
                  </>
                )}

                {filteredMembers.length === 0 && filteredAgents.length === 0 && assigneeFilter && (
                  <div className="px-2 py-3 text-center text-sm text-muted-foreground">No results</div>
                )}
              </div>
            </PopoverContent>
          </Popover>

          <ProjectPicker
            projectId={selectedProjectId ?? null}
            onUpdate={(updates) => {
              if ("project_id" in updates) {
                setSelectedProjectId(updates.project_id ?? undefined);
              }
            }}
            align="start"
            triggerRender={<PillButton />}
          />

          <ParentIssuePicker
            parentIssueId={parentIssueId ?? null}
            onUpdate={(updates) => updateParentIssue(updates.parent_issue_id)}
            align="start"
            triggerRender={<PillButton />}
          />

          <IssueDateTimePicker
            field="start_date"
            dateTimeValue={startDate}
            onUpdate={handleDateUpdate}
            trigger={
              <PillButton>
                <CalendarDays className="size-3.5 text-muted-foreground" />
                {startDate ? (
                  <span>{shortDateTime(startDate)}</span>
                ) : (
                  <span className="text-muted-foreground">Start date</span>
                )}
              </PillButton>
            }
          />

          <IssueDateTimePicker
            field="end_date"
            dateTimeValue={endDate}
            onUpdate={handleDateUpdate}
            trigger={
              <PillButton>
                <CalendarDays className="size-3.5 text-muted-foreground" />
                {endDate ? (
                  <span>{shortDateTime(endDate)}</span>
                ) : (
                  <span className="text-muted-foreground">End date</span>
                )}
              </PillButton>
            }
          />

          <DueDatePicker
            dueDate={dueDate}
            onUpdate={handleDateUpdate}
            trigger={
              <PillButton>
                <CalendarDays className="size-3.5 text-muted-foreground" />
                {dueDateObj ? (
                  <span>{dueDateObj.toLocaleDateString("en-US", { month: "short", day: "numeric" })}</span>
                ) : (
                  <span className="text-muted-foreground">Due date</span>
                )}
              </PillButton>
            }
          />
        </div>

        {(voiceRecorder.status === "recording" || voiceTranscription.status === "transcribing" || voiceTranscript || voiceRecorder.error || voiceTranscription.error) && (
          <div className="border-t px-5 py-3 shrink-0 space-y-3">
            {voiceRecorder.status === "recording" && (
              <div className="flex items-center justify-between gap-3">
                <div className="flex items-center gap-2 text-sm">
                  <span className="size-2 rounded-full bg-destructive animate-pulse" />
                  <span>Recording</span>
                </div>
                <Button size="sm" variant="outline" onClick={stopVoiceCapture}>
                  <Square className="size-3.5" />
                  Stop
                </Button>
              </div>
            )}

            {voiceTranscription.status === "transcribing" && (
              <div className="flex items-center gap-2 text-sm text-muted-foreground">
                <Loader2 className="size-4 animate-spin" />
                <span>Transcribing recording</span>
              </div>
            )}

            {(voiceRecorder.error || voiceTranscription.error) && !voiceTranscript && (
              <div className="flex items-center justify-between gap-3 text-sm">
                <span className="text-destructive">{voiceRecorder.error ?? voiceTranscription.error}</span>
                <Button size="sm" variant="outline" onClick={discardVoiceTranscript}>Dismiss</Button>
              </div>
            )}

            {voiceTranscript && (
              <div className="space-y-3">
                <textarea
                  value={voiceTranscript}
                  onChange={(event) => setVoiceTranscript(event.target.value)}
                  className="min-h-20 w-full resize-none rounded-md border bg-background px-3 py-2 text-sm outline-none focus:ring-2 focus:ring-ring"
                  aria-label="Voice transcript"
                />
                <div className="flex items-center justify-between gap-3">
                  <label className="flex items-center gap-2 text-sm text-muted-foreground">
                    <input
                      type="checkbox"
                      checked={keepOriginalRecording}
                      onChange={(event) => setKeepOriginalRecording(event.target.checked)}
                      className="size-4"
                    />
                    Keep original recording
                  </label>
                  <div className="flex items-center gap-2">
                    <Button size="sm" variant="outline" onClick={discardVoiceTranscript}>Discard</Button>
                    <Button size="sm" onClick={insertVoiceTranscript}>Insert</Button>
                  </div>
                </div>
              </div>
            )}
          </div>
        )}

        {/* Footer */}
        <div className="flex items-center justify-between px-4 py-3 border-t shrink-0">
          <div className="flex items-center gap-2">
            <FileUploadButton
              ariaLabel="Upload attachment"
              onSelect={(file) => descEditorRef.current?.uploadFile(file)}
            />
            {voiceRecorder.supported ? (
              <Tooltip>
                <TooltipTrigger
                  render={
                    <Button
                      type="button"
                      size="icon"
                      variant="ghost"
                      onClick={voiceRecorder.status === "recording" ? stopVoiceCapture : startVoiceCapture}
                      disabled={voiceTranscription.status === "transcribing"}
                      aria-label={voiceRecorder.status === "recording" ? "Stop recording" : "Record voice"}
                    >
                      {voiceRecorder.status === "recording" ? <Square className="size-4" /> : <Mic className="size-4" />}
                    </Button>
                  }
                />
                <TooltipContent side="top">{voiceRecorder.status === "recording" ? "Stop recording" : "Record voice"}</TooltipContent>
              </Tooltip>
            ) : (
              <Tooltip>
                <TooltipTrigger
                  render={
                    <Button type="button" size="icon" variant="ghost" disabled aria-label="Recording unsupported">
                      <Mic className="size-4" />
                    </Button>
                  }
                />
                <TooltipContent side="top">Recording unsupported</TooltipContent>
              </Tooltip>
            )}
          </div>
          <Button size="sm" onClick={handleSubmit} disabled={!title.trim() || submitting}>
            {submitting ? "Creating..." : "Create Issue"}
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}
