"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { ArrowLeftRight, Check, ChevronRight, X as XIcon } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { DialogTitle } from "@multica/ui/components/ui/dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { Button } from "@multica/ui/components/ui/button";
import { Switch } from "@multica/ui/components/ui/switch";
import { api, ApiError } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import { useCurrentWorkspace } from "@multica/core/paths";
import { agentListOptions } from "@multica/core/workspace/queries";
import { useQuickCreateStore } from "@multica/core/issues/stores/quick-create-store";
import { useIssueDraftStore } from "@multica/core/issues/stores/draft-store";
import { useCreateModeStore } from "@multica/core/issues/stores/create-mode-store";
import {
  runtimeListOptions,
  checkQuickCreateCliVersion,
  readRuntimeCliVersion,
  MIN_QUICK_CREATE_CLI_VERSION,
} from "@multica/core/runtimes";
import { useFileUpload } from "@multica/core/hooks/use-file-upload";
import { formatShortcut, modKey, enterKey } from "@multica/core/platform";
import type { Agent } from "@multica/core/types";
import { ActorAvatar } from "../common/actor-avatar";
import { canAssignAgent } from "../issues/components/pickers/assignee-picker";
import { useAuthStore } from "@multica/core/auth";
import { memberListOptions } from "@multica/core/workspace/queries";
import {
  ContentEditor,
  type ContentEditorRef,
  useFileDropZone,
  FileDropOverlay,
} from "../editor";
import { FileUploadButton } from "@multica/ui/components/common/file-upload-button";

// AgentCreatePanel — agent-mode body of the create-issue dialog. Renders
// only the inner content; the surrounding `<Dialog>` AND `<DialogContent>`
// (Portal + Overlay + Popup) are owned by CreateIssueDialog so mode-switching
// swaps only this body. Lifting the Portal is what eliminates the close→open
// animation flash — Base UI replays Popup enter/exit when DialogContent is
// remounted, even inside a still-open Dialog Root.
//
// `onSwitchMode` is wired by the shell — the panel calls it (no payload from
// agent → manual; the shared draft store carries description + agent).
export function AgentCreatePanel({
  onClose,
  onSwitchMode,
  data,
}: {
  onClose: () => void;
  onSwitchMode?: () => void;
  data?: Record<string, unknown> | null;
}) {
  const workspaceName = useCurrentWorkspace()?.name;
  const wsId = useWorkspaceId();
  const userId = useAuthStore((s) => s.user?.id);
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { data: agents = [] } = useQuery(agentListOptions(wsId));

  const memberRole = useMemo(
    () => members.find((m) => m.user_id === userId)?.role,
    [members, userId],
  );

  // Visible = not archived AND assignable by this user.
  const visibleAgents = useMemo(
    () =>
      agents.filter(
        (a) => !a.archived_at && canAssignAgent(a, userId, memberRole),
      ),
    [agents, userId, memberRole],
  );

  const lastAgentId = useQuickCreateStore((s) => s.lastAgentId);
  const setLastAgentId = useQuickCreateStore((s) => s.setLastAgentId);
  const promptDraft = useQuickCreateStore((s) => s.prompt);
  const setPrompt = useQuickCreateStore((s) => s.setPrompt);
  const clearPrompt = useQuickCreateStore((s) => s.clearPrompt);
  const keepOpen = useQuickCreateStore((s) => s.keepOpen);
  const setKeepOpen = useQuickCreateStore((s) => s.setKeepOpen);
  const setLastMode = useCreateModeStore((s) => s.setLastMode);

  const [agentId, setAgentId] = useState<string | undefined>(() => {
    const seed = (data?.agent_id as string) || lastAgentId || undefined;
    if (seed && visibleAgents.some((a) => a.id === seed)) return seed;
    return visibleAgents[0]?.id;
  });

  // Re-seed once visible list resolves (queries may be empty on first render).
  useEffect(() => {
    if (agentId && visibleAgents.some((a) => a.id === agentId)) return;
    const seed = (data?.agent_id as string) || lastAgentId || undefined;
    if (seed && visibleAgents.some((a) => a.id === seed)) {
      setAgentId(seed);
      return;
    }
    const first = visibleAgents[0];
    if (first) setAgentId(first.id);
  }, [visibleAgents, agentId, data?.agent_id, lastAgentId]);

  const selectedAgent = useMemo(
    () => visibleAgents.find((a) => a.id === agentId),
    [visibleAgents, agentId],
  );

  // Daemon CLI version gate. The agent-create flow needs the runtime's
  // bundled multica CLI to be ≥ MIN_QUICK_CREATE_CLI_VERSION; older
  // daemons handle attachments and partial-failure retries incorrectly
  // (see PR #1851 / MUL-1496). Pre-check on the picker so the user gets
  // immediate feedback instead of waiting for the inbox failure; the
  // server re-validates as the trust boundary.
  const { data: runtimes = [] } = useQuery(runtimeListOptions(wsId));
  const selectedRuntime = useMemo(
    () =>
      selectedAgent?.runtime_id
        ? runtimes.find((r) => r.id === selectedAgent.runtime_id)
        : undefined,
    [runtimes, selectedAgent?.runtime_id],
  );
  const versionCheck = useMemo(
    () => checkQuickCreateCliVersion(readRuntimeCliVersion(selectedRuntime?.metadata)),
    [selectedRuntime?.metadata],
  );
  const versionBlocked = versionCheck.state !== "ok";

  const initialPrompt = (data?.prompt as string) || promptDraft;
  // The editor is uncontrolled — we read the latest markdown via the ref at
  // submit/switch time. `hasContent` mirrors emptiness so the Create button
  // can disable correctly without a controlled-input rerender on every keystroke.
  const editorRef = useRef<ContentEditorRef>(null);
  const [hasContent, setHasContent] = useState(initialPrompt.trim().length > 0);
  const [submitting, setSubmitting] = useState(false);
  const [justSent, setJustSent] = useState(false);
  const [sentCount, setSentCount] = useState(0);
  const [error, setError] = useState<string | null>(null);

  // Image paste/drop support: route uploads through the same helper Advanced
  // uses, so users can paste screenshots straight into the prompt and the
  // agent receives them as embedded markdown image URLs in the prompt.
  const { uploadWithToast, uploading } = useFileUpload(api);
  const handleUploadFile = useCallback(
    (file: File) => uploadWithToast(file),
    [uploadWithToast],
  );
  const { isDragOver, dropZoneProps } = useFileDropZone({
    onDrop: (files) => files.forEach((f) => editorRef.current?.uploadFile(f)),
  });

  useEffect(() => {
    // Defer focus so it lands after the dialog's focus trap has settled —
    // otherwise the trap can bounce focus back to the first focusable header
    // button on the next tick.
    const id = requestAnimationFrame(() => editorRef.current?.focus());
    return () => cancelAnimationFrame(id);
  }, []);

  const submit = async () => {
    const md = editorRef.current?.getMarkdown()?.trim() ?? "";
    if (!md || !agentId || submitting || versionBlocked || uploading) return;
    setSubmitting(true);
    setError(null);
    try {
      await api.quickCreateIssue({ agent_id: agentId, prompt: md });
      setLastAgentId(agentId);
      clearPrompt();
      setLastMode("agent");
      toast.success("Sent to agent — you'll get an inbox notification when it's done", {
        duration: 4000,
      });
      if (keepOpen) {
        // Stay open for continuous creation — clear the editor so the
        // user can immediately type the next prompt.
        editorRef.current?.clearContent();
        setHasContent(false);
        setSentCount((c) => c + 1);
        setJustSent(true);
        setTimeout(() => setJustSent(false), 1500);
        requestAnimationFrame(() => editorRef.current?.focus());
      } else {
        onClose();
      }
    } catch (e) {
      // Server returns 422 with { code, ... } for the structured rejection
      // paths the modal cares about. Surface the reason in-modal so the
      // user can switch to a live agent / upgrade their daemon without
      // leaving the flow.
      if (e instanceof ApiError && e.body && typeof e.body === "object") {
        const body = e.body as {
          code?: string;
          reason?: string;
          current_version?: string;
          min_version?: string;
        };
        if (body.code === "agent_unavailable") {
          setError(body.reason || "Agent is unavailable. Pick another agent.");
          setSubmitting(false);
          return;
        }
        if (body.code === "daemon_version_unsupported") {
          // Race fallback: the picker pre-check should normally catch this,
          // but a runtime can silently re-register with an older CLI between
          // pre-check and submit. Same wording as the inline notice for
          // consistency.
          const cur = body.current_version || "unknown";
          setError(
            `This agent's daemon CLI (${cur}) is below the required ${body.min_version || MIN_QUICK_CREATE_CLI_VERSION}. Upgrade the daemon to use Create with agent.`,
          );
          setSubmitting(false);
          return;
        }
      }
      setError("Failed to submit. Try again.");
    } finally {
      setSubmitting(false);
    }
  };

  // Switch to the manual form, carrying what the user typed over as the
  // description (markdown, including any pasted images) so they don't lose
  // their work. The picked agent becomes the default assignee candidate
  // (still editable). We seed the shared issue-draft store directly because
  // the manual panel reads its initial values from there. Persist the mode
  // flip so the next `c` lands in manual.
  const switchToManual = () => {
    const md = editorRef.current?.getMarkdown() ?? "";
    useIssueDraftStore.getState().setDraft({
      description: md,
      ...(agentId
        ? { assigneeType: "agent" as const, assigneeId: agentId }
        : {}),
    });
    setLastMode("manual");
    onSwitchMode?.();
  };

  return (
    <>
        <DialogTitle className="sr-only">Quick create issue</DialogTitle>

        {/* Header */}
        <div className="flex items-center justify-between px-5 pt-3 pb-2 shrink-0">
          <div className="flex items-center gap-1.5 text-xs">
            <span className="text-muted-foreground">{workspaceName}</span>
            <ChevronRight className="size-3 text-muted-foreground/50" />
            <span className="font-medium">Create with agent</span>
          </div>
          {/* Native `title` instead of Base UI Tooltip — Tooltip opens on
              keyboard focus, and the dialog's focus trap briefly lands focus
              on the first focusable element on mount, causing the tooltip to
              auto-pop every open. */}
          <button
            type="button"
            onClick={onClose}
            title="Close"
            aria-label="Close"
            className="rounded-sm p-1.5 opacity-70 hover:opacity-100 hover:bg-accent/60 transition-all cursor-pointer"
          >
            <XIcon className="size-4" />
          </button>
        </div>

        {/* Agent picker */}
        <div className="px-5 pt-1 pb-2 shrink-0">
          <DropdownMenu>
            <DropdownMenuTrigger
              render={
                <button
                  type="button"
                  aria-label="Select agent"
                  className="flex items-center gap-2 text-xs text-muted-foreground hover:text-foreground transition-colors cursor-pointer rounded-sm px-1.5 py-1 -ml-1.5 hover:bg-accent/60"
                >
                  <span>Created by</span>
                  {selectedAgent ? (
                    <span className="flex items-center gap-1.5 text-foreground">
                      <ActorAvatar
                        actorType="agent"
                        actorId={selectedAgent.id}
                        size={16}
                      />
                      {selectedAgent.name}
                    </span>
                  ) : (
                    <span>Pick an agent…</span>
                  )}
                </button>
              }
            />
            <DropdownMenuContent align="start" className="w-64 max-h-72 overflow-y-auto">
              {visibleAgents.length === 0 ? (
                <div className="px-2 py-1.5 text-xs text-muted-foreground">
                  No agents available.
                </div>
              ) : (
                visibleAgents.map((a: Agent) => (
                  <DropdownMenuItem
                    key={a.id}
                    onClick={() => {
                      setAgentId(a.id);
                      setError(null);
                    }}
                    className="flex items-center gap-2"
                  >
                    <ActorAvatar
                      actorType="agent"
                      actorId={a.id}
                      size={16}
                    />
                    <span className="flex-1 truncate">{a.name}</span>
                    {agentId === a.id && (
                      <Check className="size-3.5 text-muted-foreground" />
                    )}
                  </DropdownMenuItem>
                ))
              )}
            </DropdownMenuContent>
          </DropdownMenu>
        </div>

        {selectedAgent && versionBlocked && (
          <div className="mx-5 mb-2 shrink-0 rounded-md border border-amber-500/30 bg-amber-500/5 px-3 py-2 text-xs text-amber-700 dark:text-amber-300">
            {versionCheck.state === "missing"
              ? `This agent's daemon doesn't report a CLI version. Create with agent needs multica CLI ≥ ${versionCheck.min}. Upgrade the daemon and reconnect, or switch to manual create.`
              : `This agent's daemon CLI is ${versionCheck.current} — Create with agent needs ≥ ${versionCheck.min}. Upgrade the daemon, or switch to manual create.`}
          </div>
        )}

        {/* Prompt — same rich editor Advanced uses, so paste/drop images,
            mentions, and formatting all work. The dropZone wrapper enables
            drag-and-drop file uploads alongside paste. */}
        {/* `flex-1 min-h-0 overflow-y-auto` so the editor area absorbs the
            remaining vertical space inside the (now max-bounded) DialogContent
            and scrolls internally. Without it, pasting an image expanded the
            editor unbounded and pushed the modal past the viewport. */}
        <div
          {...dropZoneProps}
          className="relative px-5 pb-3 flex-1 min-h-[140px] overflow-y-auto"
        >
          <ContentEditor
            ref={editorRef}
            defaultValue={initialPrompt}
            placeholder='Tell the agent what to do, e.g. "let Bohan fix the inbox loading slowness in the Web project"'
            onUpdate={(md) => {
              setHasContent(md.trim().length > 0);
              setPrompt(md);
            }}
            onUploadFile={handleUploadFile}
            onSubmit={submit}
            debounceMs={150}
          />
          {isDragOver && <FileDropOverlay />}
        </div>

        {error && (
          <div className="px-5 pb-2 text-xs text-destructive">{error}</div>
        )}

        {/* Footer */}
        <div className="flex flex-col gap-2 border-t px-4 py-3 shrink-0 sm:flex-row sm:items-center sm:justify-between">
          <div className="flex min-h-7 items-center gap-2">
            <FileUploadButton
              size="sm"
              disabled={uploading}
              onSelect={(file) => editorRef.current?.uploadFile(file)}
            />
            {keepOpen && sentCount > 0 && (
              <span className="text-xs text-emerald-600 dark:text-emerald-400">
                {sentCount} sent
              </span>
            )}
          </div>
          <div className="flex flex-wrap items-center justify-end gap-2">
            <button
              type="button"
              onClick={switchToManual}
              title="Switch to manual create — fill the fields yourself"
              className="flex shrink-0 items-center gap-1.5 text-xs px-2 py-1 rounded-sm text-muted-foreground hover:text-foreground hover:bg-accent/60 transition-colors cursor-pointer"
            >
              <ArrowLeftRight className="size-3.5" />
              Switch to Manual
            </button>
            <label className="flex shrink-0 items-center gap-1.5 text-xs text-muted-foreground cursor-pointer select-none">
              <Switch
                size="sm"
                checked={keepOpen}
                onCheckedChange={setKeepOpen}
              />
              Create another
            </label>
            <Button
              size="sm"
              onClick={submit}
              disabled={!hasContent || !agentId || submitting || versionBlocked || uploading}
              title={
                versionBlocked
                  ? `Daemon CLI must be ≥ ${versionCheck.min}`
                  : undefined
              }
              className={justSent ? "min-w-28 !bg-emerald-600 !text-white" : "min-w-28"}
            >
              {submitting ? "Sending…" : uploading ? "Uploading…" : justSent ? (
                <span className="flex items-center gap-1"><Check className="size-3.5" />Sent</span>
              ) : `Create (${formatShortcut(modKey, enterKey)})`}
            </Button>
          </div>
        </div>
    </>
  );
}
