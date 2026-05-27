"use client";

// CEREBRO-PATCH(reply-input-cerebro): cerebro modification of upstream file
// CEREBRO-PATCH(reply-input-pin): JEH-1065 — opt-in pin toggle. While pinned,
// the input behaves as `position: sticky` from the bottom — it stays in its
// natural slot inside the thread until the viewport bottom would scroll past
// it, then it floats with `position: fixed` matching the anchor's width.
// Visual styling stays identical to the unpinned input (no banner, no
// ring/shadow) so the user only sees the green pin icon as state indicator.
// The editor stays in the same React tree slot on every render so a Tiptap
// draft / caret / pending upload survives the pin toggle. Auto-unpins on
// submit and scrolls back to the originating thread.

import { useCallback, useId, useMemo, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { AlertTriangle, ArrowUp, Loader2, Maximize2, Minimize2, UserPlus } from "lucide-react";
import { ContentEditor, type ContentEditorRef } from "../../editor/content-editor";
import { FileDropOverlay } from "../../editor/file-drop-overlay";
import { useFileDropZone } from "../../editor/use-file-drop-zone";
import { FileUploadButton } from "@multica/ui/components/common/file-upload-button";
import { Tooltip, TooltipTrigger, TooltipContent } from "@multica/ui/components/ui/tooltip";
import { Popover, PopoverContent, PopoverTrigger } from "@multica/ui/components/ui/popover";
import { ActorAvatar } from "../../common/actor-avatar";
import { useFileUpload } from "@multica/core/hooks/use-file-upload";
import { api } from "@multica/core/api";
import { getCurrentWsId } from "@multica/core/platform";
import { useAuthStore } from "@multica/core/auth";
import { agentListOptions, memberListOptions } from "@multica/core/workspace/queries";
import type { Agent, MemberWithUser } from "@multica/core/types";
import { canTriggerPrivateAgentMention } from "@multica/cerebro-access/views";
import { cn } from "@multica/ui/lib/utils";
import { useSubmitOnEnter } from "@multica/cerebro-preferences/views";
import { PinButton, useFloatPosition, useInputPin } from "@multica/cerebro-pin-input";
import { useT } from "../../i18n";
import {
  getReplyTargetAgents,
  memberMentionMarkdown,
} from "./reply-targets";

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

interface ReplyInputProps {
  issueId: string;
  placeholder?: string;
  avatarType: string;
  avatarId: string;
  onSubmit: (content: string, attachmentIds?: string[]) => Promise<void>;
  size?: "sm" | "default";
  /**
   * CEREBRO-PATCH(reply-target-agent-indicator): when replying to an
   * agent-authored comment, show which agent will receive an untagged reply.
   * Explicit agent @mentions in the draft replace this fallback target.
   */
  replyTargetAgentId?: string;
}

// ---------------------------------------------------------------------------
// ReplyInput
// ---------------------------------------------------------------------------

function ReplyInput({
  issueId,
  placeholder,
  avatarType,
  avatarId,
  onSubmit,
  size = "default",
  replyTargetAgentId,
}: ReplyInputProps) {
  const { t } = useT("issues");
  const placeholderText = placeholder ?? t(($) => $.reply.placeholder);
  const editorRef = useRef<ContentEditorRef>(null);
  const measureRef = useRef<HTMLDivElement>(null);
  const anchorRef = useRef<HTMLDivElement>(null);
  const submitOnEnter = useSubmitOnEnter();
  const [isEmpty, setIsEmpty] = useState(true);
  const [markdown, setMarkdown] = useState("");
  const [isExpanded, setIsExpanded] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const uploadMapRef = useRef<Map<string, string>>(new Map());
  const { uploadWithToast } = useFileUpload(api);
  const { isDragOver, dropZoneProps } = useFileDropZone({
    onDrop: (files) => files.forEach((f) => editorRef.current?.uploadFile(f)),
  });

  const reactId = useId();
  const pinKey = `reply:${issueId}:${reactId}`;
  const { enabled: pinEnabled, isPinned, togglePin, unpin } = useInputPin(pinKey, true);
  const floatRect = useFloatPosition(anchorRef, isPinned, { mode: "sticky-bottom" });
  const isFloating = floatRect !== null;
  const wsId = getCurrentWsId();
  const userId = useAuthStore((s) => s.user?.id ?? null);
  const { data: agents = [] } = useQuery({
    ...agentListOptions(wsId ?? ""),
    enabled: !!wsId,
  });
  const { data: members = [] } = useQuery({
    ...memberListOptions(wsId ?? ""),
    enabled: !!wsId,
  });
  const myRole = members.find((m) => m.user_id === userId)?.role ?? null;
  const targetAgents = useMemo(
    () => getReplyTargetAgents({ markdown, replyTargetAgentId, agents }),
    [agents, markdown, replyTargetAgentId],
  );

  const handleUpload = useCallback(async (file: File) => {
    const result = await uploadWithToast(file, { issueId });
    if (result) {
      uploadMapRef.current.set(result.link, result.id);
    }
    return result;
  }, [uploadWithToast, issueId]);

  const handleSubmit = async () => {
    const content = editorRef.current?.getMarkdown()?.replace(/(\n\s*)+$/, "").trim();
    if (!content || submitting) return;
    // Only send attachment IDs for uploads still present in the content.
    const activeIds: string[] = [];
    for (const [url, id] of uploadMapRef.current) {
      if (content.includes(url)) activeIds.push(id);
    }
    setSubmitting(true);
    editorRef.current?.clearContent();
    setIsEmpty(true);
    try {
      await onSubmit(content, activeIds.length > 0 ? activeIds : undefined);
      editorRef.current?.clearContent();
      setIsEmpty(true);
      uploadMapRef.current.clear();
      if (isPinned) {
        unpin();
        requestAnimationFrame(() => {
          anchorRef.current?.scrollIntoView({ block: "end", behavior: "smooth" });
        });
      }
    } finally {
      setSubmitting(false);
    }
  };

  const avatarSize = size === "sm" ? 22 : 28;

  // Sticky-bottom: stay in normal flow until the viewport scrolls past the
  // anchor; then float with `position: fixed` matching the anchor's width and
  // height. Styling is unchanged so the field looks identical to its
  // not-pinned self — the only state cue is the green pin icon.
  const floatStyle = floatRect
    ? ({
        position: "fixed" as const,
        bottom: 16,
        left: floatRect.left,
        width: floatRect.width,
        zIndex: 30,
      })
    : undefined;

  return (
    <div ref={anchorRef}>
      {/* While the input is floating its natural slot is empty; reserve the
          same height with a hidden spacer so the thread layout does not jump
          (which would also break the sticky measurement and trigger a flicker
          loop). */}
      {isFloating && <div aria-hidden style={{ height: floatRect.height }} />}
      {/* CEREBRO-PATCH(reply-input-pin-floating-box): JEH-1065 — when the
          floating reply is on top of arbitrary thread content, it needs its
          own opaque box (background + border + shadow) so it does not bleed
          into the comments behind it. Layout stays identical inside the box,
          and the placeholder above keeps the thread height stable. */}
      <div
        style={floatStyle}
        className={cn(isFloating && "rounded-lg border border-border bg-background p-2 shadow-lg")}
      >
        {/* CEREBRO-PATCH(reply-input-mobile-full-width): JEH-1143 — drop the
            avatar on mobile (already visible in the comment metadata above)
            and put the action row in normal flow BELOW the editor (Slack /
            Linear / iMessage pattern), so the text input spans the card's
            full width instead of getting wrapped into a 3-words-per-line
            column. Desktop keeps the avatar; the action row sits below the
            editor on both viewports — less duplication, no absolute-position
            overlay fighting the editor for horizontal space. */}
        <div className="group/editor flex items-start gap-2.5">
          <ActorAvatar
            actorType={avatarType}
            actorId={avatarId}
            size={avatarSize}
            className="mt-0.5 shrink-0 hidden sm:block"
          />
          <div className="min-w-0 flex-1">
            <ReplyTargetBar
              agents={targetAgents}
              members={members}
              userId={userId}
              role={myRole}
              onTagOwner={(owner) => {
                editorRef.current?.insertText(` ${memberMentionMarkdown(owner)} `);
              }}
            />
            <div
              {...dropZoneProps}
              // CEREBRO-PATCH(reply-input-click-to-focus): JEH-1200 — clicks
              // anywhere in the card focus the editor (skip when the target is
              // itself interactive so buttons/links still work).
              onMouseDown={(e) => {
                if ((e.target as HTMLElement).closest("button, a, input, textarea, [contenteditable]")) return;
                e.preventDefault();
                editorRef.current?.focus();
              }}
              className={cn(
                "relative min-w-0 flex flex-col rounded-md bg-card min-h-20",
                isExpanded
                  ? "h-[60vh]"
                  : size === "sm" ? "max-h-40" : "max-h-56",
              )}
            >
              <div className="flex-1 min-h-0 overflow-y-auto">
                <div ref={measureRef}>
                  <ContentEditor
                    ref={editorRef}
                    placeholder={placeholderText}
                    onUpdate={(md) => {
                      setMarkdown(md);
                      setIsEmpty(!md.trim());
                    }}
                    onSubmit={handleSubmit}
                    onUploadFile={handleUpload}
                    debounceMs={100}
                    currentIssueId={issueId}
                    submitOnEnter={submitOnEnter}
                  />
                </div>
              </div>
              <div className="flex items-center justify-between gap-1 pt-1">
                <FileUploadButton
                  size="sm"
                  onAttach={(files) => files.forEach((f) => editorRef.current?.uploadFile(f))}
                  onEmbed={(files) => files.forEach((f) => editorRef.current?.uploadFile(f, { embedImage: true }))}
                />
                <div className="flex items-center gap-1">
                  {pinEnabled && (
                    <PinButton
                      isPinned={isPinned}
                      onToggle={togglePin}
                      pinLabel={t(($) => $.reply.pin_tooltip)}
                      unpinLabel={t(($) => $.reply.unpin_tooltip)}
                    />
                  )}
                  <Tooltip>
                    <TooltipTrigger
                      render={
                        <button
                          type="button"
                          onClick={() => {
                            setIsExpanded((v) => !v);
                            editorRef.current?.focus();
                          }}
                          aria-label={isExpanded ? t(($) => $.reply.collapse_tooltip) : t(($) => $.reply.expand_tooltip)}
                          className="inline-flex h-6 w-6 items-center justify-center rounded-sm text-muted-foreground opacity-70 hover:opacity-100 hover:bg-accent/60 transition-all cursor-pointer"
                        >
                          {isExpanded ? <Minimize2 className="h-3.5 w-3.5" /> : <Maximize2 className="h-3.5 w-3.5" />}
                        </button>
                      }
                    />
                    <TooltipContent side="top">{isExpanded ? t(($) => $.reply.collapse_tooltip) : t(($) => $.reply.expand_tooltip)}</TooltipContent>
                  </Tooltip>
                  <button
                    type="button"
                    disabled={isEmpty || submitting}
                    onClick={handleSubmit}
                    aria-label="Send"
                    className="inline-flex h-9 w-9 sm:h-7 sm:w-7 items-center justify-center rounded-full bg-primary text-primary-foreground hover:bg-primary/90 transition-colors disabled:bg-muted disabled:text-muted-foreground disabled:pointer-events-none"
                  >
                    {submitting ? (
                      <Loader2 className="h-4 w-4 sm:h-3.5 sm:w-3.5 animate-spin" />
                    ) : (
                      <ArrowUp className="h-4 w-4 sm:h-3.5 sm:w-3.5" />
                    )}
                  </button>
                </div>
              </div>
              {isDragOver && <FileDropOverlay />}
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}

interface ReplyTargetBarProps {
  agents: Agent[];
  members: MemberWithUser[];
  userId: string | null;
  role: "owner" | "admin" | "member" | null;
  onTagOwner: (owner: MemberWithUser) => void;
}

function ReplyTargetBar({
  agents,
  members,
  userId,
  role,
  onTagOwner,
}: ReplyTargetBarProps) {
  const { t } = useT("issues");
  if (agents.length === 0) return null;

  return (
    <div className="mb-1 flex min-h-7 flex-wrap items-center gap-1.5 text-[11px] text-muted-foreground">
      <span className="shrink-0 font-medium">{t(($) => $.reply.target_label)}</span>
      {agents.map((agent) => {
        const decision = canTriggerPrivateAgentMention(agent, { userId, role });
        const triggerInactiveLabel = t(($) => $.reply.target_trigger_inactive);
        const owner = agent.owner_id
          ? members.find((member) => member.user_id === agent.owner_id) ?? null
          : null;

        if (!decision.allowed) {
          return (
            <Popover key={agent.id}>
              <PopoverTrigger
                render={
                  <button
                    type="button"
                    className="inline-flex min-w-0 max-w-full items-center gap-1 rounded-md border border-red-500/45 bg-red-500/10 px-1.5 py-0.5 font-medium text-red-700 transition-colors hover:bg-red-500/15 dark:text-red-300"
                    aria-label={`${agent.name} ${triggerInactiveLabel}`}
                  >
                    <AlertTriangle className="size-3 shrink-0" />
                    <span className="truncate">{agent.name}</span>
                    <span className="shrink-0">{triggerInactiveLabel}</span>
                  </button>
                }
              />
              <PopoverContent side="top" align="start" className="w-72">
                <div className="space-y-2 text-xs">
                  <div>
                    <div className="font-medium text-foreground">{t(($) => $.reply.target_trigger_title, { name: agent.name })}</div>
                    <div className="mt-0.5 text-muted-foreground">
                      {t(($) => $.reply.target_trigger_description)}
                    </div>
                  </div>
                  {owner ? (
                    <div className="flex items-center gap-2 rounded-md border bg-card p-2">
                      <ActorAvatar actorType="member" actorId={owner.user_id} size={20} />
                      <div className="min-w-0 flex-1">
                        <div className="truncate font-medium text-foreground">{owner.name}</div>
                        <div className="truncate text-muted-foreground">{t(($) => $.reply.target_owner_label)}</div>
                      </div>
                      <button
                        type="button"
                        onClick={() => onTagOwner(owner)}
                        className="inline-flex h-7 shrink-0 items-center gap-1 rounded-md border px-2 font-medium text-foreground hover:bg-accent"
                      >
                        <UserPlus className="size-3.5" />
                        {t(($) => $.reply.target_tag_owner)}
                      </button>
                    </div>
                  ) : (
                    <div className="text-muted-foreground">{t(($) => $.reply.target_owner_missing)}</div>
                  )}
                </div>
              </PopoverContent>
            </Popover>
          );
        }

        return (
          <span
            key={agent.id}
            className="inline-flex min-w-0 max-w-full items-center gap-1 rounded-md border border-emerald-500/30 bg-emerald-500/10 px-1.5 py-0.5 font-medium text-emerald-700 dark:text-emerald-300"
          >
            <ActorAvatar actorType="agent" actorId={agent.id} size={14} />
            <span className="truncate">{agent.name}</span>
          </span>
        );
      })}
    </div>
  );
}

export { ReplyInput, type ReplyInputProps };
