"use client";

import type { ReactNode } from "react";
import { Loader2, MessageSquare } from "lucide-react";
import {
  applyDraftModelChange,
  applyDraftRuntimeChange,
  stripBuilderDraft,
  type AgentDraft,
  type BuilderRestore,
} from "@multica/core/agents";
import { isRuntimeUsableForUser } from "@multica/core/runtimes";
import type {
  ChatMessage,
  MemberWithUser,
  RuntimeDevice,
} from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import { ChatInput } from "../../chat/components/chat-input";
import {
  ChatMessageList,
  ChatMessageSkeleton,
} from "../../chat/components/chat-message-list";
import { useT } from "../../i18n";
import { AppLink } from "../../navigation";
import { ModelDropdown } from "../components/model-dropdown";
import { RuntimePicker } from "../components/runtime-picker";

/**
 * Pre-conversation card: the builder runs on a real runtime, so the runtime and
 * model have to be chosen before the first message — they are frozen onto the
 * hidden carrier agent when the session is created.
 */
export function BuilderSetup({
  draft,
  onChange,
  runtimes,
  runtimesLoading,
  members,
  currentUserId,
  selectedRuntime,
  starting,
  error,
  onStart,
  connectRuntimeHref,
  banner,
}: {
  draft: AgentDraft;
  onChange: (draft: AgentDraft) => void;
  runtimes: RuntimeDevice[];
  runtimesLoading: boolean;
  members: MemberWithUser[];
  currentUserId: string | null;
  selectedRuntime: RuntimeDevice | null;
  starting: boolean;
  error: string | null;
  onStart: () => void;
  connectRuntimeHref: string;
  /** Rendered above the card: the way back to an unfinished conversation, so
   *  this screen cannot silently start yet another one. */
  banner?: ReactNode;
}) {
  const { t } = useT("agents");
  const hasOnline = runtimes.some(
    (runtime) =>
      runtime.status === "online" &&
      isRuntimeUsableForUser(runtime, currentUserId),
  );
  return (
    <main className="flex min-h-0 flex-1 items-center justify-center overflow-y-auto px-5 py-10">
      <div className="w-full max-w-xl">
        {banner}
        <div className="rounded-xl border bg-card p-6 shadow-sm">
        <span className="flex size-11 items-center justify-center rounded-lg bg-primary/10 text-primary">
          <MessageSquare className="size-5" />
        </span>
        <h2 className="mt-5 text-title-lg font-semibold">
          {t(($) => $.creation_studio.builder.setup_title)}
        </h2>
        <p className="mt-2 text-body leading-6 text-muted-foreground">
          {t(($) => $.creation_studio.builder.setup_description)}
        </p>
        <div className="mt-6 space-y-4">
          <RuntimePicker
            runtimes={runtimes}
            runtimesLoading={runtimesLoading}
            members={members}
            currentUserId={currentUserId}
            selectedRuntimeId={draft.runtimeId}
            onSelect={(runtimeId) => {
              if (runtimeId !== draft.runtimeId) {
                onChange(applyDraftRuntimeChange(draft, runtimeId));
              }
            }}
          />
          <ModelDropdown
            runtime={selectedRuntime}
            runtimeId={selectedRuntime?.id ?? null}
            runtimeOnline={selectedRuntime?.status === "online"}
            value={draft.model}
            onChange={(model) => onChange(applyDraftModelChange(draft, model))}
            disabled={!selectedRuntime}
          />
        </div>
        {error && (
          <div role="alert" className="mt-4 text-body text-destructive">
            {error}
          </div>
        )}
        <div className="mt-6 flex justify-end">
          {hasOnline ? (
            <Button
              onClick={onStart}
              disabled={starting || selectedRuntime?.status !== "online"}
            >
              {starting && <Loader2 className="size-4 animate-spin" />}
              {t(($) => $.creation_studio.builder.start)}
            </Button>
          ) : (
            <Button
              render={<AppLink href={connectRuntimeHref} />}
              nativeButton={false}
            >
              {t(($) => $.creation_studio.builder.connect_runtime)}
            </Button>
          )}
        </div>
        </div>
      </div>
    </main>
  );
}

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
}) {
  const { t } = useT("agents");
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
          <p className="truncate text-caption text-muted-foreground">
            {t(($) => $.creation_studio.builder.chat_hint)}
          </p>
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
                  className="rounded-full border bg-background px-3 py-1.5 text-caption transition-colors hover:bg-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                >
                  {prompt}
                </button>
              ))}
            </div>
          </div>
        </div>
      )}

      {error ? (
        <div
          role="alert"
          aria-live="polite"
          className="mx-5 mb-3 rounded-md bg-destructive/5 px-3 py-2 text-body text-destructive"
        >
          {error}
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
