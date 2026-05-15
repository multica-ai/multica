"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { ArrowLeftRight, Check, ChevronRight, Maximize2, Minimize2, X as XIcon } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { DialogTitle } from "@multica/ui/components/ui/dialog";
import { Button } from "@multica/ui/components/ui/button";
import { Switch } from "@multica/ui/components/ui/switch";
import { api, ApiError } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import { useCurrentWorkspace } from "@multica/core/paths";
import { agentListOptions, squadListOptions } from "@multica/core/workspace/queries";
import { projectListOptions } from "@multica/core/projects/queries";
import {
  useQuickCreateStore,
  type QuickCreateActorType,
} from "@multica/core/issues/stores/quick-create-store";
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
import type { Agent, Squad } from "@multica/core/types";
import { ActorAvatar } from "../common/actor-avatar";
import { PillButton } from "../common/pill-button";
import { ProjectPicker } from "../projects/components/project-picker";
import { canAssignAgent } from "../issues/components/pickers/assignee-picker";
import {
  PropertyPicker,
  PickerItem,
  PickerSection,
  PickerEmpty,
} from "../issues/components/pickers/property-picker";
import { useAuthStore } from "@multica/core/auth";
import { memberListOptions } from "@multica/core/workspace/queries";
import {
  ContentEditor,
  type ContentEditorRef,
  useFileDropZone,
  FileDropOverlay,
} from "../editor";
import { FileUploadButton } from "@multica/ui/components/common/file-upload-button";
import { useT } from "../i18n";
import { matchesPinyin } from "../editor/extensions/pinyin-match";

type ActorSelection =
  | { type: "agent"; id: string }
  | { type: "squad"; id: string };

// AgentCreatePanel — agent-mode body of the create-issue dialog. Renders
// only the inner content; the surrounding `<Dialog>` AND `<DialogContent>`
// (Portal + Overlay + Popup) are owned by CreateIssueDialog so mode-switching
// swaps only this body. Lifting the Portal is what eliminates the close→open
// animation flash — Base UI replays Popup enter/exit when DialogContent is
// remounted, even inside a still-open Dialog Root.
//
// `onSwitchMode` is wired by the shell — the panel calls it with an optional
// carry payload (currently `project_id`). The shared draft store carries the
// description + agent across the agent→manual flip; project_id rides through
// the same carry channel manual→agent uses, so the manual panel reads it
// from `data?.project_id` without a parallel store.
export function AgentCreatePanel({
  onClose,
  onSwitchMode,
  data,
  isExpanded,
  setIsExpanded,
}: {
  onClose: () => void;
  onSwitchMode?: (carry?: Record<string, unknown> | null) => void;
  data?: Record<string, unknown> | null;
  /** Lifted to the shell so DialogContent's mode-aware className can react —
   *  same pattern as ManualCreatePanel. Shared across modes so the user's
   *  expand preference persists when switching between agent and manual. */
  isExpanded: boolean;
  setIsExpanded: (v: boolean) => void;
}) {
  const { t } = useT("modals");
  const workspaceName = useCurrentWorkspace()?.name;
  const wsId = useWorkspaceId();
  const userId = useAuthStore((s) => s.user?.id);
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const { data: squads = [] } = useQuery(squadListOptions(wsId));
  // Pull `isSuccess` so the stale-id sweep below can distinguish "still
  // loading" from "loaded as empty". Reading length alone treats both as
  // empty and incorrectly clears a valid persisted preference on every open.
  const { data: projects = [], isSuccess: projectsLoaded } = useQuery(
    projectListOptions(wsId),
  );

  const memberRole = useMemo(
    () => members.find((m) => m.user_id === userId)?.role,
    [members, userId],
  );

  // Visible = not archived AND assignable by this user. Squads inherit
  // their leader agent's reachability: the backend always routes a squad
  // pick to the leader, so hiding squads whose leader isn't visible keeps
  // the picker honest with what the server would actually accept.
  const visibleAgents = useMemo(
    () =>
      agents.filter(
        (a) => !a.archived_at && canAssignAgent(a, userId, memberRole),
      ),
    [agents, userId, memberRole],
  );
  const visibleAgentIds = useMemo(
    () => new Set(visibleAgents.map((a) => a.id)),
    [visibleAgents],
  );
  const visibleSquads = useMemo(
    () =>
      squads.filter(
        (s) => !s.archived_at && visibleAgentIds.has(s.leader_id),
      ),
    [squads, visibleAgentIds],
  );

  const lastActorType = useQuickCreateStore((s) => s.lastActorType);
  const lastActorId = useQuickCreateStore((s) => s.lastActorId);
  const setLastActor = useQuickCreateStore((s) => s.setLastActor);
  const lastProjectId = useQuickCreateStore((s) => s.lastProjectId);
  const setLastProjectId = useQuickCreateStore((s) => s.setLastProjectId);
  const promptDraft = useQuickCreateStore((s) => s.prompt);
  const setPrompt = useQuickCreateStore((s) => s.setPrompt);
  const clearPrompt = useQuickCreateStore((s) => s.clearPrompt);
  const keepOpen = useQuickCreateStore((s) => s.keepOpen);
  const setKeepOpen = useQuickCreateStore((s) => s.setKeepOpen);
  const setLastMode = useCreateModeStore((s) => s.setLastMode);

  // Resolve a candidate actor against the currently-visible agents / squads.
  // Returns null when the candidate doesn't exist in this workspace right
  // now (deleted, archived, permission revoked, etc.) so callers can fall
  // through to the next seed in the chain.
  const resolveActor = useCallback(
    (
      type: QuickCreateActorType | "agent" | "squad" | null | undefined,
      id: string | null | undefined,
    ): ActorSelection | null => {
      if (!type || !id) return null;
      if (type === "squad" && visibleSquads.some((s) => s.id === id)) {
        return { type: "squad", id };
      }
      if (type === "agent" && visibleAgentIds.has(id)) {
        return { type: "agent", id };
      }
      return null;
    },
    [visibleSquads, visibleAgentIds],
  );

  const seedActor = useCallback((): ActorSelection | null => {
    // Caller-provided seed wins (e.g. shell pre-seeds with `agent_id` /
    // `squad_id`), then persisted preference, then first visible agent.
    const dataAgent = data?.agent_id as string | undefined;
    const dataSquad = data?.squad_id as string | undefined;
    return (
      resolveActor("agent", dataAgent) ||
      resolveActor("squad", dataSquad) ||
      resolveActor(lastActorType, lastActorId) ||
      (visibleAgents[0]
        ? ({ type: "agent", id: visibleAgents[0].id } as const)
        : null)
    );
  }, [resolveActor, data?.agent_id, data?.squad_id, lastActorType, lastActorId, visibleAgents]);

  const [actor, setActor] = useState<ActorSelection | null>(() => seedActor());

  // Re-seed once visible lists resolve (queries may be empty on first render).
  useEffect(() => {
    if (actor && resolveActor(actor.type, actor.id)) return;
    setActor(seedActor());
  }, [actor, resolveActor, seedActor]);

  const selectedAgent = useMemo<Agent | undefined>(() => {
    if (!actor) return undefined;
    if (actor.type === "agent") return visibleAgents.find((a) => a.id === actor.id);
    const squad = visibleSquads.find((s) => s.id === actor.id);
    if (!squad) return undefined;
    return visibleAgents.find((a) => a.id === squad.leader_id);
  }, [actor, visibleAgents, visibleSquads]);

  const selectedSquad = useMemo<Squad | undefined>(() => {
    if (actor?.type !== "squad") return undefined;
    return visibleSquads.find((s) => s.id === actor.id);
  }, [actor, visibleSquads]);

  // Project selection — defaults to the last project the user picked in this
  // workspace. `data?.project_id` lets the modal opener seed a one-shot
  // override (e.g. a future "+ Issue" button on a project page); it does NOT
  // replace the persisted default.
  const [projectId, setProjectId] = useState<string | null>(() => {
    const seed = (data?.project_id as string | undefined) ?? lastProjectId;
    return seed ?? null;
  });

  // Stale-id sweep. Once the project list query has actually resolved
  // (`isSuccess` — distinct from "data is the empty default during loading"),
  // a `projectId` that isn't in the list means the project was deleted in
  // another session. Clear BOTH local state and the persisted preference;
  // dropping only local state would leave the deleted UUID in `lastProjectId`,
  // and the next open would re-seed it and submit the same dead value.
  useEffect(() => {
    if (!projectsLoaded || projectId === null) return;
    if (projects.some((p) => p.id === projectId)) return;
    setProjectId(null);
    if (lastProjectId === projectId) setLastProjectId(null);
  }, [projectsLoaded, projects, projectId, lastProjectId, setLastProjectId]);

  // Daemon CLI version gate. The agent-create flow needs the runtime's
  // bundled multica CLI to be ≥ MIN_QUICK_CREATE_CLI_VERSION; older
  // daemons handle attachments and partial-failure retries incorrectly
  // (see PR #1851 / MUL-1496). Pre-check on the picker so the user gets
  // immediate feedback instead of waiting for the inbox failure; the
  // server re-validates as the trust boundary. Dev-built daemons
  // (git-describe shape) are exempted inside checkQuickCreateCliVersion
  // — frontend and server share the same signal there, so they agree by
  // construction across web/desktop/staging without comparing env flags.
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
    if (!md || !actor || submitting || versionBlocked || uploading) return;
    setSubmitting(true);
    setError(null);
    try {
      await api.quickCreateIssue({
        ...(actor.type === "agent"
          ? { agent_id: actor.id }
          : { squad_id: actor.id }),
        prompt: md,
        project_id: projectId ?? undefined,
      });
      setLastActor(actor.type, actor.id);
      setLastProjectId(projectId);
      clearPrompt();
      setLastMode("agent");
      toast.success(t(($) => $.create_issue.agent.toast_sent), {
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
          setError(body.reason || t(($) => $.create_issue.agent.error_agent_unavailable_fallback));
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
            t(($) => $.create_issue.agent.error_daemon_version, {
              current: cur,
              min: body.min_version || MIN_QUICK_CREATE_CLI_VERSION,
            }),
          );
          setSubmitting(false);
          return;
        }
      }
      setError(t(($) => $.create_issue.agent.error_unknown));
    } finally {
      setSubmitting(false);
    }
  };

  // Switch to the manual form, carrying what the user typed over as the
  // description (markdown, including any pasted images) so they don't lose
  // their work. The picked actor (agent or squad) becomes the default
  // assignee candidate (still editable). We seed the shared issue-draft
  // store directly because the manual panel reads its initial values from
  // there. Persist the mode flip so the next `c` lands in manual.
  const switchToManual = () => {
    const md = editorRef.current?.getMarkdown() ?? "";
    useIssueDraftStore.getState().setDraft({
      description: md,
      ...(actor
        ? { assigneeType: actor.type, assigneeId: actor.id }
        : {}),
    });
    setLastMode("manual");
    // Hand the picked project to the manual panel through the same `data`
    // channel that already carries agent_id / parent_issue_id. The manual
    // panel reads `data.project_id` on mount; this preserves the user's
    // selection across the mode flip without piping a third store through.
    onSwitchMode?.(projectId ? { project_id: projectId } : null);
  };

  return (
    <>
        <DialogTitle className="sr-only">{t(($) => $.create_issue.sr_agent)}</DialogTitle>

        {/* Header */}
        <div className="flex items-center justify-between px-5 pt-3 pb-2 shrink-0">
          <div className="flex items-center gap-1.5 text-xs">
            <span className="text-muted-foreground">{workspaceName}</span>
            <ChevronRight className="size-3 text-muted-foreground/50" />
            <span className="font-medium">{t(($) => $.create_issue.agent_breadcrumb)}</span>
          </div>
          {/* Native `title` instead of Base UI Tooltip — Tooltip opens on
              keyboard focus, and the dialog's focus trap briefly lands focus
              on the first focusable element on mount, causing the tooltip to
              auto-pop every open. Same workaround applies to expand. */}
          <div className="flex items-center gap-1">
            <button
              type="button"
              onClick={() => setIsExpanded(!isExpanded)}
              title={isExpanded ? t(($) => $.common.collapse_tooltip) : t(($) => $.common.expand_tooltip)}
              aria-label={isExpanded ? t(($) => $.common.collapse_tooltip) : t(($) => $.common.expand_tooltip)}
              className="rounded-sm p-1.5 opacity-70 hover:opacity-100 hover:bg-accent/60 transition-all cursor-pointer"
            >
              {isExpanded ? <Minimize2 className="size-4" /> : <Maximize2 className="size-4" />}
            </button>
            <button
              type="button"
              onClick={onClose}
              title={t(($) => $.common.close)}
              aria-label={t(($) => $.common.close)}
              className="rounded-sm p-1.5 opacity-70 hover:opacity-100 hover:bg-accent/60 transition-all cursor-pointer"
            >
              <XIcon className="size-4" />
            </button>
          </div>
        </div>

        {/* Actor picker — agents and squads in one searchable list. Squads
            route to their leader agent on the backend; the leader runs the
            quick-create flow with the squad's Operating Protocol layered
            on top, so a squad pick is "ask this squad to file the issue". */}
        <div className="px-5 pt-1 pb-2 shrink-0">
          <ActorPicker
            actor={actor}
            visibleAgents={visibleAgents}
            visibleSquads={visibleSquads}
            selectedAgent={selectedAgent}
            selectedSquad={selectedSquad}
            onPick={(next) => {
              setActor(next);
              setError(null);
            }}
            t={t}
          />
        </div>

        {selectedAgent && versionBlocked && (
          <div className="mx-5 mb-2 shrink-0 rounded-md border border-amber-500/30 bg-amber-500/5 px-3 py-2 text-xs text-amber-700 dark:text-amber-300">
            {versionCheck.state === "missing"
              ? t(($) => $.create_issue.agent.version_missing, { min: versionCheck.min })
              : t(($) => $.create_issue.agent.version_below, {
                  current: versionCheck.current,
                  min: versionCheck.min,
                })}
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
          className="relative px-5 pb-3 flex flex-1 min-h-[140px] overflow-y-auto"
        >
          <ContentEditor
            ref={editorRef}
            defaultValue={initialPrompt}
            placeholder={t(($) => $.create_issue.agent.prompt_placeholder)}
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

        {/* Property toolbar — mirrors the manual panel's pill row so the
            project pill sits in the same place across both modes. Agent mode
            owns only the project (status / priority / assignee / due-date are
            inferred from the prompt), so it's a single pill. The pick is
            persisted per-workspace via useQuickCreateStore.lastProjectId so
            users targeting one project skip retyping "in project X". */}
        <div className="flex items-center gap-1.5 px-4 pb-2 shrink-0 flex-wrap">
          <ProjectPicker
            projectId={projectId}
            onUpdate={(u) => setProjectId(u.project_id ?? null)}
            triggerRender={<PillButton />}
            align="start"
          />
        </div>

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
                {t(($) => $.create_issue.agent.sent_count, { count: sentCount })}
              </span>
            )}
          </div>
          <div className="flex flex-wrap items-center justify-end gap-2">
            <button
              type="button"
              onClick={switchToManual}
              title={t(($) => $.create_issue.switch_to_manual_tooltip)}
              className="flex shrink-0 items-center gap-1.5 text-xs px-2 py-1 rounded-sm text-muted-foreground hover:text-foreground hover:bg-accent/60 transition-colors cursor-pointer"
            >
              <ArrowLeftRight className="size-3.5" />
              {t(($) => $.create_issue.switch_to_manual)}
            </button>
            <label className="flex shrink-0 items-center gap-1.5 text-xs text-muted-foreground cursor-pointer select-none">
              <Switch
                size="sm"
                checked={keepOpen}
                onCheckedChange={setKeepOpen}
              />
              {t(($) => $.create_issue.create_another)}
            </label>
            <Button
              size="sm"
              onClick={submit}
              disabled={!hasContent || !actor || submitting || versionBlocked || uploading}
              title={
                versionBlocked
                  ? t(($) => $.create_issue.agent.version_blocked_tooltip, { min: versionCheck.min })
                  : undefined
              }
              className={justSent ? "min-w-28 !bg-emerald-600 !text-white" : "min-w-28"}
            >
              {submitting ? t(($) => $.create_issue.agent.sending) : uploading ? t(($) => $.create_issue.agent.uploading) : justSent ? (
                <span className="flex items-center gap-1"><Check className="size-3.5" />{t(($) => $.create_issue.agent.sent_label)}</span>
              ) : `${t(($) => $.create_issue.agent.submit)} (${formatShortcut(modKey, enterKey)})`}
            </Button>
          </div>
        </div>
    </>
  );
}

// ActorPicker — the "Created by" trigger + searchable popover listing
// agents and squads. Lives in this file (not under issues/components/pickers)
// because it composes the generic PropertyPicker with a quick-create-shaped
// trigger styled to match the modal header row — promoting it would invite
// reuse pressure on a UI that's deliberately tuned for this one surface.
function ActorPicker({
  actor,
  visibleAgents,
  visibleSquads,
  selectedAgent,
  selectedSquad,
  onPick,
  t,
}: {
  actor: ActorSelection | null;
  visibleAgents: Agent[];
  visibleSquads: Squad[];
  selectedAgent: Agent | undefined;
  selectedSquad: Squad | undefined;
  onPick: (next: ActorSelection) => void;
  t: ReturnType<typeof useT<"modals">>["t"];
}) {
  const [open, setOpen] = useState(false);
  const [filter, setFilter] = useState("");
  const query = filter.trim().toLowerCase();

  const filteredAgents = useMemo(
    () => visibleAgents.filter((a) => a.name.toLowerCase().includes(query) || matchesPinyin(a.name, query)),
    [visibleAgents, query],
  );
  const filteredSquads = useMemo(
    () => visibleSquads.filter((s) => s.name.toLowerCase().includes(query) || matchesPinyin(s.name, query)),
    [visibleSquads, query],
  );

  const displayLabel = selectedSquad?.name ?? selectedAgent?.name;
  const displayActor: ActorSelection | null = selectedSquad
    ? { type: "squad", id: selectedSquad.id }
    : selectedAgent
      ? { type: "agent", id: selectedAgent.id }
      : null;

  return (
    <PropertyPicker
      open={open}
      onOpenChange={(v: boolean) => {
        setOpen(v);
        if (!v) setFilter("");
      }}
      width="w-64"
      align="start"
      searchable
      searchPlaceholder={t(($) => $.create_issue.agent.search_placeholder)}
      onSearchChange={setFilter}
      trigger={
        <span className="flex items-center gap-2 text-xs text-muted-foreground hover:text-foreground transition-colors">
          <span>{t(($) => $.create_issue.agent.created_by)}</span>
          {displayActor && displayLabel ? (
            <span className="flex items-center gap-1.5 text-foreground">
              <ActorAvatar
                actorType={displayActor.type}
                actorId={displayActor.id}
                size={16}
              />
              {displayLabel}
            </span>
          ) : (
            <span>{t(($) => $.create_issue.agent.pick_an_agent)}</span>
          )}
        </span>
      }
    >
      {filteredAgents.length === 0 && filteredSquads.length === 0 ? (
        query ? (
          <PickerEmpty />
        ) : (
          <div className="px-2 py-1.5 text-xs text-muted-foreground">
            {t(($) => $.create_issue.agent.no_agents)}
          </div>
        )
      ) : (
        <>
          {filteredAgents.length > 0 && (
            <PickerSection label={t(($) => $.create_issue.agent.agents_group)}>
              {filteredAgents.map((a) => (
                <PickerItem
                  key={a.id}
                  selected={actor?.type === "agent" && actor.id === a.id}
                  onClick={() => {
                    onPick({ type: "agent", id: a.id });
                    setOpen(false);
                  }}
                >
                  <ActorAvatar actorType="agent" actorId={a.id} size={18} />
                  <span className="truncate">{a.name}</span>
                </PickerItem>
              ))}
            </PickerSection>
          )}
          {filteredSquads.length > 0 && (
            <PickerSection label={t(($) => $.create_issue.agent.squads_group)}>
              {filteredSquads.map((s) => (
                <PickerItem
                  key={s.id}
                  selected={actor?.type === "squad" && actor.id === s.id}
                  onClick={() => {
                    onPick({ type: "squad", id: s.id });
                    setOpen(false);
                  }}
                >
                  <ActorAvatar actorType="squad" actorId={s.id} size={18} />
                  <span className="truncate">{s.name}</span>
                </PickerItem>
              ))}
            </PickerSection>
          )}
        </>
      )}
    </PropertyPicker>
  );
}
