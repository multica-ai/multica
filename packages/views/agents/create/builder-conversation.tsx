"use client";

import { X } from "@multica/ui/icons";
import { stripBuilderDraft, type BuilderRestore } from "@multica/core/agents";
import type { ChatMessage } from "@multica/core/types";
import { IconButton } from "@multica/ui/components/common/icon-button";
import { cn } from "@multica/ui/lib/utils";
import { ChatInput } from "../../chat/components/chat-input";
import { CHAT_COLUMN, CHAT_GUTTER } from "../../chat/components/chat-column";
import {
  ChatMessageList,
  ChatMessageSkeleton,
} from "../../chat/components/chat-message-list";
import { useT } from "../../i18n";

export function BuilderConversation({
  sessionId,
  messages,
  loading,
  pendingTask,
  runtimeOnline,
  onSend,
  onStop,
  restoreDraftRequest,
  onRestoreDraftApplied,
  error,
  onDismissError,
}: {
  sessionId: string;
  messages: ChatMessage[];
  loading: boolean;
  pendingTask:
    | { task_id?: string; status?: string; created_at?: string }
    | undefined;
  runtimeOnline: boolean;
  /** `commitInput` is the composer's clear; the owner runs it as soon as the
   *  server accepts the message. The prompt buttons below send without one. */
  onSend: (content: string, commitInput?: () => void) => Promise<boolean>;
  onStop: () => void;
  restoreDraftRequest: BuilderRestore | null;
  onRestoreDraftApplied: () => void;
  error: string | null;
  onDismissError?: () => void;
}) {
  const { t } = useT("agents");
  const { t: tModals } = useT("modals");
  const pending = !!pendingTask?.task_id;
  const draftKey = `agent-builder:${sessionId}`;
  const prompts = [
    t(($) => $.creation_studio.builder.prompt_review),
    t(($) => $.creation_studio.builder.prompt_research),
    t(($) => $.creation_studio.builder.prompt_assistant),
  ];

  return (
    // `@container`: this is one column of the studio's split layout, so the
    // shared chat gutter must size against the column, not the viewport.
    <section className="flex h-full min-h-0 flex-col bg-background @container">
      <header className="flex min-h-14 shrink-0 items-center justify-between gap-4 border-b px-5 py-2.5">
        <div className="min-w-0">
          <h2 className="truncate text-body font-semibold">
            {t(($) => $.creation_studio.builder.chat_title)}
          </h2>
        </div>
        <div className="flex shrink-0 items-center gap-1.5 text-caption text-muted-foreground">
          <span
            className={cn(
              "size-2 rounded-full",
              runtimeOnline ? "bg-success" : "bg-muted-foreground/40",
            )}
            aria-hidden="true"
          />
          {runtimeOnline
            ? t(($) => $.creation_studio.builder.runtime_online)
            : t(($) => $.creation_studio.builder.runtime_offline)}
        </div>
      </header>

      {loading ? (
        <ChatMessageSkeleton />
      ) : messages.length > 0 || pending ? (
        <ChatMessageList
          messages={messages}
          pendingTask={pendingTask}
          availability={runtimeOnline ? "online" : "offline"}
          // Applies to the live stream as well as history, which is what keeps
          // the half-written JSON block off the screen mid-reply.
          transformContent={stripBuilderDraft}
        />
      ) : (
        <div className="flex min-h-0 flex-1 items-center justify-center overflow-y-auto px-5 py-8">
          <div className="w-full max-w-xl text-center">
            <h3 className="text-balance text-title font-semibold">
              {t(($) => $.creation_studio.builder.empty_title)}
            </h3>
            <p className="mx-auto mt-2 max-w-md text-pretty text-body leading-6 text-muted-foreground">
              {t(($) => $.creation_studio.builder.empty_description)}
            </p>
            <div className="mt-5 flex flex-wrap justify-center gap-2">
              {prompts.map((prompt) => (
                <button
                  key={prompt}
                  type="button"
                  onClick={() => void onSend(prompt)}
                  className="rounded-md border bg-background px-3 py-1.5 text-caption transition-colors hover:bg-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                >
                  {prompt}
                </button>
              ))}
            </div>
          </div>
        </div>
      )}

      {error ? (
        <div className={cn("mb-3", CHAT_GUTTER)}>
          <div
            role="alert"
            aria-live="polite"
            className={cn(
              CHAT_COLUMN,
              "flex items-start gap-2 rounded-md bg-destructive/5 px-3 py-2 text-body text-destructive",
            )}
          >
            <span className="min-w-0 flex-1">{error}</span>
            {onDismissError && (
              <IconButton
                type="button"
                variant="ghost"
                size="icon-xs"
                className="shrink-0"
                label={tModals(($) => $.common.close)}
                onClick={onDismissError}
              >
                <X className="size-4" />
              </IconButton>
            )}
          </div>
        </div>
      ) : null}

      <ChatInput
        onSend={(content, _attachmentIds, commitInput) =>
          onSend(content, commitInput)
        }
        onStop={onStop}
        isRunning={pending}
        disabled={!runtimeOnline}
        agentName={t(($) => $.creation_studio.builder.chat_title)}
        draftKeyOverride={draftKey}
        editorKeyOverride={draftKey}
        restoreDraftRequest={restoreDraftRequest}
        onRestoreDraftApplied={onRestoreDraftApplied}
      />
    </section>
  );
}
